package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
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
	testResourcePreviewRedirectsToSignedCDN(t, false)
}

func TestAdminResourcePreviewRedirectsToSignedCDN(t *testing.T) {
	testResourcePreviewRedirectsToSignedCDN(t, true)
}

func testResourcePreviewRedirectsToSignedCDN(t *testing.T, adminRoute bool) {
	t.Helper()
	t.Setenv("REDIS_URL", "")
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	gin.SetMode(gin.TestMode)
	var heads, gets atomic.Int32
	var headStatus atomic.Int32
	headStatus.Store(http.StatusOK)
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Query().Get("token"), "test-access:") || r.URL.Query().Get("e") == "" {
			t.Error("CDN request must be signed")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if r.Method == http.MethodHead {
			heads.Add(1)
			w.WriteHeader(int(headStatus.Load()))
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
	for _, id := range []string{"owner", "other", "admin"} {
		role := model.UserRoleUser
		if id == "admin" {
			role = model.UserRoleAdmin
		}
		if err := db.Create(&model.User{ID: id, Username: id, Role: role, Status: model.UserStatusActive}).Error; err != nil {
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
	if adminRoute {
		if err := db.Create(&model.UserOSSSetting{ID: "owner-storage", UserID: "owner", Enabled: true, ValueJSON: string(settings)}).Error; err != nil {
			t.Fatal(err)
		}
		// A mismatched administrator/platform configuration must never sign the owner's URL.
		wrongSettings := strings.ReplaceAll(string(settings), "test-access", "wrong-access")
		if err := db.Create(&model.UserOSSSetting{ID: "admin-storage", UserID: "admin", Enabled: true, ValueJSON: wrongSettings}).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Model(&model.SystemSetting{}).Where("key = ?", "oss").Update("value_json", wrongSettings).Error; err != nil {
			t.Fatal(err)
		}
	}
	resource := model.Resource{
		ID: "preview-video", UserID: "owner", Kind: "video", Status: model.ResourceStatusReady,
		Provider: "qiniu", Endpoint: "https://up-z0.qiniup.com", Bucket: "test-bucket",
		ObjectKey: "users/owner/video/clip.mp4", MimeType: "video/mp4", Size: 8,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	svc := service.New(repository.New(db), dataDir)
	t.Cleanup(func() { _ = svc.Close() })
	router := gin.New()
	RegisterUserDataRoutes(router.Group("/api"), svc)
	RegisterAdminStorageRoutes(router.Group("/api"), svc)
	actor := "owner"
	endpoint := "/api/resources/" + resource.ID + "/file"
	if adminRoute {
		actor = "admin"
		endpoint = "/api/admin/resources/" + resource.ID + "/file"
	}
	request := func(user, query string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, endpoint+query, nil)
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
		{query: "?direct=1&download=1&filename=" + url.QueryEscape("\u7ec8\u7a3f.mp4"), name: "\u7ec8\u7a3f.mp4"},
		{query: "?download=1", name: resource.ID},
		{}, // A preview after downloading must not inherit attachment mode.
	} {
		res := request(actor, tc.query)
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
		if tc.name == "" && (res.Header().Get("Content-Disposition") != "" || location.Query().Get("response-content-disposition") != "") {
			t.Fatal("preview must not carry download attachment headers or overrides")
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
	deniedStatus := http.StatusNotFound
	if adminRoute {
		deniedStatus = http.StatusForbidden
		if res := request("owner", ""); res.Code != deniedStatus || res.Header().Get("Location") != "" {
			t.Fatalf("resource owner must not use admin endpoint: status=%d", res.Code)
		}
	}
	if res := request("other", ""); res.Code != deniedStatus || res.Header().Get("Location") != "" {
		t.Fatalf("cross-user preview status=%d", res.Code)
	}

	// Follow the redirect as a browser would, keeping the video byte range.
	preview := request(actor, "")
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
	proxy := request(actor, "?proxy=1&direct=1&download=1&filename=proxy.mp4")
	if proxy.Code != http.StatusTemporaryRedirect || !strings.HasPrefix(proxy.Header().Get("Location"), cdn.URL) || gets.Load() != 1 {
		t.Fatalf("legacy proxy flag must not relay cloud bytes: status=%d", proxy.Code)
	}
	resolved := request(actor, "?resolve=1")
	var envelope struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resolved.Body.Bytes(), &envelope); err != nil || resolved.Code != http.StatusOK || !strings.HasPrefix(envelope.Data.URL, cdn.URL) || gets.Load() != 1 {
		t.Fatal("resolving the CDN address must return JSON without reading media")
	}
	if denied := request("other", "?resolve=1"); denied.Code != deniedStatus {
		t.Fatal("resolving the CDN address must retain authorization")
	}
	routePrefix := "/api/resources/"
	if adminRoute {
		routePrefix = "/api/admin/resources/"
	}
	{
		for _, status := range []int{http.StatusNotFound, http.StatusServiceUnavailable} {
			headStatus.Store(int32(status))
			fallback := resource
			fallback.ID = "fallback-" + strconv.Itoa(status)
			fallback.ObjectKey = "users/owner/video/" + fallback.ID + ".mp4"
			if err := db.Create(&fallback).Error; err != nil {
				t.Fatal(err)
			}
			endpoint = routePrefix + fallback.ID + "/file"
			res := request(actor, "")
			if res.Code != http.StatusServiceUnavailable || res.Header().Get("Location") != "" || gets.Load() != 1 {
				t.Fatalf("failed HEAD must not relay media: status=%d", res.Code)
			}
			headStatus.Store(http.StatusOK)
			// Failed probes use the shared cache's one-second cooldown.
			time.Sleep(1100 * time.Millisecond)
			if retry := request(actor, ""); retry.Code != http.StatusTemporaryRedirect {
				t.Fatal("CDN recovery must be retried after the failure cooldown")
			}
		}
		local := model.Resource{ID: "local-file", UserID: "owner", Provider: "local", Kind: "file", Status: model.ResourceStatusReady, ObjectKey: "test.txt", Size: 8, MimeType: "text/plain"}
		if err := db.Create(&local).Error; err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(dataDir, "resources"), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dataDir, "resources", local.ObjectKey), []byte("textmore"), 0o600); err != nil {
			t.Fatal(err)
		}
		endpoint = routePrefix + local.ID + "/file"
		res := request(actor, "?download=1&filename=test.txt")
		if res.Code != http.StatusPartialContent || res.Header().Get("Content-Range") != "bytes 0-3/8" || res.Body.String() != "text" || res.Header().Get("Location") != "" || res.Header().Get("Content-Disposition") != "attachment; filename=test.txt" {
			t.Fatal("local download must retain its body and attachment filename")
		}
		if err := db.Model(&local).Update("status", model.ResourceStatusFailed).Error; err != nil {
			t.Fatal(err)
		}
		if res := request(actor, ""); res.Code != http.StatusBadRequest || res.Header().Get("Location") != "" {
			t.Fatalf("failed resource must not redirect: status=%d", res.Code)
		}
		endpoint = routePrefix + "missing/file"
		if res := request(actor, ""); res.Code != http.StatusNotFound || res.Header().Get("Location") != "" {
			t.Fatalf("missing resource must not redirect: status=%d", res.Code)
		}
	}
}
