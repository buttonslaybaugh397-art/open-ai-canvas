package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestIsAIStarsLabBaseURL(t *testing.T) {
	for _, value := range []string{
		"https://api.video.aistarslab.com/openapi",
		"https://api.video.aistarslab.com/openapi/",
	} {
		if !isAIStarsLabBaseURL(value) {
			t.Fatalf("isAIStarsLabBaseURL(%q) = false", value)
		}
	}
	if isAIStarsLabBaseURL("https://api.video.aistarslab.com/v1") {
		t.Fatal("non-openapi path must not use the AIStarsLab catalog adapter")
	}
}

func TestFetchAIStarsLabCatalogSupportsCurrentDurationContract(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/openapi/generation/config" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		if request.Header.Get("Accept") != "application/json" {
			t.Fatalf("accept = %q", request.Header.Get("Accept"))
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"code":0,"msg":"success","data":{"imageConfig":[],"videoConfig":[{"channel":"seedance","title":"Seedance","defaultOption":true,"models":[{"model":"seedance-2.5","label":"Seedance 2.5","qualities":[{"quality":"720P"}],"modes":["text2video","image2video"],"aspectRatios":["16:9","9:16"],"duration":{"min":4,"max":30,"options":null},"inputImagesMax":30,"inputVideosMax":10,"inputAudiosMax":10}]}]}}`))
	}))
	defer upstream.Close()

	items, err := (&Service{}).fetchAIStarsLabCatalog(context.Background(), upstream.URL+"/openapi", "test-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].AIStarsLab == nil || items[0].ID != "seedance:seedance-2.5" {
		t.Fatalf("items = %#v", items)
	}
	route := items[0].AIStarsLab
	if route.DurationMin != 4 || route.DurationMax != 30 || len(route.Duration) != 0 || route.InputImagesMax != 30 || route.InputVideosMax != 10 || route.InputAudiosMax != 10 {
		t.Fatalf("route = %#v", route)
	}
	if strings.Join(route.Qualities, ",") != "720p" {
		t.Fatalf("qualities = %#v", route.Qualities)
	}
}

func TestFetchAIStarsLabCatalogKeepsEveryRouteForDuplicateModel(t *testing.T) {
	t.Setenv("CANVAS_ALLOW_PRIVATE_UPSTREAMS", "true")
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"code":0,"msg":"success","data":{"imageConfig":[{"channel":"47","title":"线路A","defaultOption":"false","models":[{"model":"gpt-image-2","label":"GPT Image 2","qualities":[{"quality":"1K"},{"quality":"2K"}],"aspectRatios":["1:1"],"inputImagesMax":1}]},{"channel":"54","title":"线路B","defaultOption":true,"models":[{"model":"gpt-image-2","label":"GPT Image 2","qualities":[{"quality":"2K"}],"aspectRatios":["16:9"],"inputImagesMax":9}]}],"videoConfig":[]}}`))
	}))
	defer upstream.Close()

	items, err := (&Service{}).fetchAIStarsLabCatalog(context.Background(), upstream.URL+"/openapi", "test-key", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].ID != "47:gpt-image-2" || items[0].DisplayName != "GPT Image 2（线路A）" || items[0].AIStarsLab.InputImagesMax != 1 {
		t.Fatalf("items[0] = %#v", items[0])
	}
	if items[1].ID != "54:gpt-image-2" || items[1].DisplayName != "GPT Image 2（线路B）" || items[1].AIStarsLab.InputImagesMax != 9 {
		t.Fatalf("items[1] = %#v", items[1])
	}
	if strings.Join(items[0].AIStarsLab.Qualities, ",") != "1K,2K" {
		t.Fatalf("image qualities = %#v", items[0].AIStarsLab.Qualities)
	}
}

func TestDiscoveredAIStarsLabModelKeepsRouteCapabilityContract(t *testing.T) {
	catalog := &ChannelModelCatalogItem{
		ID:          "54:seedance-2.5",
		DisplayName: "Seedance 2.5（高速线路）",
		AIStarsLab: &AIStarsLabCatalogRoute{
			Channel: "54", Capability: "video", Model: "seedance-2.5",
			Qualities: []string{"720p", "1080p"}, AspectRatios: []string{"16:9", "9:16"},
			DurationMin: 4, DurationMax: 30, Modes: []string{"text2video", "image2video"},
			InputImagesMax: 9, InputVideosMax: 2, InputAudiosMax: 1,
		},
	}
	discovered := discoveredChannelModel(model.ModelChannel{ID: "channel"}, catalog.ID, catalog)
	if discovered.Protocol != model.ChannelInterfaceAIStarsLabVideo || discovered.Capability != "video" || discovered.Enabled || discovered.PriceConfigured {
		t.Fatalf("discovered = %#v", discovered)
	}
	config, err := DecodeModelCapabilityConfig(discovered.CapabilityConfigJSON)
	if err != nil {
		t.Fatal(err)
	}
	if config.AIStarsLab == nil || config.AIStarsLab.Channel != "54" || config.AIStarsLab.Model != "seedance-2.5" {
		t.Fatalf("route = %#v", config.AIStarsLab)
	}
	video := config.Video
	if video == nil || video.Duration.Selection != "range" || video.Duration.Min != 4 || video.Duration.Max != 30 || video.Duration.Default != 4 {
		t.Fatalf("duration = %#v", video)
	}
	if strings.Join(video.Resolutions, ",") != "720p,1080p" || strings.Join(video.Ratios, ",") != "16:9,9:16" {
		t.Fatalf("video = %#v", video)
	}
	if video.References.MaxImages != 9 || video.References.MaxVideos != 2 || video.References.MaxAudios != 1 {
		t.Fatalf("references = %#v", video.References)
	}
}
