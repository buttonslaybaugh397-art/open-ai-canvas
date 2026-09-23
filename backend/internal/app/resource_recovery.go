package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/platform"
)

type resourceAvailability uint8

const (
	resourceAvailableInCloud resourceAvailability = iota + 1
	resourceAvailableLocally
	resourceUnavailable
)

func resourceDeliveryUnavailable(message string, cause error) *AppError {
	err := WrapAppError(http.StatusServiceUnavailable, message, cause)
	err.Retryable = true
	return err
}

func (s *Service) cloudResourceAvailability(ctx context.Context, resource *model.Resource, setting ossSettingValue) (resourceAvailability, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	probeContext, cancel := context.WithTimeout(ctx, resourceRedirectProbeTimeout)
	defer cancel()
	s.resourceProbeOnce.Do(func() {
		s.resourceProbeCache = platform.NewBoundedReadCache[string, resourceAvailability](4096, 1<<20, 64, resourceRedirectProbeTTL)
	})
	key := resourceRedirectProbeKey(resource, "")
	return s.resourceProbeCache.Get(probeContext, key, func(loadContext context.Context) (resourceAvailability, int, error) {
		availability, err := s.inspectCloudResourceAvailability(loadContext, resource, setting)
		return availability, 256, err
	})
}

func (s *Service) redirectResourceAvailable(ctx context.Context, resource *model.Resource, redirectURL string) (bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	probeContext, cancel := context.WithTimeout(ctx, resourceRedirectProbeTimeout)
	defer cancel()
	s.resourceProbeOnce.Do(func() {
		s.resourceProbeCache = platform.NewBoundedReadCache[string, resourceAvailability](4096, 1<<20, 64, resourceRedirectProbeTTL)
	})
	key := resourceRedirectProbeKey(resource, redirectURL)
	availability, err := s.resourceProbeCache.Get(probeContext, key, func(loadContext context.Context) (resourceAvailability, int, error) {
		exists, probeErr := probeResourceURL(loadContext, redirectURL)
		if probeErr != nil {
			return 0, 256, probeErr
		}
		if exists {
			return resourceAvailableInCloud, 256, nil
		}
		// A missing CDN edge object can become available on the next retry.
		return resourceUnavailable, 256, errResourceObjectMissing
	})
	if errors.Is(err, errResourceObjectMissing) {
		return false, nil
	}
	return availability == resourceAvailableInCloud, err
}

func (s *Service) inspectCloudResourceAvailability(ctx context.Context, resource *model.Resource, setting ossSettingValue) (resourceAvailability, error) {
	exists, err := probeOSSObject(ctx, setting, resource.ObjectKey)
	if err != nil {
		return 0, err
	}
	if exists {
		return resourceAvailableInCloud, nil
	}
	if s.localResourceObjectAvailable(resource.ObjectKey, resource.Size) {
		s.scheduleCloudResourceRecovery(resource.ID)
		return resourceAvailableLocally, nil
	}
	s.markResourceUnrecoverable(resource, "对象存储与服务器本地副本均不存在")
	return resourceUnavailable, nil
}

func (s *Service) clearResourceProbeCache() {
	if s.resourceProbeCache != nil {
		s.resourceProbeCache.Clear()
	}
}

func (s *Service) scheduleCloudResourceRecovery(resourceID string) {
	if strings.TrimSpace(resourceID) == "" {
		return
	}
	s.runWorkerTask(func() {
		if err := s.recoverCloudResourceFromLocal(resourceID); err != nil {
			log.Printf("resource cloud recovery failed: resource=%s err=%v", resourceID, err)
		}
	})
}

func (s *Service) recoverCloudResourceFromLocal(resourceID string) error {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	resource, err := s.repo.Resource(resourceID)
	if err != nil {
		return err
	}
	if resource.Status != model.ResourceStatusReady || resource.Provider == "local" {
		return nil
	}
	setting, err := s.ossSettingForResource(resource.UserID, resource)
	if err != nil {
		return err
	}
	setting.Provider = firstNonEmpty(resource.Provider, setting.Provider)
	setting.Endpoint = firstNonEmpty(resource.Endpoint, setting.Endpoint)
	setting.Bucket = firstNonEmpty(resource.Bucket, setting.Bucket)
	ctx, cancel := context.WithTimeout(context.Background(), resourceRedirectProbeTimeout)
	exists, probeErr := probeOSSObject(ctx, setting, resource.ObjectKey)
	cancel()
	if probeErr != nil {
		return resourceDeliveryUnavailable("无法确认云端对象状态，稍后重试补传", probeErr)
	}
	if exists {
		return nil
	}
	body, info, err := s.openLocalResourceObject(resource.ObjectKey)
	if err != nil {
		return err
	}
	defer body.Close()
	etag, err := putOSSObject(setting, resource.ObjectKey, resource.MimeType, info.Size(), body)
	if err != nil {
		return err
	}
	updated, err := s.repo.MarkResourceCloudRecovered(resource.UserID, resource.ID, resource.Provider, resource.ObjectKey, info.Size(), etag)
	if err != nil {
		return err
	}
	if !updated {
		return errors.New("资源状态已变化，已停止回写恢复结果")
	}
	s.clearResourceProbeCache()
	if err := s.deleteLocalResourceObject(resource.ObjectKey); err != nil {
		log.Printf("resource recovery local cleanup failed: resource=%s err=%v", resource.ID, err)
	}
	return nil
}

func (s *Service) scheduleLocalResourcePromotion(resource *model.Resource) {
	if resource == nil || resource.Provider != "local" || resource.Status != model.ResourceStatusReady {
		return
	}
	resourceID := resource.ID
	s.runWorkerTask(func() {
		if err := s.promoteLocalResourceToObjectStorage(resourceID); err != nil {
			log.Printf("local resource object-storage promotion skipped: resource=%s err=%v", resourceID, err)
		}
	})
}

func (s *Service) promoteLocalResourceToObjectStorage(resourceID string) error {
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	resource, err := s.repo.Resource(resourceID)
	if err != nil {
		return err
	}
	if resource.Status != model.ResourceStatusReady || resource.Provider != "local" {
		return nil
	}
	setting, storageSettingID, enabled, err := s.activeResourceOSSSetting(resource.UserID)
	if err != nil || !enabled {
		return err
	}
	body, info, err := s.openLocalResourceObject(resource.ObjectKey)
	if err != nil {
		return err
	}
	defer body.Close()
	fileName := path.Base(resource.ObjectKey)
	objectKey := ossObjectKey(setting, resource.UserID, resource.Kind, fileName, resource.MimeType, time.Now())
	etag, err := putOSSObject(setting, objectKey, resource.MimeType, info.Size(), body)
	if err != nil {
		return err
	}
	updated, err := s.repo.PromoteLocalResource(resource.UserID, resource.ID, resource.ObjectKey, setting.Provider, setting.Endpoint, setting.Bucket, storageSettingID, objectKey, info.Size(), etag)
	if err != nil {
		return err
	}
	if !updated {
		return errors.New("资源状态已变化，已停止切换对象存储")
	}
	if err := s.deleteLocalResourceObject(resource.ObjectKey); err != nil {
		log.Printf("promoted resource local cleanup failed: resource=%s err=%v", resource.ID, err)
	}
	s.clearResourceProbeCache()
	return nil
}

func (s *Service) markResourceUnrecoverable(resource *model.Resource, reason string) {
	if resource == nil {
		return
	}
	changed, err := s.repo.MarkReadyResourceFailed(resource.UserID, resource.ID, resource.Provider, resource.ObjectKey, reason)
	if err != nil {
		log.Printf("mark unrecoverable resource failed: resource=%s err=%v", resource.ID, err)
		return
	}
	if !changed {
		return
	}
	failed := *resource
	failed.Status = model.ResourceStatusFailed
	failed.Error = reason
	cleanup := func() {
		if cleanupErr := s.cleanupDetachedUserResources(failed.UserID, []model.Resource{failed}); cleanupErr != nil {
			log.Printf("unrecoverable resource cleanup deferred: resource=%s err=%v", failed.ID, cleanupErr)
		}
	}
	if !s.runWorkerTask(cleanup) {
		cleanup()
	}
}

func (s *Service) openLocalRecoveryStream(resource *model.Resource) (*ResourceStream, error) {
	body, info, err := s.openLocalResourceObject(resource.ObjectKey)
	if err != nil {
		return nil, err
	}
	recoveryBody := &afterCloseReadSeekCloser{ReadSeekCloser: body, afterClose: func() {
		s.scheduleCloudResourceRecovery(resource.ID)
	}}
	return &ResourceStream{Resource: resource, Body: recoveryBody, StatusCode: 200, ContentLength: info.Size(), AcceptRanges: "bytes"}, nil
}

type afterCloseReadSeekCloser struct {
	io.ReadSeekCloser
	once       sync.Once
	afterClose func()
}

func (body *afterCloseReadSeekCloser) ReadAt(p []byte, offset int64) (int, error) {
	reader, ok := body.ReadSeekCloser.(io.ReaderAt)
	if !ok {
		return 0, errors.New("底层资源不支持随机读取")
	}
	return reader.ReadAt(p, offset)
}

func (body *afterCloseReadSeekCloser) Close() error {
	err := body.ReadSeekCloser.Close()
	body.once.Do(body.afterClose)
	return err
}

func (s *Service) localResourceObjectAvailable(objectKey string, expectedSize int64) bool {
	body, info, err := s.openLocalResourceObject(objectKey)
	if err != nil {
		return false
	}
	_ = body.Close()
	return expectedSize <= 0 || info.Size() == expectedSize
}

func (s *Service) openLocalResourceObject(objectKey string) (*os.File, os.FileInfo, error) {
	root, err := filepath.Abs(filepath.Join(s.dataDir, "resources"))
	if err != nil {
		return nil, nil, err
	}
	cleanKey := strings.TrimLeft(strings.TrimSpace(objectKey), "/\\")
	if cleanKey == "" {
		return nil, nil, os.ErrNotExist
	}
	target, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(cleanKey)))
	if err != nil {
		return nil, nil, err
	}
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, nil, errors.New("本地恢复文件路径超出资源目录")
	}
	file, err := os.Open(target)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, nil, err
	}
	if info.IsDir() {
		_ = file.Close()
		return nil, nil, errors.New("本地恢复路径指向目录")
	}
	resolvedRoot, rootErr := filepath.EvalSymlinks(root)
	resolvedTarget, targetErr := filepath.EvalSymlinks(target)
	if rootErr != nil || targetErr != nil {
		_ = file.Close()
		return nil, nil, fmt.Errorf("校验本地恢复路径失败：%w", errors.Join(rootErr, targetErr))
	}
	resolvedRelative, err := filepath.Rel(resolvedRoot, resolvedTarget)
	if err != nil || resolvedRelative == "." || resolvedRelative == ".." || strings.HasPrefix(resolvedRelative, ".."+string(filepath.Separator)) {
		_ = file.Close()
		return nil, nil, errors.New("本地恢复文件真实路径超出资源目录")
	}
	resolvedInfo, err := os.Stat(resolvedTarget)
	if err != nil || !os.SameFile(info, resolvedInfo) {
		_ = file.Close()
		return nil, nil, errors.New("本地恢复文件已发生变化")
	}
	return file, info, nil
}
