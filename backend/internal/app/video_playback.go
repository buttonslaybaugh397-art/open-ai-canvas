package app

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/assets"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/platform"

	"gorm.io/gorm"
)

const (
	playbackTimeout        = 15 * time.Minute
	playbackLeaseTTL       = playbackTimeout + time.Minute
	playbackConcurrency    = 2
	playbackMaxSourceBytes = int64(1 << 30)
	playbackMaxOutputBytes = int64(1 << 30)
	playbackFormats        = "mov,matroska,webm,avi,flv,mpegts,mpeg,ogg"
)

func (s *Service) RequestResourcePlayback(ctx context.Context, userID, id string) (*model.Resource, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, Unauthorized("请先登录")
	}
	if id == "" || assets.ValidID(id) != id {
		return nil, BadAuthRequest("资源 ID 无效")
	}
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	resource, err := s.repo.ResourceForUser(userID, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NotFound("资源不存在")
	}
	if err != nil {
		return nil, err
	}
	if resource.Kind != "video" {
		return nil, BadAuthRequest("仅视频资源可创建播放副本")
	}
	if resource.Status != model.ResourceStatusReady {
		return nil, NewAppError(http.StatusConflict, "原视频资源尚未就绪")
	}
	if resource.PlaybackStatus == model.PlaybackStatusProcessing && time.Since(resource.UpdatedAt) > playbackLeaseTTL {
		if err := s.repo.ExpireResourcePlayback(userID, id, time.Now().Add(-playbackLeaseTTL)); err != nil {
			return nil, err
		}
		return s.Resource(userID, id)
	}
	switch resource.PlaybackStatus {
	case model.PlaybackStatusReady, model.PlaybackStatusProcessing, model.PlaybackStatusFailed:
		resource.PublicURL = ""
		return resource, nil
	case "", model.PlaybackStatusNone:
	default:
		return nil, NewAppError(http.StatusConflict, "播放副本状态无效")
	}

	binary, binaryErr := renderFfmpegBinary()
	if binaryErr == nil {
		binary, binaryErr = exec.LookPath(binary)
	}
	var release func()
	if binaryErr == nil {
		if s.coordinator == nil || s.runtimeErr != nil {
			return nil, NewAppError(http.StatusServiceUnavailable, "播放适配协调服务不可用")
		}
		var acquired bool
		release, acquired, err = s.coordinator.Acquire(ctx, "video-playback", playbackConcurrency, playbackLeaseTTL)
		if err != nil {
			return nil, NewAppError(http.StatusServiceUnavailable, "播放适配协调服务不可用")
		}
		if !acquired {
			return nil, RateLimited("播放适配任务已达并发上限，请稍后重试")
		}
	}
	claimKey := newID() + ".mp4"
	claimed, err := s.repo.ClaimResourcePlayback(userID, id, claimKey)
	if err != nil || !claimed {
		if release != nil {
			release()
		}
		if err != nil {
			return nil, err
		}
		return s.Resource(userID, id)
	}
	resource.PlaybackObjectKey = claimKey
	resource.PlaybackStatus = model.PlaybackStatusProcessing
	resource.PlaybackError = ""
	if binaryErr != nil {
		if _, err := persistPlaybackCompletion(s.repo, resource, model.PlaybackStatusFailed, "播放适配不可用，请管理员安装 FFmpeg 或检查 CANVAS_FFMPEG_PATH"); err != nil {
			return nil, err
		}
	} else if !s.startPlaybackTask(*resource, binary, release) {
		release()
		if _, err := persistPlaybackCompletion(s.repo, resource, model.PlaybackStatusFailed, "服务正在停止，无法创建播放副本"); err != nil {
			return nil, err
		}
	}
	return s.Resource(userID, id)
}

func (s *Service) startPlaybackTask(resource model.Resource, binary string, release func()) bool {
	s.workerRuntimeMu.Lock()
	defer s.workerRuntimeMu.Unlock()
	if s.playbackClosed {
		return false
	}
	if s.playbackWorkers == nil {
		s.playbackWorkers = &platform.Worker{}
	}
	s.playbackWorkers.Start()
	return s.playbackWorkers.GoLoop(func(parent context.Context) {
		defer release()
		ctx, cancel := context.WithTimeout(parent, playbackTimeout)
		defer cancel()
		status, message := model.PlaybackStatusFailed, "播放副本处理失败，请下载原件"
		var published string
		defer func() {
			if recover() != nil {
				status, message = model.PlaybackStatusFailed, "播放副本处理失败，请下载原件"
			}
			s.storageMu.Lock()
			committed, err := persistPlaybackCompletion(s.repo, &resource, status, message)
			s.storageMu.Unlock()
			if err != nil {
				log.Printf("playback completion failed: error_type=%T", err)
			}
			if (!committed || err != nil || status != model.PlaybackStatusReady) && published != "" {
				_ = os.Remove(published)
			}
		}()
		var err error
		published, err = s.transcodePlayback(ctx, &resource, binary)
		if err == nil {
			status, message = model.PlaybackStatusReady, ""
		} else if ctx.Err() != nil {
			message = "播放副本处理超时或已取消，请下载原件"
		}
	})
}

func (s *Service) stopPlaybackWorkers() error {
	s.workerRuntimeMu.Lock()
	s.playbackClosed = true
	workers := s.playbackWorkers
	s.workerRuntimeMu.Unlock()
	if workers == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return workers.Stop(ctx)
}

type playbackCompleter interface {
	CompleteResourcePlayback(userID, id, claimKey, status, message string) (bool, error)
}

func persistPlaybackCompletion(repo playbackCompleter, resource *model.Resource, status, message string) (bool, error) {
	var err error
	for range playbackPersistAttempts {
		var changed bool
		changed, err = repo.CompleteResourcePlayback(resource.UserID, resource.ID, resource.PlaybackObjectKey, status, message)
		if err == nil {
			return changed, nil
		}
	}
	return false, err
}

func (s *Service) transcodePlayback(ctx context.Context, resource *model.Resource, binary string) (string, error) {
	if resource.Size > playbackMaxSourceBytes {
		return "", errors.New("playback source exceeds limit")
	}
	base, err := filepath.Abs(filepath.Join(s.dataDir, playbackDirName))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(base, 0o750); err != nil {
		return "", err
	}
	work, err := os.MkdirTemp(base, ".work-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(work)
	src := filepath.Join(work, "input")
	if err := s.materializePlaybackSource(ctx, resource, src); err != nil {
		return "", err
	}
	dst := filepath.Join(work, "output.mp4")
	if err := runH264Transcode(ctx, binary, src, dst); err != nil {
		return "", err
	}
	info, err := os.Stat(dst)
	if err != nil || info.Size() == 0 || info.Size() >= playbackMaxOutputBytes || probeVideoCodec(dst) != videoCodecH264 {
		return "", errors.New("invalid playback output")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// Unique claim keys avoid trusting or overwriting output from an earlier worker.
	published := filepath.Join(base, resource.PlaybackObjectKey)
	if err := os.Rename(dst, published); err != nil {
		return "", err
	}
	return published, nil
}

func (s *Service) materializePlaybackSource(ctx context.Context, resource *model.Resource, dst string) error {
	stream, err := s.openResourceRangeContext(ctx, resource.UserID, resource, "")
	if err != nil {
		return err
	}
	defer stream.Body.Close()
	stop := context.AfterFunc(ctx, func() { _ = stream.Body.Close() })
	defer stop()
	if stream.StatusCode != http.StatusOK || stream.ContentLength > playbackMaxSourceBytes {
		return errors.New("invalid playback source response")
	}
	file, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(file, io.LimitReader(stream.Body, playbackMaxSourceBytes+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if written == 0 || written > playbackMaxSourceBytes || (resource.Size > 0 && written != resource.Size) {
		return errors.New("invalid playback source length")
	}
	return nil
}

func playbackInputFormat(src string) (string, error) {
	file, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer file.Close()
	var header [16]byte
	n, _ := io.ReadFull(file, header[:])
	if n < 12 {
		return "", errors.New("unsupported playback container")
	}
	switch string(header[4:8]) {
	case "ftyp", "moov", "mdat", "wide", "free", "skip", "pnot":
		return "mov", nil
	}
	switch {
	case string(header[:4]) == "\x1a\x45\xdf\xa3":
		return "matroska", nil
	case string(header[:4]) == "RIFF" && string(header[8:12]) == "AVI ":
		return "avi", nil
	case string(header[:3]) == "FLV":
		return "flv", nil
	case string(header[:4]) == "OggS":
		return "ogg", nil
	case string(header[:4]) == "\x00\x00\x01\xba":
		return "mpeg", nil
	case header[0] == 0x47 || header[4] == 0x47:
		return "mpegts", nil
	default:
		return "", errors.New("unsupported playback container")
	}
}

func playbackFFmpegArgs(src, dst, format string) []string {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin", "-y",
		"-max_alloc", "268435456", "-threads", "2",
		"-protocol_whitelist", "file", "-format_whitelist", playbackFormats,
		"-f", format,
	}
	// MOV private options are rejected by other demuxers. Force the detected
	// demuxer so a disguised playlist cannot select a more permissive parser.
	if format == "mov" {
		args = append(args, "-enable_drefs", "0", "-use_absolute_path", "0")
	}
	return append(args, "-i", src,
		"-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn", "-map_metadata", "-1",
		"-c:v", "libx264", "-profile:v", "main", "-preset", "veryfast", "-crf", "23", "-threads", "2",
		"-pix_fmt", "yuv420p", "-filter_threads", "1", "-vf", "scale=trunc(iw/2)*2:trunc(ih/2)*2",
		"-c:a", "aac", "-ac", "2", "-b:a", "128k", "-movflags", "+faststart",
		"-fs", strconv.FormatInt(playbackMaxOutputBytes, 10), "-f", "mp4", dst,
	)
}

func runH264Transcode(ctx context.Context, binary, src, dst string) error {
	format, err := playbackInputFormat(src)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, binary, playbackFFmpegArgs(src, dst, format)...)
	cmd.WaitDelay = 2 * time.Second
	// FFmpeg diagnostics can contain input paths and attacker-controlled metadata.
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Run(); err != nil {
		return errors.New("playback transcoding failed")
	}
	return nil
}
