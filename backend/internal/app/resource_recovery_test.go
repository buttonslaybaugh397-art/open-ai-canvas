package app

import (
	"context"
	"encoding/json"
	"hash/crc64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func TestMissingCloudResourceUsesLocalCopyAndUploadsAfterDelivery(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	uploaded := make(chan []byte, 1)
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead:
			w.WriteHeader(http.StatusNotFound)
		case http.MethodPut:
			payload, _ := io.ReadAll(r.Body)
			uploaded <- payload
			w.Header().Set("x-cos-hash-crc64ecma", strconv.FormatUint(crc64.Checksum(payload, crc64.MakeTable(crc64.ECMA)), 10))
			w.Header().Set("ETag", `"recovered"`)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer storage.Close()

	svc, db, dataDir := newResourceDeletionTestService(t)
	configureRecoveryStorage(t, svc, storage.URL)
	startRecoveryTestWorker(t, svc)
	resource := model.Resource{
		ID: "resource-cloud-recovery", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady,
		Provider: tencentCOSProvider, Endpoint: storage.URL, Bucket: "test-bucket", ObjectKey: "users/user-1/image/recovery.png",
		MimeType: "image/png", Size: int64(len("local-copy")),
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	writeRecoveryFile(t, dataDir, resource.ObjectKey, []byte("local-copy"))

	stream, err := svc.openResourceRange(resource.UserID, &resource, "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(stream.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if string(content) != "local-copy" {
		t.Fatalf("recovery stream = %q", content)
	}

	select {
	case payload := <-uploaded:
		if string(payload) != "local-copy" {
			t.Fatalf("uploaded recovery payload = %q", payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("local recovery copy was not uploaded")
	}
	waitForResource(t, db, resource.ID, func(current *model.Resource) bool {
		return current.Status == model.ResourceStatusReady && current.ETag == "recovered"
	})
}

func TestTemporaryCloudFailureUsesLocalCopyWithoutMarkingResourceFailed(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server does not support connection hijacking")
		}
		connection, _, err := hijacker.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		_ = connection.Close()
	}))
	defer storage.Close()

	svc, db, dataDir := newResourceDeletionTestService(t)
	configureRecoveryStorage(t, svc, storage.URL)
	resource := model.Resource{
		ID: "resource-cloud-temporary-failure", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady,
		Provider: tencentCOSProvider, Endpoint: storage.URL, Bucket: "test-bucket", ObjectKey: "users/user-1/image/temporary.png",
		MimeType: "image/png", Size: int64(len("local-copy")),
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	writeRecoveryFile(t, dataDir, resource.ObjectKey, []byte("local-copy"))

	stream, err := svc.openResourceRange(resource.UserID, &resource, "")
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(stream.Body)
	if closeErr := stream.Body.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "local-copy" {
		t.Fatalf("recovery stream = %q", content)
	}
	var current model.Resource
	if err := db.First(&current, "id = ?", resource.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Status != model.ResourceStatusReady || current.Error != "" {
		t.Fatalf("temporary failure changed resource status: %#v", current)
	}
}

func TestLocalResourcePromotesToActiveObjectStorage(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	uploaded := make(chan []byte, 1)
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		payload, _ := io.ReadAll(r.Body)
		uploaded <- payload
		w.Header().Set("x-cos-hash-crc64ecma", strconv.FormatUint(crc64.Checksum(payload, crc64.MakeTable(crc64.ECMA)), 10))
		w.Header().Set("ETag", `"promoted"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer storage.Close()

	svc, db, dataDir := newResourceDeletionTestService(t)
	configureRecoveryStorage(t, svc, storage.URL)
	startRecoveryTestWorker(t, svc)
	resource := model.Resource{
		ID: "resource-local-promotion", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady,
		Provider: "local", ObjectKey: "users/user-1/image/local.png", MimeType: "image/png", Size: int64(len("local-resource")),
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	writeRecoveryFile(t, dataDir, resource.ObjectKey, []byte("local-resource"))

	stream, err := svc.openResourceRange(resource.UserID, &resource, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Body.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case payload := <-uploaded:
		if string(payload) != "local-resource" {
			t.Fatalf("promoted payload = %q", payload)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("local resource was not promoted")
	}
	waitForResource(t, db, resource.ID, func(current *model.Resource) bool {
		return current.Provider == tencentCOSProvider && current.ObjectKey != resource.ObjectKey && current.ETag == "promoted"
	})
}

func TestUnrecoverableDetachedResourceIsRemoved(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	storage := missingRecoveryStorage()
	defer storage.Close()
	svc, db, _ := newResourceDeletionTestService(t)
	configureRecoveryStorage(t, svc, storage.URL)
	resource := model.Resource{
		ID: "resource-unrecoverable-detached", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady,
		Provider: tencentCOSProvider, Endpoint: storage.URL, Bucket: "test-bucket", ObjectKey: "users/user-1/image/missing.png",
		MimeType: "image/png", Size: 10,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.openResourceRange(resource.UserID, &resource, ""); err == nil {
		t.Fatal("missing resource did not return an error")
	}
	var count int64
	if err := db.Model(&model.Resource{}).Where("id = ?", resource.ID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("unreferenced unrecoverable resource count = %d, want 0", count)
	}
}

func TestUnrecoverableReferencedResourceIsMarkedFailed(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	storage := missingRecoveryStorage()
	defer storage.Close()
	svc, db, _ := newResourceDeletionTestService(t)
	configureRecoveryStorage(t, svc, storage.URL)
	resource := model.Resource{
		ID: "resource-unrecoverable-referenced", UserID: "user-1", Kind: "image", Status: model.ResourceStatusReady,
		Provider: tencentCOSProvider, Endpoint: storage.URL, Bucket: "test-bucket", ObjectKey: "users/user-1/image/referenced.png",
		MimeType: "image/png", Size: 10,
	}
	canvas := model.CanvasProject{
		ID: "canvas-with-missing-resource", UserID: resource.UserID, Title: "失效资源引用画布",
		PayloadJSON: `{"nodes":[{"data":{"storageKey":"resource:resource-unrecoverable-referenced"}}]}`,
	}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&canvas).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.openResourceRange(resource.UserID, &resource, ""); err == nil {
		t.Fatal("missing referenced resource did not return an error")
	}
	var current model.Resource
	if err := db.First(&current, "id = ?", resource.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.Status != model.ResourceStatusFailed || current.Error == "" {
		t.Fatalf("referenced resource = %#v, want failed record", current)
	}
}

func configureRecoveryStorage(t *testing.T, svc *Service, endpoint string) {
	t.Helper()
	settingJSON, err := json.Marshal(ossSettingValue{
		Enabled: true, Provider: tencentCOSProvider, Endpoint: endpoint, Bucket: "test-bucket",
		AccessKeyID: "test-id", AccessKeySecret: "test-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.SaveSystemSetting(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(settingJSON)}); err != nil {
		t.Fatal(err)
	}
}

func startRecoveryTestWorker(t *testing.T, svc *Service) {
	t.Helper()
	if _, started := svc.backgroundWorkers().Start(); !started {
		t.Fatal("resource recovery test worker did not start")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := svc.StopWorker(ctx); err != nil {
			t.Errorf("stop recovery worker: %v", err)
		}
	})
}

func writeRecoveryFile(t *testing.T, dataDir string, objectKey string, content []byte) {
	t.Helper()
	target := filepath.Join(dataDir, "resources", filepath.FromSlash(objectKey))
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, content, 0o640); err != nil {
		t.Fatal(err)
	}
}

func waitForResource(t *testing.T, db *gorm.DB, id string, condition func(*model.Resource) bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var resource model.Resource
		if err := db.First(&resource, "id = ?", id).Error; err == nil && condition(&resource) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("resource %s did not reach expected state", id)
}

func missingRecoveryStorage() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}
