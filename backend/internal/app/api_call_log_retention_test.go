package app

import (
	"encoding/json"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLogAPICallRetainsLatestLogsAfterConfiguredCount(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:app-api-call-log-retention?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(
		&model.SystemSetting{},
		&model.Asset{},
		&model.CanvasProject{},
		&model.Task{},
		&model.TaskLog{},
		&model.Result{},
		&model.TaskTextDelta{},
		&model.ApiCallLog{},
		&model.ModelPricing{},
		&model.BillingOrder{},
	); err != nil {
		t.Fatal(err)
	}
	policy := defaultRuntimePolicy()
	policy.Resource.APICallLogCount = 2
	encoded, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: "runtime_policy", ValueJSON: string(encoded)}).Error; err != nil {
		t.Fatal(err)
	}

	service := &Service{repo: repository.New(db)}
	base := time.Date(2026, 9, 23, 14, 0, 0, 0, time.UTC)
	for index, id := range []string{"old-1", "old-2", "new-1"} {
		if err := service.LogAPICall(model.ApiCallLog{
			ID: id, UserID: "user-1", Capability: "text", RequestKind: "create",
			CreatedAt: base.Add(time.Duration(index) * time.Minute),
		}); err != nil {
			t.Fatalf("LogAPICall(%s): %v", id, err)
		}
	}

	var logs []model.ApiCallLog
	if err := db.Where("user_id = ?", "user-1").Order("created_at asc").Find(&logs).Error; err != nil {
		t.Fatal(err)
	}
	if len(logs) != 2 || logs[0].ID != "old-2" || logs[1].ID != "new-1" {
		t.Fatalf("retained logs = %#v, want old-2 and new-1", logs)
	}
}
