package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestStorageUsageRouteMeasuresOwnerFilesAndFailsClosed(t *testing.T) {
	t.Setenv("REDIS_URL", "")
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.AuthSession{}, &model.SystemSetting{}, &model.Resource{}, &model.SessionFile{}, &model.ResourceDeletionJob{}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&model.User{ID: "usage-owner", Username: "usage-owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
		&model.AuthSession{ID: "usage-session", UserID: "usage-owner", TokenHash: auth.HashToken("usage-token"), ExpiresAt: time.Now().Add(time.Hour)},
		&model.Resource{ID: "media", UserID: "usage-owner", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "media.png", Size: 10000},
		&model.Resource{ID: "other", UserID: "other", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "other.png", Size: 50000},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "resources"), 0o750); err != nil {
		t.Fatal(err)
	}
	mediaPath := filepath.Join(root, "resources", "media.png")
	if err := os.WriteFile(mediaPath, []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := service.New(repository.New(db), root)
	previous := runtimeService
	runtimeService = svc
	t.Cleanup(func() { runtimeService = previous; _ = svc.Close() })
	router := gin.New()
	RegisterUserDataRoutes(router.Group("/api"), svc)
	request := func(query string, authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/resources/storage-usage"+query, nil)
		if authenticated {
			req.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: "usage-session.usage-token"})
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	assertUsage := func(want int64) {
		t.Helper()
		res := request("?refresh=1&userId=other", true)
		var body struct {
			Code int `json:"code"`
			Data struct {
				Usage struct {
					UsedBytes            int64     `json:"usedBytes"`
					ResourceBytes        int64     `json:"resourceBytes"`
					SessionBytes         int64     `json:"sessionBytes"`
					PendingDeletionBytes int64     `json:"pendingDeletionBytes"`
					CheckedAt            time.Time `json:"checkedAt"`
				} `json:"usage"`
			} `json:"data"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil || res.Code != 200 || body.Code != 0 ||
			body.Data.Usage.UsedBytes != want || body.Data.Usage.ResourceBytes != want || body.Data.Usage.CheckedAt.IsZero() {
			t.Fatalf("usage response: %d %s %v", res.Code, res.Body, err)
		}
	}
	assertUsage(5)
	assertUsage(5)
	if err := os.Remove(mediaPath); err != nil {
		t.Fatal(err)
	}
	assertUsage(0)
	if res := request("", false); res.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated usage returned %d", res.Code)
	}
	if res := request("?refresh=invalid", true); res.Code != http.StatusBadRequest {
		t.Fatalf("invalid refresh returned %d", res.Code)
	}
	if err := db.Migrator().DropTable(&model.ResourceDeletionJob{}); err != nil {
		t.Fatal(err)
	}
	if res := request("", true); res.Code != http.StatusInternalServerError {
		t.Fatalf("database failure must not return zero usage: %d %s", res.Code, res.Body)
	}
	for range 3 {
		_ = request("?refresh=1", true)
	}
	if res := request("?refresh=1", true); res.Code != http.StatusTooManyRequests {
		t.Fatalf("unlimited forced recount: %d %s", res.Code, res.Body)
	}
}
