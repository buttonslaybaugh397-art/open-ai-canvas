package handler

import (
	"errors"
	"mime"
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

func TestResourceDownloadStreamsAttachmentAndDistinguishesMissingFromDatabaseFailure(t *testing.T) {
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
	if err := db.AutoMigrate(&model.User{}, &model.AuthSession{}, &model.SystemSetting{}, &model.Resource{}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []any{
		&model.User{ID: "owner", Username: "owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
		&model.AuthSession{ID: "session", UserID: "owner", TokenHash: auth.HashToken("download-token"), ExpiresAt: time.Now().Add(time.Hour)},
		&model.Resource{ID: "media", UserID: "owner", Status: model.ResourceStatusReady, Provider: "local", Kind: "image", MimeType: "image/png", ObjectKey: "media.png", Size: 5},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	dataDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, "resources"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "resources", "media.png"), []byte("image"), 0o600); err != nil {
		t.Fatal(err)
	}
	svc := service.New(repository.New(db), dataDir)
	t.Cleanup(func() { _ = svc.Close() })
	router := gin.New()
	RegisterUserDataRoutes(router.Group("/api"), svc)
	request := func(path, byteRange string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: "session.download-token"})
		req.Header.Set("Range", byteRange)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		return res
	}
	res := request("/api/resources/media/file?direct=1&download=1&filename=canvas.png", "")
	disposition, params, err := mime.ParseMediaType(res.Header().Get("Content-Disposition"))
	if res.Code != 200 || res.Body.String() != "image" || err != nil || disposition != "attachment" || params["filename"] != "canvas.png" {
		t.Fatalf("attachment response: %d %v %s", res.Code, res.Header(), res.Body.String())
	}
	res = request("/api/resources/media/file?download=1", "bytes=1-2")
	if res.Code != http.StatusPartialContent || res.Body.String() != "ma" {
		t.Fatalf("range download: %d %s", res.Code, res.Body.String())
	}
	if res = request("/api/resources/missing", ""); res.Code != http.StatusNotFound {
		t.Fatalf("missing resource: %d %s", res.Code, res.Body.String())
	}
	if err := db.Callback().Query().Before("gorm:query").Register("resource_lookup_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "resources" {
			_ = tx.AddError(errors.New("database unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	if res = request("/api/resources/media", ""); res.Code != http.StatusInternalServerError {
		t.Fatalf("database failures must not trigger 404 recovery: %d %s", res.Code, res.Body.String())
	}
}
