package app

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"golang.org/x/crypto/bcrypt"
)

func TestAdminPasswordResetPreservesAccountAndRevokesOnlyTargetSessions(t *testing.T) {
	svc, db, actor, user := newAdminRenameTestService(t)
	if err := db.AutoMigrate(&model.UserIdentity{}, &model.UserDailyActivity{}); err != nil {
		t.Fatal(err)
	}
	oldHash, err := hashPassword("old-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(user).Update("password_hash", oldHash).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.ensureSignupBonus(user.ID); err != nil {
		t.Fatal(err)
	}
	beforeAccount, err := svc.repo.CreditAccount(user.ID)
	if err != nil {
		t.Fatal(err)
	}
	sessions := []model.AuthSession{
		{ID: "target-1", UserID: user.ID, TokenHash: hashToken("first"), ExpiresAt: time.Now().Add(time.Hour)},
		{ID: "target-2", UserID: user.ID, TokenHash: hashToken("second"), ExpiresAt: time.Now().Add(time.Hour)},
		{ID: "actor-session", UserID: actor.ID, TokenHash: hashToken("actor"), ExpiresAt: time.Now().Add(time.Hour)},
	}
	if err := db.Create(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	const password = " new-password-123 "
	updated, err := svc.UpdateUser(actor, user.ID, UpdateUserRequest{Password: password})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != user.ID || updated.Username != user.Username || updated.Email != user.Email || updated.Role != user.Role || updated.Status != user.Status || updated.DisplayName != user.DisplayName {
		t.Fatalf("password-only change modified account identity: %#v", updated)
	}
	if bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte(password)) != nil || bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("old-password-123")) == nil {
		t.Fatal("password was not replaced exactly")
	}
	for _, cookie := range []string{"target-1.first", "target-2.second"} {
		_, err := svc.CurrentUser(cookie)
		assertAdminRenameStatus(t, err, http.StatusUnauthorized)
	}
	if _, err := svc.CurrentUser("actor-session.actor"); err != nil {
		t.Fatalf("reset revoked unrelated admin session: %v", err)
	}
	_, err = svc.Login(LoginRequest{Username: user.Username, Password: "old-password-123"})
	assertAdminRenameStatus(t, err, http.StatusUnauthorized)
	if _, err := svc.Login(LoginRequest{Username: user.Username, Password: password}); err != nil {
		t.Fatalf("new password login failed: %v", err)
	}
	afterAccount, err := svc.repo.CreditAccount(user.ID)
	if err != nil || afterAccount.AvailableMicrocredits != beforeAccount.AvailableMicrocredits || afterAccount.ReservedMicrocredits != beforeAccount.ReservedMicrocredits || afterAccount.Version != beforeAccount.Version {
		t.Fatalf("password reset changed credits: account=%#v err=%v", afterAccount, err)
	}
	var audit model.AdminAuditEvent
	if err := db.First(&audit, "action = ? AND target_id = ?", "user.update", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(audit.MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["passwordChanged"] != true || metadata["sessionsRevoked"] != true || audit.ActorUserID != actor.ID {
		t.Fatalf("missing safe password audit: %#v", audit)
	}
	for _, record := range []any{updated, audit} {
		encoded, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), password) || strings.Contains(string(encoded), updated.PasswordHash) {
			t.Fatal("password or hash leaked into public response or audit")
		}
	}
}

func TestAdminPasswordResetValidationAndPermissions(t *testing.T) {
	svc, db, actor, user := newAdminRenameTestService(t)
	if err := db.Create(&model.AuthSession{ID: "keep-session", UserID: user.ID}).Error; err != nil {
		t.Fatal(err)
	}
	for _, password := range []string{"short", strings.Repeat("a", 73), strings.Repeat("密", 25)} {
		_, err := svc.UpdateUser(actor, user.ID, UpdateUserRequest{Password: password})
		assertAdminRenameStatus(t, err, http.StatusBadRequest)
	}
	_, err := svc.UpdateUser(nil, user.ID, UpdateUserRequest{Password: "new-password"})
	assertAdminRenameStatus(t, err, http.StatusUnauthorized)
	_, err = svc.UpdateUser(user, user.ID, UpdateUserRequest{Password: "new-password"})
	assertAdminRenameStatus(t, err, http.StatusForbidden)
	_, err = svc.UpdateUser(user, actor.ID, UpdateUserRequest{Password: "new-password"})
	assertAdminRenameStatus(t, err, http.StatusForbidden)
	_, err = svc.UpdateUser(actor, "missing", UpdateUserRequest{Password: "new-password"})
	assertAdminRenameStatus(t, err, http.StatusNotFound)
	stored, err := svc.repo.User(user.ID)
	if err != nil || stored.PasswordHash != user.PasswordHash {
		t.Fatalf("rejected reset changed password: %v", err)
	}
	var count int64
	if err := db.Model(&model.AuthSession{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("rejected reset removed session: count=%d err=%v", count, err)
	}
	if _, err := svc.UpdateUser(actor, user.ID, UpdateUserRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.AuthSession{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("profile-only update removed session: count=%d err=%v", count, err)
	}
}

func TestAdminPasswordResetWithoutEmailKeepsDisabledStatus(t *testing.T) {
	svc, db, actor, user := newAdminRenameTestService(t)
	if err := db.Model(user).Updates(map[string]any{"email": "", "password_hash": "", "status": model.UserStatusDisabled}).Error; err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateUser(actor, user.ID, UpdateUserRequest{Password: "recovered-password"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Email != "" || updated.Status != model.UserStatusDisabled || bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("recovered-password")) != nil {
		t.Fatal("reset must work without email and must not enable disabled accounts")
	}
	_, err = svc.Login(LoginRequest{Username: user.Username, Password: "recovered-password"})
	assertAdminRenameStatus(t, err, http.StatusForbidden)
}

func TestPasswordValidationLimits(t *testing.T) {
	for _, password := range []string{"12345678", strings.Repeat("a", 72), strings.Repeat("密", 24)} {
		if err := validatePassword(password); err != nil {
			t.Fatalf("valid password rejected: %v", err)
		}
	}
}
