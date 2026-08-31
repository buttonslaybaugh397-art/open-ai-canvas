package protocol

import (
	"context"
	"testing"
)

func TestAIStarsLabVideoAdapterBuildsContractRequest(t *testing.T) {
	adapter, ok := Builtins().Get("aistarslab-video")
	if !ok {
		t.Fatal("aistarslab video adapter is not registered")
	}

	tests := []struct {
		name     string
		images   []MediaReference
		wantMode string
	}{
		{name: "text to video", wantMode: "text2video"},
		{name: "image to video", images: []MediaReference{{URL: "https://cdn.example/start.png"}}, wantMode: "image2video"},
		{name: "frames to video", images: []MediaReference{{URL: "https://cdn.example/start.png"}, {URL: "https://cdn.example/end.png"}}, wantMode: "frames2video"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{
				Model: "seedance-2.5", Prompt: "a cinematic shot", Duration: 10, AspectRatio: "16:9", Resolution: "720p",
				Images: test.images, Extra: map[string]any{"aistarslabChannel": "54", "aistarslabModel": "seedance-2.5", "aistarslabMode": test.wantMode},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if spec.Method != "POST" || spec.Path != "/generation/create/video" {
				t.Fatalf("create spec = %#v", spec)
			}
			body, ok := spec.Body.(map[string]any)
			if !ok || body["channel"] != "54" || body["model"] != "seedance-2.5" || body["mode"] != test.wantMode || body["duration"] != 10 {
				t.Fatalf("create body = %#v", spec.Body)
			}
		})
	}
}

func TestAIStarsLabAdapterParsesNumericStatusAndOutputs(t *testing.T) {
	video, ok := Builtins().Get("aistarslab-video")
	if !ok {
		t.Fatal("aistarslab video adapter is not registered")
	}
	created, err := video.ParseCreate(context.Background(), []byte(`{"code":0,"data":{"taskId":"task-1","status":1}}`))
	if err != nil || created.TaskID != "task-1" || created.Status != StatusPending {
		t.Fatalf("created = %#v, err = %v", created, err)
	}
	processing, err := video.ParsePoll(context.Background(), PollContext{TaskID: created.TaskID}, []byte(`{"code":0,"data":{"taskId":"task-1","status":2}}`))
	if err != nil || processing.Status != StatusProcessing {
		t.Fatalf("processing = %#v, err = %v", processing, err)
	}
	completed, err := video.ParsePoll(context.Background(), PollContext{TaskID: created.TaskID}, []byte(`{"code":0,"data":{"taskId":"task-1","status":3,"outputs":["https://cdn.example/result.mp4"]}}`))
	if err != nil || completed.Status != StatusSucceeded || completed.Result == nil || len(completed.Result.Videos) != 1 || completed.Result.Videos[0].URL != "https://cdn.example/result.mp4" {
		t.Fatalf("completed = %#v, err = %v", completed, err)
	}
	failed, err := video.ParsePoll(context.Background(), PollContext{TaskID: created.TaskID}, []byte(`{"code":0,"data":{"taskId":"task-1","status":4,"errorMessage":"quota exceeded"}}`))
	if err != nil || failed.Status != StatusFailed || failed.Message != "quota exceeded" {
		t.Fatalf("failed = %#v, err = %v", failed, err)
	}
}

func TestAIStarsLabBundledManifestContainsResponseMapping(t *testing.T) {
	var found bool
	for _, manifest := range BundledHostManifests() {
		if manifest.Metadata.ID != "aistarslab-channel" {
			continue
		}
		for _, provider := range manifest.Contributes.Providers {
			if provider.ID == "aistarslab-video" {
				found = true
				if provider.Create.Path != "/generation/create/video" || provider.Response.StatusPaths[0] != "data.status" || provider.Response.ResultPaths[0] != "data.outputs" {
					t.Fatalf("provider = %#v", provider)
				}
			}
		}
	}
	if !found {
		t.Fatal("aistarslab video provider is missing from bundled manifest")
	}
}
