package database

import (
	"errors"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestMigrateSchemaV3IsolatesLegacyTeamAssetsByOwner(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DSN: "file:migration-team-assets-v3?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&schemaMigration{}); err != nil {
		t.Fatal(err)
	}
	legacyTables := []string{
		`CREATE TABLE team_assets (id text PRIMARY KEY, owner_user_id text NOT NULL, folder_id text, kind text, category text, status text, title text, payload_json text, created_at datetime, updated_at datetime)`,
		`CREATE TABLE team_asset_folders (id text PRIMARY KEY, owner_user_id text NOT NULL, name text, created_at datetime, updated_at datetime)`,
		`CREATE TABLE team_asset_resources (team_asset_id text NOT NULL, resource_id text NOT NULL, created_at datetime, PRIMARY KEY (team_asset_id, resource_id))`,
	}
	for _, statement := range legacyTables {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create legacy table: %v", err)
		}
	}
	now := time.Now().UTC()
	for _, record := range []schemaMigration{
		{Version: 1, Name: "baseline_gorm_schema", Checksum: baselineSchemaChecksum, AppliedAt: now},
		{Version: 2, Name: "schema_migrations_applied_at_index", Checksum: schemaMigrationAppliedAtIndexChecksum, AppliedAt: now},
	} {
		if err := db.Create(&record).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, values := range [][]any{
		{"asset-a", "owner-a", "folder-a", "image", "other", "confirmed", "A", `{"id":"asset-a","kind":"image"}`},
		{"asset-b", "owner-b", "folder-b", "text", "other", "confirmed", "B", `{"id":"asset-b","kind":"text"}`},
	} {
		if err := db.Exec(`INSERT INTO team_assets (id, owner_user_id, folder_id, kind, category, status, title, payload_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, append(values, now, now)...).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, values := range [][]any{{"folder-a", "owner-a", "  Characters  "}, {"folder-b", "owner-b", "Scenes"}} {
		if err := db.Exec(`INSERT INTO team_asset_folders (id, owner_user_id, name, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`, append(values, now, now)...).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec(`INSERT INTO team_asset_resources (team_asset_id, resource_id, created_at) VALUES (?, ?, ?)`, "asset-a", "resource-a", now).Error; err != nil {
		t.Fatal(err)
	}

	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}

	var teams []model.Team
	if err := db.Order("created_by_user_id").Find(&teams).Error; err != nil {
		t.Fatal(err)
	}
	if len(teams) != 2 || teams[0].CreatedByUserID != "owner-a" || teams[1].CreatedByUserID != "owner-b" || teams[0].ID == teams[1].ID {
		t.Fatalf("legacy owners were not isolated into private teams: %#v", teams)
	}
	for _, team := range teams {
		if team.AssetLimit != 5000 || team.StorageLimit != int64(100)<<30 {
			t.Fatalf("team quota defaults were not backfilled: %#v", team)
		}
	}
	teamByOwner := map[string]string{teams[0].CreatedByUserID: teams[0].ID, teams[1].CreatedByUserID: teams[1].ID}
	for ownerID, teamID := range teamByOwner {
		var member model.TeamMember
		if err := db.First(&member, "team_id = ? AND user_id = ?", teamID, ownerID).Error; err != nil {
			t.Fatalf("missing owner membership for %s: %v", ownerID, err)
		}
		if member.Role != model.TeamMemberRoleOwner || member.Status != model.TeamMemberStatusActive {
			t.Fatalf("invalid migrated membership: %#v", member)
		}
	}
	var assets []model.TeamAsset
	if err := db.Order("id").Find(&assets).Error; err != nil {
		t.Fatal(err)
	}
	if len(assets) != 2 || assets[0].TeamID != teamByOwner["owner-a"] || assets[1].TeamID != teamByOwner["owner-b"] {
		t.Fatalf("legacy assets crossed team boundaries: %#v", assets)
	}
	if assets[0].SourceAssetID != assets[0].ID || assets[1].SourceAssetID != assets[1].ID {
		t.Fatalf("legacy source ids were not preserved: %#v", assets)
	}
	var folders []model.TeamAssetFolder
	if err := db.Order("id").Find(&folders).Error; err != nil {
		t.Fatal(err)
	}
	if len(folders) != 2 || folders[0].TeamID != teamByOwner["owner-a"] || folders[1].TeamID != teamByOwner["owner-b"] || folders[0].NameKey != "characters" {
		t.Fatalf("legacy folders were not isolated and normalized: %#v", folders)
	}
	var link model.TeamAssetResource
	if err := db.First(&link, "team_asset_id = ? AND resource_id = ?", "asset-a", "resource-a").Error; err != nil {
		t.Fatalf("legacy resource link was lost: %v", err)
	}
}

func TestMigrateSchemaRecordsAndValidatesVersion(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DSN: "file:migration-version?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	status, err := ReadSchemaStatus(db)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Ready || status.Current != CurrentSchemaVersion {
		t.Fatalf("unexpected schema status: %#v", status)
	}
	if status.Current != 7 || !db.Migrator().HasColumn(&model.Team{}, "Description") || !db.Migrator().HasColumn(&model.Team{}, "AssetLimit") || !db.Migrator().HasColumn(&model.Team{}, "StorageLimit") || !db.Migrator().HasTable(&model.TeamAuditEvent{}) || !db.Migrator().HasTable(&model.TeamInvitation{}) || !db.Migrator().HasColumn(&model.Resource{}, "upload_key") || !db.Migrator().HasIndex(&model.Resource{}, "idx_resources_user_upload_key") {
		t.Fatalf("schema v7 is incomplete: %#v", status)
	}
	if !db.Migrator().HasIndex(&schemaMigration{}, "idx_schema_migrations_applied_at") {
		t.Fatal("schema migration v2 did not create the applied_at index")
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatalf("migration should be idempotent: %v", err)
	}
}

func TestMigrateSchemaV7AddsResourceUploadKeyToExistingSchema(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DSN: "file:migration-resource-upload-key?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE resources (id TEXT PRIMARY KEY, user_id TEXT NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateResourceUploadKey(db); err != nil {
		t.Fatalf("migrate existing resource schema: %v", err)
	}
	if !db.Migrator().HasColumn(&model.Resource{}, "upload_key") {
		t.Fatal("resource upload_key column was not added")
	}
	if !db.Migrator().HasIndex(&model.Resource{}, "idx_resources_user_upload_key") {
		t.Fatal("resource upload key index was not added")
	}
	if err := db.Exec(`INSERT INTO resources (id, user_id, upload_key) VALUES (?, ?, ?)`, "resource-1", "user-1", "same-upload").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO resources (id, user_id, upload_key) VALUES (?, ?, ?)`, "resource-2", "user-1", "same-upload").Error; err == nil {
		t.Fatal("duplicate resource upload key should be rejected")
	}
	if err := db.Exec(`INSERT INTO resources (id, user_id, upload_key) VALUES (?, ?, ?)`, "resource-3", "user-2", "same-upload").Error; err != nil {
		t.Fatalf("different users should be able to reuse upload identities: %v", err)
	}
}

func TestMigrateSchemaRejectsChecksumMismatch(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DSN: "file:migration-checksum?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&schemaMigration{}).Where("version = ?", CurrentSchemaVersion).Update("checksum", "changed").Error; err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err == nil || !strings.Contains(err.Error(), "校验和不一致") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	if err := RequireSchemaVersion(db); err == nil || !strings.Contains(err.Error(), "校验和不一致") {
		t.Fatalf("schema verification must reject checksum mismatch, got %v", err)
	}
}

func TestMigrateSchemaRollsBackFailedMigration(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DSN: "file:migration-rollback?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateSchema(db); err != nil {
		t.Fatal(err)
	}

	original := schemaMigrations
	schemaMigrations = append(append([]migration(nil), original...), migration{
		version:  CurrentSchemaVersion + 1,
		name:     "rollback_probe",
		checksum: "sha256:rollback-probe",
		apply: func(tx *gorm.DB) error {
			if err := tx.Exec("CREATE TABLE migration_rollback_probe (id INTEGER PRIMARY KEY)").Error; err != nil {
				return err
			}
			return errors.New("forced migration failure")
		},
	})
	t.Cleanup(func() { schemaMigrations = original })

	if err := MigrateSchema(db); err == nil || !strings.Contains(err.Error(), "forced migration failure") {
		t.Fatalf("expected forced migration failure, got %v", err)
	}
	if db.Migrator().HasTable("migration_rollback_probe") {
		t.Fatal("failed migration left a partial table behind")
	}
	var count int64
	if err := db.Model(&schemaMigration{}).Where("version = ?", CurrentSchemaVersion+1).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("failed migration was recorded: %d", count)
	}
}

func TestRequireSchemaVersionRejectsUninitializedDatabase(t *testing.T) {
	db, err := Open(Config{Driver: "sqlite", DSN: "file:migration-uninitialized?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := RequireSchemaVersion(db); err == nil || !strings.Contains(err.Error(), "请先执行 migrate-schema up") {
		t.Fatalf("expected missing migration error, got %v", err)
	}
}
