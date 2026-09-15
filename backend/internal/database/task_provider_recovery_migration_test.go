package database

import (
	"testing"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestProviderRecoveryMigrationPreservesExistingTaskAndIsIdempotent(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("CREATE TABLE tasks (id TEXT PRIMARY KEY, status TEXT, provider_request_id TEXT, input_json TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO tasks (id, status, provider_request_id, input_json) VALUES (?, ?, ?, ?)", "existing", "failed", "upstream", "original-config").Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := migrateTaskProviderRecovery(db); err != nil {
			t.Fatal(err)
		}
	}
	var task model.Task
	if err := db.First(&task, "id = ?", "existing").Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != model.TaskStatusFailed || task.ProviderRecoveryAt != nil || task.ProviderRequestID != "upstream" || task.InputJSON != "original-config" {
		t.Fatalf("migration altered existing task: %+v", task)
	}
}
