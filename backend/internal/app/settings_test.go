package app

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMissingOSSSettingsReturnNormalizedDefaults(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.UserOSSSetting{}, &model.StorageLocation{}); err != nil {
		t.Fatal(err)
	}
	svc := New(repository.New(db), t.TempDir())
	admin := &model.User{ID: "admin", Role: model.UserRoleAdmin}

	platformSetting, err := svc.AdminOSSSetting(admin)
	if err != nil {
		t.Fatal(err)
	}
	assertDefaultOSSSetting(t, platformSetting)

	userSetting, err := svc.UserOSSSetting(&model.User{ID: "user-1"})
	if err != nil {
		t.Fatal(err)
	}
	assertDefaultOSSSetting(t, userSetting)
}

func TestAdminOSSSettingAllowsCredentialResetAfterEncryptionKeyLoss(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	const endpoint = "https://127.0.0.1"

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.StorageLocation{}, &model.AdminAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db)
	originalService := New(repo, t.TempDir())
	originalValue, err := originalService.encryptOSSSettingSecrets(ossSettingValue{
		Enabled: true, Provider: aliyunOSSProvider, Endpoint: endpoint, Bucket: "media",
		AccessKeyID: "old-access-key", AccessKeySecret: "old-secret", PathPrefix: "uploads",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(originalValue)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveSystemSetting(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(encoded)}); err != nil {
		t.Fatal(err)
	}

	restoredService := New(repo, t.TempDir())
	admin := &model.User{ID: "admin", Role: model.UserRoleAdmin}
	setting, err := restoredService.AdminOSSSetting(admin)
	if err != nil {
		t.Fatalf("AdminOSSSetting() error = %v", err)
	}
	if !setting.CredentialsRequireReset || setting.HasAccessKeySecret {
		t.Fatalf("recovered setting = %+v", setting)
	}
	if setting.Provider != aliyunOSSProvider || setting.Endpoint != endpoint || setting.Bucket != "media" || setting.AccessKeyID != "old-access-key" {
		t.Fatalf("non-secret fields were not preserved: %+v", setting)
	}
	if _, _, err := restoredService.readOSSSetting(); !errors.Is(err, errSettingSecretDecryption) {
		t.Fatalf("readOSSSetting() error = %v, want credential decryption error", err)
	}
	stored, err := repo.SystemSetting(ossSettingKey)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ValueJSON != string(encoded) {
		t.Fatal("admin recovery read unexpectedly rewrote encrypted credentials")
	}
	if _, err := restoredService.AdminOSSCredentials(admin); err == nil {
		t.Fatal("AdminOSSCredentials() should reject credentials encrypted with the lost key")
	} else {
		var appErr *AppError
		if !errors.As(err, &appErr) || appErr.Status != 409 {
			t.Fatalf("AdminOSSCredentials() error = %v, want conflict", err)
		}
	}

	updated, err := restoredService.UpdateOSSSetting(admin, OSSSettingRequest{
		Enabled: true, Provider: aliyunOSSProvider, Endpoint: endpoint, Bucket: "media",
		AccessKeyID: "new-access-key", AccessKeySecret: "new-secret", PathPrefix: "uploads",
	})
	if err != nil {
		t.Fatalf("UpdateOSSSetting() error = %v", err)
	}
	if updated.CredentialsRequireReset || !updated.HasAccessKeySecret {
		t.Fatalf("updated setting = %+v", updated)
	}
	_, decrypted, err := restoredService.readOSSSetting()
	if err != nil {
		t.Fatal(err)
	}
	if decrypted.AccessKeyID != "new-access-key" || decrypted.AccessKeySecret != "new-secret" {
		t.Fatalf("decrypted updated setting = %+v", decrypted)
	}
	credentials, err := restoredService.AdminOSSCredentials(admin)
	if err != nil {
		t.Fatalf("AdminOSSCredentials() error = %v", err)
	}
	if credentials.Provider != aliyunOSSProvider || credentials.AccessKeyID != "new-access-key" || credentials.AccessKeySecret != "new-secret" {
		t.Fatalf("revealed credentials = %+v", credentials)
	}
	var audit model.AdminAuditEvent
	if err := db.Where("action = ? AND actor_user_id = ?", "storage.credentials.read", admin.ID).First(&audit).Error; err != nil {
		t.Fatalf("credential read audit missing: %v", err)
	}
	if strings.Contains(audit.MetadataJSON, "new-secret") {
		t.Fatal("credential read audit contains plaintext secret")
	}
}

func assertDefaultOSSSetting(t *testing.T, value *PublicOSSSetting) {
	t.Helper()
	if value.Provider != aliyunOSSProvider || value.PathPrefix != defaultOSSPathPrefix || value.S3Preset != "custom" {
		t.Fatalf("default OSS setting = %+v", value)
	}
}
