package protocol

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestYU25SeedanceManifestBuildsCanonicalVideoRequest(t *testing.T) {
	adapter := yu25SeedanceManifestAdapter(t)
	request := GenerationRequest{
		Capability:  CapabilityVideo,
		Model:       "seedance-2.5-pro",
		Prompt:      "保持人物身份一致，镜头缓慢推进",
		Duration:    8,
		AspectRatio: "16:9",
		Resolution:  "720p",
		Images: []MediaReference{
			{URL: "https://cdn.example/second.png", Order: 2, Role: "reference_image"},
			{URL: "https://cdn.example/first.png", Order: 1, Role: "reference_image"},
		},
		Videos: []MediaReference{{URL: "https://cdn.example/motion.mp4", Order: 1}},
		Audios: []MediaReference{{URL: "https://cdn.example/voice.wav", Order: 1}},
	}
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Method != "POST" || spec.Path != "/v1/videos" || spec.ContentType != "application/json" || spec.Auth.Type != "bearer" {
		t.Fatalf("create spec = %#v", spec)
	}
	body := manifestTestBody(t, spec)
	if body["model"] != request.Model || body["prompt"] != request.Prompt || body["seconds"] != float64(8) || body["resolution"] != "720p" || body["aspect_ratio"] != "16:9" {
		t.Fatalf("canonical fields = %#v", body)
	}
	images, _ := body["images"].([]any)
	if len(images) != 2 || images[0] != "https://cdn.example/first.png" || images[1] != "https://cdn.example/second.png" {
		t.Fatalf("ordered images = %#v", images)
	}
	videos, _ := body["video_urls"].([]any)
	audios, _ := body["audio_urls"].([]any)
	if len(videos) != 1 || videos[0] != "https://cdn.example/motion.mp4" || len(audios) != 1 || audios[0] != "https://cdn.example/voice.wav" {
		t.Fatalf("reference media = videos:%#v audios:%#v", videos, audios)
	}
	for _, forbidden := range []string{"duration", "ratio", "size", "image_refs", "video_refs", "audio_refs"} {
		if _, ok := body[forbidden]; ok {
			t.Fatalf("duplicate alias %q leaked into request: %#v", forbidden, body)
		}
	}
}

func TestYU25SeedanceManifestParsesStatusesAndNestedResults(t *testing.T) {
	adapter := yu25SeedanceManifestAdapter(t)
	ctx := context.Background()
	cases := []struct {
		name   string
		body   string
		status Status
		url    string
	}{
		{name: "queued", body: `{"request_id":"task-queued","status":"queued","video_url":"https://cdn.example/not-ready.mp4"}`, status: StatusPending},
		{name: "processing", body: `{"id":"task-processing","state":"in_progress"}`, status: StatusProcessing},
		{name: "success nested", body: `{"data":{"task_id":"task-success","status":"completed","data":{"url":"https://cdn.example/result.mp4"}}}`, status: StatusSucceeded, url: "https://cdn.example/result.mp4"},
		{name: "rejected", body: `{"id":"task-rejected","status":"rejected","error":{"message":"model_not_found"}}`, status: StatusFailed},
		{name: "cancelled", body: `{"id":"task-cancelled","status":"canceled"}`, status: StatusCancelled},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result, err := adapter.ParsePoll(ctx, PollContext{TaskID: "fallback-task"}, []byte(test.body))
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != test.status {
				t.Fatalf("status = %q, want %q; result = %#v", result.Status, test.status, result)
			}
			if test.url == "" {
				if result.Result != nil {
					t.Fatalf("non-terminal result = %#v", result.Result)
				}
				return
			}
			if result.TaskID != "task-success" || result.Result == nil || len(result.Result.Videos) != 1 || result.Result.Videos[0].URL != test.url {
				t.Fatalf("success result = %#v", result)
			}
		})
	}
}

func TestYU25SeedanceManifestUsesAuthenticatedContentFallback(t *testing.T) {
	adapter := yu25SeedanceManifestAdapter(t)
	resultAdapter, ok := adapter.(ResultAdapter)
	if !ok {
		t.Fatal("YU25 adapter does not implement ResultAdapter")
	}
	result, err := resultAdapter.BuildResult(context.Background(), PollContext{TaskID: "task/content with slash"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Method != "GET" || result.Path != "/v1/videos/task%2Fcontent%20with%20slash/content" || result.Headers["Accept"] != "video/mp4" || result.Auth.Type != "bearer" {
		t.Fatalf("result request = %#v", result)
	}
}

func TestYU25SeedanceManifestRejectsMissingRequiredInput(t *testing.T) {
	adapter := yu25SeedanceManifestAdapter(t)
	_, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: "seedance-2.5-pro", Prompt: "", Duration: 0}})
	if err == nil || !strings.Contains(err.Error(), "模型名和提示词") {
		t.Fatalf("missing input error = %v", err)
	}
	_, err = adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: "seedance-2.5-pro", Prompt: "clip", Duration: 8, AspectRatio: "2:1"}})
	if err == nil || !strings.Contains(err.Error(), "画幅") {
		t.Fatalf("invalid ratio error = %v", err)
	}
}

func yu25SeedanceManifestAdapter(t *testing.T) Adapter {
	t.Helper()
	path := filepath.Join("..", "..", "..", "plugin-packages", "yu25-seedance-video", "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := LoadManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func TestYU25SeedanceManifestHasNoUnexpectedCreateAliases(t *testing.T) {
	adapter := yu25SeedanceManifestAdapter(t)
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: "SD 2.0", Prompt: "clip", Duration: 10, Output: OutputOptions{Duration: 10, AspectRatio: "16:9", Resolution: "720p"}}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(spec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "video_duration") || strings.Contains(string(encoded), "referenceVideos") || strings.Contains(string(encoded), "referenceAudios") {
		t.Fatalf("legacy alias leaked into body: %s", encoded)
	}
}
