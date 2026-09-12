package repository

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestUpdateAdminUserRechecksDuplicateAndStaleUsername(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.AuthSession{}, &model.AdminAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	users := []model.User{{ID: "one", Username: "first-user"}, {ID: "two", Username: "second-user"}}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	duplicate := users[0]
	duplicate.Username = "SECOND-USER"
	if err := repo.UpdateAdminUser(&duplicate, users[0].Username, false, &model.AdminAuditEvent{ID: "duplicate"}); !errors.Is(err, ErrUsernameExists) {
		t.Fatalf("duplicate error=%v, want ErrUsernameExists", err)
	}
	updated := users[0]
	updated.Username = "renamed-user"
	if err := repo.UpdateAdminUser(&updated, users[0].Username, false, &model.AdminAuditEvent{ID: "rename"}); err != nil {
		t.Fatal(err)
	}
	stale := users[0]
	stale.DisplayName = "Stale update"
	if err := repo.UpdateAdminUser(&stale, users[0].Username, false, &model.AdminAuditEvent{ID: "stale"}); !errors.Is(err, ErrUserChanged) {
		t.Fatalf("stale write error=%v, want ErrUserChanged", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := repo.RecordUserLogin(users[0].ID, now); err != nil {
		t.Fatal(err)
	}
	if err := repo.DisableUser(users[0].ID, now); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.User(users[0].ID)
	if err != nil || stored.Username != updated.Username || stored.DisplayName != updated.DisplayName || stored.LastLoginAt == nil || !stored.LastLoginAt.Equal(now) || stored.Status != model.UserStatusDisabled {
		t.Fatalf("unrelated write overwrote rename: user=%#v err=%v", stored, err)
	}
	var count int64
	if err := db.Model(&model.AdminAuditEvent{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("audit count=%d err=%v, want 1", count, err)
	}
}

func TestUpdateAdminUserPostgresConcurrentRename(t *testing.T) {
	dsn := os.Getenv("CANVAS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set CANVAS_TEST_POSTGRES_DSN to run isolated PostgreSQL rename coverage")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("CANVAS_TEST_POSTGRES_DSN must be a PostgreSQL URL")
	}
	base, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlBase, _ := base.DB()
	t.Cleanup(func() { _ = sqlBase.Close() })
	// Only this uniquely named test schema is created and removed; public data is never touched.
	schema := fmt.Sprintf("test_admin_rename_%d", time.Now().UnixNano())
	if err := base.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := base.Exec("DROP SCHEMA " + schema + " CASCADE").Error; err != nil {
			t.Error(err)
		}
	})
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.User{}, &model.AuthSession{}, &model.AdminAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	users := []model.User{{ID: "one", Username: "first-user"}, {ID: "two", Username: "second-user"}}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for i, user := range users {
		workers.Add(1)
		go func(i int, user model.User) {
			defer workers.Done()
			old := user.Username
			user.Username = []string{"Shared-Name", "shared-name"}[i]
			<-start
			results <- New(db).UpdateAdminUser(&user, old, false, &model.AdminAuditEvent{ID: user.ID})
		}(i, user)
	}
	close(start)
	workers.Wait()
	close(results)
	var successes, conflicts int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrUsernameExists):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent rename error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent rename successes=%d conflicts=%d", successes, conflicts)
	}
}
