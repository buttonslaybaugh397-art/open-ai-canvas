package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/kernel"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newProfileTestService(t *testing.T) (*Service, *gorm.DB, *model.User) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.UserIdentity{}, &model.AuthSession{}, &model.AdminAuditEvent{}, &model.CreditAccount{}, &model.CreditLedgerEntry{}, &model.Task{}); err != nil {
		t.Fatal(err)
	}
	hash, err := HashPassword("profile-test-password")
	if err != nil {
		t.Fatal(err)
	}
	user := &model.User{ID: "profile-user", Username: "original-user", DisplayName: "Original User", Email: "profile@example.com", Role: model.UserRoleUser, Status: model.UserStatusActive, PasswordHash: hash}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	return New(repository.New(db), nil, nil), db, user
}

func TestUpdateOwnDisplayNamePreservesIdentityAndHistory(t *testing.T) {
	svc, db, user := newProfileTestService(t)
	account := model.CreditAccount{UserID: user.ID, AvailableMicrocredits: 75_000_000, ReservedMicrocredits: 5_000_000, Version: 9}
	ledger := model.CreditLedgerEntry{ID: "ledger", UserID: user.ID, Type: model.CreditLedgerConsume, AmountMicrocredits: 2_000_000}
	task := model.Task{ID: "task", UserID: user.ID, Status: model.TaskStatusSucceeded}
	identity := model.UserIdentity{ID: "identity", UserID: user.ID, Provider: "linuxdo", Subject: "test-subject", ProviderUsername: "provider-name", AvatarURL: "https://example.com/avatar.png"}
	for _, record := range []any{&account, &ledger, &task, &identity} {
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	session, err := svc.createAuthSession(user)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateOwnDisplayName(user, UpdateOwnDisplayNameRequest{DisplayName: "  新的显示名称  "})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != user.ID || updated.Username != user.Username || updated.PasswordHash != user.PasswordHash || updated.Role != user.Role || updated.Status != user.Status || updated.Email != user.Email || updated.DisplayName != "新的显示名称" || updated.IdentityID != identity.Subject || updated.IdentityUsername != identity.ProviderUsername || updated.AvatarURL != identity.AvatarURL {
		t.Fatalf("rename changed unrelated profile fields: %#v", updated)
	}
	current, err := svc.CurrentUser(session.Session)
	if err != nil || current.ID != user.ID || current.Username != user.Username || current.DisplayName != updated.DisplayName {
		t.Fatalf("existing session: %#v %v", current, err)
	}
	for _, name := range []string{user.Username, user.Email} {
		login, err := svc.Login(LoginRequest{Username: name, Password: "profile-test-password"})
		if err != nil || login.User.ID != user.ID || login.User.Username != user.Username || login.User.DisplayName != updated.DisplayName {
			t.Fatalf("unchanged username/email login: %#v %v", login, err)
		}
	}
	_, err = svc.Login(LoginRequest{Username: updated.DisplayName, Password: "profile-test-password"})
	assertProfileStatus(t, err, http.StatusUnauthorized)
	var storedAccount model.CreditAccount
	var storedLedger model.CreditLedgerEntry
	var storedTask model.Task
	if err := db.First(&storedAccount, "user_id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedLedger, "id = ?", ledger.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedAccount.AvailableMicrocredits != account.AvailableMicrocredits || storedAccount.ReservedMicrocredits != account.ReservedMicrocredits || storedAccount.Version != account.Version || storedLedger.UserID != user.ID || storedLedger.AmountMicrocredits != ledger.AmountMicrocredits || storedTask.UserID != user.ID || storedTask.Status != task.Status {
		t.Fatal("rename changed credits or history")
	}
	var audit model.AdminAuditEvent
	if err := db.First(&audit).Error; err != nil {
		t.Fatal(err)
	}
	var metadata map[string]string
	if err := json.Unmarshal([]byte(audit.MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if audit.ActorUserID != user.ID || audit.TargetID != user.ID || audit.Action != "user.display_name.update_self" || metadata["previousDisplayName"] != user.DisplayName || metadata["displayName"] != updated.DisplayName {
		t.Fatalf("unexpected audit: %#v", audit)
	}
}

func TestUpdateOwnDisplayNameValidationAndAccess(t *testing.T) {
	svc, db, user := newProfileTestService(t)
	for _, name := range []string{"", " \t ", strings.Repeat("名", 41), "line\nbreak", "control\x00name", "line\u2028break", string([]byte{0xff})} {
		_, err := svc.UpdateOwnDisplayName(user, UpdateOwnDisplayNameRequest{DisplayName: name})
		assertProfileStatus(t, err, http.StatusBadRequest)
	}
	for _, actor := range []*model.User{nil, {}, {ID: "missing"}} {
		_, err := svc.UpdateOwnDisplayName(actor, UpdateOwnDisplayNameRequest{DisplayName: "新名字"})
		assertProfileStatus(t, err, http.StatusUnauthorized)
	}
	other := model.User{ID: "other", Username: "occupied", DisplayName: "同名用户", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	if err := db.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{user.DisplayName, "同名用户", "小", "中文 Name @", strings.Repeat("名", 40), strings.Repeat("\U0001F3AC", 40)} {
		updated, err := svc.UpdateOwnDisplayName(user, UpdateOwnDisplayNameRequest{DisplayName: name})
		if err != nil || updated.DisplayName != name || updated.Username != user.Username {
			t.Fatalf("valid rename: %#v %v", updated, err)
		}
		*user = updated.User
	}
	stale := *user
	stale.DisplayName = "stale-name"
	_, err := svc.UpdateOwnDisplayName(&stale, UpdateOwnDisplayNameRequest{DisplayName: "新名字"})
	assertProfileStatus(t, err, http.StatusConflict)
	if err := db.Model(user).Update("status", model.UserStatusDisabled).Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.UpdateOwnDisplayName(user, UpdateOwnDisplayNameRequest{DisplayName: "新名字"})
	assertProfileStatus(t, err, http.StatusForbidden)
}

func TestUpdateOwnDisplayNameRollsBackAndRejectsStaleWrites(t *testing.T) {
	svc, db, user := newProfileTestService(t)
	if err := db.Exec("CREATE TRIGGER reject_self_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateOwnDisplayName(user, UpdateOwnDisplayNameRequest{DisplayName: "新名字"}); err == nil {
		t.Fatal("expected audit failure")
	}
	stored, err := svc.repo.User(user.ID)
	if err != nil || stored.Username != user.Username || stored.DisplayName != user.DisplayName {
		t.Fatalf("failed rename persisted: %#v %v", stored, err)
	}
	if err := db.Exec("DROP TRIGGER reject_self_audit").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.User{}).Where("id = ?", user.ID).Updates(map[string]any{"role": model.UserRoleAdmin, "username": "admin-renamed", "password_hash": "new-hash"}).Error; err != nil {
		t.Fatal(err)
	}
	updated, err := svc.repo.UpdateOwnDisplayName(user.ID, user.DisplayName, "新名字", time.Now(), &model.AdminAuditEvent{ID: "rename"})
	if err != nil || updated.Role != model.UserRoleAdmin || updated.DisplayName != "新名字" || updated.Username != "admin-renamed" || updated.PasswordHash != "new-hash" {
		t.Fatalf("self rename overwrote concurrent profile change: %#v %v", updated, err)
	}
	_, err = svc.repo.UpdateOwnDisplayName(user.ID, user.DisplayName, "过时名字", time.Now(), &model.AdminAuditEvent{ID: "stale"})
	if !errors.Is(err, repository.ErrUserChanged) {
		t.Fatalf("stale rename: %v", err)
	}
	if err := db.Model(user).Update("status", model.UserStatusDisabled).Error; err != nil {
		t.Fatal(err)
	}
	_, err = svc.repo.UpdateOwnDisplayName(user.ID, "新名字", "停用名字", time.Now(), &model.AdminAuditEvent{ID: "disabled"})
	if !errors.Is(err, repository.ErrUserChanged) {
		t.Fatalf("disabled rename: %v", err)
	}
	var count int64
	if err := db.Model(&model.AdminAuditEvent{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("audit count=%d err=%v", count, err)
	}
}

func assertProfileStatus(t *testing.T, err error, status int) {
	t.Helper()
	var appErr *kernel.AppError
	if !errors.As(err, &appErr) || appErr.Status != status {
		t.Fatalf("error=%v want HTTP %d", err, status)
	}
}
