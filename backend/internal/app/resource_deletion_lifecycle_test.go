package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestResourceDeletionUsesWorkerLifecycle(t *testing.T) {
	for _, entry := range []string{"asset", "detached"} {
		for _, state := range []string{"not-started", "draining", "running"} {
			t.Run(entry+"/"+state, func(t *testing.T) {
				t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
				entered := make(chan struct{})
				release := make(chan struct{})
				var once sync.Once
				unblock := func() { once.Do(func() { close(release) }) }
				storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodDelete {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					close(entered)
					<-release
					w.WriteHeader(http.StatusNoContent)
				}))
				t.Cleanup(storage.Close)
				svc, db, dataDir := newResourceDeletionTestService(t)
				t.Cleanup(func() {
					unblock()
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					if err := svc.StopWorker(ctx); err != nil {
						t.Errorf("stop deletion worker: %v", err)
					}
				})
				resource := model.Resource{
					ID: "resource-lifecycle", UserID: "user-1", Kind: "image",
					Status: model.ResourceStatusReady, Provider: "local",
					ObjectKey: "users/user-1/image/lifecycle.png",
				}
				if state == "running" {
					configureRecoveryStorage(t, svc, storage.URL)
					resource.Provider = tencentCOSProvider
					resource.Endpoint = storage.URL
					resource.Bucket = "test-bucket"
				} else {
					writeRecoveryFile(t, dataDir, resource.ObjectKey, []byte("image"))
				}
				if state != "not-started" {
					svc.backgroundWorkers().Start()
				}
				if state == "draining" {
					svc.backgroundWorkers().BeginDrain()
				}
				if err := db.Create(&resource).Error; err != nil {
					t.Fatal(err)
				}
				if entry == "asset" {
					asset := model.Asset{ID: "asset-lifecycle", UserID: resource.UserID,
						PayloadJSON: `{"data":{"storageKey":"resource:resource-lifecycle"}}`}
					if err := db.Create(&asset).Error; err != nil {
						t.Fatal(err)
					}
					if err := svc.DeleteUserAsset(asset.UserID, asset.ID); err != nil {
						t.Fatal(err)
					}
				} else if err := svc.cleanupDetachedUserResources(resource.UserID, []model.Resource{resource}); err != nil {
					t.Fatal(err)
				}

				if state == "running" {
					select {
					case <-entered:
					case <-time.After(3 * time.Second):
						t.Fatal("deletion did not reach object storage")
					}
					if count := svc.backgroundWorkers().ActiveTaskCount(); count != 1 {
						t.Fatalf("active deletion tasks = %d, want 1", count)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
					err := svc.StopWorker(ctx)
					cancel()
					if !errors.Is(err, context.DeadlineExceeded) {
						t.Fatalf("StopWorker returned before in-flight deletion completed: %v", err)
					}
					unblock()
					ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					if err := svc.StopWorker(ctx); err != nil {
						t.Fatal(err)
					}
				} else {
					var job model.ResourceDeletionJob
					if err := db.First(&job, "resource_id = ?", resource.ID).Error; err != nil {
						t.Fatal(err)
					}
					if job.Status != model.ResourceDeletionStatusPending || job.Attempts != 0 {
						t.Fatalf("inactive worker claimed persisted job: %#v", job)
					}
					if _, err := os.Stat(filepath.Join(dataDir, "resources", filepath.FromSlash(resource.ObjectKey))); err != nil {
						t.Fatalf("inactive worker deleted local object: %v", err)
					}
					// Persisted jobs remain available to the next periodic drain.
					svc.drainResourceDeletionJobs(1)
				}
				var remaining int64
				if err := db.Model(&model.ResourceDeletionJob{}).Where("resource_id = ?", resource.ID).Count(&remaining).Error; err != nil {
					t.Fatal(err)
				}
				if remaining != 0 {
					t.Fatalf("deletion was not completed: %d jobs remain", remaining)
				}
			})
		}
	}
}
