package protocol

import (
	"context"
	"strings"
	"testing"
)

func TestPaipuV2VideoModelsUseCapabilitySpecificBodies(t *testing.T) {
	ctx := context.Background()
	adapter := officialPackageAdapter(t, "paipu-net.yingce-plugin", "paipu-net-video")
	request := GenerationRequest{
		Prompt: "video", Duration: 8, AspectRatio: "16:9", Resolution: "1080p",
		Images: []MediaReference{{URL: "https://example.com/reference.png"}},
		Videos: []MediaReference{{URL: "https://example.com/motion.mp4"}},
		Audios: []MediaReference{{URL: "https://example.com/voice.wav"}},
	}

	request.Model = "lec-h3video-2k"
	spec, err := adapter.BuildCreate(ctx, RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	h3 := manifestTestBody(t, spec)
	if h3["model"] != "lec-h3video-2k" || h3["prompt"] != "video" || h3["aspect_ratio"] != "16:9" {
		t.Fatalf("H3 body = %#v", h3)
	}
	for _, forbidden := range []string{"duration", "resolution", "videos", "audios"} {
		if _, ok := h3[forbidden]; ok {
			t.Fatalf("H3 body contains unsupported %q: %#v", forbidden, h3)
		}
	}

	request.Model = "unknown-video-model"
	spec, err = adapter.BuildCreate(ctx, RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	unknown := manifestTestBody(t, spec)
	if len(unknown) != 2 || unknown["model"] != "unknown-video-model" || unknown["prompt"] != "video" {
		t.Fatalf("unknown model body = %#v", unknown)
	}
}

func TestPaipuV2FaceModelValidatesReferencesAndDefaultsMode(t *testing.T) {
	ctx := context.Background()
	adapter := officialPackageAdapter(t, "paipu-net.yingce-plugin", "paipu-net-video")
	request := GenerationRequest{Model: "lec-ty-face-processing-1-0", Prompt: "must not be sent", AspectRatio: "1:1"}

	if _, err := adapter.BuildCreate(ctx, RequestContext{Request: request}); err == nil || !strings.Contains(err.Error(), "至少提供 1 张") {
		t.Fatalf("missing face reference error = %v", err)
	}

	request.Images = []MediaReference{{URL: "https://example.com/one.png"}, {URL: "https://example.com/two.png"}}
	if _, err := adapter.BuildCreate(ctx, RequestContext{Request: request}); err == nil || !strings.Contains(err.Error(), "恰好提供 1 张") {
		t.Fatalf("multiple face references error = %v", err)
	}

	request.Images = request.Images[:1]
	spec, err := adapter.BuildCreate(ctx, RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	body := manifestTestBody(t, spec)
	if body["mode"] != "黑白素描1" || body["model"] != request.Model || body["prompt"] != nil {
		t.Fatalf("face model body = %#v", body)
	}
}

func TestPaipuV2PollStatesAndResultPaths(t *testing.T) {
	ctx := context.Background()
	adapter := officialPackageAdapter(t, "paipu-net.yingce-plugin", "paipu-net-video")
	for _, test := range []struct {
		name   string
		body   string
		status Status
	}{
		{name: "reference materializing", body: `{"id":"task","status":"REFERENCE_MATERIALIZING"}`, status: StatusProcessing},
		{name: "submitting", body: `{"id":"task","status":"SUBMITTING"}`, status: StatusProcessing},
		{name: "upstream processing", body: `{"id":"task","status":"UPSTREAM_PROCESSING"}`, status: StatusProcessing},
		{name: "result storage wait", body: `{"id":"task","status":"RESULT_STORAGE_WAIT"}`, status: StatusProcessing},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := adapter.ParsePoll(ctx, PollContext{TaskID: "fallback"}, []byte(test.body))
			if err != nil || result.Status != test.status || result.Result != nil {
				t.Fatalf("result=%#v err=%v", result, err)
			}
		})
	}

	unknown, err := adapter.ParsePoll(ctx, PollContext{TaskID: "fallback"}, []byte(`{"id":"task","status":"future_state"}`))
	if err != nil || unknown.Status != StatusFailed || !strings.Contains(unknown.Message, "future_state") {
		t.Fatalf("unknown status result=%#v err=%v", unknown, err)
	}

	empty, err := adapter.ParsePoll(ctx, PollContext{TaskID: "fallback"}, []byte(`{"id":"task","status":"completed"}`))
	if err != nil || empty.Status != StatusFailed || !strings.Contains(empty.Message, "没有返回结果") {
		t.Fatalf("empty completed result=%#v err=%v", empty, err)
	}

	for _, test := range []struct {
		body string
		url  string
	}{
		{body: `{"id":"task","status":"completed","metadata":{"url":"https://example.com/metadata.mp4"}}`, url: "https://example.com/metadata.mp4"},
		{body: `{"id":"task","status":"completed","data":[{"url":"https://example.com/data.mp4"}]}`, url: "https://example.com/data.mp4"},
	} {
		result, err := adapter.ParsePoll(ctx, PollContext{TaskID: "fallback"}, []byte(test.body))
		if err != nil || result.Status != StatusSucceeded || result.Result == nil || len(result.Result.Videos) != 1 || result.Result.Videos[0].URL != test.url {
			t.Fatalf("nested result=%#v err=%v", result, err)
		}
	}
}
