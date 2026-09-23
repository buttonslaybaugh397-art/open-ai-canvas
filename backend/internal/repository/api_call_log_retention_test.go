package repository

import (
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openAPICallLogRetentionDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:api-call-log-retention-"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.Asset{},
		&model.CanvasProject{},
		&model.Task{},
		&model.TaskLog{},
		&model.Result{},
		&model.TaskTextDelta{},
		&model.ApiCallLog{},
		&model.BillingOrder{},
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCreateAPICallLogWithRetentionKeepsLatestLogsAndScopesByUser(t *testing.T) {
	db := openAPICallLogRetentionDB(t)
	repo := New(db)
	base := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	for index, id := range []string{"old-1", "old-2", "old-3"} {
		if err := db.Create(&model.ApiCallLog{ID: id, UserID: "user-1", CreatedAt: base.Add(time.Duration(index) * time.Minute)}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Create(&model.ApiCallLog{ID: "other-user", UserID: "user-2", CreatedAt: base}).Error; err != nil {
		t.Fatal(err)
	}

	incoming := model.ApiCallLog{ID: "new-1", UserID: "user-1", CreatedAt: base.Add(4 * time.Minute)}
	if err := repo.CreateAPICallLogWithRetention(&incoming, 3, 0, 0); err != nil {
		t.Fatal(err)
	}

	var logs []model.ApiCallLog
	if err := db.Where("user_id = ?", "user-1").Order("created_at asc").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 3 || logs[0].ID != "old-2" || logs[1].ID != "old-3" || logs[2].ID != "new-1" {
		t.Fatalf("retained logs = %#v, want old-2, old-3, new-1", logs)
	}
	var otherCount int64
	if err := db.Model(&model.ApiCallLog{}).Where("user_id = ?", "user-2").Count(&otherCount).Error; err != nil {
		t.Fatal(err)
	}
	if otherCount != 1 {
		t.Fatalf("other user log count = %d, want 1", otherCount)
	}
}

func TestCreateAPICallLogWithRetentionProtectsActiveAndBillingRecords(t *testing.T) {
	db := openAPICallLogRetentionDB(t)
	repo := New(db)
	now := time.Date(2026, 9, 23, 13, 0, 0, 0, time.UTC)
	tasks := []model.Task{
		{ID: "running-task", UserID: "user-1", Status: model.TaskStatusRunning, BillingOrderID: "running-order"},
		{ID: "replay-task", UserID: "user-1", Status: model.TaskStatusTextReplay},
		{ID: "done-task", UserID: "user-1", Status: model.TaskStatusSucceeded},
	}
	if err := db.Create(&tasks).Error; err != nil {
		t.Fatal(err)
	}
	orders := []model.BillingOrder{
		{ID: "running-order", UserID: "user-1", IdempotencyKey: "running", TaskID: "running-task", Status: model.BillingStatusRunning},
		{ID: "uncertain-order", UserID: "user-1", IdempotencyKey: "uncertain", Status: model.BillingStatusUncertain},
		{ID: "settled-order", UserID: "user-1", IdempotencyKey: "settled", Status: model.BillingStatusSettled},
	}
	if err := db.Create(&orders).Error; err != nil {
		t.Fatal(err)
	}
	logs := []model.ApiCallLog{
		{ID: "safe-old", UserID: "user-1", CreatedAt: now},
		{ID: "active", UserID: "user-1", TaskID: "running-task", BillingOrderID: "running-order", CreatedAt: now.Add(time.Minute)},
		{ID: "replay", UserID: "user-1", TaskID: "replay-task", CreatedAt: now.Add(2 * time.Minute)},
		{ID: "uncertain", UserID: "user-1", BillingOrderID: "uncertain-order", CreatedAt: now.Add(3 * time.Minute)},
		{ID: "done", UserID: "user-1", TaskID: "done-task", BillingOrderID: "settled-order", CreatedAt: now.Add(4 * time.Minute)},
	}
	if err := db.Create(&logs).Error; err != nil {
		t.Fatal(err)
	}

	incoming := model.ApiCallLog{ID: "new-1", UserID: "user-1", CreatedAt: now.Add(5 * time.Minute)}
	if err := repo.CreateAPICallLogWithRetention(&incoming, 2, 0, 0); err != nil {
		t.Fatal(err)
	}

	var retained []model.ApiCallLog
	if err := db.Where("user_id = ?", "user-1").Find(&retained).Error; err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]bool, len(retained))
	for _, log := range retained {
		ids[log.ID] = true
	}
	for _, id := range []string{"active", "replay", "uncertain", "new-1"} {
		if !ids[id] {
			t.Fatalf("protected log %q was pruned; retained ids = %#v", id, ids)
		}
	}
	if ids["safe-old"] || ids["done"] {
		t.Fatalf("completed logs should be eligible for pruning; retained ids = %#v", ids)
	}
}

func TestCreateAPICallLogWithRetentionRollsBackCleanupWhenTaskDataQuotaFails(t *testing.T) {
	db := openAPICallLogRetentionDB(t)
	repo := New(db)
	oldLogs := []model.ApiCallLog{
		{ID: "old-1", UserID: "user-1", Path: "old1", CreatedAt: time.Now().Add(-2 * time.Minute)},
		{ID: "old-2", UserID: "user-1", Path: "old2", CreatedAt: time.Now().Add(-time.Minute)},
	}
	if err := db.Create(&oldLogs).Error; err != nil {
		t.Fatal(err)
	}

	incoming := model.ApiCallLog{ID: "new-1", UserID: "user-1", CreatedAt: time.Now()}
	err := repo.CreateAPICallLogWithRetention(&incoming, 2, 4, 1)
	if !errors.Is(err, ErrTaskDataQuotaExceeded) {
		t.Fatalf("CreateAPICallLogWithRetention() error = %v, want ErrTaskDataQuotaExceeded", err)
	}
	var count int64
	if err := db.Model(&model.ApiCallLog{}).Where("user_id = ?", "user-1").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("log count after rollback = %d, want 2", count)
	}
	for _, id := range []string{"old-1", "old-2"} {
		var stored model.ApiCallLog
		if err := db.First(&stored, "id = ?", id).Error; err != nil {
			t.Fatalf("old log %s missing after rollback: %v", id, err)
		}
	}
}
