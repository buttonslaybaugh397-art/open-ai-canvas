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

func aiStarsLabVideoTestInput(route *AIStarsLabCapabilityConfig, seconds string, images []providerMedia) canvasGenerationInput {
	return canvasGenerationInput{
		Prompt:          "海边日落",
		Config:          providerConfig{Model: "seedance-2.5", Size: "16:9", VQuality: "720p", VideoSeconds: seconds, CapabilityConfig: &ModelCapabilityConfig{AIStarsLab: route}},
		ReferenceImages: images,
	}
}

func TestAiStarsLabVideoRequestBodySendsRequiredContractFields(t *testing.T) {
	// channel、quality、aspectRatio、duration 均为官方必填项；早期实现把 channel 写死为空字符串且丢失 quality。
	route := &AIStarsLabCapabilityConfig{Channel: "12", Capability: "video", Model: "seedance-2.5", Qualities: []string{"720p", "1080p"}, Modes: []string{"text2video", "image2video"}}
	body, err := aiStarsLabVideoRequestBody(aiStarsLabVideoTestInput(route, "10", nil))
	if err != nil {
		t.Fatalf("aiStarsLabVideoRequestBody() error = %v", err)
	}
	if body["channel"] != "12" || body["quality"] != "720p" || body["aspectRatio"] != "16:9" || body["duration"] != 10 {
		t.Fatalf("AIStarsLab video body = %#v", body)
	}
	if body["mode"] != "text2video" {
		t.Fatalf("AIStarsLab text2video mode = %#v", body["mode"])
	}
}

func TestAiStarsLabVideoRequestBodyDerivesModeFromReferenceImages(t *testing.T) {
	route := &AIStarsLabCapabilityConfig{Channel: "12", Model: "seedance-2.5", Qualities: []string{"720p"}, Modes: []string{"text2video", "image2video", "frames2video"}}
	single, err := aiStarsLabVideoRequestBody(aiStarsLabVideoTestInput(route, "5", []providerMedia{{URL: "https://example.com/1.png"}}))
	if err != nil || single["mode"] != "image2video" {
		t.Fatalf("single reference mode = %#v, err = %v", single["mode"], err)
	}
	// 官方规则：正好 2 张参考图且模型声明 frames2video 时，按首尾帧处理。
	frames, err := aiStarsLabVideoRequestBody(aiStarsLabVideoTestInput(route, "5", []providerMedia{{URL: "https://example.com/1.png"}, {URL: "https://example.com/2.png"}}))
	if err != nil || frames["mode"] != "frames2video" {
		t.Fatalf("frames mode = %#v, err = %v", frames["mode"], err)
	}
}

func TestAiStarsLabVideoRequestBodyRejectsMissingChannel(t *testing.T) {
	// 线路编码缺失时必须明确失败，不能用空字符串让上游猜线路。
	if _, err := aiStarsLabVideoRequestBody(aiStarsLabVideoTestInput(nil, "5", nil)); err == nil {
		t.Fatal("missing AIStarsLab channel must fail closed")
	}
}

func TestAiStarsLabRequestQualityFallsBackToDeclaredOption(t *testing.T) {
	route := &AIStarsLabCapabilityConfig{Channel: "12", Qualities: []string{"720p", "1080p"}}
	if quality := aiStarsLabRequestQuality(route, nil, "4k"); quality != "720p" {
		t.Fatalf("unsupported quality fallback = %q", quality)
	}
	if quality := aiStarsLabRequestQuality(route, nil, "1080P"); quality != "1080p" {
		t.Fatalf("case-insensitive quality = %q", quality)
	}
}

func TestAiStarsLabImageRequestBodySendsChannelAndQuality(t *testing.T) {
	route := &AIStarsLabCapabilityConfig{Channel: "21", Capability: "image", Model: "example-image", Qualities: []string{"1K"}, AspectRatios: []string{"1:1", "16:9"}, InputImagesMax: 9}
	body, err := aiStarsLabImageRequestBody(canvasGenerationInput{
		Prompt: "一只白色陶瓷杯",
		Config: providerConfig{Model: "example-image", Size: "16:9", Quality: "auto", CapabilityConfig: &ModelCapabilityConfig{AIStarsLab: route}},
	})
	if err != nil {
		t.Fatalf("aiStarsLabImageRequestBody() error = %v", err)
	}
	if body["channel"] != "21" || body["quality"] != "1K" || body["aspectRatio"] != "16:9" || body["n"] != 1 {
		t.Fatalf("AIStarsLab image body = %#v", body)
	}
}

func TestFirstStringInListReadsOfficialOutputsArray(t *testing.T) {
	// 官方结果只在 outputs 数组，早期实现读标量 output 字段会把成功任务当成无结果。
	if value := firstStringInList([]interface{}{"", "https://example.com/out.mp4"}); value != "https://example.com/out.mp4" {
		t.Fatalf("firstStringInList() = %q", value)
	}
	if value := firstStringInList(nil); value != "" {
		t.Fatalf("firstStringInList(nil) = %q", value)
	}
}
