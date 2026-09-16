package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/protocol"
)

func TestTianYueHostMediaPolicyAndFieldMapping(t *testing.T) {
	center, err := newPluginRuntime(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := withProtocolRegistry(context.Background(), center.registrySnapshot())
	input := canvasGenerationInput{
		Mode: "video", Prompt: "test",
		Config:          providerConfig{InterfaceType: "tianyue-video", Model: "test-model-se", VideoSeconds: "8", VQuality: "1080p", Size: "9:16"},
		ReferenceImages: []providerMedia{{URL: "https://203.0.113.10/image.jpg"}},
		ReferenceVideos: []providerMedia{{URL: "https://203.0.113.10/video.mp4"}},
		ReferenceAudios: []providerMedia{{URL: "https://203.0.113.10/audio.mp3"}},
	}
	policy := providerMediaHydrationPolicyFor(ctx, input)
	if !policy.requireURL || !policy.preferURL {
		t.Fatal("TianYue did not require URL hydration")
	}
	request, err := prepareCustomProtocolRequest(input)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := generationProtocolAdapterForContext(ctx, input.Config.InterfaceType)
	if err != nil || adapter == nil {
		t.Fatalf("host adapter=%v, error=%v", adapter, err)
	}
	spec, err := adapter.BuildCreate(ctx, protocol.RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"model": "test-model-se", "prompt": "test", "video_duration": 8, "duration": 8,
		"resolution": "1080p", "aspect_ratio": "9:16",
		"image_urls": []string{"https://203.0.113.10/image.jpg"},
		"video_urls": []string{"https://203.0.113.10/video.mp4"},
		"audio_urls": []string{"https://203.0.113.10/audio.mp3"},
	}
	if !reflect.DeepEqual(spec.Body, want) {
		t.Fatalf("request body = %#v", spec.Body)
	}
	if err := validateGenerationInterfaceWithRegistry(center.registrySnapshot(), "image", "tianyue-video"); err == nil {
		t.Fatal("video plugin advertised unimplemented image support")
	}
	var found bool
	for _, plugin := range center.list() {
		if plugin.Manifest.ID != "tianyue-channel" {
			continue
		}
		found = true
		provider := plugin.Manifest.Contributes.Providers[0]
		if plugin.Status != "enabled" || provider.Label != "天悦视频" || provider.BaseURL != "https://api.tianyue.xyz" ||
			!strings.Contains(plugin.Manifest.Documentation, "video_duration") {
			t.Fatalf("frontend plugin contract = %+v", plugin)
		}
	}
	if !found {
		t.Fatal("TianYue plugin was not installed on startup")
	}
}

func TestTianYueReferencesRejectNonPublicURLs(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "")
	for _, raw := range []string{
		"blob:https://canvas.example/ref", "data:image/png;base64,aW1hZ2U=", "file:///tmp/ref",
		"http://127.0.0.1/ref", "http://localhost/ref", "http://10.0.0.1/ref", "http://[::1]/ref",
		"https://user:password@cdn.example/ref", "https://",
	} {
		for _, kind := range []string{"image", "video", "audio"} {
			t.Run(kind+"/"+raw, func(t *testing.T) {
				input := canvasGenerationInput{Config: providerConfig{InterfaceType: "tianyue-video"}}
				media := []providerMedia{{URL: raw}}
				switch kind {
				case "image":
					input.ReferenceImages = media
				case "video":
					input.ReferenceVideos = media
				case "audio":
					input.ReferenceAudios = media
				}
				if _, err := prepareCustomProtocolRequest(input); err == nil {
					t.Fatal("non-public reference accepted")
				}
			})
		}
	}
}

func TestTianYueResumeOnlyQueriesExistingTask(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	var polls, downloads, creates int
	var upstream *httptest.Server
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			creates++
			http.Error(w, "unexpected create/upload", 500)
			return
		}
		switch r.URL.Path {
		case "/v1/videos/existing":
			polls++
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Error("query lost Bearer auth")
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"task_id":"existing","status":"completed","url":%q}`, upstream.URL+"/result")
		case "/result":
			downloads++
			if r.Header.Get("Authorization") != "Bearer test-key" {
				t.Error("same-origin result lost Bearer auth")
			}
			w.Header().Set("Content-Type", "video/mp4")
			fmt.Fprint(w, "test-media")
		default:
			t.Errorf("unexpected request: %s", r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	config := providerConfig{InterfaceType: "tianyue-video", BaseURL: upstream.URL, Model: "test-model", APIKey: "test-key"}
	ctx := context.Background()
	ctx = context.WithValue(ctx, providerAnalyticsKey{}, providerAnalyticsContext{ProviderRequestID: "existing"})
	adapter, _ := protocol.Builtins().Get("tianyue-video")
	// Expired creation inputs must not be revalidated, uploaded or submitted on resume.
	result, err := runProtocolAdapterTask(ctx, canvasGenerationInput{
		Mode: "video", Config: config,
		ReferenceImages: []providerMedia{{DataURL: "data:image/png;base64,aW1hZ2U="}},
	}, adapter)
	if err != nil || result["mode"] != "video" {
		t.Fatalf("result=%v error=%v", result, err)
	}
	if polls != 1 || downloads != 1 || creates != 0 {
		t.Fatalf("poll/download/create=%d/%d/%d", polls, downloads, creates)
	}
}

func TestTianYuePollTimingAndBaseURL(t *testing.T) {
	if policy := protocolPollPolicyFor("tianyue-video", defaultVideoPollPolicy()); policy.InitialDelay != 5*time.Second || policy.Interval != 5*time.Second {
		t.Fatalf("TianYue poll policy=%+v", policy)
	}
	if policy := protocolPollPolicyFor("weijin-video", defaultVideoPollPolicy()); policy.InitialDelay != 10*time.Second || policy.Interval != 10*time.Second {
		t.Fatalf("Weijin poll policy changed: %+v", policy)
	}
	for _, baseURL := range []string{"https://api.tianyue.xyz", "https://api.tianyue.xyz/", "https://api.tianyue.xyz/v1", "https://api.tianyue.xyz/v1/"} {
		for _, path := range []string{"/v1/videos", "/v1/videos/task-1"} {
			if got := ChannelAPIURLForProtocol(baseURL, path, model.ChannelInterfaceTianYueVideo); got != "https://api.tianyue.xyz"+path {
				t.Fatalf("base=%s path=%s got=%s", baseURL, path, got)
			}
		}
	}
}

func TestTianYueDefaultVideoOptions(t *testing.T) {
	profile := DefaultModelCapabilityConfigForModel("tianyue-video", "configured-model").Video
	if !reflect.DeepEqual(profile.Ratios, []string{"16:9", "9:16", "4:3", "3:4", "1:1"}) ||
		!reflect.DeepEqual(profile.Resolutions, []string{"480p", "720p", "1080p"}) {
		t.Fatalf("TianYue protocol options=%+v", profile)
	}
	config := &ModelCapabilityConfig{Version: 1, Video: profile}
	profile.Duration = VideoDurationConfig{Selection: "enum", Values: []int{20}, Default: 20}
	profile.References.MaxImages = 12
	normalized, err := NormalizeModelCapabilityConfigForModel("video", "tianyue-video", "configured-model", config)
	if err != nil || normalized.Video.Duration.Default != 20 || normalized.Video.References.MaxImages != 12 {
		t.Fatalf("configured model limits were overwritten: %+v, %v", normalized, err)
	}
}
