package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"infinite-canvas/backend/internal/model"
)

func TestFetchWeijinCatalogMapsOfficialCapabilities(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/models" || request.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("request = %s, authorization = %q", request.URL.Path, request.Header.Get("Authorization"))
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"data":[{"id":"seedance2.0-one-full-flex-720p","resolution":"720p","durations_seconds":[5,10,15],"ratios":["16:9","9:16"],"max_images":9,"max_videos":3,"max_video_duration_seconds":15,"max_audios":3,"audio_requires_image":true}]}`))
	}))
	defer upstream.Close()

	svc, _ := newChannelModelTestService(t)
	svc.runtimeCapabilities = RuntimeCapabilities{desktopLocalChannels: true}
	items, err := svc.FetchChannelModelCatalog(context.Background(), &model.User{ID: "admin"}, ChannelModelsRequest{BaseURL: upstream.URL, AllowLocalChannel: true, APIKey: "test-key", ConnectionType: "weijin"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("catalog = %#v", items)
	}
	item := items[0]
	if item.ID != "seedance2.0-one-full-flex-720p" || item.SupportedEndpointTypes[0] != "weijin-video" || pointerValue(item.MaxImages) != 9 || pointerValue(item.MaxVideos) != 3 || pointerValue(item.MaxVideoDuration) != 15 || pointerValue(item.MaxAudios) != 3 || item.AudioRequiresImage == nil || !*item.AudioRequiresImage {
		t.Fatalf("catalog item = %#v", item)
	}
	discovered := discoveredChannelModel(model.ModelChannel{ID: "weijin", BaseURL: "https://www.weijinapi.top"}, item.ID, item.SupportedEndpointTypes, &item)
	if discovered.Protocol != model.ChannelInterfaceWeijinVideo || discovered.Capability != "video" {
		t.Fatalf("discovered model = %#v", discovered)
	}
	config, err := DecodeModelCapabilityConfig(discovered.CapabilityConfigJSON)
	if err != nil || config.Video == nil {
		t.Fatalf("capability config = %#v, err = %v", config, err)
	}
	if config.Video.References.MaxImages != 9 || config.Video.References.MaxVideos != 3 || config.Video.References.MaxVideoDuration != 15 || config.Video.References.MaxAudios != 3 || config.Video.Duration.Default != 5 || config.Video.DefaultRatio != "16:9" || config.Video.DefaultResolution != "720p" {
		t.Fatalf("video capability = %#v", config.Video)
	}
}
