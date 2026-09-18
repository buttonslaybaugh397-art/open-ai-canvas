package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"infinite-canvas/backend/internal/auth"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"
)

func TestResourcePreviewRedirectsToSignedCDN(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	gin.SetMode(gin.TestMode)
	var heads, gets atomic.Int32
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") == "" || r.URL.Query().Get("e") == "" {
			t.Error("CDN request must be signed")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Method == http.MethodHead {
			heads.Add(1)
			w.WriteHeader(http.StatusOK)
			return
		}
		gets.Add(1)
		if r.Header.Get("Range") != "bytes=0-3" {
			t.Error("video range was not preserved")
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", "bytes 0-3/8")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("clip"))
	}))
	defer cdn.Close()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.AuthSession{}, &model.SystemSetting{}, &model.UserOSSSetting{}, &model.StorageLocation{}, &model.Resource{}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"owner", "other"} {
		if err := db.Create(&model.User{ID: id, Username: id, Role: model.UserRoleUser, Status: model.UserStatusActive}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.AuthSession{ID: id, UserID: id, TokenHash: auth.HashToken("preview-token"), ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	settings, err := json.Marshal(map[string]any{
		"enabled": true, "provider": "qiniu", "endpoint": "https://up-z0.qiniup.com", "cdnBaseUrl": cdn.URL,
		"bucket": "test-bucket", "accessKeyId": "test-access", "accessKeySecret": "test-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: "oss", ValueJSON: string(settings)}).Error; err != nil {
		t.Fatal(err)
	}
	resource := model.Resource{
		ID: "preview-video", UserID: "owner", Kind: "video", Status: model.ResourceStatusReady,
		Provider: "qiniu", Endpoint: "https://up-z0.qiniup.com", Bucket: "test-bucket",
		ObjectKey: "users/owner/video/clip.mp4", MimeType: "video/mp4", Size: 8,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	svc := service.New(repository.New(db), t.TempDir())
	t.Cleanup(func() { _ = svc.Close() })
	router := gin.New()
	RegisterUserDataRoutes(router.Group("/api"), svc)
	request := func(user, query string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/resources/"+resource.ID+"/file"+query, nil)
		req.Header.Set("Range", "bytes=0-3")
		if user != "" {
			req.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: user + ".preview-token"})
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	for _, tc := range []struct {
		query, name string
	}{
		{},
		{query: "?variant=playback"},
		{query: "?direct=1&download=1&filename=" + url.QueryEscape("final video.mp4"), name: "final video.mp4"},
	} {
		res := request("owner", tc.query)
		if res.Code != http.StatusTemporaryRedirect {
			t.Fatalf("preview status=%d, want 307; body=%s", res.Code, res.Body.String())
		}
		if res.Header().Get("Cache-Control") != "private, no-store" || res.Header().Get("Vary") != "Cookie" || res.Header().Get("Referrer-Policy") != "no-referrer" {
			t.Fatal("signed redirects must be private, uncached and omit referrers")
		}
		location, err := url.Parse(res.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(location.String(), cdn.URL+"/") || location.Query().Get("attname") != tc.name {
			t.Fatal("redirect must target CDN with the correct attachment mode")
		}
		if gets.Load() != 0 {
			t.Fatal("preview handler must not download or relay the media")
		}
	}
	if heads.Load() != 1 {
		t.Fatalf("HEAD probes=%d, want one cached probe", heads.Load())
	}
	if res := request("", ""); res.Code != http.StatusUnauthorized || res.Header().Get("Location") != "" {
		t.Fatalf("anonymous preview status=%d", res.Code)
	}
	if res := request("other", ""); res.Code != http.StatusNotFound || res.Header().Get("Location") != "" {
		t.Fatalf("cross-user preview status=%d", res.Code)
	}

	// Follow the redirect as a browser would, keeping the video byte range.
	preview := request("owner", "")
	req, err := http.NewRequest(http.MethodGet, preview.Header().Get("Location"), nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-3")
	res, err := cdn.Client().Do(req)
	if err != nil {
		t.Fatal("direct CDN request failed")
	}
	body, readErr := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if readErr != nil || res.StatusCode != http.StatusPartialContent || string(body) != "clip" || gets.Load() != 1 {
		t.Fatal("direct CDN playback did not return the requested media range")
	}
	proxy := request("owner", "?proxy=1&direct=1")
	if proxy.Code != http.StatusPartialContent || proxy.Header().Get("Location") != "" || proxy.Body.String() != "clip" {
		t.Fatalf("explicit proxy did not preserve range delivery: status=%d", proxy.Code)
	}
}
