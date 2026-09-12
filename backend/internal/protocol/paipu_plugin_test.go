package protocol

import (
	"context"
	"testing"
)

func TestPaipuV2PreservesMediaRequestsAndPolling(t *testing.T) {
	ctx := context.Background()
	image := officialPackageAdapter(t, "paipu-net.yingce-plugin", "paipu-net-image")
	spec, err := image.BuildCreate(ctx, RequestContext{Request: GenerationRequest{
		Model: "image-model", Prompt: "image", Quality: "auto",
		Images: []MediaReference{{URL: "https://example.com/reference.png"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	body := manifestTestBody(t, spec)
	if spec.Path != "/v1/images/generations" || body["response_format"] != "url" || body["quality"] != nil || body["n"] != nil {
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
	spec, err = video.BuildCreate(ctx, RequestContext{Request: GenerationRequest{Model: "video-model", Prompt: "video", Duration: 8, AspectRatio: "16:9", Resolution: "720p"}})
	if err != nil {
		t.Fatal(err)
	}
	body = manifestTestBody(t, spec)
	if spec.Path != "/v1/videos" || body["duration"] != float64(8) || body["aspect_ratio"] != "16:9" || body["resolution"] != "720p" {
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
