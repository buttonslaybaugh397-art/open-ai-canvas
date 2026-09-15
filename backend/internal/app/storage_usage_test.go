package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func writeUsageTestFile(t *testing.T, root, path string, size int) string {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, bytes.Repeat([]byte("x"), size), 0o600); err != nil {
		t.Fatal(err)
	}
	return full
}

func TestStorageUsageCountsPhysicalObjectsAndDeletionOutbox(t *testing.T) {
	svc := newResourceTestService(t)
	readyPath := writeUsageTestFile(t, svc.dataDir, "resources/ready", 10)
	writeUsageTestFile(t, svc.dataDir, "resources/failed", 20)
	deletedPath := writeUsageTestFile(t, svc.dataDir, "resources/deleting", 30)
	sessionPath := writeUsageTestFile(t, svc.dataDir, "uploads/file", 40)
	writeUsageTestFile(t, svc.dataDir, "resources/other-user", 100)
	// System derivatives and unowned files are not part of account file quota.
	writeUsageTestFile(t, svc.dataDir, "playback/preview.mp4", 100)
	writeUsageTestFile(t, svc.dataDir, "resources/unowned", 100)
	for _, row := range []any{
		&model.Resource{ID: "ready", UserID: "user-1", ObjectKey: "ready", Status: model.ResourceStatusReady, Size: 1000},
		&model.Resource{ID: "duplicate", UserID: "user-1", ObjectKey: "./ready", Provider: "local", Status: model.ResourceStatusReady, Size: 2000},
		&model.Resource{ID: "failed", UserID: "user-1", ObjectKey: "failed", Status: model.ResourceStatusFailed, Size: 3000},
		&model.Resource{ID: "missing", UserID: "user-1", ObjectKey: "missing", Status: model.ResourceStatusReady, Size: 4000},
		&model.Resource{ID: "other", UserID: "user-2", ObjectKey: "other-user", Status: model.ResourceStatusReady},
		&model.ResourceDeletionJob{ID: "deletion", ResourceID: "deleted", UserID: "user-1", ObjectKey: "deleting", Status: model.ResourceDeletionStatusPending},
		&model.ResourceDeletionJob{ID: "deletion-duplicate", ResourceID: "ready", UserID: "user-1", ObjectKey: "ready", Status: model.ResourceDeletionStatusProcessing},
		&model.SessionFile{ID: "file", UserID: "user-1", Path: sessionPath, Size: 10000},
		&model.SessionFile{ID: "file-duplicate", UserID: "user-1", Path: sessionPath, Size: 10000},
	} {
		if err := svc.repo.Create(row); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		usage, err := svc.AccountFileStorageUsage("user-1")
		if err != nil || usage.UsedBytes != 100 || usage.ResourceBytes != 30 || usage.SessionBytes != 40 || usage.PendingDeletionBytes != 30 || usage.CheckedAt.IsZero() {
			t.Fatalf("physical usage must not accumulate: %+v %v", usage, err)
		}
	}
	if err := os.Remove(deletedPath); err != nil {
		t.Fatal(err)
	}
	usage, err := svc.AccountFileStorageUsage("user-1")
	if err != nil || usage.UsedBytes != 70 || usage.PendingDeletionBytes != 0 {
		t.Fatalf("physical deletion did not release capacity: %+v %v", usage, err)
	}
	if err := os.Remove(readyPath); err != nil {
		t.Fatal(err)
	}
	usage, err = svc.AccountFileStorageUsage("user-1")
	if err != nil || usage.UsedBytes != 60 {
		t.Fatalf("missing source was still charged: %+v %v", usage, err)
	}
	other, err := svc.AccountFileStorageUsage("user-2")
	if err != nil || other.UsedBytes != 100 {
		t.Fatalf("cross-account statistics: %+v %v", other, err)
	}
}

func TestStorageUsageRejectsInvalidPathsAndDatabaseFailures(t *testing.T) {
	root := t.TempDir()
	writeUsageTestFile(t, root, "nested/file", 3)
	for _, path := range []string{"../outside", filepath.Join(root, "nested", "file"), ".", "nested"} {
		if _, err := statStoredLocalFile(root, path); err == nil {
			t.Fatalf("accepted invalid path %q", path)
		}
	}
	t.Run("symlink escape", func(t *testing.T) {
		outside := writeUsageTestFile(t, t.TempDir(), "outside", 3)
		if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := statStoredLocalFile(root, "link"); err == nil {
			t.Fatal("symlink escaped storage root")
		}
	})
	svc, db := newResourceTestServiceWithDB(t)
	if _, err := svc.AccountFileStorageUsage(""); err == nil {
		t.Fatal("empty user must fail")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.AccountFileStorageUsageContext(ctx, "user-1", true); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	if err := svc.repo.Create(&model.SessionFile{ID: "escape", UserID: "user-1", Path: filepath.Join(t.TempDir(), "outside")}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AccountFileStorageUsage("user-1"); err == nil {
		t.Fatal("session path outside uploads accepted")
	}
	if err := db.Migrator().DropTable(&model.ResourceDeletionJob{}); err != nil {
		t.Fatal(err)
	}
	if usage, err := svc.AccountFileStorageUsage("empty-user"); err == nil || usage != nil {
		t.Fatalf("database failure treated as empty account: %+v %v", usage, err)
	}
	if _, err := svc.reserveUserUploadQuota("empty-user", 1); err == nil {
		t.Fatal("database failure allowed upload")
	}
}

func storageUsageCloudService(t *testing.T, handler http.Handler) (*Service, *httptest.Server) {
	t.Helper()
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	svc := newResourceTestService(t)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	setting, err := json.Marshal(ossSettingValue{Provider: s3Provider, Region: "us-east-1", Endpoint: server.URL,
		Bucket: "bucket", AccessKeyID: "test-id", AccessKeySecret: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.Create(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(setting)}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "duplicate"} {
		if err := svc.repo.Create(&model.Resource{ID: id, UserID: "user-1", Status: model.ResourceStatusReady,
			Provider: s3Provider, Endpoint: server.URL, Bucket: "bucket", ObjectKey: "object", Size: 999}); err != nil {
			t.Fatal(err)
		}
	}
	return svc, server
}

func TestStorageUsageCloudCacheRecountAndQuotaBypass(t *testing.T) {
	var calls, size, status atomic.Int64
	size.Store(7)
	status.Store(200)
	svc, server := storageUsageCloudService(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodHead || !strings.HasPrefix(r.URL.Path, "/bucket/object") || r.Header.Get("Authorization") == "" {
			t.Errorf("invalid metadata request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Length", strconv.FormatInt(size.Load(), 10))
		w.WriteHeader(int(status.Load()))
	}))
	for range 2 {
		usage, err := svc.AccountFileStorageUsage("user-1")
		if err != nil || usage.UsedBytes != 7 {
			t.Fatalf("HEAD size not used: %+v %v", usage, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("duplicates/cache made %d requests", calls.Load())
	}
	size.Store(11)
	usage, err := svc.AccountFileStorageUsageContext(context.Background(), "user-1", true)
	if err != nil || usage.UsedBytes != 11 || calls.Load() != 2 {
		t.Fatalf("recount did not bypass cache: %+v %v requests=%d", usage, err, calls.Load())
	}
	_, _ = svc.AccountFileStorageUsage("user-1")
	status.Store(403)
	if _, err := svc.reserveUserUploadQuota("user-1", 1); err == nil {
		t.Fatal("upload trusted stale read projection")
	}
	if svc.pendingStorage["user-1"] != 0 {
		t.Fatal("failed measurement reserved quota")
	}
	status.Store(404)
	usage, err = svc.AccountFileStorageUsageContext(context.Background(), "user-1", true)
	if err != nil || usage.UsedBytes != 0 {
		t.Fatalf("missing cloud object still charged: %+v %v", usage, err)
	}
	// A cached missing result must not allow a newly present object past quota.
	_, _ = svc.AccountFileStorageUsage("user-1")
	status.Store(200)
	size.Store(100)
	if _, err := svc.reserveUserStoredFileQuota("user-1", 1, 1000, 1000, 100, "single"); err == nil {
		t.Fatal("quota trusted cached missing object")
	}
	size.Store(math.MaxInt64)
	if err := svc.repo.Create(&model.Resource{ID: "another", UserID: "user-1", Status: model.ResourceStatusReady,
		Provider: s3Provider, Endpoint: server.URL, Bucket: "bucket", ObjectKey: "object-two"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AccountFileStorageUsageContext(context.Background(), "user-1", true); err == nil || !strings.Contains(err.Error(), "超出有效范围") {
		t.Fatalf("overflow accepted: %v", err)
	}
}

func TestStorageUsageCombinesLocalCurrentAndHistoricalObjectStores(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	svc := newResourceTestService(t)
	var oldCalls, currentCalls atomic.Int32
	var currentStatus atomic.Int32
	currentStatus.Store(http.StatusOK)
	oldServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oldCalls.Add(1)
		sizes := map[string]int{"/old-bucket/same-key": 11, "/old-bucket/unbound": 13, "/old-bucket/deleting": 17}
		size, exists := sizes[r.URL.Path]
		if !exists || r.Method != http.MethodHead || !strings.Contains(r.Header.Get("Authorization"), "Credential=test-old-key/") {
			t.Errorf("historical metadata request lost its location or credentials: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(size))
	}))
	defer oldServer.Close()
	currentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentCalls.Add(1)
		if r.Method != http.MethodHead || r.URL.Path != "/current-bucket/same-key" ||
			!strings.Contains(r.Header.Get("Authorization"), "Credential=test-current-key/") {
			t.Errorf("current metadata request used the wrong account, location or credentials: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Length", "19")
		w.WriteHeader(int(currentStatus.Load()))
	}))
	defer currentServer.Close()
	oldSetting := ossSettingValue{Provider: s3Provider, Region: "us-east-1", Endpoint: oldServer.URL, Bucket: "old-bucket",
		AccessKeyID: "test-old-key", AccessKeySecret: "test-old-secret", CDNBaseURL: "https://old-cdn.invalid"}
	currentSetting := ossSettingValue{Provider: s3Provider, Region: "us-east-1", Endpoint: currentServer.URL, Bucket: "current-bucket",
		AccessKeyID: "test-current-key", AccessKeySecret: "test-current-secret", CDNBaseURL: "https://current-cdn.invalid"}
	oldJSON, err := json.Marshal(oldSetting)
	if err != nil {
		t.Fatal(err)
	}
	currentJSON, err := json.Marshal(currentSetting)
	if err != nil {
		t.Fatal(err)
	}
	writeUsageTestFile(t, svc.dataDir, "resources/local", 23)
	sessionPath := writeUsageTestFile(t, svc.dataDir, "uploads/attachment", 29)
	for _, row := range []any{
		&model.UserOSSSetting{ID: "old-setting", UserID: "user-1", Enabled: false, ValueJSON: string(oldJSON), CreatedAt: time.Now().Add(-time.Hour)},
		&model.UserOSSSetting{ID: "current-setting", UserID: "user-1", Enabled: true, ValueJSON: string(currentJSON), CreatedAt: time.Now()},
		&model.StorageLocation{ID: "current-location", Scope: "user", OwnerID: "user-1", Provider: s3Provider, ValueJSON: string(currentJSON), Active: true},
		&model.Resource{ID: "local", UserID: "user-1", Provider: "local", Status: model.ResourceStatusReady, ObjectKey: "local", Size: 9999},
		&model.Resource{ID: "old", UserID: "user-1", Provider: s3Provider, Status: model.ResourceStatusReady, Endpoint: oldServer.URL, Bucket: "old-bucket", StorageSettingID: "old-setting", ObjectKey: "same-key", Size: 9999},
		&model.Resource{ID: "old-duplicate", UserID: "user-1", Provider: s3Provider, Status: model.ResourceStatusReady, Endpoint: oldServer.URL, Bucket: "old-bucket", StorageSettingID: "old-setting", ObjectKey: "same-key", Size: 9999},
		&model.Resource{ID: "old-unbound", UserID: "user-1", Provider: s3Provider, Status: model.ResourceStatusReady, Endpoint: oldServer.URL, Bucket: "old-bucket", ObjectKey: "unbound", Size: 9999},
		&model.Resource{ID: "current", UserID: "user-1", Provider: s3Provider, Status: model.ResourceStatusReady, Endpoint: currentServer.URL, Bucket: "current-bucket", StorageSettingID: "current-location", ObjectKey: "same-key", Size: 9999},
		&model.Resource{ID: "foreign", UserID: "user-2", Provider: s3Provider, Status: model.ResourceStatusReady, Endpoint: currentServer.URL, Bucket: "current-bucket", ObjectKey: "foreign", Size: 9999},
		&model.ResourceDeletionJob{ID: "deleting", ResourceID: "removed", UserID: "user-1", Provider: s3Provider, Endpoint: oldServer.URL, Bucket: "old-bucket", StorageSettingID: "old-setting", ObjectKey: "deleting", Status: model.ResourceDeletionStatusPending},
		&model.SessionFile{ID: "attachment", UserID: "user-1", Path: sessionPath, Size: 9999},
	} {
		if err := svc.repo.Create(row); err != nil {
			t.Fatal(err)
		}
	}
	usage, err := svc.AccountFileStorageUsageContext(context.Background(), "user-1", true)
	if err != nil || usage.UsedBytes != 112 || usage.ResourceBytes != 66 || usage.SessionBytes != 29 || usage.PendingDeletionBytes != 17 {
		t.Fatalf("mixed physical storage was not fully measured: %+v %v", usage, err)
	}
	if oldCalls.Load() != 3 || currentCalls.Load() != 1 {
		t.Fatalf("wrong source queries or duplicate counting: old=%d current=%d", oldCalls.Load(), currentCalls.Load())
	}
	currentStatus.Store(http.StatusForbidden)
	if usage, err := svc.AccountFileStorageUsageContext(context.Background(), "user-1", true); err == nil || usage != nil {
		t.Fatalf("unavailable object store returned a partial total: %+v %v", usage, err)
	}
}

func TestStorageQuotaRechecksConcurrentSnapshotAndKeepsDailyUsage(t *testing.T) {
	svc := newResourceTestService(t)
	path := writeUsageTestFile(t, svc.dataDir, "resources/file", 10)
	if err := svc.repo.Create(&model.Resource{ID: "file", UserID: "user-1", ObjectKey: "file", Status: model.ResourceStatusReady, Size: 100000}); err != nil {
		t.Fatal(err)
	}
	day, err := svc.reserveUserStoredFileQuota("user-1", 10, 100, 10000, 100, "single")
	if err != nil {
		t.Fatal(err)
	}
	svc.commitUserUploadQuota("user-1", 10)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	usage, err := svc.AccountFileStorageUsage("user-1")
	if err != nil || usage.UsedBytes != 0 {
		t.Fatalf("usage after deletion: %+v %v", usage, err)
	}
	daily, err := svc.repo.DailyUploadBytes("user-1", day)
	if err != nil || daily != 10 {
		t.Fatalf("deletion reset daily volume: %d %v", daily, err)
	}
	var accepted atomic.Int32
	var group sync.WaitGroup
	for range 20 {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, err := svc.reserveUserStoredFileQuota("user-1", 10, 100, 10000, 100, "single"); err == nil {
				accepted.Add(1)
			}
		}()
	}
	group.Wait()
	if accepted.Load() != 9 || svc.pendingStorage["user-1"] != 90 {
		t.Fatalf("concurrent reservations accepted=%d pending=%d", accepted.Load(), svc.pendingStorage["user-1"])
	}
}

func TestStorageQuotaRetriesSnapshotChanges(t *testing.T) {
	var svc *Service
	var calls atomic.Int32
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			writeUsageTestFile(t, svc.dataDir, "resources/concurrent", 99)
			if err := svc.repo.Create(&model.Resource{ID: "concurrent", UserID: "user-1", ObjectKey: "concurrent", Status: model.ResourceStatusReady}); err != nil {
				t.Error(err)
			}
		}
		w.Header().Set("Content-Length", "1")
	})
	svc, _ = storageUsageCloudService(t, handler)
	if _, err := svc.reserveUserStoredFileQuota("user-1", 1, 100, 10000, 100, "single"); err == nil {
		t.Fatal("stale snapshot allowed upload")
	}
	if calls.Load() != 2 {
		t.Fatalf("changed snapshot was not remeasured: calls=%d", calls.Load())
	}
}

func TestStorageUsageDatabaseErrorDoesNotReturnCachedTotal(t *testing.T) {
	svc, db := newResourceTestServiceWithDB(t)
	if _, err := svc.AccountFileStorageUsage("user-1"); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register("usage-failure", func(tx *gorm.DB) {
		_ = tx.AddError(errors.New("database unavailable"))
	}); err != nil {
		t.Fatal(err)
	}
	if usage, err := svc.AccountFileStorageUsage("user-1"); err == nil || usage != nil {
		t.Fatalf("database error returned cached total: %+v %v", usage, err)
	}
}

func TestStorageSnapshotRejectsInvalidRevision(t *testing.T) {
	snapshot := repository.FileStorageSnapshot{Resources: []model.Resource{{UpdatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}}
	if _, err := storageSnapshotRevision(snapshot); err == nil {
		t.Fatal("invalid records were assigned a shared empty revision")
	}
}

func TestStorageUsageFollowsArchivedAssetThroughDeletionWorker(t *testing.T) {
	svc, db, root := newResourceDeletionTestService(t)
	defer svc.Close()
	writeUsageTestFile(t, root, "resources/archived.png", 42)
	resource := &model.Resource{ID: "archived-file", UserID: "user-1", ObjectKey: "archived.png", Provider: "local", Status: model.ResourceStatusReady, Size: 10000}
	for _, row := range []any{
		resource,
		&model.Asset{ID: "archived-asset", UserID: "user-1", Status: "archived", PayloadJSON: `{"data":{"storageKey":"resource:archived-file"}}`},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	usage, err := svc.AccountFileStorageUsage("user-1")
	if err != nil || usage.UsedBytes != 42 || usage.ResourceBytes != 42 {
		t.Fatalf("recycle bin was not charged: %+v %v", usage, err)
	}
	// Drive the transaction and worker separately to observe the outbox before
	// cleanup; the public deletion entry point launches that worker immediately.
	jobs := resourceDeletionJobs("user-1", map[string]*model.Resource{resourceStorageIdentity(resource): resource})
	if err := svc.repo.DeleteAssetAndResources("user-1", "archived-asset", []string{resource.ID}, jobs); err != nil {
		t.Fatal(err)
	}
	usage, err = svc.AccountFileStorageUsage("user-1")
	if err != nil || usage.UsedBytes != 42 || usage.PendingDeletionBytes != 42 {
		t.Fatalf("database deletion released physical bytes early: %+v %v", usage, err)
	}
	svc.drainResourceDeletionJobs(1)
	usage, err = svc.AccountFileStorageUsage("user-1")
	if err != nil || usage.UsedBytes != 0 || usage.PendingDeletionBytes != 0 {
		t.Fatalf("completed cleanup retained usage: %+v %v", usage, err)
	}
}

func TestStorageUsageFailureDoesNotBlockAdminUserDetails(t *testing.T) {
	svc, db, root := newResourceDeletionTestService(t)
	defer svc.Close()
	writeUsageTestFile(t, root, "resources/admin-usage", 7)
	user := model.User{ID: "usage-user", Username: "usage-user", Status: model.UserStatusActive, Role: model.UserRoleUser}
	for _, row := range []any{
		&user,
		&model.CreditAccount{UserID: user.ID},
		&model.Resource{ID: "usage-resource", UserID: user.ID, Status: model.ResourceStatusReady, ObjectKey: "admin-usage", Size: 999},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	admin := &model.User{ID: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive}
	detail, err := svc.AdminUserDetail(admin, user.ID)
	if err != nil || detail.StoredFileBytes == nil || *detail.StoredFileBytes != 7 {
		t.Fatalf("admin did not use physical size: %+v %v", detail, err)
	}
	if err := db.Callback().Query().Before("gorm:query").Register("usage-unavailable", func(tx *gorm.DB) {
		if tx.Statement.Table == "resources" {
			_ = tx.AddError(errors.New("storage unavailable"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	detail, err = svc.AdminUserDetail(admin, user.ID)
	if err != nil || detail.StoredFileBytes != nil || detail.FileStorageError == "" || detail.User.ID != user.ID {
		t.Fatalf("storage outage hid user details or pretended zero: %+v %v", detail, err)
	}
	if _, err := svc.AdminUserDetail(&user, user.ID); err == nil {
		t.Fatal("storage projection bypassed admin authorization")
	}
}
