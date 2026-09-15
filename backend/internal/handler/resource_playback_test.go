package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/auth"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestResourcePlaybackRoutesRangeAndOriginalDownload(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	t.Setenv("CANVAS_FFMPEG_PATH", filepath.Join(t.TempDir(), "missing-ffmpeg"))
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.AuthSession{}, &model.SystemSetting{}, &model.UserOSSSetting{}, &model.StorageLocation{}, &model.Resource{}); err != nil {
		t.Fatal(err)
	}
	originalServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/original.mov" {
			t.Errorf("unexpected original path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "video/quicktime")
		_, _ = w.Write([]byte("original-movie"))
	}))
	defer originalServer.Close()
	settingJSON, _ := json.Marshal(map[string]any{"enabled": true, "provider": "aliyun", "endpoint": "https://oss.example.com", "bucket": "bucket", "accessKeyId": "test-key", "accessKeySecret": "test-secret", "cdnBaseUrl": originalServer.URL})
	for _, row := range []any{
		&model.User{ID: "owner", Username: "owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
		&model.AuthSession{ID: "session", UserID: "owner", TokenHash: auth.HashToken("playback-token"), ExpiresAt: time.Now().Add(time.Hour)},
		&model.SystemSetting{Key: "oss", ValueJSON: string(settingJSON)},
		&model.Resource{ID: "cloud", UserID: "owner", Status: model.ResourceStatusReady, Provider: "aliyun", Endpoint: "https://oss.example.com", Bucket: "bucket", Kind: "video", MimeType: "video/quicktime", ObjectKey: "original.mov", PlaybackStatus: model.PlaybackStatusReady, PlaybackObjectKey: "compatible.mp4"},
		&model.Resource{ID: "plain", UserID: "owner", Status: model.ResourceStatusReady, Provider: "local", Kind: "video", PlaybackStatus: model.PlaybackStatusNone},
		&model.Resource{ID: "foreign", UserID: "other", Status: model.ResourceStatusReady, Kind: "video"},
		&model.Resource{ID: "image", UserID: "owner", Status: model.ResourceStatusReady, Kind: "image"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "playback"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "playback", "compatible.mp4"), []byte("compatibility-video"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := service.New(repository.New(db), dataDir)
	previous := runtimeService
	runtimeService = svc
	t.Cleanup(func() { runtimeService = previous; _ = svc.Close() })
	router := gin.New()
	RegisterUserDataRoutes(router.Group("/api"), svc)
	request := func(method, path string, authenticated bool, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if authenticated {
			req.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: "session.playback-token"})
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	for _, tc := range []struct {
		id     string
		auth   bool
		status int
	}{{"plain", false, 401}, {"foreign", true, 404}, {"image", true, 400}, {"missing", true, 404}, {"plain", true, 200}, {"plain", true, 200}, {"cloud", true, 200}} {
		res := request(http.MethodPost, "/api/resources/"+tc.id+"/playback", tc.auth, nil)
		if res.Code != tc.status {
			t.Fatalf("POST %s = %d, want %d: %s", tc.id, res.Code, tc.status, res.Body.String())
		}
		if tc.status == 200 {
			var envelope struct {
				Code int                               `json:"code"`
				Data struct{ Resource model.Resource } `json:"data"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil || envelope.Code != 0 || envelope.Data.Resource.ID != tc.id {
				t.Fatalf("bad playback contract: %s %v", res.Body.String(), err)
			}
			if tc.id == "plain" && envelope.Data.Resource.PlaybackStatus != model.PlaybackStatusFailed {
				t.Fatal("missing FFmpeg was not an explicit failed state")
			}
		}
	}
	path := "/api/resources/cloud/file?variant=playback&direct=1"
	res := request(http.MethodGet, path, true, map[string]string{"Range": "bytes=1-4"})
	if res.Code != http.StatusPartialContent || res.Body.String() != "ompa" || res.Header().Get("Content-Type") != "video/mp4" || res.Header().Get("Content-Range") != "bytes 1-4/19" {
		t.Fatalf("cloud playback range failed: %d %v %s", res.Code, res.Header(), res.Body.String())
	}
	etag := res.Header().Get("ETag")
	if !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) || !strings.Contains(etag, ":pb:") {
		t.Fatalf("playback ETag = %q", etag)
	}
	if res := request(http.MethodGet, path, true, map[string]string{"If-None-Match": etag}); res.Code != 304 {
		t.Fatalf("conditional playback: %d", res.Code)
	}
	res = request(http.MethodGet, path, true, map[string]string{"Range": "bytes=1-4", "If-Range": `"original"`})
	if res.Code != http.StatusOK || res.Body.String() != "compatibility-video" {
		t.Fatalf("If-Range mismatch should return whole playback: %d %s", res.Code, res.Body.String())
	}
	res = request(http.MethodGet, path+"&download=1", true, nil)
	if res.Code != http.StatusOK || res.Body.String() != "original-movie" || res.Header().Get("Content-Disposition") != "attachment" {
		t.Fatalf("download did not use original: %d %v %s", res.Code, res.Header(), res.Body.String())
	}
	if res := request(http.MethodGet, path, false, nil); res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated playback = %d", res.Code)
	}
	policy, err := svc.RuntimePolicy()
	if err != nil {
		t.Fatal(err)
	}
	for range policy.Request.ResourceImportPerMinute {
		_, _ = svc.AllowRequest(context.Background(), "resources-playback:owner", policy.Request.ResourceImportPerMinute, time.Minute)
	}
	if res := request(http.MethodPost, "/api/resources/cloud/playback", true, nil); res.Code != 429 || res.Header().Get("Retry-After") == "" {
		t.Fatalf("playback rate limit = %d %s", res.Code, res.Body.String())
	}
}
