package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"infinite-canvas/backend/internal/service"
)

// 分片上传会话：把“导入本地媒体”拆成 开始→逐片→合并 三段，单片上限 8MB，
// 文件整体不再受 multipart 单请求大小限制（对齐 Concat 桌面端“任意大小直接入库”的体验）。
// 会话状态只存在内存；重启后需重新导入。片级瞬时失败复用原会话重试。
const (
	chunkUploadChunkSize   = 8 << 20
	chunkUploadSlackBytes  = 64 << 10 // MaxBytesReader 允许的超片余量
	chunkUploadTTL         = 90 * time.Minute
	chunkUploadMaxPerUser  = 32
	chunkUploadBodyCapJSON = 16 << 10
)

type chunkedUploadSession struct {
	mu             sync.RWMutex
	closed         bool
	ID             string
	UserID         string
	FileName       string
	Kind           string
	Size           int64
	Width          int
	Height         int
	DurationMs     int64
	IdempotencyKey string
	ChunkCount     int
	Dir            string
	CreatedAt      time.Time
}

var chunkUploadSessions = struct {
	sync.Mutex
	m map[string]*chunkedUploadSession
}{m: make(map[string]*chunkedUploadSession)}

func newUploadSessionID() string {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
}

func (s *chunkedUploadSession) chunkPath(index int) string {
	return filepath.Join(s.Dir, fmt.Sprintf("chunk-%d", index))
}

func (s *chunkedUploadSession) hasAllChunks() bool {
	for i := 0; i < s.ChunkCount; i++ {
		info, err := os.Stat(s.chunkPath(i))
		if err != nil || info.Size() != s.chunkSizeAt(i) {
			return false
		}
	}
	return true
}

// chunkSizeAt 返回第 index 片的期望字节数（末片按文件余量，其余固定 chunkSize）。
func (s *chunkedUploadSession) chunkSizeAt(index int) int64 {
	if index == s.ChunkCount-1 {
		rest := s.Size - int64(index)*chunkUploadChunkSize
		if rest < 0 {
			return 0
		}
		return rest
	}
	return chunkUploadChunkSize
}

func removeExpiredChunkSessions() {
	now := time.Now()
	var expired []*chunkedUploadSession
	chunkUploadSessions.Lock()
	for id, sess := range chunkUploadSessions.m {
		if now.Sub(sess.CreatedAt) > chunkUploadTTL {
			expired = append(expired, sess)
			delete(chunkUploadSessions.m, id)
		}
	}
	chunkUploadSessions.Unlock()
	for _, sess := range expired {
		removeChunkSessionFiles(sess)
	}
}

func takeChunkSession(id string) *chunkedUploadSession {
	removeExpiredChunkSessions()
	chunkUploadSessions.Lock()
	defer chunkUploadSessions.Unlock()
	return chunkUploadSessions.m[id]
}

func dropChunkSession(id string) {
	chunkUploadSessions.Lock()
	sess := chunkUploadSessions.m[id]
	delete(chunkUploadSessions.m, id)
	chunkUploadSessions.Unlock()
	if sess != nil {
		removeChunkSessionFiles(sess)
	}
}

func removeChunkSessionFiles(sess *chunkedUploadSession) {
	sess.mu.Lock()
	defer sess.mu.Unlock()
	sess.closed = true
	_ = os.RemoveAll(sess.Dir)
}

func chunkUploadIdentity(header, body string) (string, error) {
	header, body = strings.TrimSpace(header), strings.TrimSpace(body)
	if header != "" && body != "" && header != body {
		return "", fmt.Errorf("上传幂等标识不一致")
	}
	if header != "" {
		return header, nil
	}
	return body, nil
}

func (s *chunkedUploadSession) writeChunk(index int, body io.Reader) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return service.NotFound("上传会话已结束，请重新导入")
	}
	if index < 0 || index >= s.ChunkCount || s.chunkSizeAt(index) <= 0 {
		return service.BadAuthRequest("非法的分片序号")
	}
	// 每次尝试使用独立临时文件，完整接收后发布，失败不破坏已确认分片。
	dst, err := os.CreateTemp(s.Dir, "incoming-*")
	if err != nil {
		return err
	}
	defer os.Remove(dst.Name())
	_, copyErr := io.CopyN(dst, body, s.chunkSizeAt(index))
	closeErr := dst.Close()
	if copyErr != nil {
		return service.BadAuthRequest(fmt.Sprintf("分片 %d 上传不完整，请重试", index))
	}
	if closeErr != nil {
		return closeErr
	}
	var probe [1]byte
	extra, readErr := body.Read(probe[:])
	if extra != 0 || readErr != io.EOF {
		return service.BadAuthRequest(fmt.Sprintf("分片 %d 长度无效，请重试", index))
	}
	return os.Rename(dst.Name(), s.chunkPath(index))
}

// RegisterChunkedUploadRoutes 注册本地媒体分片上传三条接口（POST 开始 / PUT 上传片 / POST 合并）。
func RegisterChunkedUploadRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.POST("/resources/uploads", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		policy, available := loadRuntimePolicy(c, svc)
		if !available || !enforceRateLimit(c, "resources-upload:"+user.ID, policy.Request.ResourceUploadPerMinute, time.Minute) {
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, chunkUploadBodyCapJSON)
		var req struct {
			FileName       string `json:"fileName"`
			Kind           string `json:"kind"`
			Size           int64  `json:"size"`
			Width          int    `json:"width"`
			Height         int    `json:"height"`
			DurationMs     int64  `json:"durationMs"`
			IdempotencyKey string `json:"idempotencyKey"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		if req.FileName == "" || len(req.FileName) > 255 {
			fail(c, http.StatusBadRequest, fmt.Errorf("文件名不能为空且不能超过 255 个字符"))
			return
		}
		if req.Size <= 0 {
			fail(c, http.StatusBadRequest, fmt.Errorf("文件大小必须大于 0"))
			return
		}
		uploadIdentity, err := chunkUploadIdentity(c.GetHeader("X-Idempotency-Key"), req.IdempotencyKey)
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		// 超账号存储总量的文件无论如何都会失败，提前给出明确提示。
		if policy.Resource.StoredFileGB > 0 && req.Size > int64(policy.Resource.StoredFileGB)<<30 {
			fail(c, http.StatusBadRequest, fmt.Errorf("文件超过账号存储总量上限 %dGB", policy.Resource.StoredFileGB))
			return
		}
		// 同一用户并发会话数兜底，防内存占用失控。
		removeExpiredChunkSessions()
		active := 0
		chunkUploadSessions.Lock()
		for _, sess := range chunkUploadSessions.m {
			if sess.UserID == user.ID {
				active++
			}
		}
		chunkUploadSessions.Unlock()
		if active >= chunkUploadMaxPerUser {
			fail(c, http.StatusTooManyRequests, fmt.Errorf("同时进行中的上传过多，请稍后重试"))
			return
		}
		dir, err := os.MkdirTemp("", "canvas-chunk-upload-*")
		if err != nil {
			failService(c, err)
			return
		}
		session := &chunkedUploadSession{
			ID:             newUploadSessionID(),
			UserID:         user.ID,
			FileName:       req.FileName,
			Kind:           req.Kind,
			Size:           req.Size,
			Width:          req.Width,
			Height:         req.Height,
			DurationMs:     req.DurationMs,
			IdempotencyKey: uploadIdentity,
			ChunkCount:     int((req.Size + chunkUploadChunkSize - 1) / chunkUploadChunkSize),
			Dir:            dir,
			CreatedAt:      time.Now(),
		}
		if session.IdempotencyKey == "" {
			session.IdempotencyKey = "chunk-upload:" + session.ID
		}
		chunkUploadSessions.Lock()
		chunkUploadSessions.m[session.ID] = session
		chunkUploadSessions.Unlock()
		ok(c, gin.H{"uploadId": session.ID, "chunkSize": chunkUploadChunkSize, "chunkCount": session.ChunkCount})
	})

	r.PUT("/resources/uploads/:id/chunks/:index", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		session := takeChunkSession(c.Param("id"))
		if session == nil || session.UserID != user.ID {
			fail(c, http.StatusNotFound, fmt.Errorf("上传会话不存在或已过期，请重新导入"))
			return
		}
		index, err := strconv.Atoi(c.Param("index"))
		if err != nil || index < 0 || index >= session.ChunkCount {
			fail(c, http.StatusBadRequest, fmt.Errorf("非法的分片序号"))
			return
		}
		expected := session.chunkSizeAt(index)
		if expected <= 0 {
			fail(c, http.StatusBadRequest, fmt.Errorf("非法的分片序号"))
			return
		}
		// 单片限长（期望长度 + 少量余量），超长直接中断，避免内存/磁盘被恶意占用。
		body := http.MaxBytesReader(c.Writer, c.Request.Body, expected+chunkUploadSlackBytes)
		receiveStarted := time.Now()
		err = session.writeChunk(index, body)
		appendUploadTiming(c, "receive", receiveStarted)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"index": index})
	})

	r.POST("/resources/uploads/:id/complete", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		id := c.Param("id")
		session := takeChunkSession(id)
		if session == nil || session.UserID != user.ID {
			fail(c, http.StatusNotFound, fmt.Errorf("上传会话不存在或已过期，请重新导入"))
			return
		}
		session.mu.Lock()
		cleanup := false
		defer func() {
			session.mu.Unlock()
			if cleanup {
				dropChunkSession(id)
			}
		}()
		if session.closed {
			fail(c, http.StatusNotFound, fmt.Errorf("上传会话已结束，请重新导入"))
			return
		}
		if !session.hasAllChunks() {
			fail(c, http.StatusBadRequest, fmt.Errorf("上传文件不完整，请重新导入"))
			return
		}
		mergedPath := filepath.Join(session.Dir, "merged")
		mergeStarted := time.Now()
		merged, err := os.OpenFile(mergedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			failService(c, err)
			return
		}
		total := int64(0)
		for i := 0; i < session.ChunkCount; i++ {
			part, openErr := os.Open(session.chunkPath(i))
			if openErr != nil {
				_ = merged.Close()
				failService(c, openErr)
				return
			}
			n, copyErr := io.Copy(merged, part)
			_ = part.Close()
			if copyErr != nil {
				_ = merged.Close()
				failService(c, copyErr)
				return
			}
			total += n
		}
		if closeErr := merged.Close(); closeErr != nil {
			failService(c, closeErr)
			return
		}
		if total != session.Size {
			session.closed, cleanup = true, true
			fail(c, http.StatusBadRequest, fmt.Errorf("上传文件不完整，请重新导入"))
			return
		}
		fh, err := os.Open(mergedPath)
		if err != nil {
			failService(c, err)
			return
		}
		defer fh.Close()
		appendUploadTiming(c, "merge", mergeStarted)
		storeStarted := time.Now()
		resource, svcErr := svc.UploadResourceFile(user.ID, session.FileName, session.Size, session.Kind, session.Width, session.Height, session.DurationMs, fh, session.IdempotencyKey)
		appendUploadTiming(c, "store", storeStarted)
		session.closed, cleanup = true, true
		if svcErr != nil {
			failService(c, svcErr)
			return
		}
		ok(c, gin.H{"resource": resource})
	})
}
