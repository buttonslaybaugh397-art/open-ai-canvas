package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestResourceRepairRouteContract(t *testing.T) {
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
	if err := db.AutoMigrate(&model.User{}, &model.AuthSession{}, &model.SystemSetting{}, &model.Resource{}, &model.Asset{}, &model.CanvasProject{}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []any{
		&model.User{ID: "owner", Username: "owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
		&model.AuthSession{ID: "session", UserID: "owner", TokenHash: auth.HashToken("repair-token"), ExpiresAt: time.Now().Add(time.Hour)},
		&model.Resource{ID: "new", UserID: "owner", Status: model.ResourceStatusReady},
		&model.Resource{ID: "foreign", UserID: "other", Status: model.ResourceStatusFailed},
		&model.Resource{ID: "pending", UserID: "owner", Status: model.ResourceStatusPending},
		&model.Asset{ID: "asset", UserID: "owner", PayloadJSON: `{"storageKey":"resource:old"}`},
		&model.CanvasProject{ID: "canvas", UserID: "owner", PayloadJSON: `{"nodes":[{"type":"image","metadata":{"assetId":"asset","storageKey":"resource:old"}}]}`},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := service.New(repository.New(db), t.TempDir())
	previous := runtimeService
	runtimeService = svc
	t.Cleanup(func() { runtimeService = previous; _ = svc.Close() })
	router := gin.New()
	RegisterUserDataRoutes(router.Group("/api"), svc)
	request := func(id, body string, authenticated bool, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/resources/"+id+"/repair-references", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if authenticated {
			req.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: "session.repair-token"})
		}
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("repair %s: status=%d want=%d body=%s", id, res.Code, want, res.Body.String())
		}
		return res
	}
	const body = `{"replacementResourceId":"new"}`
	request("old", body, false, http.StatusUnauthorized)
	request("old", `{`, true, http.StatusBadRequest)
	request("old", `{}`, true, http.StatusBadRequest)
	request("new", body, true, http.StatusBadRequest)
	request("old", `{"replacementResourceId":"missing"}`, true, http.StatusNotFound)
	request("foreign", body, true, http.StatusForbidden)
	request("pending", body, true, http.StatusConflict)
	for range 2 {
		res := request("old", body, true, http.StatusOK)
		var envelope struct {
			Code int `json:"code"`
			Data struct {
				Repaired bool `json:"repaired"`
			} `json:"data"`
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &envelope); err != nil || envelope.Code != 0 || !envelope.Data.Repaired || envelope.Msg != "ok" {
			t.Fatalf("invalid envelope: %s, %v", res.Body.String(), err)
		}
		if res.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("repair response should not be cached")
		}
	}
	for _, value := range []any{&model.Asset{}, &model.CanvasProject{}} {
		var payload struct{ PayloadJSON string }
		if err := db.Model(value).Where("user_id = ?", "owner").Select("payload_json").Take(&payload).Error; err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(payload.PayloadJSON, "resource:new") || strings.Contains(payload.PayloadJSON, "resource:old") {
			t.Fatalf("endpoint did not repair owned document: %s", payload.PayloadJSON)
		}
	}
	request("frontend-only", body, true, http.StatusOK)
	policy, err := svc.RuntimePolicy()
	if err != nil {
		t.Fatal(err)
	}
	for range policy.Request.AssetWritePerMinute {
		if _, err := svc.AllowRequest(context.Background(), "resources-repair:owner", policy.Request.AssetWritePerMinute, time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	limited := request("old", body, true, http.StatusTooManyRequests)
	if limited.Header().Get("Retry-After") == "" || !strings.Contains(limited.Body.String(), `"reason":"rate_limited"`) {
		t.Fatalf("rate limit response missing retry contract: %s", limited.Body.String())
	}
}
