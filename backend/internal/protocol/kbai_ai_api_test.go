package protocol

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestKBAIPluginVideoAndImageContracts(t *testing.T) {
	packagePath := filepath.Join("..", "..", "..", "plugin-packages", "kbai-ai-api.yingce-plugin")
	data, err := os.ReadFile(packagePath)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := ParsePluginPackage(data)
	if err != nil {
		t.Fatal(err)
	}
	adapters, err := LoadInstalledProviders(pkg.ManifestRaw, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(adapters) != 2 {
		t.Fatalf("KBAI adapters = %d, want 2", len(adapters))
	}

	var video, image Adapter
	for _, adapter := range adapters {
		switch adapter.Metadata().ID {
		case "kbai-video":
			video = adapter
		case "kbai-image":
			image = adapter
		}
	}
	if video == nil || image == nil {
		t.Fatalf("KBAI adapters = %#v", adapters)
	}

	videoRequest := GenerationRequest{
		Model: "your-video-model", Prompt: "电影感城市街道", Duration: 5,
		AspectRatio: "9:16", Resolution: "720p",
		Images: []MediaReference{{URL: "https://cdn.example/person.png", Order: 1}},
		Videos: []MediaReference{{URL: "https://cdn.example/action.mp4", Order: 0}},
		Audios: []MediaReference{{URL: "https://cdn.example/voice.mp3", Order: 0}},
	}
	create, err := video.BuildCreate(context.Background(), RequestContext{Request: videoRequest})
	if err != nil {
		t.Fatal(err)
	}
	if create.Method != "POST" || create.Path != "/v1/videos" || create.Auth.Type != "bearer" {
		t.Fatalf("video create = %#v", create)
	}
	body := manifestTestBody(t, create)
	if body["model"] != videoRequest.Model || body["prompt"] != videoRequest.Prompt || body["aspect_ratio"] != "9:16" || body["seconds"] != float64(5) || body["resolution"] != "720p" {
		t.Fatalf("video body = %#v", body)
	}
	for key, want := range map[string]string{"reference_image_urls": "https://cdn.example/person.png", "reference_videos": "https://cdn.example/action.mp4", "reference_audios": "https://cdn.example/voice.mp3"} {
		values, ok := body[key].([]any)
		if !ok || len(values) != 1 || values[0] != want {
			t.Fatalf("video %s = %#v", key, body[key])
		}
	}

	created, err := video.ParseCreate(context.Background(), []byte(`{"id":"65","status":"queued","task_id":65}`))
	if err != nil || created.TaskID != "65" || created.Status != StatusPending {
		t.Fatalf("video create response = %#v, err = %v", created, err)
	}
	poll, err := video.BuildPoll(context.Background(), PollContext{TaskID: created.TaskID})
	if err != nil || poll.Path != "/v1/videos/65" {
		t.Fatalf("video poll = %#v, err = %v", poll, err)
	}
	completed, err := video.ParsePoll(context.Background(), PollContext{TaskID: "65"}, []byte(`{"id":"65","status":"succeeded","download_url":"https://oss.example/video.mp4"}`))
	if err != nil || completed.Status != StatusSucceeded || completed.Result == nil || len(completed.Result.Videos) != 1 || completed.Result.Videos[0].URL != "https://oss.example/video.mp4" || !completed.Result.Videos[0].Ephemeral {
		t.Fatalf("video completed = %#v, err = %v", completed, err)
	}
	failed, err := video.ParsePoll(context.Background(), PollContext{TaskID: "65"}, []byte(`{"status":"failed","api_error":{"message":"积分不足","code":"insufficient_credits"}}`))
	if err != nil || failed.Status != StatusFailed || failed.Message != "积分不足" {
		t.Fatalf("video failed = %#v, err = %v", failed, err)
	}

	imageCreate, err := image.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: "your-image-model", Prompt: "一只橘猫", AspectRatio: "1:1", Quality: "1K"}})
	if err != nil {
		t.Fatal(err)
	}
	if imageCreate.Path != "/v1/images/generations" || imageCreate.ContentType != "application/json" || len(imageCreate.Files) != 0 {
		t.Fatalf("image generation = %#v", imageCreate)
	}
	imageBody := manifestTestBody(t, imageCreate)
	if imageBody["size"] != "1024x1024" || imageBody["quality"] != "1K" || imageBody["n"] != float64(1) {
		t.Fatalf("image body = %#v", imageBody)
	}

	imageEdit, err := image.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Model: "your-image-model", Prompt: "参考输入图片生成海报", AspectRatio: "3:4", Images: []MediaReference{{URL: "https://cdn.example/input.png"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if imageEdit.Path != "/v1/images/edits" || imageEdit.ContentType != "multipart/form-data" || len(imageEdit.Files) != 1 || imageEdit.Files[0].Name != "image" {
		t.Fatalf("image edit = %#v", imageEdit)
	}
	editBody := manifestTestBody(t, imageEdit)
	if editBody["size"] != "1024x1536" || imageEdit.Files[0].Reference.URL != "https://cdn.example/input.png" {
		t.Fatalf("image edit payload = body:%#v files:%#v", editBody, imageEdit.Files)
	}
	if _, err := image.BuildCreate(context.Background(), RequestContext{Request: GenerationRequest{Images: []MediaReference{{URL: "https://cdn.example/one.png"}, {URL: "https://cdn.example/two.png"}}}}); err == nil {
		t.Fatal("image edit accepted more than one input image")
	}

	result, err := image.ParseCreate(context.Background(), []byte(`{"data":[{"url":"https://oss.example/image.png"}]}`))
	if err != nil || result.Status != StatusSucceeded || result.Result == nil || len(result.Result.Images) != 1 || result.Result.Images[0].URL != "https://oss.example/image.png" {
		t.Fatalf("image response = %#v, err = %v", result, err)
	}

	encoded, err := json.Marshal(pkg.Manifest)
	if err != nil || len(encoded) == 0 {
		t.Fatalf("manifest marshal failed: %v", err)
	}
}
