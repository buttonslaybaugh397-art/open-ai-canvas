package canvas

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/kernel"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type repairTestHost struct {
	nopHost
	locked bool
	locks  int
}

func (h *repairTestHost) WithStorageLock(fn func() error) error {
	h.locked = true
	h.locks++
	defer func() { h.locked = false }()
	return fn()
}

func newResourceRepairTestService(t *testing.T) (*Service, *gorm.DB, *repairTestHost) {
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
	host := &repairTestHost{}
	return New(repository.New(db), host), db, host
}

func seedResourceRepairDocuments(t *testing.T, db *gorm.DB) (model.Asset, []model.CanvasProject) {
	t.Helper()
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	asset := model.Asset{
		ID: "asset", UserID: "owner", Title: "retained asset title", Kind: "image", FolderID: "folder",
		PayloadJSON: `{"id":"asset","updatedAt":"2026-01-01T00:00:00Z","metadata":{"resourceKey":"resource:old"},"data":{"storageKey":"resource:old","dataUrl":"/api/resources/old/file"},"coverUrl":"/api/resources/old/file","prompt":"mention resource:old"}`,
		CreatedAt:   at, UpdatedAt: at,
	}
	canvases := []model.CanvasProject{
		{
			ID: "open-canvas", UserID: "owner", Title: "retained canvas title", ProjectID: "project",
			PayloadJSON: `{"id":"open-canvas","updatedAt":"2026-01-01T00:00:00Z","nodes":[{"type":"image","metadata":{"assetId":"asset","storageKey":"resource:old","content":"/api/resources/old/file?proxy=1#view","x":9007199254740993}}]}`,
			CreatedAt:   at, UpdatedAt: at,
		},
		{
			ID: "unloaded-canvas", UserID: "owner", Title: "unloaded", ProjectID: "other-project",
			PayloadJSON: `{"id":"unloaded-canvas","timeline":{"clips":[{"directMedia":{"kind":"image","assetId":"asset","url":"/api/resources/old/file","dataUrl":"resource:old"}}]}}`,
			CreatedAt:   at, UpdatedAt: at,
		},
	}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&canvases).Error; err != nil {
		t.Fatal(err)
	}
	return asset, canvases
}

func TestRepairResourceReferencesUpdatesAssetsAndAllCanvases(t *testing.T) {
	for _, oldStatus := range []model.ResourceStatus{"", model.ResourceStatusFailed, model.ResourceStatusDeleted} {
		t.Run("old-"+string(oldStatus), func(t *testing.T) {
			svc, db, host := newResourceRepairTestService(t)
			asset, canvases := seedResourceRepairDocuments(t, db)
			replacement := model.Resource{ID: "new", UserID: "owner", Status: model.ResourceStatusReady, CreatedAt: asset.CreatedAt, UpdatedAt: asset.UpdatedAt}
			if err := db.Create(&replacement).Error; err != nil {
				t.Fatal(err)
			}
			var old model.Resource
			if oldStatus != "" {
				old = model.Resource{ID: "old", UserID: "owner", Status: oldStatus, ObjectKey: "keep-object", Error: "keep-error", CreatedAt: asset.CreatedAt, UpdatedAt: asset.UpdatedAt}
				if err := db.Create(&old).Error; err != nil {
					t.Fatal(err)
				}
			}
			foreign := model.Asset{ID: "foreign-asset", UserID: "other", PayloadJSON: asset.PayloadJSON, CreatedAt: asset.CreatedAt, UpdatedAt: asset.UpdatedAt}
			foreignCanvas := model.CanvasProject{ID: "foreign-canvas", UserID: "other", PayloadJSON: canvases[0].PayloadJSON, CreatedAt: asset.CreatedAt, UpdatedAt: asset.UpdatedAt}
			if err := db.Create(&foreign).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&foreignCanvas).Error; err != nil {
				t.Fatal(err)
			}
			candidate := asset
			candidate.PayloadJSON = strings.ReplaceAll(asset.PayloadJSON, "resource:old", "resource:new")
			candidate.PayloadJSON = strings.ReplaceAll(candidate.PayloadJSON, "/resources/old/", "/resources/new/")
			if err := svc.ValidateAssetCanvasReferences("owner", candidate); err == nil {
				t.Fatal("normal asset replacement guard unexpectedly allowed the change")
			}
			if err := db.Callback().Update().Before("gorm:update").Register("repair:assert-lock", func(tx *gorm.DB) {
				if !host.locked {
					_ = tx.AddError(errors.New("repair write without storage lock"))
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := svc.RepairResourceReferences("owner", "old", "new"); err != nil {
				t.Fatal(err)
			}
			if host.locks != 1 || host.locked {
				t.Fatalf("storage lock state: %#v", host)
			}
			savedAsset, err := svc.repo.AssetForUser("owner", asset.ID)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(savedAsset.PayloadJSON, `"resourceKey":"resource:old"`) || !strings.Contains(savedAsset.PayloadJSON, `"resourceKey":"resource:new"`) || !strings.Contains(savedAsset.PayloadJSON, `"prompt":"mention resource:old"`) {
				t.Fatalf("unexpected asset payload: %s", savedAsset.PayloadJSON)
			}
			expectedAsset := asset
			expectedAsset.PayloadJSON = savedAsset.PayloadJSON
			if !reflect.DeepEqual(expectedAsset, *savedAsset) {
				t.Fatalf("asset metadata or timestamp changed: %#v", savedAsset)
			}
			for _, original := range canvases {
				saved, err := svc.repo.CanvasProjectForUser("owner", original.ID)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(saved.PayloadJSON, "resource:old") || strings.Contains(saved.PayloadJSON, "/resources/old/") {
					t.Fatalf("canvas was not repaired: %s", saved.PayloadJSON)
				}
				if err := svc.ValidateCanvasMediaAssets("owner", []byte(saved.PayloadJSON)); err != nil {
					t.Fatalf("repaired canvas violates existing guard: %v", err)
				}
				original.PayloadJSON = saved.PayloadJSON
				if !reflect.DeepEqual(original, *saved) {
					t.Fatalf("canvas metadata or timestamp changed: %#v", saved)
				}
			}
			if !strings.Contains(savedAsset.PayloadJSON, `"updatedAt":"2026-01-01T00:00:00Z"`) {
				t.Fatal("JSON updatedAt changed")
			}
			savedForeign, err := svc.repo.AssetForUser("other", foreign.ID)
			if err != nil || !reflect.DeepEqual(foreign, *savedForeign) {
				t.Fatalf("foreign asset changed: %v", err)
			}
			savedForeignCanvas, err := svc.repo.CanvasProjectForUser("other", foreignCanvas.ID)
			if err != nil || !reflect.DeepEqual(foreignCanvas, *savedForeignCanvas) {
				t.Fatalf("foreign canvas changed: %v", err)
			}
			savedReplacement, err := svc.repo.Resource("new")
			if err != nil || !reflect.DeepEqual(replacement, *savedReplacement) {
				t.Fatalf("replacement resource changed: %v", err)
			}
			if oldStatus != "" {
				savedOld, err := svc.repo.Resource("old")
				if err != nil || !reflect.DeepEqual(old, *savedOld) {
					t.Fatalf("old resource changed: %v", err)
				}
			} else if _, err := svc.repo.Resource("old"); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("missing old resource was created: %v", err)
			}
			if err := db.Exec(`CREATE TRIGGER reject_repeat_asset BEFORE UPDATE ON assets BEGIN SELECT RAISE(ABORT, 'unexpected repeated write'); END`).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec(`CREATE TRIGGER reject_repeat_canvas BEFORE UPDATE ON canvas_projects BEGIN SELECT RAISE(ABORT, 'unexpected repeated write'); END`).Error; err != nil {
				t.Fatal(err)
			}
			if err := svc.RepairResourceReferences("owner", "old", "new"); err != nil {
				t.Fatalf("repeat should perform no writes: %v", err)
			}
		})
	}
}

func TestRepairResourceReferencesRejectsInvalidOwnershipAndState(t *testing.T) {
	for _, test := range []struct {
		name       string
		userID     string
		oldID      string
		newID      string
		oldOwner   string
		oldStatus  model.ResourceStatus
		newOwner   string
		newStatus  model.ResourceStatus
		wantStatus int
	}{
		{name: "empty user", oldID: "old", newID: "new", wantStatus: 401},
		{name: "empty old", userID: "owner", newID: "new", wantStatus: 400},
		{name: "empty replacement", userID: "owner", oldID: "old", wantStatus: 400},
		{name: "same ids", userID: "owner", oldID: "old", newID: "old", wantStatus: 400},
		{name: "invalid old", userID: "owner", oldID: "../old", newID: "new", wantStatus: 400},
		{name: "missing replacement", userID: "owner", oldID: "old", newID: "new", wantStatus: 404},
		{name: "foreign replacement", userID: "owner", oldID: "old", newID: "new", newOwner: "other", newStatus: model.ResourceStatusReady, wantStatus: 404},
		{name: "pending replacement", userID: "owner", oldID: "old", newID: "new", newOwner: "owner", newStatus: model.ResourceStatusPending, wantStatus: 409},
		{name: "failed replacement", userID: "owner", oldID: "old", newID: "new", newOwner: "owner", newStatus: model.ResourceStatusFailed, wantStatus: 409},
		{name: "deleted replacement", userID: "owner", oldID: "old", newID: "new", newOwner: "owner", newStatus: model.ResourceStatusDeleted, wantStatus: 409},
		{name: "foreign old", userID: "owner", oldID: "old", newID: "new", oldOwner: "other", oldStatus: model.ResourceStatusFailed, newOwner: "owner", newStatus: model.ResourceStatusReady, wantStatus: 403},
		{name: "ready old", userID: "owner", oldID: "old", newID: "new", oldOwner: "owner", oldStatus: model.ResourceStatusReady, newOwner: "owner", newStatus: model.ResourceStatusReady, wantStatus: 409},
		{name: "pending old", userID: "owner", oldID: "old", newID: "new", oldOwner: "owner", oldStatus: model.ResourceStatusPending, newOwner: "owner", newStatus: model.ResourceStatusReady, wantStatus: 409},
		{name: "unknown old", userID: "owner", oldID: "old", newID: "new", oldOwner: "owner", oldStatus: "unknown", newOwner: "owner", newStatus: model.ResourceStatusReady, wantStatus: 409},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, db, _ := newResourceRepairTestService(t)
			asset, canvases := seedResourceRepairDocuments(t, db)
			for _, resource := range []model.Resource{
				{ID: "old", UserID: test.oldOwner, Status: test.oldStatus},
				{ID: "new", UserID: test.newOwner, Status: test.newStatus},
			} {
				if resource.UserID != "" {
					if err := db.Create(&resource).Error; err != nil {
						t.Fatal(err)
					}
				}
			}
			err := svc.RepairResourceReferences(test.userID, test.oldID, test.newID)
			var appErr *kernel.AppError
			if !errors.As(err, &appErr) || appErr.Status != test.wantStatus {
				t.Fatalf("error = %v, want HTTP %d", err, test.wantStatus)
			}
			assertResourceRepairDocumentsUnchanged(t, svc, asset, canvases)
		})
	}
}

func TestRepairResourceReferencesFailureRollsBack(t *testing.T) {
	for _, failure := range []string{"canvas update", "malformed canvas"} {
		t.Run(failure, func(t *testing.T) {
			svc, db, _ := newResourceRepairTestService(t)
			asset, canvases := seedResourceRepairDocuments(t, db)
			if err := db.Create(&model.Resource{ID: "new", UserID: "owner", Status: model.ResourceStatusReady}).Error; err != nil {
				t.Fatal(err)
			}
			if failure == "canvas update" {
				if err := db.Exec(`CREATE TRIGGER reject_repair BEFORE UPDATE ON canvas_projects BEGIN SELECT RAISE(ABORT, 'injected repair failure'); END`).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				canvases[0].PayloadJSON = `{"nodes":`
				if err := db.Model(&model.CanvasProject{}).Where("id = ?", canvases[0].ID).UpdateColumn("payload_json", canvases[0].PayloadJSON).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := svc.RepairResourceReferences("owner", "old", "new"); err == nil {
				t.Fatal("expected repair failure")
			}
			assertResourceRepairDocumentsUnchanged(t, svc, asset, canvases)
		})
	}
}

func TestRepairResourceReferencesNoMatchingOwnedRecords(t *testing.T) {
	svc, db, _ := newResourceRepairTestService(t)
	if err := db.Create(&model.Resource{ID: "new", UserID: "owner", Status: model.ResourceStatusReady}).Error; err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := svc.RepairResourceReferences("owner", "frontend-only-old", "new"); err != nil {
			t.Fatal(err)
		}
	}
	for _, entity := range []any{&model.Asset{}, &model.CanvasProject{}} {
		var count int64
		if err := db.Model(entity).Count(&count).Error; err != nil || count != 0 {
			t.Fatalf("repair created records: count=%d, err=%v", count, err)
		}
	}
}

func assertResourceRepairDocumentsUnchanged(t *testing.T, svc *Service, asset model.Asset, canvases []model.CanvasProject) {
	t.Helper()
	savedAsset, err := svc.repo.AssetForUser(asset.UserID, asset.ID)
	if err != nil || !reflect.DeepEqual(asset, *savedAsset) {
		t.Fatalf("asset changed on failure: %#v, %v", savedAsset, err)
	}
	for _, original := range canvases {
		saved, err := svc.repo.CanvasProjectForUser(original.UserID, original.ID)
		if err != nil || !reflect.DeepEqual(original, *saved) {
			t.Fatalf("canvas changed on failure: %#v, %v", saved, err)
		}
	}
}
