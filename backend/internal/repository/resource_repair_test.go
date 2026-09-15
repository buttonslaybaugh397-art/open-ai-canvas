package repository

import (
	"errors"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newResourceRepairTestRepository(t *testing.T) (*Repository, *gorm.DB, model.Resource, ResourceReferenceUpdate, ResourceReferenceUpdate) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.AutoMigrate(&model.Resource{}, &model.Asset{}, &model.CanvasProject{}); err != nil {
		t.Fatal(err)
	}
	replacement := model.Resource{ID: "new", UserID: "owner", Status: model.ResourceStatusReady}
	asset := model.Asset{ID: "asset", UserID: "owner", PayloadJSON: `{"storageKey":"resource:old","title":"original"}`}
	canvas := model.CanvasProject{ID: "canvas", UserID: "owner", PayloadJSON: `{"content":"resource:old","title":"original"}`}
	for _, value := range []any{&replacement, &asset, &canvas} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	assetUpdate := ResourceReferenceUpdate{ID: asset.ID, PreviousJSON: asset.PayloadJSON, PreviousUpdatedAt: asset.UpdatedAt, PayloadJSON: `{"storageKey":"resource:new","title":"original"}`}
	canvasUpdate := ResourceReferenceUpdate{ID: canvas.ID, PreviousJSON: canvas.PayloadJSON, PreviousUpdatedAt: canvas.UpdatedAt, PayloadJSON: `{"content":"resource:new","title":"original"}`}
	return New(db), db, replacement, assetUpdate, canvasUpdate
}

func TestRepairResourceReferencesCASPreservesConcurrentEdits(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*gorm.DB, ResourceReferenceUpdate) error
	}{
		{"payload without timestamp", func(db *gorm.DB, snapshot ResourceReferenceUpdate) error {
			return db.Model(&model.CanvasProject{}).Where("id = ?", snapshot.ID).
				UpdateColumn("payload_json", `{"content":"resource:old","title":"concurrent edit"}`).Error
		}},
		{"timestamp without payload", func(db *gorm.DB, snapshot ResourceReferenceUpdate) error {
			return db.Model(&model.CanvasProject{}).Where("id = ?", snapshot.ID).
				UpdateColumn("updated_at", snapshot.PreviousUpdatedAt.Add(time.Minute)).Error
		}},
		{"ownership", func(db *gorm.DB, snapshot ResourceReferenceUpdate) error {
			return db.Model(&model.CanvasProject{}).Where("id = ?", snapshot.ID).UpdateColumn("user_id", "other").Error
		}},
		{"deleted document", func(db *gorm.DB, snapshot ResourceReferenceUpdate) error {
			return db.Delete(&model.CanvasProject{}, "id = ?", snapshot.ID).Error
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, db, replacement, asset, canvas := newResourceRepairTestRepository(t)
			if err := test.edit(db, canvas); err != nil {
				t.Fatal(err)
			}
			var before model.CanvasProject
			beforeErr := db.First(&before, "id = ?", canvas.ID).Error
			if beforeErr != nil && !errors.Is(beforeErr, gorm.ErrRecordNotFound) {
				t.Fatal(beforeErr)
			}
			err := repo.RepairResourceReferences("owner", "old", nil, replacement, []ResourceReferenceUpdate{asset}, []ResourceReferenceUpdate{canvas})
			if !errors.Is(err, ErrResourceReferenceRepairConflict) {
				t.Fatalf("repair = %v, want CAS conflict", err)
			}
			savedAsset, err := repo.AssetForUser("owner", asset.ID)
			if err != nil || savedAsset.PayloadJSON != asset.PreviousJSON || !savedAsset.UpdatedAt.Equal(asset.PreviousUpdatedAt) {
				t.Fatalf("earlier asset update was not rolled back: %#v, %v", savedAsset, err)
			}
			var after model.CanvasProject
			afterErr := db.First(&after, "id = ?", canvas.ID).Error
			if beforeErr != nil {
				if !errors.Is(afterErr, gorm.ErrRecordNotFound) {
					t.Fatalf("deleted canvas was recreated: %v", afterErr)
				}
			} else if afterErr != nil || after.PayloadJSON != before.PayloadJSON || after.UserID != before.UserID || !after.UpdatedAt.Equal(before.UpdatedAt) {
				t.Fatalf("concurrent edit was overwritten: %#v, %v", after, afterErr)
			}
		})
	}
}

func TestRepairResourceReferencesRechecksResourceSnapshots(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*gorm.DB) error
	}{
		{"replacement pending", func(db *gorm.DB) error {
			return db.Model(&model.Resource{}).Where("id = ?", "new").UpdateColumn("status", model.ResourceStatusPending).Error
		}},
		{"replacement ownership", func(db *gorm.DB) error {
			return db.Model(&model.Resource{}).Where("id = ?", "new").UpdateColumn("user_id", "other").Error
		}},
		{"replacement disappeared", func(db *gorm.DB) error {
			return db.Delete(&model.Resource{}, "id = ?", "new").Error
		}},
		{"old appeared foreign", func(db *gorm.DB) error {
			return db.Create(&model.Resource{ID: "old", UserID: "other", Status: model.ResourceStatusFailed}).Error
		}},
		{"old appeared ready", func(db *gorm.DB) error {
			return db.Create(&model.Resource{ID: "old", UserID: "owner", Status: model.ResourceStatusReady}).Error
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, db, replacement, asset, canvas := newResourceRepairTestRepository(t)
			if err := test.edit(db); err != nil {
				t.Fatal(err)
			}
			err := repo.RepairResourceReferences("owner", "old", nil, replacement, []ResourceReferenceUpdate{asset}, []ResourceReferenceUpdate{canvas})
			if !errors.Is(err, ErrResourceReferenceRepairConflict) {
				t.Fatalf("repair = %v, want resource conflict", err)
			}
			savedAsset, err := repo.AssetForUser("owner", asset.ID)
			if err != nil || savedAsset.PayloadJSON != asset.PreviousJSON {
				t.Fatalf("asset changed despite resource conflict: %#v, %v", savedAsset, err)
			}
			savedCanvas, err := repo.CanvasProjectForUser("owner", canvas.ID)
			if err != nil || savedCanvas.PayloadJSON != canvas.PreviousJSON {
				t.Fatalf("canvas changed despite resource conflict: %#v, %v", savedCanvas, err)
			}
		})
	}
}
