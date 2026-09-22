package protocol

import (
	"context"
	"strings"
	"testing"
)

func TestPaipuV2PreservesMediaRequestsAndPolling(t *testing.T) {
	ctx := context.Background()
	image := officialPackageAdapter(t, "paipu-net.yingce-plugin", "paipu-net-image")
	spec, err := image.BuildCreate(ctx, RequestContext{Request: GenerationRequest{
		Model: "lec-ac-image-2-5-flare", Prompt: "image", Quality: "auto",
		Images: []MediaReference{{URL: "https://example.com/reference.png"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	body := manifestTestBody(t, spec)
	if spec.Path != "/v1/images/generations" || body["model"] != "lec-ac-image-2-5-flare" || body["response_format"] != "url" || body["output_format"] != "jpeg" || body["quality"] != nil || body["n"] != nil {
		t.Fatalf("image mapping changed: %#v", body)
	}
	images, ok := body["images"].([]any)
	if !ok || len(images) != 1 || images[0] != "https://example.com/reference.png" {
		t.Fatalf("image references = %#v", body["images"])
	}
	result, err := image.ParseCreate(ctx, []byte(`{"data":[{"url":"https://example.com/result.png"}]}`))
	if err != nil || result.Status != StatusSucceeded || result.Result == nil || len(result.Result.Images) != 1 {
		t.Fatalf("image result=%#v err=%v", result, err)
	}

	video := officialPackageAdapter(t, "paipu-net.yingce-plugin", "paipu-net-video")
	spec, err = video.BuildCreate(ctx, RequestContext{Request: GenerationRequest{Model: "lec-vg-seedance-2-5-kk", Prompt: "video", Duration: 8, AspectRatio: "16:9", Resolution: "720p", Images: []MediaReference{{URL: "https://example.com/reference.png"}}, ProviderOptions: map[string]map[string]any{"paipu-net-video": {"duration": 12, "aspect_ratio": "9:16", "resolution": "1080p"}}}})
	if err != nil {
		t.Fatal(err)
	}
	body = manifestTestBody(t, spec)
	if spec.Path != "/v1/videos" || body["duration"] != float64(12) || body["aspect_ratio"] != "9:16" || body["resolution"] != "1080p" || body["model"] != "lec-vg-seedance-2-5-kk" {
		t.Fatalf("video mapping changed: %#v", body)
	}
	pollContext := PollContext{TaskID: "task-123"}
	poll, err := video.BuildPoll(ctx, pollContext)
	if err != nil || poll.Path != "/v1/videos/task-123" {
		t.Fatalf("poll=%#v err=%v", poll, err)
	}
	pending, err := video.ParsePoll(ctx, pollContext, []byte(`{"id":"task-123","status":"in_progress"}`))
	if err != nil || pending.Status != StatusProcessing || pending.Result != nil {
		t.Fatalf("pending=%#v err=%v", pending, err)
	}
	completed, err := video.ParsePoll(ctx, pollContext, []byte(`{"id":"task-123","status":"completed","metadata":{"url":"https://example.com/result.mp4"}}`))
	if err != nil || completed.Status != StatusSucceeded || completed.Result == nil || len(completed.Result.Videos) != 1 {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
}

func TestPaipuV2PreservesTextAndAgentRequests(t *testing.T) {
	ctx := context.Background()
	text := officialPackageAdapter(t, "paipu-net.yingce-plugin", "paipu-net-text")
	spec, err := text.BuildCreate(ctx, RequestContext{Request: GenerationRequest{Model: "text-model", Prompt: "hello", Extra: map[string]any{"temperature": 0, "top_p": 0.9, "max_completion_tokens": 128}}})
	if err != nil {
		t.Fatal(err)
	}
	body := manifestTestBody(t, spec)
	if spec.Path != "/v1/chat/completions" || body["temperature"] != nil || body["top_p"] != 0.9 || body["max_completion_tokens"] != float64(128) || body["messages"] == nil {
		t.Fatalf("text mapping changed: %#v", body)
	}
	agent, ok := text.(AgentAdapter)
	if !ok {
		t.Fatal("text provider lost agent capability")
	}
	agentSpec, err := agent.BuildAgent(ctx, AgentRequestContext{Model: "text-model", Request: map[string]any{"chatCompletion": map[string]any{
		"messages": []any{map[string]any{"role": "user", "content": "inspect"}},
		"tools":    []any{map[string]any{"type": "function"}}, "tool_choice": "required",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	agentBody := manifestTestBody(t, agentSpec)
	if agentBody["messages"] == nil || agentBody["tools"] == nil || agentBody["tool_choice"] != "required" {
		t.Fatalf("agent mapping changed: %#v", agentBody)
	}
}

func TestPaipuV2ImageModelsUseOnlySupportedParameters(t *testing.T) {
	ctx := context.Background()
	adapter := officialPackageAdapter(t, "paipu-net.yingce-plugin", "paipu-net-image")
	baseRequest := GenerationRequest{
		Prompt:      "image",
		AspectRatio: "3:2",
		Resolution:  "2K",
		Quality:     "high",
		ImageCount:  4,
		Images:      []MediaReference{{URL: "https://example.com/reference.png"}},
		ProviderOptions: map[string]map[string]any{"paipu-net-image": {
			"aspect_ratio": "1:1", "resolution": "4K", "quality": "low", "n": 2,
			"output_format": "png", "response_format": "url",
		}},
	}

	build := func(model string) map[string]any {
		t.Helper()
		request := baseRequest
		request.Model = model
		spec, err := adapter.BuildCreate(ctx, RequestContext{Request: request})
		if err != nil {
			t.Fatal(err)
		}
		return manifestTestBody(t, spec)
	}

	banana := build("lec-ac-banana-flash")
	if banana["model"] != "lec-ac-banana-flash" || banana["aspect_ratio"] != "1:1" || banana["resolution"] != "4K" {
		t.Fatalf("banana body = %#v", banana)
	}
	for _, forbidden := range []string{"quality", "n", "output_format", "response_format"} {
		if _, ok := banana[forbidden]; ok {
			t.Fatalf("banana body contains unsupported %q: %#v", forbidden, banana)
		}
	}

	tinySnow := build("lec-tinysnow-image-2")
	if tinySnow["model"] != "lec-tinysnow-image-2" || tinySnow["n"] != float64(2) || tinySnow["quality"] != "low" || tinySnow["response_format"] != "b64_json" {
		t.Fatalf("TinySnow body = %#v", tinySnow)
	}
	if tinySnow["aspect_ratio"] != "1:1" || tinySnow["resolution"] != "4K" {
		t.Fatalf("TinySnow provider options were not applied: %#v", tinySnow)
	}

	result, err := adapter.ParseCreate(ctx, []byte(`{"data":[{"b64_json":"aW1hZ2U="}]}`))
	if err != nil || result.Status != StatusSucceeded || result.Result == nil || len(result.Result.Images) != 1 {
		t.Fatalf("TinySnow result=%#v err=%v", result, err)
	}
	if got := result.Result.Images[0].DataURL; !strings.HasPrefix(got, "data:image/") || !strings.HasSuffix(got, "aW1hZ2U=") {
		t.Fatalf("TinySnow DataURL = %q", got)
	}
}
