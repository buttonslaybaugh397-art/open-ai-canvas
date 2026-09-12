package handler

import (
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
)

func TestOwnDisplayNameRoute(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body          string
		anonymous, disabled bool
		status              int
	}{
		{name: "anonymous", body: `{"displayName":"新的名称"}`, anonymous: true, status: 401},
		{name: "disabled", body: `{"displayName":"新的名称"}`, disabled: true, status: 403},
		{name: "own rename", body: `{"displayName":"新的名称"}`, status: 200},
		{name: "missing", body: `{}`, status: 400},
		{name: "invalid", body: `{"displayName":" "}`, status: 400},
		{name: "wrong type", body: `{"displayName":123}`, status: 400},
		{name: "forged target", body: `{"displayName":"新的名称","id":"admin"}`, status: 400},
		{name: "forged role", body: `{"displayName":"新的名称","role":"admin"}`, status: 400},
		{name: "forged password", body: `{"displayName":"新的名称","password":"injected"}`, status: 400},
		{name: "forged login name", body: `{"displayName":"新的名称","username":"changed-login"}`, status: 400},
		{name: "extra JSON", body: `{"displayName":"新的名称"}{}`, status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			t.Cleanup(func() { _ = sqlDB.Close() })
			if err := db.AutoMigrate(&model.User{}, &model.UserIdentity{}, &model.AuthSession{}, &model.AdminAuditEvent{}); err != nil {
				t.Fatal(err)
			}
			user := model.User{ID: "user", Username: "original", DisplayName: "Display", Role: model.UserRoleUser, Status: model.UserStatusActive, PasswordHash: "private-hash"}
			if tc.disabled {
				user.Status = model.UserStatusDisabled
			}
			admin := model.User{ID: "admin", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
			if err := db.Create(&[]model.User{user, admin}).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.AuthSession{ID: "session", UserID: user.ID, TokenHash: auth.HashToken("test-token"), ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
				t.Fatal(err)
			}
			svc := service.New(repository.New(db), t.TempDir())
			previous := runtimeService
			runtimeService = svc
			t.Cleanup(func() { runtimeService = previous })
			router := gin.New()
			RegisterAuthRoutes(router.Group("/api"), svc)
			req := httptest.NewRequest(http.MethodPatch, "/api/auth/display-name", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if !tc.anonymous {
				req.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: "session.test-token"})
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != tc.status {
				t.Fatalf("status=%d want=%d response=%s", res.Code, tc.status, res.Body.String())
			}
			if res.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("response must not be cached")
			}
			if strings.Contains(res.Body.String(), "private-hash") || strings.Contains(res.Body.String(), "passwordHash") {
				t.Fatal("secret leaked")
			}
			if len(res.Result().Cookies()) != 0 {
				t.Fatal("rename must not replace session")
			}
			var stored model.User
			if err := db.First(&stored, "id = ?", user.ID).Error; err != nil {
				t.Fatal(err)
			}
			wantDisplayName := user.DisplayName
			if tc.status == 200 {
				wantDisplayName = "新的名称"
			}
			if stored.Username != user.Username || stored.Role != user.Role || stored.PasswordHash != user.PasswordHash || stored.DisplayName != wantDisplayName {
				t.Fatalf("unexpected stored user: %#v", stored)
			}
			other, err := repository.New(db).User(admin.ID)
			if err != nil || other.Username != admin.Username || other.Role != admin.Role || other.DisplayName != admin.DisplayName {
				t.Fatalf("other account changed: %#v %v", other, err)
			}
			if tc.status == 200 {
				var payload struct {
					Code int `json:"code"`
					Data struct {
						User service.AuthUser `json:"user"`
					} `json:"data"`
				}
				if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
					t.Fatal(err)
				}
				if payload.Code != 0 || payload.Data.User.ID != user.ID || payload.Data.User.Username != user.Username || payload.Data.User.DisplayName != wantDisplayName {
					t.Fatalf("unexpected envelope: %s", res.Body.String())
				}
				for attempt := 2; attempt <= 11; attempt++ {
					repeat := httptest.NewRequest(http.MethodPatch, "/api/auth/display-name", strings.NewReader(tc.body))
					repeat.Header.Set("Content-Type", "application/json")
					repeat.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: "session.test-token"})
					response := httptest.NewRecorder()
					router.ServeHTTP(response, repeat)
					wantStatus := http.StatusOK
					if attempt == 11 {
						wantStatus = http.StatusTooManyRequests
						if response.Header().Get("Retry-After") == "" {
							t.Fatal("rate limit missing retry delay")
						}
					}
					if response.Code != wantStatus {
						t.Fatalf("attempt %d: status=%d want=%d", attempt, response.Code, wantStatus)
					}
				}
				var auditCount int64
				if err := db.Model(&model.AdminAuditEvent{}).Count(&auditCount).Error; err != nil || auditCount != 1 {
					t.Fatalf("unchanged display name must not duplicate audit: count=%d err=%v", auditCount, err)
				}
				oldRoute := httptest.NewRecorder()
				router.ServeHTTP(oldRoute, httptest.NewRequest(http.MethodPatch, "/api/auth/username", strings.NewReader(`{"username":"changed-login"}`)))
				if oldRoute.Code != http.StatusNotFound {
					t.Fatal("self-service login rename must not be exposed")
				}
			}
		})
	}
}
