package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if len(items) != 1 || items[0].AIStarsLab == nil || items[0].ID != "seedance:seedance-2.5" {
		t.Fatalf("items = %#v", items)
	}
	route := items[0].AIStarsLab
	if route.DurationMin != 4 || route.DurationMax != 30 || len(route.Duration) != 0 || route.InputImagesMax != 30 || route.InputVideosMax != 10 || route.InputAudiosMax != 10 {
		t.Fatalf("route = %#v", route)
	}
}

// 官方的模型身份是「线路 + 模型」：同一个模型名在不同线路下的价格、画质、时长和参考素材上限都不同，
// 因此每条线路都必须独立成条，不能按模型名去重把非默认线路静默丢掉。
func TestFetchAIStarsLabCatalogKeepsEveryRouteForDuplicateModel(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"code":0,"msg":"success","data":{"imageConfig":[{"channel":"47","title":"线路A","defaultOption":"false","models":[{"model":"gpt-image-2","label":"GPT Image 2","qualities":[],"aspectRatios":["1:1"],"inputImagesMax":1}]},{"channel":"54","title":"线路B","defaultOption":true,"models":[{"model":"gpt-image-2","label":"GPT Image 2","qualities":[],"aspectRatios":["16:9"],"inputImagesMax":9}]}],"videoConfig":[]}}`))
	}))
	defer upstream.Close()

	svc, _ := newChannelModelTestService(t)
	svc.runtimeCapabilities = RuntimeCapabilities{desktopLocalChannels: true}
	items, err := svc.FetchChannelModelCatalog(context.Background(), &model.User{ID: "admin"}, ChannelModelsRequest{BaseURL: upstream.URL + "/openapi", AllowLocalChannel: true, APIKey: "test-key", ConnectionType: "aistarslab"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %#v", items)
	}
	if items[0].ID != "47:gpt-image-2" || items[0].AIStarsLab == nil || items[0].AIStarsLab.Channel != "47" || items[0].AIStarsLab.InputImagesMax != 1 {
		t.Fatalf("items[0] = %#v", items[0])
	}
	if items[1].ID != "54:gpt-image-2" || items[1].AIStarsLab == nil || items[1].AIStarsLab.Channel != "54" || items[1].AIStarsLab.InputImagesMax != 9 {
		t.Fatalf("items[1] = %#v", items[1])
	}
	// 同名模型必须能在后台被区分，否则管理员无法知道自己定价的是哪条线路。
	if items[0].DisplayName == items[1].DisplayName {
		t.Fatalf("display names must differ: %q", items[0].DisplayName)
	}
	if items[0].DisplayName != "GPT Image 2（线路A）" || items[1].DisplayName != "GPT Image 2（线路B）" {
		t.Fatalf("display names = %q / %q", items[0].DisplayName, items[1].DisplayName)
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

// 目录 ID 现在是 `<线路>:<模型>`；早期只存模型名的存量记录不在目录里，
// 不能被常规同步遍历到，却可能已启用已定价并被用户选中。
func TestAiStarsLabCatalogItemForLegacyKeyResolvesUniqueModelName(t *testing.T) {
	catalog := []ChannelModelCatalogItem{
		{ID: "47:seedance-2.0", SupportedEndpointTypes: []string{"aistarslab-video"}, AIStarsLab: &AIStarsLabCatalogRoute{Channel: "47", Capability: "video", Model: "seedance-2.0"}},
		{ID: "54:seedance-2.5", SupportedEndpointTypes: []string{"aistarslab-video"}, AIStarsLab: &AIStarsLabCatalogRoute{Channel: "54", Capability: "video", Model: "seedance-2.5", DurationMin: 4, DurationMax: 30}},
		{ID: "61:seedance-2.5", SupportedEndpointTypes: []string{"aistarslab-video"}, AIStarsLab: &AIStarsLabCatalogRoute{Channel: "61", Capability: "video", Model: "seedance-2.5", DurationMin: 4, DurationMax: 12}},
	}
	matched := aiStarsLabCatalogItemForLegacyKey(catalog, "seedance-2.0")
	if matched == nil || matched.ID != "47:seedance-2.0" {
		t.Fatalf("matched = %#v", matched)
	}
	// 模型名横跨多条线路时无法判定用户当初买的是哪条，不能拿任意线路蒙对。
	if got := aiStarsLabCatalogItemForLegacyKey(catalog, "seedance-2.5"); got != nil {
		t.Fatalf("ambiguous model name must not match = %#v", got)
	}
	// 正规目录 key 走常规同步路径，不需要遗留修复。
	if got := aiStarsLabCatalogItemForLegacyKey(catalog, "54:seedance-2.5"); got != nil {
		t.Fatalf("canonical key should not match legacy resolver = %#v", got)
	}
}

// modelKey 是 `<线路>:<模型>`，同步必须把线路块写进能力合同，
// 且不得逆转管理员已确认的定价与启用状态。
func TestSyncAIStarsLabContractWritesRouteForPrefixedModelKey(t *testing.T) {
	channel := model.ModelChannel{ID: "aistarslab", BaseURL: "https://api.video.aistarslab.com/openapi"}
	legacy := model.ChannelModel{ModelKey: "54:seedance-2.5", Capability: "video", Protocol: model.ChannelInterfaceAIStarsLabVideo, Enabled: true, PriceConfigured: true, CapabilityConfigJSON: `{"version":1}`, CapabilityVersion: 1}
	catalog := &ChannelModelCatalogItem{ID: "54:seedance-2.5", SupportedEndpointTypes: []string{"aistarslab-video"}, AIStarsLab: &AIStarsLabCatalogRoute{Channel: "54", Capability: "video", Model: "seedance-2.5", DurationMin: 4, DurationMax: 30}}
	if !syncChannelModelContract(channel, &legacy, catalog.SupportedEndpointTypes, catalog) {
		t.Fatal("expected legacy record to be repaired")
	}
	config, err := DecodeModelCapabilityConfig(legacy.CapabilityConfigJSON)
	if err != nil {
		t.Fatal(err)
	}
	if config.AIStarsLab == nil || config.AIStarsLab.Channel != "54" || config.AIStarsLab.Model != "seedance-2.5" {
		t.Fatalf("repaired route = %#v", config.AIStarsLab)
	}
	if !legacy.Enabled || !legacy.PriceConfigured {
		t.Fatalf("repaired model = %#v", legacy)
	}
}

func TestAiStarsLabRequestModelPrefersCatalogModelName(t *testing.T) {
	route := &AIStarsLabCapabilityConfig{Channel: "54", Capability: "video", Model: "seedance-2.5"}
	if got := aiStarsLabRequestModel(route, "54:seedance-2.5"); got != "seedance-2.5" {
		t.Fatalf("model = %q, want seedance-2.5", got)
	}
	if got := aiStarsLabRequestModel(&AIStarsLabCapabilityConfig{Channel: "54"}, "seedance-2.5"); got != "seedance-2.5" {
		t.Fatalf("fallback model = %q", got)
	}
}

// 线路块缺失的记录不能要求用户先去后台重新拉取模型，否则已定价启用的模型直接无法生成；
// modelKey 自带 `<线路>:` 前缀，可以现场还原出可用线路。
func TestAiStarsLabRouteDerivesChannelFromPrefixedModelKey(t *testing.T) {
	route := aiStarsLabRoute(nil, "54:seedance-2.5")
	if route == nil || route.Channel != "54" || route.Model != "seedance-2.5" {
		t.Fatalf("derived route = %#v", route)
	}
	// 还原线路无法得知参考素材上限，必须置 -1 交由上游校验，不能让默认 0 把参考图卡成“最多 0 张”。
	if route.InputImagesMax != -1 || route.InputVideosMax != -1 || route.InputAudiosMax != -1 {
		t.Fatalf("derived reference limits = %#v", route)
	}
	// 目录下发的线路块是首选来源，不能被 modelKey 还原值覆盖。
	catalog := &ModelCapabilityConfig{AIStarsLab: &AIStarsLabCapabilityConfig{Channel: "54", Capability: "video", Model: "seedance-2.5", InputImagesMax: 9}}
	if got := aiStarsLabRoute(catalog, "54:seedance-2.5"); got == nil || got.InputImagesMax != 9 {
		t.Fatalf("catalog route = %#v", got)
	}
	// 早期只存模型名的记录无线路信息，无法还原时仍需明确失败，不能把模型名当线路发给上游。
	if got := aiStarsLabRoute(nil, "seedance-2.5"); got != nil {
		t.Fatalf("canonical key must not derive a route = %#v", got)
	}
}

// 线路块缺失但 modelKey 带线路前缀时，请求体必须能正常构造，且 model 只发去前缀后的官方模型名。
func TestAiStarsLabVideoRequestBodyRecoversRouteFromPrefixedModelKey(t *testing.T) {
	input := canvasGenerationInput{
		Prompt:          "海边日落",
		Config:          providerConfig{Model: "54:seedance-2.5", Size: "16:9", VQuality: "720p", VideoSeconds: "5"},
		ReferenceImages: []providerMedia{{URL: "https://example.com/1.png"}},
	}
	body, err := aiStarsLabVideoRequestBody(input)
	if err != nil {
		t.Fatalf("aiStarsLabVideoRequestBody() error = %v", err)
	}
	if body["channel"] != "54" || body["model"] != "seedance-2.5" {
		t.Fatalf("recovered body = %#v", body)
	}
}

// AIStarsLab 的 base 已经是 `/openapi` API 根，官方路径直接挂在它下面；
// 再补 `/v1` 会变成 `/openapi/v1/generation/create/video` 并被上游以 No static resource 拒绝。
func TestApiURLKeepsOpenAPIBaseWithoutVersionSegment(t *testing.T) {
	const base = "https://api.video.aistarslab.com/openapi"
	if got := apiURL(base, "/generation/create/video"); got != base+"/generation/create/video" {
		t.Fatalf("apiURL() = %q", got)
	}
	if got := apiURL(base+"/", "/generation/status"); got != base+"/generation/status" {
		t.Fatalf("apiURL() with trailing slash = %q", got)
	}
	// 普通 OpenAI 兼容 base 仍需补 `/v1`，不能因本次修复退化。
	if got := apiURL("https://api.example.com", "/images/generations"); got != "https://api.example.com/v1/images/generations" {
		t.Fatalf("openai-compatible apiURL() = %q", got)
	}
	if got := apiURL("https://api.example.com/v1", "/images/generations"); got != "https://api.example.com/v1/images/generations" {
		t.Fatalf("versioned apiURL() = %q", got)
	}
}

// 系统渠道的线路块必须从服务端记录回填：未定价模型不会随公开渠道下发 capabilityConfig，
// 早期前端缓存也可能没有这个字段；若只信浏览器上报，后台重新拉取过模型后仍会报“缺少线路编码”。
func TestResolveProviderConfigRestoresAIStarsLabRouteFromChannelModel(t *testing.T) {
	svc, db := newChannelModelTestService(t)
	svc.runtimeCapabilities = RuntimeCapabilities{desktopLocalChannels: true}
	channel := model.ModelChannel{ID: newID(), Name: "AIStarsLab", Scope: "system", BaseURL: "https://api.video.aistarslab.com/openapi", APIKey: "test-key", APIFormat: "openai", Enabled: true, ModelsJSON: `["seedance-2.5"]`}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	capability := `{"version":1,"aistarslab":{"channel":"54","capability":"video","model":"seedance-2.5"}}`
	channelModel := model.ChannelModel{ID: newID(), ChannelID: channel.ID, ModelKey: "seedance-2.5", DisplayName: "Seedance 2.5", Capability: "video", Protocol: model.ChannelInterfaceAIStarsLabVideo, Enabled: true, CapabilityConfigJSON: capability}
	if err := db.Create(&channelModel).Error; err != nil {
		t.Fatal(err)
	}
	// 浏览器没有上报 capabilityConfig，这是未定价模型与旧缓存的真实情况。
	resolved, err := svc.resolveProviderConfig(providerConfig{ChannelID: channel.ID, Model: "seedance-2.5"})
	if err != nil {
		t.Fatalf("resolveProviderConfig() error = %v", err)
	}
	if resolved.CapabilityConfig == nil || resolved.CapabilityConfig.AIStarsLab == nil {
		t.Fatalf("resolved capability config = %#v", resolved.CapabilityConfig)
	}
	if resolved.CapabilityConfig.AIStarsLab.Channel != "54" {
		t.Fatalf("restored route = %#v", resolved.CapabilityConfig.AIStarsLab)
	}
	// 回填后请求体才能正常构造。
	body, err := aiStarsLabVideoRequestBody(canvasGenerationInput{Prompt: "海边日落", Config: resolved})
	if err != nil {
		t.Fatalf("aiStarsLabVideoRequestBody() error = %v", err)
	}
	if body["channel"] != "54" || body["model"] != "seedance-2.5" {
		t.Fatalf("request body = %#v", body)
	}
}

// AIStarsLab 只接受公网 URL 参考素材，必须进入签名链路；否则素材库资源会直接报“仅支持公网 URL”。
func TestAiStarsLabProtocolsRequirePublicReferenceURL(t *testing.T) {
	for _, protocol := range []model.ChannelInterfaceType{model.ChannelInterfaceAIStarsLabImage, model.ChannelInterfaceAIStarsLabVideo} {
		if !protocolRequiresPublicReferenceURL(string(protocol)) {
			t.Fatalf("protocol %q should require public reference URL", protocol)
		}
	}
	if protocolRequiresPublicReferenceURL(string(model.ChannelInterfaceChatCompletion)) {
		t.Fatal("chat-completion should not require public reference URL")
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

// 同一模型在多条线路下的价格和能力不同，拉取必须为每条线路各建一条独立的待配置记录，
// 否则管理员只能定价默认线路，其余线路永远选不到。
func TestDiscoveredChannelModelKeepsEveryAIStarsLabRoute(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"code":0,"msg":"success","data":{"imageConfig":[],"videoConfig":[{"channel":"47","title":"普通线路","defaultOption":true,"models":[{"model":"seedance-2.5","label":"Seedance 2.5","qualities":[{"quality":"720p"}],"aspectRatios":["16:9"],"duration":{"min":4,"max":12},"inputImagesMax":1}]},{"channel":"54","title":"高速线路","defaultOption":false,"models":[{"model":"seedance-2.5","label":"Seedance 2.5","qualities":[{"quality":"1080p"}],"aspectRatios":["16:9","9:16"],"duration":{"min":4,"max":30},"inputImagesMax":9}]}]}}`))
	}))
	defer upstream.Close()

	svc, _ := newChannelModelTestService(t)
	svc.runtimeCapabilities = RuntimeCapabilities{desktopLocalChannels: true}
	catalog, err := svc.FetchChannelModelCatalog(context.Background(), &model.User{ID: "admin"}, ChannelModelsRequest{BaseURL: upstream.URL + "/openapi", AllowLocalChannel: true, APIKey: "test-key", ConnectionType: "aistarslab"})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) != 2 {
		t.Fatalf("catalog = %#v", catalog)
	}
	channel := model.ModelChannel{ID: "aistarslab", BaseURL: "https://api.video.aistarslab.com/openapi"}
	routes := make(map[string]*AIStarsLabCapabilityConfig, len(catalog))
	for index := range catalog {
		item := catalog[index]
		discovered := discoveredChannelModel(channel, item.ID, item.SupportedEndpointTypes, &item)
		if discovered.Enabled || discovered.PriceConfigured {
			t.Fatalf("discovered model must stay unpriced: %#v", discovered)
		}
		// 展示名必须带上线路，否则后台两条同名记录无法区分。
		if !strings.Contains(discovered.DisplayName, "线路") {
			t.Fatalf("display name = %q", discovered.DisplayName)
		}
		config, err := DecodeModelCapabilityConfig(discovered.CapabilityConfigJSON)
		if err != nil {
			t.Fatal(err)
		}
		if config.AIStarsLab == nil || config.AIStarsLab.Model != "seedance-2.5" {
			t.Fatalf("route = %#v", config.AIStarsLab)
		}
		if _, exists := routes[discovered.ModelKey]; exists {
			t.Fatalf("duplicate model key %q", discovered.ModelKey)
		}
		routes[discovered.ModelKey] = config.AIStarsLab
		// 能力参数必须按线路各自落库，不能被另一条线路覆盖。
		if config.Video == nil || config.Video.Duration.Max != config.AIStarsLab.DurationMax || config.Video.References.MaxImages != config.AIStarsLab.InputImagesMax {
			t.Fatalf("video config = %#v, route = %#v", config.Video, config.AIStarsLab)
		}
	}
	normal, fast := routes["47:seedance-2.5"], routes["54:seedance-2.5"]
	if normal == nil || fast == nil {
		t.Fatalf("routes = %#v", routes)
	}
	if normal.DurationMax != 12 || normal.InputImagesMax != 1 || fast.DurationMax != 30 || fast.InputImagesMax != 9 {
		t.Fatalf("normal = %#v, fast = %#v", normal, fast)
	}
}
