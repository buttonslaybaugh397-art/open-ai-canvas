package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

func TestTeamAssetFolderLifecycle(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := model.User{ID: "owner-1", Username: "owner", DisplayName: "Owner", Role: model.UserRoleUser}
	member := model.User{ID: "member-1", Username: "member", DisplayName: "Member", Role: model.UserRoleUser}
	admin := model.User{ID: "admin-1", Username: "admin", DisplayName: "Admin", Role: model.UserRoleAdmin}
	if err := db.Create(&[]model.User{owner, member, admin}).Error; err != nil {
		t.Fatal(err)
	}

	folder, err := svc.CreateTeamAssetFolder(&owner, "Concept art")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RenameTeamAssetFolder(&member, folder.ID, "Blocked"); err == nil {
		t.Fatal("member renamed another user's team folder")
	}
	renamed, err := svc.RenameTeamAssetFolder(&admin, folder.ID, "References")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Name != "References" {
		t.Fatalf("renamed folder = %#v", renamed)
	}

	now := time.Now()
	asset := model.TeamAsset{
		ID:          "asset-1",
		OwnerUserID: owner.ID,
		Kind:        "text",
		Title:       "Opening scene",
		PayloadJSON: `{"id":"asset-1","kind":"text","title":"Opening scene","folderId":""}`,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MoveTeamAsset(&member, asset.ID, folder.ID); err == nil {
		t.Fatal("member moved another user's team asset")
	}
	moved, err := svc.MoveTeamAsset(&owner, asset.ID, folder.ID)
	if err != nil {
		t.Fatal(err)
	}
	var movedPayload map[string]any
	if err := json.Unmarshal(moved.Asset, &movedPayload); err != nil {
		t.Fatal(err)
	}
	if movedPayload["folderId"] != folder.ID {
		t.Fatalf("moved payload folderId = %v", movedPayload["folderId"])
	}

	if err := svc.DeleteTeamAssetFolder(&owner, folder.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.repo.TeamAsset(asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.FolderID != "" {
		t.Fatalf("stored folderId = %q", stored.FolderID)
	}
	var storedPayload map[string]any
	if err := json.Unmarshal([]byte(stored.PayloadJSON), &storedPayload); err != nil {
		t.Fatal(err)
	}
	if storedPayload["folderId"] != "" {
		t.Fatalf("stored payload folderId = %v", storedPayload["folderId"])
	}
	if _, err := svc.repo.TeamAssetFolder(folder.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("deleted folder lookup error = %v", err)
	}
}

func newTeamAssetTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.TeamAsset{}, &model.TeamAssetFolder{}, &model.TeamAssetResource{}); err != nil {
		t.Fatal(err)
	}
	return &Service{repo: repository.New(db), dataDir: t.TempDir()}, db
}
