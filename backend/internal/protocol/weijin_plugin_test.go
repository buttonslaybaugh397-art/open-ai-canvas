package protocol

import (
	"context"
	"reflect"
	"testing"
)

func TestWeijinVideoAdapterMapsOfficialRequest(t *testing.T) {
	adapter, ok := Builtins().Get("weijin-video")
	if !ok {
		t.Fatal("weijin video adapter is not registered")
	}
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
		Model: "seedance2.0-one-full-flex-720p", Prompt: "城市夜景", Duration: 15, AspectRatio: "16:9", Resolution: "720p",
		Images: []MediaReference{{URL: "https://example.com/image.jpg"}},
		Videos: []MediaReference{{URL: "https://example.com/video.mp4"}},
		Audios: []MediaReference{{URL: "https://example.com/audio.mp3"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	body := spec.Body.(map[string]any)
	if spec.Path != "/v1/videos" || body["seconds"] != 15 || body["aspect_ratio"] != "16:9" {
		t.Fatalf("request = %#v", spec)
	}
	if !reflect.DeepEqual(body["images"], []string{"https://example.com/image.jpg"}) || !reflect.DeepEqual(body["videos"], []string{"https://example.com/video.mp4"}) || !reflect.DeepEqual(body["audios"], []string{"https://example.com/audio.mp3"}) {
		t.Fatalf("media fields = %#v", body)
	}
	if body["duration_seconds"] != nil || body["size"] != nil || body["resolution"] != nil {
		t.Fatalf("legacy or fixed-model fields must not be sent: %#v", body)
	}

	empty, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: "video-model", Prompt: "test", Duration: 5, Resolution: "auto"}})
	if err != nil {
		t.Fatal(err)
	}
	emptyBody := empty.Body.(map[string]any)
	for _, field := range []string{"images", "videos", "audios", "resolution"} {
		if _, exists := emptyBody[field]; exists {
			t.Fatalf("empty optional field %q was not omitted: %#v", field, emptyBody)
		}
	}

	flexible, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: "future-flexible-video", Prompt: "test", Duration: 5, Resolution: "1080p"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := flexible.Body.(map[string]any)["resolution"]; got != "1080p" {
		t.Fatalf("flexible model resolution = %#v", got)
	}
}

func TestWeijinVideoAdapterPreservesOfficialStates(t *testing.T) {
	adapter, _ := Builtins().Get("weijin-video")
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"id":"task-1","task_id":"task-1","status":"queued"}`))
	if err != nil || created.TaskID != "task-1" || created.Status != StatusPending {
		t.Fatalf("created = %#v, err = %v", created, err)
	}
	processing, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: "task-1"}, []byte(`{"id":"task-1","status":"in_progress","progress":50}`))
	if err != nil || processing.Status != StatusProcessing {
		t.Fatalf("processing = %#v, err = %v", processing, err)
	}
	completed, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: "task-1"}, []byte(`{"id":"task-1","status":"completed","result_url":"https://www.weijinapi.top/v1/videos/task-1/content"}`))
	if err != nil || completed.Status != StatusSucceeded || completed.Result == nil || completed.Result.Videos[0].URL != "https://www.weijinapi.top/v1/videos/task-1/content" {
		t.Fatalf("completed = %#v, err = %v", completed, err)
	}
	failed, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: "task-1"}, []byte(`{"id":"task-1","status":"failed","result_url":"https://example.com/stale.mp4","error":{"code":"generation_failed","message":"video generation failed"}}`))
	if err != nil || failed.Status != StatusFailed || failed.Message != "video generation failed" || failed.Result != nil {
		t.Fatalf("failed = %#v, err = %v", failed, err)
	}
	if _, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: "task-1"}, []byte(`{"id":"task-1","status":"completed"}`)); err == nil {
		t.Fatal("completed response without result URL must fail")
	}
}

func TestWeijinBundledManifestDeclaresBearerProvider(t *testing.T) {
	for _, manifest := range BundledHostManifests() {
		if manifest.Metadata.ID != "weijin-channel" {
			continue
		}
		if len(manifest.Contributes.Providers) != 1 {
			t.Fatalf("providers = %#v", manifest.Contributes.Providers)
		}
		provider := manifest.Contributes.Providers[0]
		if provider.ID != "weijin-video" || provider.BaseURL != "https://www.weijinapi.top" || provider.Auth.Type != "bearer" || !provider.RequiresPublicMediaURLs {
			t.Fatalf("provider = %#v", provider)
		}
		return
	}
	t.Fatal("weijin bundled manifest is missing")
}
