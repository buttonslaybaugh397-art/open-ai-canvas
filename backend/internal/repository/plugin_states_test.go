package repository

import (
	"fmt"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPluginStateLookupsTreatMissingRowsAsDefaults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:plugin-states-test-%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.PluginPlatformState{}, &model.UserPluginState{}); err != nil {
		t.Fatal(err)
	}
	repo := New(db)

	platform, err := repo.PluginPlatformState("missing-plugin")
	if err != nil || platform != nil {
		t.Fatalf("platform state = %#v, error = %v; want nil without error", platform, err)
	}
	user, err := repo.UserPluginState("user", "missing-plugin")
	if err != nil || user != nil {
		t.Fatalf("user state = %#v, error = %v; want nil without error", user, err)
	}

	if err := db.Create(&model.PluginPlatformState{PluginID: "known", Available: true}).Error; err != nil {
		t.Fatal(err)
	}
	platform, err = repo.PluginPlatformState("known")
	if err != nil || platform == nil || !platform.Available {
		t.Fatalf("known platform state = %#v, error = %v", platform, err)
	}
}
