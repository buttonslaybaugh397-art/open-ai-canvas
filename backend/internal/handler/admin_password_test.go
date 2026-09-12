package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAdminPasswordRoutePermissionsAndSelfCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name       string
		actorID    string
		targetID   string
		wantStatus int
		clearSelf  bool
	}{
		{"anonymous", "", "user", http.StatusUnauthorized, false},
		{"regular user", "user", "admin", http.StatusForbidden, false},
		{"admin resets user", "admin", "user", http.StatusOK, false},
		{"admin resets self", "admin", "admin", http.StatusOK, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			t.Cleanup(func() { _ = sqlDB.Close() })
			if err := db.AutoMigrate(&model.User{}, &model.AuthSession{}, &model.AdminAuditEvent{}); err != nil {
				t.Fatal(err)
			}
			users := []model.User{
				{ID: "admin", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive},
				{ID: "user", Username: "user", Role: model.UserRoleUser, Status: model.UserStatusActive},
			}
			if err := db.Create(&users).Error; err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256([]byte("test-token"))
			for _, user := range users {
				if err := db.Create(&model.AuthSession{ID: user.ID + "-session", UserID: user.ID, TokenHash: hex.EncodeToString(sum[:]), ExpiresAt: time.Now().Add(time.Hour)}).Error; err != nil {
					t.Fatal(err)
				}
			}
			svc := service.New(repository.New(db), t.TempDir())
			router := gin.New()
			RegisterAdminRoutes(router.Group("/api"), svc)
			req := httptest.NewRequest(http.MethodPatch, "/api/admin/users/"+tc.targetID, strings.NewReader(`{"password":"test-new-password"}`))
			req.Header.Set("Content-Type", "application/json")
			if tc.actorID != "" {
				req.AddCookie(&http.Cookie{Name: service.SessionCookieName, Value: tc.actorID + "-session.test-token"})
			}
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != tc.wantStatus {
				t.Fatalf("status=%d want=%d response=%s", res.Code, tc.wantStatus, res.Body.String())
			}
			if strings.Contains(res.Body.String(), "test-new-password") || strings.Contains(res.Body.String(), "passwordHash") {
				t.Fatal("password leaked into response")
			}
			cleared := false
			for _, cookie := range res.Result().Cookies() {
				if cookie.Name == service.SessionCookieName && cookie.MaxAge < 0 {
					cleared = true
				}
			}
			if cleared != tc.clearSelf {
				t.Fatalf("cleared cookie=%v want=%v", cleared, tc.clearSelf)
			}
			if tc.wantStatus == http.StatusOK {
				if res.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("password reset response must not be cached")
				}
				var count int64
				if err := db.Model(&model.AuthSession{}).Where("user_id = ?", tc.targetID).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("target sessions remain: count=%d err=%v", count, err)
				}
			}
		})
	}
}
