package repository

import (
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProviderRecoveryLeaseHandoffAndRestart(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+newRepositoryID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}, &model.TaskTextDelta{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db)
	task := &model.Task{
		ID: "recovery", UserID: "owner", Status: model.TaskStatusFailed, Type: "canvas_video",
		ProviderRequestID: "existing-upstream", BillingOrderID: "original-order",
		InputJSON: `{"config":{"secret":"retained"}}`, Error: "timeout", CompletedAt: ptrTime(time.Now()),
	}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimFailedTaskProviderRecovery(task.ID, "other-user", "manual-recovery:wrong", time.Minute); !errors.Is(err, ErrTaskProviderRecoveryConflict) {
		t.Fatalf("cross-user claim: %v", err)
	}
	task.LeaseOwner = "manual-recovery:one"
	if err := repo.ClaimFailedTaskProviderRecovery(task.ID, task.UserID, task.LeaseOwner, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := repo.ClaimFailedTaskProviderRecovery(task.ID, task.UserID, "manual-recovery:two", time.Minute); !errors.Is(err, ErrTaskProviderRecoveryConflict) {
		t.Fatalf("concurrent recovery: %v", err)
	}
	if _, err := repo.RetryTaskWithBilling(task.UserID, task, nil, 100); !errors.Is(err, ErrTaskNotRetryable) {
		t.Fatalf("retry bypassed the live manual recovery lease: %v", err)
	}
	stale := *task
	stale.LeaseOwner = "manual-recovery:stale"
	if _, err := repo.ResumeFailedTaskProviderPolling(&stale, "processing"); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("stale owner resume: %v", err)
	}
	resumed, err := repo.ResumeFailedTaskProviderPolling(task, "processing")
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Status != model.TaskStatusRunning || resumed.Error != "" || resumed.CompletedAt != nil ||
		resumed.ProviderRecoveryAt == nil || resumed.NextPollAt == nil || resumed.LeaseOwner != "" ||
		resumed.InputJSON != task.InputJSON || resumed.BillingOrderID != task.BillingOrderID {
		t.Fatalf("invalid handoff: %+v", resumed)
	}
	if _, err := repo.ResumeFailedTaskProviderPolling(task, "processing"); !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("duplicate resume: %v", err)
	}
	if claimed, err := repo.ClaimNextTask("too-early", time.Minute); err != nil || claimed != nil {
		t.Fatalf("early claim: %+v, %v", claimed, err)
	}
	if err := db.Model(task).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	// A new repository/worker can resume solely from durable state.
	claimed, err := New(db).ClaimNextTask("restarted-worker", time.Minute)
	if err != nil || claimed == nil {
		t.Fatalf("restart claim: %+v, %v", claimed, err)
	}
	if claimed.ProviderRequestID != task.ProviderRequestID || !claimed.ProviderRecoveryAt.Equal(*resumed.ProviderRecoveryAt) {
		t.Fatalf("restart lost original provider identity: %+v", claimed)
	}
	if err := repo.ReleaseTaskProviderRecovery(task.ID, task.LeaseOwner); err != nil {
		t.Fatal(err)
	}
	stored, _ := repo.Task(task.ID)
	if stored.LeaseOwner != "restarted-worker" {
		t.Fatal("old manual release removed the worker lease")
	}
	// A deliberate new submission, unlike worker recovery, clears the marker.
	if err := db.Model(task).Updates(map[string]any{"status": model.TaskStatusFailed, "lease_owner": "", "lease_expires_at": nil}).Error; err != nil {
		t.Fatal(err)
	}
	retried, err := repo.RetryTaskWithBilling(task.UserID, task, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if retried.ProviderRecoveryAt != nil || retried.ProviderRequestID != "" || retried.Status != model.TaskStatusQueued {
		t.Fatalf("new submission inherited recovery identity: %+v", retried)
	}
}
