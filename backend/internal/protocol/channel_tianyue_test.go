package protocol

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestTianYueVideoRequestBillingAndReferences(t *testing.T) {
	adapter, ok := Builtins().Get("tianyue-video")
	if !ok {
		t.Fatal("TianYue adapter is not registered")
	}
	for _, tt := range []struct {
		model string
		usage int
	}{
		{"test-model", 1},
		{"test-model-se", 6},
		{" test-model-se ", 6},
		{"test-se-model", 1},
	} {
		t.Run(tt.model, func(t *testing.T) {
			request := GenerationRequest{
				Model: tt.model, Prompt: " test prompt ", Duration: 6, Resolution: "720P", AspectRatio: "16:9",
				Images:          []MediaReference{{URL: "https://cdn.example/a.jpg"}, {URL: "https://cdn.example/b.jpg"}},
				Videos:          []MediaReference{{URL: "https://cdn.example/ref.mp4"}},
				Audios:          []MediaReference{{URL: "https://cdn.example/ref.mp3"}},
				Extra:           map[string]any{"duration": 99, "video_duration": 99, "seconds": 99, "billingMode": "per_second"},
				ProviderOptions: map[string]map[string]any{"tianyue": {"duration": 99}},
			}
			spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
			if err != nil {
				t.Fatal(err)
			}
			if spec.Method != http.MethodPost || spec.Path != "/v1/videos" || spec.ContentType != "application/json" {
				t.Fatalf("unexpected request: %+v", spec)
			}
			want := map[string]any{
				"model": strings.TrimSpace(tt.model), "prompt": "test prompt",
				"duration": tt.usage, "video_duration": 6, "resolution": "720p", "aspect_ratio": "16:9",
				"image_urls": []string{"https://cdn.example/a.jpg", "https://cdn.example/b.jpg"},
				"video_urls": []string{"https://cdn.example/ref.mp4"}, "audio_urls": []string{"https://cdn.example/ref.mp3"},
			}
			if !reflect.DeepEqual(spec.Body, want) {
				t.Fatalf("body = %#v, want %#v", spec.Body, want)
			}
		})
	}
}

func TestTianYueVideoEmptyReferencesAndRequiredFields(t *testing.T) {
	valid := GenerationRequest{Model: "test-model", Prompt: "test", Duration: 6, Resolution: "720"}
	body, err := tianYueVideoBody(valid)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(body)
	if !strings.Contains(string(encoded), `"image_urls":[]`) || body["resolution"] != "720p" {
		t.Fatalf("required array / numeric resolution: %s", encoded)
	}
	for _, key := range []string{"audio_urls", "video_urls", "aspect_ratio"} {
		if _, ok := body[key]; ok {
			t.Fatalf("empty optional field %s was sent", key)
		}
	}
	for _, tt := range []struct {
		name   string
		mutate func(*GenerationRequest)
	}{
		{"missing-model", func(r *GenerationRequest) { r.Model = "" }},
		{"missing-prompt", func(r *GenerationRequest) { r.Prompt = " " }},
		{"missing-duration", func(r *GenerationRequest) { r.Duration = 0 }},
		{"negative-duration", func(r *GenerationRequest) { r.Duration = -1 }},
		{"missing-resolution", func(r *GenerationRequest) { r.Resolution = "" }},
		{"auto-resolution", func(r *GenerationRequest) { r.Resolution = "auto" }},
		{"unsupported-resolution", func(r *GenerationRequest) { r.Resolution = "2160p" }},
		{"unsupported-ratio", func(r *GenerationRequest) { r.AspectRatio = "21:9" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := valid
			tt.mutate(&request)
			if _, err := tianYueVideoBody(request); err == nil {
				t.Fatal("invalid request accepted")
			}
		})
	}
}

func TestTianYueVideoStatesAndResultURLs(t *testing.T) {
	adapter := tianYueVideoAdapter()
	for _, tt := range []struct {
		name    string
		body    string
		status  Status
		wantErr bool
	}{
		{"queued", `{"id":"task-1","status":"queued"}`, StatusPending, false},
		{"processing", `{"id":"task-1","status":"in_progress"}`, StatusProcessing, false},
		{"failed", `{"id":"task-1","status":"failed","error":{"message":"rejected"},"video_url":"https://cdn.example/result.mp4"}`, StatusFailed, false},
		{"video-url", `{"id":"task-1","status":"completed","video_url":"https://cdn.example/result.mp4"}`, StatusSucceeded, false},
		{"url", `{"id":"task-1","status":"completed","url":"https://cdn.example/result.mp4"}`, StatusSucceeded, false},
		{"metadata-url", `{"task_id":"task-1","status":"completed","metadata":{"url":"https://cdn.example/result.mp4"}}`, StatusSucceeded, false},
		{"unknown-status", `{"id":"task-1","status":"succeeded","url":"https://cdn.example/result.mp4"}`, "", true},
		{"no-status", `{"id":"task-1"}`, "", true},
		{"no-url", `{"id":"task-1","status":"completed"}`, "", true},
		{"invalid-url-type", `{"id":"task-1","status":"completed","url":42}`, "", true},
		{"malformed", `{`, "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			created, createErr := adapter.ParseCreate(context.Background(), []byte(tt.body))
			polled, pollErr := adapter.ParsePoll(context.Background(), PollContext{TaskID: "task-1"}, []byte(tt.body))
			if (createErr != nil) != tt.wantErr || (pollErr != nil) != tt.wantErr {
				t.Fatalf("create error=%v, poll error=%v", createErr, pollErr)
			}
			if tt.wantErr {
				return
			}
			if created.TaskID != "task-1" || polled.TaskID != "task-1" || created.Status != tt.status || polled.Status != tt.status {
				t.Fatalf("create=%+v poll=%+v", created, polled)
			}
			if tt.status == StatusSucceeded {
				if polled.Result == nil || len(polled.Result.Videos) != 1 || polled.Result.Videos[0].URL != "https://cdn.example/result.mp4" || created.Result == nil {
					t.Fatalf("missing video output: create=%+v poll=%+v", created, polled)
				}
			} else if created.Result != nil || polled.Result != nil {
				t.Fatal("non-success returned output")
			}
			if tt.status == StatusFailed && (created.Message != "rejected" || polled.Message != "rejected") {
				t.Fatal("upstream failure message was lost")
			}
		})
	}
}

func TestTianYueVideoTaskIdentityAndManifest(t *testing.T) {
	adapter := tianYueVideoAdapter()
	ctx := context.Background()
	if _, err := adapter.ParseCreate(ctx, []byte(`{"status":"queued"}`)); err == nil {
		t.Fatal("create without ID accepted")
	}
	result, err := adapter.ParseCreate(ctx, []byte(`{"task_id":"task-1","id":"other","status":"queued"}`))
	if err != nil || result.TaskID != "task-1" {
		t.Fatalf("task_id precedence: %+v, %v", result, err)
	}
	if _, err := adapter.ParsePoll(ctx, PollContext{TaskID: "task-1"}, []byte(`{"id":"other","status":"queued"}`)); err == nil {
		t.Fatal("poll switched to another task")
	}
	if _, err := adapter.BuildPoll(ctx, PollContext{}); err == nil {
		t.Fatal("empty poll ID accepted")
	}
	spec, err := adapter.BuildPoll(ctx, PollContext{TaskID: "a/b?c"})
	if err != nil || spec.Method != http.MethodGet || spec.Path != "/v1/videos/a%2Fb%3Fc" {
		t.Fatalf("poll spec=%+v, err=%v", spec, err)
	}
	if adapter.(ResultCapability).ResultAvailable() {
		t.Fatal("invented an undocumented content endpoint")
	}
	manifest := tianYueChannelManifest()
	provider := manifest.Contributes.Providers[0]
	if manifest.Metadata.ID != "tianyue-channel" || manifest.Runtime.Backend != bundledHostProvidersRuntime ||
		provider.ID != "tianyue-video" || provider.BaseURL != "https://api.tianyue.xyz" ||
		provider.Auth.Type != "bearer" || !provider.RequiresPublicMediaURLs {
		t.Fatalf("manifest = %+v", manifest)
	}
	if !strings.Contains(manifest.Metadata.Documentation, "duration") || strings.Contains(manifest.Metadata.Documentation, "{{") {
		t.Fatal("missing rendered billing contract")
	}
}
