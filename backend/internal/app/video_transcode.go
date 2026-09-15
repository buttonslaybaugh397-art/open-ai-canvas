package app

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"errors"

	"infinite-canvas/backend/internal/model"
)

// 播放副本转码：HEVC/H.265 原片在 Chrome/Firefox 等无法解码（<video> 黑屏），
// 上传就绪后若探测到 hvc1/hev1 且本机可用 ffmpeg，则异步转 H.264/AAC 到本地
// playback 目录，供 file 端点 variant=playback 读取。自动探测只处理本地原件；
// 云端原件仅由显式的播放适配请求触发。

// ErrPlaybackNotReady 表示资源没有可用的浏览器兼容播放副本（未转码/转码中/失败），
// file 端点应回退 serve 原件。
var ErrPlaybackNotReady = errors.New("播放副本尚未就绪")

const (
	playbackDirName         = "playback"
	videoCodecH264          = "h264"
	videoCodecH265          = "h265"
	videoCodecAV1           = "av1"
	videoCodecVP9           = "vp9"
	videoCodecMPEG4         = "mpeg4"
	probeMaxMoovSize        = 128 << 20
	playbackPersistAttempts = 3
)

// probeVideoCodec 解析本地 mp4 的 stsd 首个视频 sample entry fourcc，返回 h264/h265 等。
// 非 mp4 容器或解析失败返回空串（调用方按“无需转码”处理，前端仍可用原件）。
func probeVideoCodec(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	return probeVideoCodecReader(f)
}

func probeVideoCodecReader(f io.ReaderAt) string {
	// 顶层 box 遍历（跳过 mdat 数据体），定位 moov。
	var moovSize int64
	var moovData []byte
	pos := int64(0)
	for {
		boxType, size, err := readMP4BoxHeaderAt(f, pos)
		if err != nil {
			return ""
		}
		switch boxType {
		case "moov":
			if size > probeMaxMoovSize {
				return ""
			}
			moovData = make([]byte, size-8)
			if _, err := f.ReadAt(moovData, pos+8); err != nil {
				return ""
			}
			moovSize = size
		}
		pos += size
		if moovSize != 0 {
			break
		}
		if size < 8 {
			return ""
		}
	}
	return codecFromMoov(moovData)
}

// readMP4BoxHeaderAt 定位文件 pos 处的 box（8/16 字节头），返回类型与总大小。
func readMP4BoxHeaderAt(r io.ReaderAt, pos int64) (string, int64, error) {
	var hdr [16]byte
	if _, err := r.ReadAt(hdr[:8], pos); err != nil {
		return "", 0, err
	}
	size := int64(binary.BigEndian.Uint32(hdr[:4]))
	boxType := string(hdr[4:8])
	if size == 1 {
		if _, err := r.ReadAt(hdr[8:16], pos+8); err != nil {
			return "", 0, err
		}
		size = int64(binary.BigEndian.Uint64(hdr[8:16]))
	}
	if size < 8 {
		return "", 0, fmt.Errorf("invalid box size %d", size)
	}
	return boxType, size, nil
}

// codecFromMoov 在 moov 子树中找出所有 stsd，取首个视频 sample entry fourcc。
func codecFromMoov(moov []byte) string {
	for _, stsdBody := range boxBodies(moov, "stsd") {
		// stsd = fullbox(4) + entry_count(4) + entries…
		// 首个 sample entry：entry_size(4) + fourcc(4) → fourcc 在 body+12。
		if stsdBody+16 > len(moov) {
			continue
		}
		fourcc := string(moov[stsdBody+12 : stsdBody+16])
		switch fourcc {
		case "avc1":
			return videoCodecH264
		case "hvc1", "hev1":
			return videoCodecH265
		case "av01":
			return videoCodecAV1
		case "vp09":
			return videoCodecVP9
		case "mp4v":
			return videoCodecMPEG4
		}
	}
	return ""
}

// boxBodies 在 data 中递归查找类型为 want 的 box，返回其 body 起始偏移。
// 只下钻容器 box（moov/trak/mdia/minf/stbl），避免误入 sample entry 内部。
func boxBodies(data []byte, want string) []int {
	var out []int
	var walk func(start, end, depth int)
	walk = func(start, end, depth int) {
		if depth > 32 {
			return
		}
		pos := start
		for pos+8 <= end {
			size := int(binary.BigEndian.Uint32(data[pos : pos+4]))
			boxType := string(data[pos+4 : pos+8])
			hdr := 8
			if size == 1 {
				if pos+16 > end {
					return
				}
				size = int(binary.BigEndian.Uint64(data[pos+8 : pos+16]))
				hdr = 16
			}
			if size < 8 || pos+size > end {
				return
			}
			if boxType == want {
				out = append(out, pos+hdr)
			}
			switch boxType {
			case "moov", "trak", "mdia", "minf", "stbl":
				walk(pos+hdr, pos+size, depth+1)
			}
			pos += size
		}
	}
	walk(0, len(data), 0)
	return out
}

// Automatic probing remains local-only; explicit requests can force any supported container.
func (s *Service) maybeStartPlaybackTranscode(resource *model.Resource) {
	if resource == nil || resource.Kind != "video" || resource.Status != model.ResourceStatusReady {
		return
	}
	if resource.Provider != "local" {
		markPlaybackNone(s, resource)
		return
	}
	if resource.PlaybackStatus != "" && resource.PlaybackStatus != model.PlaybackStatusNone {
		return
	}
	stream, err := s.openResourceRangeContext(context.Background(), resource.UserID, resource, "")
	if err != nil {
		return
	}
	reader, ok := stream.Body.(io.ReaderAt)
	codec := ""
	if ok {
		codec = probeVideoCodecReader(reader)
	}
	stream.Body.Close()
	switch codec {
	case videoCodecH265, videoCodecMPEG4:
		if next, err := s.RequestResourcePlayback(context.Background(), resource.UserID, resource.ID); err == nil {
			resource.PlaybackStatus = next.PlaybackStatus
			resource.PlaybackObjectKey = next.PlaybackObjectKey
			resource.PlaybackError = next.PlaybackError
		}
	case videoCodecH264, videoCodecAV1, videoCodecVP9, "":
		markPlaybackNone(s, resource)
	}
}

// markPlaybackNone 将资源标记为无需播放副本（幂等）。写失败必须可见：空状态会被前端当成 processing 轮询。
func markPlaybackNone(s *Service, resource *model.Resource) {
	if resource.PlaybackStatus == model.PlaybackStatusNone {
		return
	}
	updated, err := s.repo.MarkResourcePlaybackNone(resource.UserID, resource.ID)
	if err != nil {
		log.Printf("playback mark none failed: error_type=%T", err)
	} else if updated {
		resource.PlaybackStatus = model.PlaybackStatusNone
	}
}

func clipText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// OpenResourcePlaybackRange 打开浏览器兼容播放副本（本地 ffmpeg 转码的 H.264）。
// 副本始终来自本地 playback 目录，与原件的存储 Provider 无关。
func (s *Service) OpenResourcePlaybackRange(userID string, resourceID string) (*ResourceStream, error) {
	resource, err := s.repo.ResourceForUser(userID, resourceID)
	if err != nil {
		return nil, err
	}
	if resource == nil || resource.Kind != "video" || resource.Status != model.ResourceStatusReady ||
		resource.PlaybackStatus != model.PlaybackStatusReady || resource.PlaybackObjectKey == "" {
		return nil, ErrPlaybackNotReady
	}
	root, err := os.OpenRoot(filepath.Join(s.dataDir, playbackDirName))
	if err != nil {
		return nil, err
	}
	defer root.Close()
	body, err := root.Open(filepath.FromSlash(resource.PlaybackObjectKey))
	if err != nil {
		return nil, err
	}
	playback := *resource
	playback.Provider = "local"
	playback.MimeType = "video/mp4"
	playback.ObjectKey = filepath.Join(playbackDirName, resource.PlaybackObjectKey)
	st, err := body.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
		body.Close()
		return nil, ErrPlaybackNotReady
	}
	return &ResourceStream{Resource: &playback, Body: body, StatusCode: http.StatusOK, ContentLength: st.Size(), AcceptRanges: "bytes"}, nil
}

// BackfillPlaybackTranscodes 在服务启动后扫描存量本地视频：未判定 codec 的补判定，
// H.265/MPEG-4 Part 2 触发转码、H.264 标记 none；再对旧规则遗留的 none 行做一次
// 有界重判（见 PlaybackNoneVideos）。幂等：maybeStartPlaybackTranscode 先置
// processing/none 再入库，重复扫描不会重复转码。
func (s *Service) BackfillPlaybackTranscodes() {
	if err := s.repo.ExpireResourcePlaybacks(time.Now().Add(-playbackLeaseTTL)); err != nil {
		log.Printf("playback expiration failed: error_type=%T", err)
		return
	}
	// Bound the scan even when capacity or persistence is unavailable.
	if resources, err := s.repo.PlaybackPendingVideos(20); err == nil {
		for i := range resources {
			s.maybeStartPlaybackTranscode(&resources[i])
		}
	}
	// 旧版本曾把 H.265/MPEG-4 Part 2 误判为浏览器可播并落 none；对存量 none 行
	// 做一次有界重判（H.264 保持 none，H.265/MPEG-4 Part 2 触发转码），使 codec
	// 判定规则的变更覆盖规则变更前已导入的文件。每次启动最多重判 20 条最旧行，
	// 天然收敛且不会重复转码（claim 原子地把 none → processing）。
	legacy, err := s.repo.PlaybackNoneVideos(20)
	if err == nil {
		for i := range legacy {
			s.maybeStartPlaybackTranscode(&legacy[i])
		}
	}
}
