package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"infinite-canvas/backend/internal/protocol"
)

type hostChannelDispatchCase struct {
	pluginID   string
	providerID string
	mode       string
	model      string
	basePath   string
	createPath string
	pollPath   string
}

var hostChannelDispatchCases = []hostChannelDispatchCase{
	{"globalaiopc-channel", "globalaiopc-image", "image", "seedream_5.0Pro", "/kyyReactApiServer", "/v2/model-center/tasks", "/v2/model-center/tasks/task-1"},
	{"globalaiopc-channel", "globalaiopc-video", "video", "sd_2.0_special", "/kyyReactApiServer", "/v2/model-center/tasks", "/v2/model-center/tasks/task-1"},
	{"huiquyun-channel", "huiquyun-video", "video", "test-video", "/v1", "/videos/generations", "/videos/task-1"},
	{"aistarslab-channel", "aistarslab-image", "image", "test-channel:test-model", "/openapi", "/generation/create/image", "/generation/status?taskId=task-1"},
	{"aistarslab-channel", "aistarslab-video", "video", "test-channel:test-model", "/openapi", "/generation/create/video", "/generation/status?taskId=task-1"},
	{"weijin-channel", "weijin-video", "video", "dreamina-2.0-720p", "", "/v1/videos", "/v1/videos/task-1"},
	{"tianyue-channel", "tianyue-video", "video", "test-video", "", "/v1/videos", "/v1/videos/task-1"},
	{"tianyue-channel", "tianyue-video", "video", "test-video-se", "", "/v1/videos", "/v1/videos/task-1"},
}

func TestHostChannelPluginsDispatchThroughCanvasGeneration(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	center, err := newPluginRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, tt hostChannelDispatchCase, input canvasGenerationInput) (map[string]interface{}, error) {
		ctx = withProtocolRegistry(ctx, center.registrySnapshot())
		if tt.mode == "image" {
			return runImageTask(ctx, input)
		}
		policy := defaultVideoPollPolicy()
		policy.InitialDelay = time.Millisecond
		policy.Interval = time.Millisecond
		policy.Sleep = func(context.Context, time.Duration) error { return nil }
		return runVideoTaskWithPolicy(ctx, input, policy)
	}
	for _, tt := range hostChannelDispatchCases {
		t.Run(tt.providerID, func(t *testing.T) {
			var creates, polls, downloads atomic.Int32
			var upstream *httptest.Server
			upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodPost && r.URL.RequestURI() == tt.basePath+tt.createPath:
					creates.Add(1)
					if r.Header.Get("Authorization") != "Bearer test-key" {
						t.Error("channel authorization was not preserved")
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Errorf("channel create body is not JSON: %v", err)
					}
					if strings.HasPrefix(tt.providerID, "aistarslab-") && (body["channel"] != "test-channel" || body["model"] != "test-model") {
						t.Errorf("AIStarsLab route = %v", body)
					}
					if tt.providerID == "tianyue-video" {
						usage := float64(1)
						if strings.HasSuffix(tt.model, "-se") {
							usage = 6
						}
						if body["duration"] != usage || body["video_duration"] != float64(6) || body["resolution"] != "720p" {
							t.Errorf("TianYue usage/duration/resolution = %v", body)
						}
					}
					if tt.providerID == "weijin-video" || tt.providerID == "tianyue-video" {
						_, _ = w.Write([]byte(`{"id":"task-1","status":"queued"}`))
					} else {
						_, _ = w.Write([]byte(`{"code":0,"data":{"id":"task-1","taskId":"task-1","status":"pending"}}`))
					}
				case r.Method == http.MethodGet && r.URL.RequestURI() == tt.basePath+tt.pollPath:
					polls.Add(1)
					resultURL := upstream.URL + "/result"
					switch {
					case strings.HasPrefix(tt.providerID, "aistarslab-"):
						_, _ = fmt.Fprintf(w, `{"code":0,"data":{"taskId":"task-1","status":3,"outputs":[%q]}}`, resultURL)
					case tt.providerID == "weijin-video":
						_, _ = fmt.Fprintf(w, `{"id":"task-1","status":"completed","result_url":%q}`, resultURL)
					case tt.providerID == "tianyue-video":
						_, _ = fmt.Fprintf(w, `{"task_id":"task-1","status":"completed","metadata":{"url":%q}}`, resultURL)
					default:
						_, _ = fmt.Fprintf(w, `{"code":0,"data":{"id":"task-1","status":"succeeded","image_url":%q,"video_url":%q}}`, resultURL, resultURL)
					}
				case r.Method == http.MethodGet && r.URL.Path == "/result":
					downloads.Add(1)
					mimeType := "video/mp4"
					if tt.mode == "image" {
						mimeType = "image/png"
					}
					w.Header().Set("Content-Type", mimeType)
					_, _ = w.Write([]byte("test-media"))
				default:
					t.Errorf("plugin dispatched to unexpected endpoint: %s %s", r.Method, r.URL.RequestURI())
					http.NotFound(w, r)
				}
			}))
			defer upstream.Close()

			input := canvasGenerationInput{
				Mode: tt.mode, Prompt: "channel dispatch test",
				Config: providerConfig{
					InterfaceType: tt.providerID, Model: tt.model, BaseURL: upstream.URL + tt.basePath,
					APIFormat: "openai", APIKey: "test-key",
					Size: "16:9", VideoSeconds: "6", VQuality: "720p",
				},
			}
			result, err := run(context.Background(), tt, input)
			if err != nil {
				t.Fatalf("canvas generation: %v", err)
			}
			if result["mode"] != tt.mode || creates.Load() != 1 || polls.Load() != 1 || downloads.Load() != 1 {
				t.Fatalf("mode=%v, create/poll/download=%d/%d/%d", result["mode"], creates.Load(), polls.Load(), downloads.Load())
			}
			resumeCtx := context.WithValue(context.Background(), providerAnalyticsKey{}, providerAnalyticsContext{ProviderRequestID: "task-1"})
			if _, err := run(resumeCtx, tt, input); err != nil {
				t.Fatalf("resume canvas generation: %v", err)
			}
			if creates.Load() != 1 || polls.Load() != 2 || downloads.Load() != 2 {
				t.Fatalf("resume create/poll/download=%d/%d/%d", creates.Load(), polls.Load(), downloads.Load())
			}
		})
	}
}

func TestHostChannelPluginsNeverFallBackWhenUnavailable(t *testing.T) {
	center, err := newPluginRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, pluginID := range []string{"globalaiopc-channel", "huiquyun-channel", "aistarslab-channel", "weijin-channel", "tianyue-channel"} {
		if _, err := center.setEnabled(pluginID, false); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Error(w, "must not reach upstream", http.StatusBadRequest)
	}))
	defer upstream.Close()

	for _, state := range []struct {
		name     string
		registry *protocol.Registry
	}{
		{"disabled", center.registrySnapshot()},
		{"missing", emptyProtocolRegistry},
	} {
		for _, tt := range hostChannelDispatchCases {
			t.Run(state.name+"/"+tt.providerID, func(t *testing.T) {
				config := providerConfig{InterfaceType: tt.providerID, Model: tt.model, BaseURL: upstream.URL, APIKey: "test-key"}
				ctx := withProtocolRegistry(context.Background(), state.registry)
				input := canvasGenerationInput{Mode: tt.mode, Prompt: "test", Config: config}
				var err error
				if tt.mode == "image" {
					_, err = runImageTask(ctx, input)
				} else {
					_, err = runVideoTask(ctx, input)
				}
				if err == nil {
					t.Fatal("unavailable channel plugin was accepted")
				}
			})
		}
	}
	if requests.Load() != 0 {
		t.Fatalf("unavailable plugins sent %d upstream requests", requests.Load())
	}
}
