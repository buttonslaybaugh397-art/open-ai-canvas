package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"infinite-canvas/backend/internal/model"
)

const mediaStagingBudget int64 = 512 << 20
const mediaStagingTTL = 24 * time.Hour

const (
	mediaDownloadAttempts       = 3
	mediaDownloadRetryBase      = time.Second
	mediaDownloadMinTimeout     = 3 * time.Minute
	mediaDownloadMaxTimeout     = 30 * time.Minute
	mediaDownloadBytesPerSecond = int64(512 << 10)
)

var mediaStagingMu sync.Mutex

// Reserve the maximum response size before opening a file. Temporary files are
// sparse reservations, so concurrent downloads cannot oversubscribe this budget.
func (s *Service) newMediaTemp(limit int64) (*os.File, error) {
	mediaStagingMu.Lock()
	defer mediaStagingMu.Unlock()
	dir := filepath.Join(s.dataDir, "media-staging")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var used int64
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "result-") || !entry.Type().IsRegular() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if time.Since(info.ModTime()) > mediaStagingTTL {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return nil, err
			}
			continue
		}
		used += info.Size()
	}
	if limit <= 0 || limit > mediaStagingBudget || used > mediaStagingBudget-limit {
		return nil, errors.New("作品暂存空间不足，请稍后重试保存")
	}
	file, err := os.CreateTemp(dir, "result-*")
	if err != nil {
		return nil, err
	}
	if err := file.Truncate(limit); err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, err
	}
	return file, nil
}

func (s *Service) stageInlineMedia(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("上游返回空作品")
	}
	file, err := s.newMediaTemp(int64(len(data)))
	if err != nil {
		return "", err
	}
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(file.Name())
		return "", err
	}
	return filepath.Base(file.Name()), nil
}

func (s *Service) downloadTaskMedia(ctx context.Context, config providerConfig, url, mode string) (name, mimeType string, err error) {
	policy, err := s.RuntimePolicy()
	if err != nil {
		return "", "", err
	}
	limit := min(megabytes(policy.Resource.GeneratedFileMB), mediaStagingBudget)
	if _, err := ValidateOutboundURL(url); err != nil {
		return "", "", err
	}
	started := time.Now()
	statusCode := 0
	var lastRequest *http.Request
	var file *os.File
	var fileName string
	var reservedBytes int64
	var downloadedBytes int64
	expectedBytes := int64(-1)
	contentType := ""
	defer func() {
		// Signed paths and net/url errors can contain bearer credentials. Keep
		// the real error for retry classification, never in request logs.
		if lastRequest == nil {
			return
		}
		logged := lastRequest.Clone(lastRequest.Context())
		redactedURL := *lastRequest.URL
		redactedURL.Path, redactedURL.RawPath, redactedURL.RawQuery, redactedURL.Fragment = "/task-media", "", "", ""
		redactedURL.User = nil
		logged.URL = &redactedURL
		var safeErr error
		if err != nil {
			safeErr = errors.New("结果下载失败")
		}
		recordProviderRequest(logged, started, statusCode, nil, safeErr)
	}()
	defer func() {
		if file != nil {
			_ = file.Close()
			if err != nil {
				_ = os.Remove(file.Name())
			}
		}
	}()

	var lastErr error
	for attempt := 0; attempt < mediaDownloadAttempts; attempt++ {
		if ctx.Err() != nil {
			return "", "", ctx.Err()
		}
		attemptTimeout := mediaDownloadTimeout(limit)
		downloadCtx, cancel := context.WithTimeout(withProviderRequestKind(ctx, "download"), attemptTimeout)
		req, requestErr := http.NewRequestWithContext(downloadCtx, http.MethodGet, url, nil)
		if requestErr != nil {
			cancel()
			return "", "", requestErr
		}
		lastRequest = req
		rangeRequested := downloadedBytes > 0
		if rangeRequested {
			req.Header.Set("Range", "bytes="+strconv.FormatInt(downloadedBytes, 10)+"-")
		}
		if sameProviderOrigin(config.BaseURL, url) {
			applyProviderAuth(req, config)
			ApplyOutboundHeaders(req, config.Headers)
		}
		ApplyDefaultOutboundHeaders(req)
		response, requestErr := OutboundHTTPClient(attemptTimeout).Do(req)
		if requestErr != nil {
			cancel()
			lastErr = requestErr
			if attempt+1 < mediaDownloadAttempts && retryableMediaDownloadError(requestErr) {
				if waitErr := sleepContext(ctx, mediaDownloadRetryDelay(attempt, 0)); waitErr != nil {
					return "", "", waitErr
				}
				continue
			}
			return "", "", requestErr
		}
		statusCode = response.StatusCode
		if statusCode < 200 || statusCode >= 300 {
			retryAfter := parseRetryAfter(response.Header.Get("Retry-After"), time.Now())
			lastErr = providerHTTPError{StatusCode: statusCode, Status: response.Status, RetryAfter: retryAfter}
			_ = response.Body.Close()
			cancel()
			if statusCode == http.StatusRequestedRangeNotSatisfiable && rangeRequested {
				_, _, total, ok := parseMediaContentRange(response.Header.Get("Content-Range"))
				if ok && total == downloadedBytes {
					expectedBytes = total
					break
				}
			}
			if attempt+1 < mediaDownloadAttempts && retryableMediaDownloadError(lastErr) {
				if waitErr := sleepContext(ctx, mediaDownloadRetryDelay(attempt, retryAfter)); waitErr != nil {
					return "", "", waitErr
				}
				continue
			}
			return "", "", lastErr
		}

		responseStart, _, responseTotal, hasContentRange := parseMediaContentRange(response.Header.Get("Content-Range"))
		if rangeRequested && statusCode == http.StatusPartialContent {
			if !hasContentRange || responseStart != downloadedBytes {
				_ = response.Body.Close()
				cancel()
				lastErr = errors.New("生成文件分片响应位置无效")
				downloadedBytes = 0
				expectedBytes = -1
				if file != nil {
					if truncateErr := file.Truncate(0); truncateErr != nil {
						return "", "", truncateErr
					}
				}
				if attempt+1 < mediaDownloadAttempts {
					continue
				}
				return "", "", lastErr
			}
			if responseTotal >= 0 {
				expectedBytes = responseTotal
			}
		} else if statusCode == http.StatusOK {
			// Some providers ignore Range. Restart from byte zero instead of
			// appending a second full response to the partial file.
			if rangeRequested {
				downloadedBytes = 0
				expectedBytes = -1
				if file != nil {
					if truncateErr := file.Truncate(0); truncateErr != nil {
						_ = response.Body.Close()
						cancel()
						return "", "", truncateErr
					}
				}
			}
			if response.ContentLength >= 0 {
				expectedBytes = response.ContentLength
			}
		}
		if response.ContentLength >= 0 && statusCode == http.StatusPartialContent && expectedBytes < 0 {
			expectedBytes = downloadedBytes + response.ContentLength
		}
		if expectedBytes > limit {
			_ = response.Body.Close()
			cancel()
			return "", "", errors.New("生成文件超过大小限制")
		}
		if file == nil {
			reservation := limit
			if expectedBytes > 0 {
				reservation = expectedBytes
			}
			file, err = s.newMediaTemp(reservation)
			if err != nil {
				_ = response.Body.Close()
				cancel()
				return "", "", err
			}
			fileName = filepath.Base(file.Name())
			reservedBytes = reservation
		}
		if expectedBytes > reservedBytes {
			_ = response.Body.Close()
			cancel()
			return "", "", errors.New("生成文件超过暂存空间限制")
		}
		if _, err = file.Seek(downloadedBytes, io.SeekStart); err != nil {
			_ = response.Body.Close()
			cancel()
			return "", "", err
		}
		remaining := reservedBytes - downloadedBytes
		size, copyErr := io.Copy(file, io.LimitReader(response.Body, remaining+1))
		_ = response.Body.Close()
		cancel()
		downloadedBytes += size
		if size > remaining {
			lastErr = errors.New("生成文件超过大小限制")
		} else if copyErr != nil {
			lastErr = copyErr
		} else if response.ContentLength >= 0 && size != response.ContentLength {
			lastErr = io.ErrUnexpectedEOF
		} else if expectedBytes >= 0 && downloadedBytes < expectedBytes {
			lastErr = io.ErrUnexpectedEOF
		} else {
			if expectedBytes < 0 {
				expectedBytes = downloadedBytes
			}
			if downloadedBytes != expectedBytes {
				lastErr = io.ErrUnexpectedEOF
			} else {
				contentType = response.Header.Get("Content-Type")
				break
			}
		}
		_ = file.Truncate(downloadedBytes)
		if attempt+1 < mediaDownloadAttempts && retryableMediaDownloadError(lastErr) {
			if waitErr := sleepContext(ctx, mediaDownloadRetryDelay(attempt, 0)); waitErr != nil {
				return "", "", waitErr
			}
			continue
		}
		return "", "", lastErr
	}
	if lastErr != nil && downloadedBytes != expectedBytes {
		return "", "", lastErr
	}
	if file == nil || downloadedBytes <= 0 {
		return "", "", errors.New("生成文件为空或超过大小限制")
	}
	if err = file.Truncate(downloadedBytes); err != nil {
		return "", "", err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return "", "", err
	}
	head := make([]byte, min(downloadedBytes, 512))
	if _, err = io.ReadFull(file, head); err != nil {
		return "", "", err
	}
	mimeType = normalizedMediaMimeType(contentType, head)
	if mode == "audio" && mimeType == "application/ogg" {
		mimeType = "audio/ogg"
	}
	if !strings.HasPrefix(mimeType, mode+"/") {
		return "", "", errors.New("上游返回的内容不是有效的" + mode + "媒体")
	}
	if err = file.Sync(); err != nil {
		return "", "", err
	}
	if err = file.Close(); err != nil {
		return "", "", err
	}
	file = nil
	return fileName, mimeType, nil
}

func mediaDownloadTimeout(limit int64) time.Duration {
	if limit <= 0 {
		return mediaDownloadMaxTimeout
	}
	seconds := (limit + mediaDownloadBytesPerSecond - 1) / mediaDownloadBytesPerSecond
	timeout := 2*time.Minute + time.Duration(seconds)*time.Second
	return min(max(timeout, mediaDownloadMinTimeout), mediaDownloadMaxTimeout)
}

func mediaDownloadRetryDelay(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return min(retryAfter, 2*time.Minute)
	}
	return mediaDownloadRetryBase * time.Duration(1<<attempt)
}

func retryableMediaDownloadError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var upstream providerHTTPError
	if errors.As(err, &upstream) {
		return upstream.StatusCode == http.StatusRequestTimeout || upstream.StatusCode == http.StatusTooEarly || upstream.StatusCode == http.StatusTooManyRequests || upstream.StatusCode >= 500
	}
	return retryableProtocolMediaDownload(err)
}

func parseMediaContentRange(value string) (start, end, total int64, ok bool) {
	value = strings.TrimSpace(value)
	if !strings.HasPrefix(value, "bytes ") {
		return 0, 0, 0, false
	}
	parts := strings.SplitN(strings.TrimPrefix(value, "bytes "), "/", 2)
	if len(parts) != 2 {
		return 0, 0, 0, false
	}
	rangePart := strings.SplitN(parts[0], "-", 2)
	if len(rangePart) != 2 {
		if parts[0] != "*" {
			return 0, 0, 0, false
		}
		start, end = -1, -1
	} else {
		var err error
		start, err = strconv.ParseInt(rangePart[0], 10, 64)
		if err != nil {
			return 0, 0, 0, false
		}
		end, err = strconv.ParseInt(rangePart[1], 10, 64)
		if err != nil || start < 0 || end < start {
			return 0, 0, 0, false
		}
	}
	total = -1
	if parts[1] != "*" {
		parsed, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || parsed < 0 {
			return 0, 0, 0, false
		}
		total = parsed
	}
	return start, end, total, true
}

func mediaUploadKey(taskID string, index int) *string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("task-media:%s:%d", taskID, index)))
	key := hex.EncodeToString(sum[:])
	return &key
}

func (s *Service) storeTaskMediaFile(task *model.Task, index int, path, mimeType, kind string, existing *model.Resource) (*model.Resource, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("暂存作品不是普通文件")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("暂存作品不是普通文件")
	}
	width, height := 0, 0
	if kind == "image" {
		config, _, err := image.DecodeConfig(file)
		if err != nil {
			// The standard library does not decode WebP/AVIF. Verify their
			// container signatures instead of trusting a response MIME header.
			head := make([]byte, 64)
			n, _ := file.ReadAt(head, 0)
			head = head[:n]
			webp := mimeType == "image/webp" && n >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP"
			avif := mimeType == "image/avif" && n >= 16 && string(head[4:8]) == "ftyp" && (strings.Contains(string(head[8:]), "avif") || strings.Contains(string(head[8:]), "avis"))
			if !webp && !avif {
				return nil, fmt.Errorf("图片内容校验失败：%w", err)
			}
		}
		width, height = config.Width, config.Height
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
	}
	day, err := s.reserveGeneratedResourceQuota(task.UserID, info.Size())
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			s.releaseUserUploadQuota(task.UserID, day, info.Size())
		}
	}()
	var resource *model.Resource
	if existing == nil {
		resource, _, err = s.storeResourceWithWriter(task.UserID, kind, "generated."+extensionFromMimeType(mimeType), mimeType, info.Size(), width, height, 0, file, mediaUploadKey(task.ID, index), false, s.storeTaskMediaObject)
	} else {
		// Reuse the same object key after a failed upload; no orphan per retry.
		resource = existing
		if resource.UserID != task.UserID || resource.Size != info.Size() || resource.MimeType != mimeType {
			return nil, errors.New("恢复文件与原始资源不一致")
		}
		resource.ETag, err = s.storeTaskMediaObject(resource, "generated."+extensionFromMimeType(mimeType), file)
		if err == nil {
			resource.Status, resource.Error, resource.UpdatedAt = model.ResourceStatusReady, "", time.Now()
			err = s.repo.SaveResource(resource)
		}
	}
	if err != nil {
		return nil, err
	}
	s.commitUserUploadQuota(task.UserID, info.Size())
	committed = true
	if existing != nil {
		s.recordActivity(task.UserID, "resource", 1)
		s.maybeStartPlaybackTranscode(resource)
	}
	return resource, nil
}

// Unlike ordinary uploads, generated output preserves an OSS failure for
// explicit recovery instead of silently changing the storage destination.
func (s *Service) storeTaskMediaObject(resource *model.Resource, _ string, body io.Reader) (string, error) {
	if resource.Provider == "local" {
		return "", writeLocalResourceObject(filepath.Join(s.dataDir, "resources", filepath.FromSlash(resource.ObjectKey)), body)
	}
	setting, err := s.ossSettingForResource(resource.UserID, resource)
	if err != nil {
		return "", err
	}
	return putOSSObject(setting, resource.ObjectKey, resource.MimeType, resource.Size, body)
}

// Cleanup is best effort only after durable completion. Expired leftovers are
// also collected on staging allocation, never by deleting resource objects.
func (s *Service) cleanupMediaCheckpointFiles(task *model.Task) {
	checkpoint, err := s.decodeMediaCheckpoint(task)
	if err != nil {
		return
	}
	for _, item := range checkpoint.Items {
		if path := s.mediaTempPath(item.TempName); path != "" {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				_ = s.log(task.UserID, task.ID, "warn", "清理作品暂存文件失败", "")
			}
		}
	}
}
