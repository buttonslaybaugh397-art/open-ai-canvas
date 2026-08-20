package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestFetchAIStarsLabCatalogSupportsCurrentDurationContract(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/openapi/generation/config" {
			t.Fatalf("path = %q", request.URL.Path)
		}
		if request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"code":0,"msg":"success","data":{"imageConfig":[],"videoConfig":[{"channel":"seedance","title":"Seedance","description":null,"defaultOption":true,"models":[{"model":"seedance-2.5","label":"Seedance 2.5","qualities":[{"quality":"720p"}],"modes":["text2video","image2video"],"aspectRatios":["16:9","9:16"],"duration":{"min":4,"max":30,"options":null},"inputImagesMax":30,"inputVideosMax":10,"inputAudiosMax":10}]}]}}`))
	}))
	defer upstream.Close()

	svc, _ := newChannelModelTestService(t)
	svc.runtimeCapabilities = RuntimeCapabilities{desktopLocalChannels: true}
	items, err := svc.FetchChannelModelCatalog(context.Background(), &model.User{ID: "admin"}, ChannelModelsRequest{BaseURL: upstream.URL + "/openapi", AllowLocalChannel: true, APIKey: "test-key", ConnectionType: "aistarslab"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].AIStarsLab == nil {
		t.Fatalf("items = %#v", items)
	}
	route := items[0].AIStarsLab
	if route.DurationMin != 4 || route.DurationMax != 30 || len(route.Duration) != 0 || route.InputImagesMax != 30 || route.InputVideosMax != 10 || route.InputAudiosMax != 10 {
		t.Fatalf("route = %#v", route)
	}
}

func TestFetchAIStarsLabCatalogPrefersDefaultChannelForDuplicateModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"code":0,"msg":"success","data":{"imageConfig":[{"channel":"fallback","defaultOption":"false","models":[{"model":"gpt-image-2","label":"Fallback","qualities":[],"aspectRatios":["1:1"],"inputImagesMax":1}]},{"channel":"preferred","defaultOption":true,"models":[{"model":"gpt-image-2","label":"Preferred","qualities":[],"aspectRatios":["16:9"],"inputImagesMax":9}]}],"videoConfig":[]}}`))
	}))
	defer upstream.Close()

	svc, _ := newChannelModelTestService(t)
	svc.runtimeCapabilities = RuntimeCapabilities{desktopLocalChannels: true}
	items, err := svc.FetchChannelModelCatalog(context.Background(), &model.User{ID: "admin"}, ChannelModelsRequest{BaseURL: upstream.URL + "/openapi", AllowLocalChannel: true, APIKey: "test-key", ConnectionType: "aistarslab"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].DisplayName != "Preferred" || items[0].AIStarsLab == nil || items[0].AIStarsLab.Channel != "preferred" || items[0].AIStarsLab.InputImagesMax != 9 {
		t.Fatalf("items = %#v", items)
	}
}

func TestSyncAIStarsLabModelUsesDurationRange(t *testing.T) {
	channel := model.ModelChannel{BaseURL: "https://api.video.aistarslab.com/openapi"}
	item := model.ChannelModel{ModelKey: "seedance-2.5", Protocol: model.ChannelInterfaceChatCompletion, Capability: "text"}
	catalog := &ChannelModelCatalogItem{AIStarsLab: &AIStarsLabCatalogRoute{Channel: "seedance", Capability: "video", Model: "seedance-2.5", DurationMin: 4, DurationMax: 30}}
	if !syncChannelModelContract(channel, &item, nil, catalog) {
		t.Fatal("expected contract update")
	}
	config, err := DecodeModelCapabilityConfig(item.CapabilityConfigJSON)
	if err != nil {
		t.Fatal(err)
	}
	if config.Video == nil || config.Video.Duration.Selection != "range" || config.Video.Duration.Min != 4 || config.Video.Duration.Max != 30 || config.Video.Duration.Default != 4 {
		t.Fatalf("duration = %#v", config.Video)
	}
}
