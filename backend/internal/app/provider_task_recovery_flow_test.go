package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
)

func TestManualRecoveryResumesWorkerWithoutSubmissionOrExtraReservation(t *testing.T) {
	for _, tt := range []struct {
		protocol      string
		directSuccess bool
	}{
		{"weijin-video", false}, {"weijin-video", true},
		{"tianyue-video", false}, {"tianyue-video", true},
	} {
		t.Run(fmt.Sprintf("%s/direct-success-%t", tt.protocol, tt.directSuccess), func(t *testing.T) {
			directSuccess := tt.directSuccess
			base, db := newTimelineTaskTestService(t)
			dir := t.TempDir()
			svc := NewWithRuntimeCapabilities(base.repo, dir, RuntimeCapabilities{desktopLocalChannels: true})
			var polls, downloads, creates atomic.Int32
			var upstream *httptest.Server
			upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					creates.Add(1)
					http.Error(w, "must never create", 500)
					return
				}
				switch r.URL.Path {
				case "/v1/videos/existing-task":
					count := polls.Add(1)
					w.Header().Set("Content-Type", "application/json")
					if !directSuccess && count == 2 {
						http.Error(w, "temporary upstream failure", http.StatusServiceUnavailable)
					} else if !directSuccess && count < 4 {
						fmt.Fprint(w, `{"id":"existing-task","status":"in_progress"}`)
					} else {
						fmt.Fprintf(w, `{"id":"existing-task","status":"completed","video_url":%q}`, upstream.URL+"/result")
					}
				case "/result":
					downloads.Add(1)
					w.Header().Set("Content-Type", "video/mp4")
					fmt.Fprint(w, "recovery-test-video")
				default:
					http.NotFound(w, r)
				}
			}))
			defer upstream.Close()
			input, _ := json.Marshal(canvasGenerationInput{Mode: "video", Prompt: "test",
				Config:   providerConfig{InterfaceType: tt.protocol, BaseURL: upstream.URL, APIKey: "test-key", Model: "test-model", AllowLocalChannel: true},
				Metadata: map[string]any{"nodeId": "user-node"},
			})
			var protected map[string]any
			if err := json.Unmarshal(input, &protected); err != nil {
				t.Fatal(err)
			}
			if err := svc.protectTaskSecrets(protected); err != nil {
				t.Fatal(err)
			}
			encryptedJSON, err := json.Marshal(protected)
			if err != nil {
				t.Fatal(err)
			}
			encrypted := string(encryptedJSON)
			task := &model.Task{ID: "task", UserID: "owner", Type: "canvas_video", Status: model.TaskStatusFailed,
				ProviderRequestID: "existing-task", BillingOrderID: "order", InputJSON: encrypted, Error: "timeout", CompletedAt: ptr(time.Now())}
			for _, record := range []any{
				&model.CreditAccount{UserID: "owner", AvailableMicrocredits: 9_000_000},
				&model.BillingOrder{ID: "order", UserID: "owner", TaskID: task.ID, IdempotencyKey: "task:task", Status: model.BillingStatusRefunded,
					BillingMode: "fixed", AmountMicrocredits: 3_000_000, RefundedAmountMicrocredits: 3_000_000, RefundedAt: ptr(time.Now())},
				task,
				&model.ApiCallLog{ID: "failed-log", UserID: "owner", TaskID: task.ID, Capability: "video", ProviderRequestID: "existing-task"},
			} {
				if err := db.Create(record).Error; err != nil {
					t.Fatal(err)
				}
			}
			if _, err := svc.AdminQueryFailedVideoTask(context.Background(), &model.User{ID: "not-admin"}, "failed-log"); err == nil {
				t.Fatal("non-admin recovery was accepted")
			}
			result, err := svc.AdminQueryFailedVideoTask(context.Background(), &model.User{ID: "admin", Role: model.UserRoleAdmin}, "failed-log")
			if err != nil {
				t.Fatal(err)
			}
			if !directSuccess {
				if !result.PollingResumed || result.Recovered || result.Task.Status != model.TaskStatusRunning || result.Task.ProviderRecoveryAt == nil {
					t.Fatalf("manual recovery response: %+v", result)
				}
				order, _ := svc.repo.BillingOrder("order")
				if order.Status != model.BillingStatusRefunded {
					t.Fatal("pending recovery changed billing")
				}
				stored, _ := svc.repo.Task(task.ID)
				if stored.InputJSON != encrypted {
					t.Fatal("recovery lost the encrypted worker config")
				}
				// Construct a fresh service, then drive the actual worker dispatch.
				svc = NewWithRuntimeCapabilities(base.repo, dir, RuntimeCapabilities{desktopLocalChannels: true})
				for range 3 {
					if err := db.Model(task).Update("next_poll_at", time.Now().Add(-time.Second)).Error; err != nil {
						t.Fatal(err)
					}
					if err := svc.taskWorker().processNextTask(); err != nil {
						t.Fatal(err)
					}
					stored, _ := svc.repo.Task(task.ID)
					if stored.Status == model.TaskStatusRunning && (stored.NextPollAt == nil || stored.LeaseOwner != "") {
						t.Fatal("pending/error poll did not release its lease into the durable queue")
					}
				}
			}
			stored, err := svc.repo.Task(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != model.TaskStatusSucceeded || stored.ProviderRequestID != "existing-task" || stored.ResultJSON == "" || stored.ProviderRecoveryAt == nil {
				t.Fatalf("completed task: %+v", stored)
			}
			if summary := taskSummaryForOutput(*stored); summary.ProviderRecoveryAt == nil || summary.ClientContext.NodeID != "user-node" {
				t.Fatalf("missing public recovery correlation: %+v", summary)
			}
			if creates.Load() != 0 || downloads.Load() != 1 {
				t.Fatalf("create/download calls = %d/%d", creates.Load(), downloads.Load())
			}
			if _, err := svc.QueryFailedVideoTask(context.Background(), "owner", task.ID); err == nil {
				t.Fatal("duplicate recovery was accepted")
			}
			if err := svc.taskWorker().processNextTask(); err != nil {
				t.Fatal(err)
			}
			var account model.CreditAccount
			db.First(&account, "user_id = ?", "owner")
			var count int64
			db.Model(&model.CreditLedgerEntry{}).Where("billing_order_id = ? AND type = ?", "order", model.CreditLedgerConsume).Count(&count)
			if account.AvailableMicrocredits != 6_000_000 || account.ReservedMicrocredits != 0 || count != 1 {
				t.Fatalf("recovery billing = %+v, consume entries=%d", account, count)
			}
		})
	}
}
