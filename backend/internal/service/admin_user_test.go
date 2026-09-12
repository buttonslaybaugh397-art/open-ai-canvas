package service

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func newAdminRenameTestService(t *testing.T) (*Service, *gorm.DB, *model.User, *model.User) {
	t.Helper()
	db := newBulkUserTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	actor := &model.User{ID: "admin-1", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	user := &model.User{ID: "user-1", Username: "original-user", DisplayName: "Original User", Email: "original@example.com", Role: model.UserRoleUser, Status: model.UserStatusActive, PasswordHash: "unchanged-hash"}
	if err := db.Create(&[]model.User{*actor, *user}).Error; err != nil {
		t.Fatal(err)
	}
	return &Service{repo: repository.New(db)}, db, actor, user
}

func TestUpdateUserRenamePreservesAccountHistoryAndSession(t *testing.T) {
	svc, db, actor, original := newAdminRenameTestService(t)
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	account := model.CreditAccount{UserID: original.ID, AvailableMicrocredits: 75_000_000, ReservedMicrocredits: 5_000_000, Version: 9}
	ledger := model.CreditLedgerEntry{ID: "ledger-1", UserID: original.ID, Type: model.CreditLedgerConsume, AmountMicrocredits: 2_000_000}
	task := model.Task{ID: "task-1", UserID: original.ID, Status: model.TaskStatusSucceeded}
	session := model.AuthSession{ID: "session-1", UserID: original.ID, TokenHash: hashToken("session-token"), ExpiresAt: time.Now().Add(time.Hour)}
	for _, record := range []any{&account, &ledger, &task, &session} {
		if err := db.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	name := "  Renamed_User-9  "
	updated, err := svc.UpdateUser(actor, original.ID, UpdateUserRequest{Username: &name})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != original.ID || updated.Username != strings.TrimSpace(name) || updated.PasswordHash != original.PasswordHash || updated.Email != original.Email || updated.DisplayName != original.DisplayName || updated.Role != original.Role || updated.Status != original.Status {
		t.Fatalf("unexpected renamed account: %#v", updated)
	}
	current, err := svc.CurrentUser("session-1.session-token")
	if err != nil || current.Username != updated.Username || current.ID != original.ID {
		t.Fatalf("existing session lost account identity: user=%#v err=%v", current, err)
	}
	var storedAccount model.CreditAccount
	if err := db.First(&storedAccount, "user_id = ?", original.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedAccount.AvailableMicrocredits != account.AvailableMicrocredits || storedAccount.ReservedMicrocredits != account.ReservedMicrocredits || storedAccount.Version != account.Version {
		t.Fatalf("rename changed credits: %#v", storedAccount)
	}
	var storedLedger model.CreditLedgerEntry
	if err := db.First(&storedLedger, "id = ?", ledger.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedLedger.UserID != original.ID || storedLedger.AmountMicrocredits != ledger.AmountMicrocredits {
		t.Fatalf("rename changed ledger: %#v", storedLedger)
	}
	var storedTask model.Task
	if err := db.First(&storedTask, "id = ?", task.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storedTask.UserID != original.ID || storedTask.Status != task.Status {
		t.Fatalf("rename changed task: %#v", storedTask)
	}
	var audit model.AdminAuditEvent
	if err := db.First(&audit, "action = ? AND target_id = ?", "user.update", original.ID).Error; err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(audit.MetadataJSON), &metadata); err != nil {
		t.Fatal(err)
	}
	if audit.ActorUserID != actor.ID || metadata["previousUsername"] != original.Username || metadata["username"] != updated.Username {
		t.Fatalf("rename audit missing before/after: %#v", audit)
	}
}

func TestUpdateUserRenamedLoginAndEmailKeepWorking(t *testing.T) {
	svc, db, actor, user := newAdminRenameTestService(t)
	if err := db.AutoMigrate(&model.UserIdentity{}, &model.UserDailyActivity{}); err != nil {
		t.Fatal(err)
	}
	hash, err := hashPassword("test-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(user).Update("password_hash", hash).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.ensureSignupBonus(user.ID); err != nil {
		t.Fatal(err)
	}
	name := "new-login"
	if _, err := svc.UpdateUser(actor, user.ID, UpdateUserRequest{Username: &name}); err != nil {
		t.Fatal(err)
	}
	for _, account := range []string{"NEW-LOGIN", user.Email} {
		result, err := svc.Login(LoginRequest{Username: account, Password: "test-password-123"})
		if err != nil {
			t.Fatalf("login with %q failed: %v", account, err)
		}
		if result.User.ID != user.ID || result.User.Username != name {
			t.Fatalf("login returned wrong account: %#v", result.User)
		}
	}
	_, err = svc.Login(LoginRequest{Username: user.Username, Password: "test-password-123"})
	assertAdminRenameStatus(t, err, http.StatusUnauthorized)
}

func TestUpdateUserUsernameValidation(t *testing.T) {
	for _, name := range []string{"", "   ", "ab", "has space", "has@sign", "中文名称", strings.Repeat("a", 33)} {
		t.Run(name, func(t *testing.T) {
			svc, _, actor, user := newAdminRenameTestService(t)
			_, err := svc.UpdateUser(actor, user.ID, UpdateUserRequest{Username: &name})
			assertAdminRenameStatus(t, err, http.StatusBadRequest)
			stored, err := svc.repo.User(user.ID)
			if err != nil || stored.Username != user.Username {
				t.Fatalf("invalid rename changed account: user=%#v err=%v", stored, err)
			}
		})
	}
}

func TestUpdateUserUsernameOptionalAndCaseInsensitive(t *testing.T) {
	svc, _, actor, user := newAdminRenameTestService(t)
	updated, err := svc.UpdateUser(actor, user.ID, UpdateUserRequest{DisplayName: "Updated Name"})
	if err != nil || updated.Username != user.Username {
		t.Fatalf("omitted username changed login: user=%#v err=%v", updated, err)
	}
	for _, name := range []string{user.Username, "ORIGINAL-USER", strings.Repeat("a", 32)} {
		updated, err = svc.UpdateUser(actor, user.ID, UpdateUserRequest{Username: &name})
		if err != nil || updated.Username != name {
			t.Fatalf("valid rename %q failed: user=%#v err=%v", name, updated, err)
		}
	}
	name := "ADMIN"
	_, err = svc.UpdateUser(actor, user.ID, UpdateUserRequest{Username: &name})
	assertAdminRenameStatus(t, err, http.StatusConflict)
}

func TestUpdateUserUsernameRequiresAdminAndExistingTarget(t *testing.T) {
	svc, _, actor, user := newAdminRenameTestService(t)
	name := "new-login"
	_, err := svc.UpdateUser(nil, user.ID, UpdateUserRequest{Username: &name})
	assertAdminRenameStatus(t, err, http.StatusUnauthorized)
	_, err = svc.UpdateUser(user, user.ID, UpdateUserRequest{Username: &name})
	assertAdminRenameStatus(t, err, http.StatusForbidden)
	_, err = svc.UpdateUser(actor, "missing", UpdateUserRequest{Username: &name})
	assertAdminRenameStatus(t, err, http.StatusNotFound)
	updated, err := svc.UpdateUser(actor, actor.ID, UpdateUserRequest{Username: &name})
	if err != nil || updated.ID != actor.ID || updated.Role != model.UserRoleAdmin || updated.Username != name {
		t.Fatalf("admin self rename failed: user=%#v err=%v", updated, err)
	}
}

func TestUpdateUserRenameAndPasswordRollbackOnAuditFailure(t *testing.T) {
	svc, db, actor, user := newAdminRenameTestService(t)
	if err := db.Create(&model.AuthSession{ID: "session-1", UserID: user.ID}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TRIGGER reject_rename_audit BEFORE INSERT ON admin_audit_events BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END").Error; err != nil {
		t.Fatal(err)
	}
	name := "new-login"
	_, err := svc.UpdateUser(actor, user.ID, UpdateUserRequest{Username: &name, Password: "new-password-123"})
	if err == nil {
		t.Fatal("expected audit failure")
	}
	stored, err := svc.repo.User(user.ID)
	if err != nil || stored.Username != user.Username || stored.PasswordHash != user.PasswordHash {
		t.Fatalf("failed audit did not roll back user: user=%#v err=%v", stored, err)
	}
	var count int64
	if err := db.Model(&model.AuthSession{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("failed audit removed session: count=%d err=%v", count, err)
	}
	if err := db.Exec("DROP TRIGGER reject_rename_audit").Error; err != nil {
		t.Fatal(err)
	}
	updated, err := svc.UpdateUser(actor, user.ID, UpdateUserRequest{Username: &name, Password: "new-password-123"})
	if err != nil || updated.Username != name || !verifyPassword("new-password-123", updated.PasswordHash) {
		t.Fatalf("rename with password failed: user=%#v err=%v", updated, err)
	}
	if err := db.Model(&model.AuthSession{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("password update did not revoke sessions: count=%d err=%v", count, err)
	}
}

func assertAdminRenameStatus(t *testing.T, err error, status int) {
	t.Helper()
	var appErr *AppError
	if !errors.As(err, &appErr) || appErr.Status != status {
		t.Fatalf("error=%v, want HTTP %d", err, status)
	}
}
