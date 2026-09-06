package service

import (
	"context"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCancelTaskCancelsRunningTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now()
	task := model.Task{
		ID:        "running-task",
		UserID:    "user-1",
		Status:    model.TaskStatusRunning,
		Stage:     "调用生成模型",
		StartedAt: &startedAt,
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	svc := &Service{repo: repository.New(db)}
	cancelled, err := svc.CancelTask(context.Background(), task.UserID, task.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if cancelled.Status != model.TaskStatusCancelled {
		t.Fatalf("CancelTask() status = %s, want cancelled", cancelled.Status)
	}

	stored, err := svc.repo.TaskForUser(task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusCancelled || stored.CompletedAt == nil {
		t.Fatalf("running task was not cancelled: status=%s completedAt=%v", stored.Status, stored.CompletedAt)
	}
}

func TestCancelTaskCancelsQueuedTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "queued-task", UserID: "user-1", Status: model.TaskStatusQueued, Stage: "等待队列调度"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	svc := &Service{repo: repository.New(db)}
	cancelled, err := svc.CancelTask(context.Background(), task.UserID, task.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if cancelled.Status != model.TaskStatusCancelled {
		t.Fatalf("CancelTask() status = %s, want cancelled", cancelled.Status)
	}

	storedTask, err := svc.repo.TaskForUser(task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusCancelled || storedTask.CompletedAt == nil {
		t.Fatalf("queued task was not cancelled: status=%s completedAt=%v", storedTask.Status, storedTask.CompletedAt)
	}
}

func TestCancelTaskCancelsPreparingTask(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	task := model.Task{ID: "preparing-task", UserID: "user-1", Status: model.TaskStatusPreparing, Stage: "正在准备生成输入"}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	svc := &Service{repo: repository.New(db)}
	cancelled, err := svc.CancelTask(context.Background(), task.UserID, task.ID)
	if err != nil {
		t.Fatalf("CancelTask() error = %v", err)
	}
	if cancelled.Status != model.TaskStatusCancelled {
		t.Fatalf("CancelTask() status = %s, want cancelled", cancelled.Status)
	}

	storedTask, err := svc.repo.TaskForUser(task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if storedTask.Status != model.TaskStatusCancelled || storedTask.CompletedAt == nil {
		t.Fatalf("preparing task was not cancelled: status=%s completedAt=%v", storedTask.Status, storedTask.CompletedAt)
	}
}

func TestCleanupExpiredPreparingTasksCancelsOnlyStalePreparingTasks(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	stale := model.Task{ID: "stale-preparing", UserID: "user-1", Status: model.TaskStatusPreparing, Stage: "正在准备生成输入", UpdatedAt: now.Add(-taskPreparationTimeout - time.Minute)}
	fresh := model.Task{ID: "fresh-preparing", UserID: "user-1", Status: model.TaskStatusPreparing, Stage: "正在准备生成输入", UpdatedAt: now.Add(-taskPreparationTimeout + time.Minute)}
	queued := model.Task{ID: "stale-queued", UserID: "user-1", Status: model.TaskStatusQueued, Stage: "等待队列调度", UpdatedAt: now.Add(-taskPreparationTimeout - time.Minute)}
	if err := db.Create(&[]model.Task{stale, fresh, queued}).Error; err != nil {
		t.Fatal(err)
	}

	svc := &Service{repo: repository.New(db)}
	if err := svc.cleanupExpiredPreparingTasks(now); err != nil {
		t.Fatalf("cleanupExpiredPreparingTasks() error = %v", err)
	}

	for _, test := range []struct {
		id   string
		want model.TaskStatus
	}{
		{id: stale.ID, want: model.TaskStatusCancelled},
		{id: fresh.ID, want: model.TaskStatusPreparing},
		{id: queued.ID, want: model.TaskStatusQueued},
	} {
		task, err := svc.repo.Task(test.id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status != test.want {
			t.Fatalf("task %s status = %s, want %s", test.id, task.Status, test.want)
		}
	}
}

func TestCancelTaskRejectsTaskWithProviderRequestID(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	task := model.Task{
		ID:                "submitted-task",
		UserID:            "user-1",
		Status:            model.TaskStatusFailed,
		ProviderRequestID: "provider-request-1",
	}
	if err := db.Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	svc := &Service{repo: repository.New(db)}
	if _, err := svc.CancelTask(context.Background(), task.UserID, task.ID); err == nil || err.Error() != "任务当前状态为 failed，无法取消" {
		t.Fatalf("CancelTask() error = %v", err)
	}

	stored, err := svc.repo.TaskForUser(task.UserID, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != model.TaskStatusFailed || stored.ProviderRequestID != task.ProviderRequestID {
		t.Fatalf("submitted task changed after cancellation attempt: status=%s providerRequestId=%q", stored.Status, stored.ProviderRequestID)
	}
}
