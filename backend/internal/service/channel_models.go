package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type ChannelModelRequest struct {
	ModelKey                     string                 `json:"modelKey"`
	DisplayName                  string                 `json:"displayName"`
	Capability                   string                 `json:"capability"`
	Protocol                     string                 `json:"protocol"`
	BillingMode                  string                 `json:"billingMode"`
	UnitPriceMicrocredits        int64                  `json:"unitPriceMicrocredits"`
	ResolutionPriceMicrocredits  map[string]int64       `json:"resolutionPriceMicrocredits"`
	InputTokenPriceMicrocredits  int64                  `json:"inputTokenPriceMicrocredits"`
	OutputTokenPriceMicrocredits int64                  `json:"outputTokenPriceMicrocredits"`
	CachedTokenPriceMicrocredits int64                  `json:"cachedTokenPriceMicrocredits"`
	PriceConfigured              bool                   `json:"priceConfigured"`
	Enabled                      *bool                  `json:"enabled"`
	CapabilityConfig             *ModelCapabilityConfig `json:"capabilityConfig"`
}

// AdminChannelModelFetchResult 是管理员从上游拉目录后的汇总：models 为去重后的标识，added 为本次新建条数。
type AdminChannelModelFetchResult struct {
	Models []string `json:"models"`
	Added  int64    `json:"added"`
}

type AdminChannelModelTestResult struct {
	DurationMs int64 `json:"durationMs"`
}

func (s *Service) EnsureSystemChannelModels() error {
	channels, err := s.repo.SystemChannels(true)
	if err != nil {
		return err
	}
	for index := range channels {
		items, err := s.repo.ChannelModels(channels[index].ID, true)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			if err := s.syncInitialChannelModels(&channels[index], channelModelNames(channels[index])); err != nil {
				return err
			}
		}
		items, err = s.repo.ChannelModels(channels[index].ID, true)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) AdminChannelModels(actor *model.User, channelID string) ([]model.ChannelModel, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if _, err := s.repo.AdminSystemChannel(channelID); err != nil {
		return nil, err
	}
	items, err := s.ensureChannelModels(channelID, true)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].ResolutionPriceMicrocredits = decodeResolutionPrices(items[index].ResolutionPricesJSON)
		if strings.TrimSpace(items[index].CapabilityConfigJSON) == "" {
			continue
		}
		var config map[string]any
		if json.Unmarshal([]byte(items[index].CapabilityConfigJSON), &config) == nil {
			items[index].CapabilityConfig = config
		}
	}
	return items, nil
}

func (s *Service) SystemChannelModel(channelID string, modelKey string) (*model.ChannelModel, error) {
	return s.repo.ChannelModelByKey(channelID, strings.TrimPrefix(strings.TrimSpace(modelKey), "models/"))
}

func (s *Service) FetchAdminChannelModels(ctx context.Context, actor *model.User, channelID string) (*AdminChannelModelFetchResult, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	channel, err := s.adminSystemChannel(channelID)
	if err != nil {
		return nil, err
	}
	headers, err := ParseOutboundHeadersJSON(channel.HeadersJSON)
	if err != nil {
		return nil, err
	}
	// 使用服务端保存的渠道密钥和请求头访问上游，避免敏感配置再次经过浏览器。
	catalog, err := s.FetchChannelModelCatalog(ctx, actor, ChannelModelsRequest{BaseURL: channel.BaseURL, AllowLocalChannel: channel.AllowLocalChannel, APIKey: channel.APIKey, APIFormat: channel.APIFormat, ConnectionType: channelConnectionType(channel), Headers: headers})
	if err != nil {
		return nil, err
	}
	models := make([]string, 0, len(catalog))
	endpointTypes := make(map[string][]string, len(catalog))
	for _, item := range catalog {
		models = append(models, item.ID)
		endpointTypes[item.ID] = item.SupportedEndpointTypes
	}
	// 只按当前未删除记录去重；重新拉取已删除模型时应生成新的待配置记录。
	existing, err := s.repo.ChannelModels(channelID, true)
	if err != nil {
		return nil, err
	}
	known := make(map[string]*model.ChannelModel, len(existing))
	for index := range existing {
		known[existing[index].ModelKey] = &existing[index]
	}
	missing := make([]model.ChannelModel, 0, len(models))
	for _, name := range models {
		if item := known[name]; item != nil {
			if syncChannelModelContract(*channel, item, endpointTypes[name], catalogItemByID(catalog, name)) {
				if err := s.repo.SaveChannelModel(item); err != nil {
					return nil, err
				}
			}
			continue
		}
		// 自动发现不能绕过定价边界；汇取云仅补全能力合同，仍需管理员定价后手动启用。
		missing = append(missing, discoveredChannelModel(*channel, name, endpointTypes[name], catalogItemByID(catalog, name)))
	}
	added, err := s.repo.CreateMissingChannelModels(missing)
	if err != nil {
		return nil, err
	}
	if added > 0 {
		s.invalidateRouteCatalog()
	}
	return &AdminChannelModelFetchResult{Models: models, Added: added}, nil
}

func discoveredChannelModel(channel model.ModelChannel, name string, endpointTypes []string, catalogItems ...*ChannelModelCatalogItem) model.ChannelModel {
	item := model.ChannelModel{ID: newID(), ChannelID: channel.ID, ModelKey: name, DisplayName: name, BillingMode: "fixed_request", Enabled: false, PriceVersion: 1}
	var catalog *ChannelModelCatalogItem
	if len(catalogItems) > 0 {
		catalog = catalogItems[0]
	}
	syncChannelModelContract(channel, &item, endpointTypes, catalog)
	return item
}

func catalogItemByID(items []ChannelModelCatalogItem, id string) *ChannelModelCatalogItem {
	for index := range items {
		if items[index].ID == id {
			return &items[index]
		}
	}
	return nil
}

func channelConnectionType(channel *model.ModelChannel) string {
	if channel != nil && isAiStarsLabBaseURL(channel.BaseURL) {
		return "aistarslab"
	}
	return ""
}

func syncChannelModelContract(channel model.ModelChannel, item *model.ChannelModel, endpointTypes []string, catalog *ChannelModelCatalogItem) bool {
	if catalog != nil && catalog.AIStarsLab != nil {
		protocol := model.ChannelInterfaceAIStarsLabVideo
		capability := "video"
		if strings.EqualFold(strings.TrimSpace(catalog.AIStarsLab.Capability), "image") {
			protocol = model.ChannelInterfaceAIStarsLabImage
			capability = "image"
		}
		changed := item.Protocol != protocol || item.Capability != capability
		item.Protocol, item.Capability = protocol, capability
		config := DefaultModelCapabilityConfigForModel(string(protocol), item.ModelKey)
		config.AIStarsLab = &AIStarsLabCapabilityConfig{Channel: catalog.AIStarsLab.Channel, Capability: catalog.AIStarsLab.Capability, Model: catalog.AIStarsLab.Model, Qualities: append([]string(nil), catalog.AIStarsLab.Qualities...), AspectRatios: append([]string(nil), catalog.AIStarsLab.AspectRatios...), Duration: append([]int(nil), catalog.AIStarsLab.Duration...), DurationMin: catalog.AIStarsLab.DurationMin, DurationMax: catalog.AIStarsLab.DurationMax, Modes: append([]string(nil), catalog.AIStarsLab.Modes...), InputImagesMax: catalog.AIStarsLab.InputImagesMax, InputVideosMax: catalog.AIStarsLab.InputVideosMax, InputAudiosMax: catalog.AIStarsLab.InputAudiosMax}
		if capability == "video" {
			config.Video.References.MaxImages = catalog.AIStarsLab.InputImagesMax
			config.Video.References.MaxVideos = catalog.AIStarsLab.InputVideosMax
			config.Video.References.MaxAudios = catalog.AIStarsLab.InputAudiosMax
			if len(catalog.AIStarsLab.Duration) > 0 {
				config.Video.Duration.Selection = "enum"
				config.Video.Duration.Values = append([]int(nil), catalog.AIStarsLab.Duration...)
				config.Video.Duration.Default = catalog.AIStarsLab.Duration[0]
			} else if catalog.AIStarsLab.DurationMin > 0 && catalog.AIStarsLab.DurationMax >= catalog.AIStarsLab.DurationMin {
				config.Video.Duration = VideoDurationConfig{Selection: "range", Min: catalog.AIStarsLab.DurationMin, Max: catalog.AIStarsLab.DurationMax, Step: 1, Default: catalog.AIStarsLab.DurationMin}
			}
			config.Video.Ratios = append([]string(nil), catalog.AIStarsLab.AspectRatios...)
			if len(config.Video.Ratios) > 0 {
				config.Video.DefaultRatio = config.Video.Ratios[0]
			}
		}
		encoded, err := json.Marshal(config)
		if err == nil && item.CapabilityConfigJSON != string(encoded) {
			item.CapabilityConfigJSON = string(encoded)
			item.CapabilityVersion++
			changed = true
		}
		return changed
	}
	return syncHuiQuYunModelContract(channel, item, endpointTypes)
}

func syncHuiQuYunModelContract(channel model.ModelChannel, item *model.ChannelModel, endpointTypes []string) bool {
	if item == nil || !isHuiQuYunBaseURL(channel.BaseURL) {
		return false
	}
	// 管理员完成定价配置后，能力与协议就是人工确认的合同；后续拉取不得再用模型名覆盖。
	if item.PriceConfigured && validHuiQuYunProtocol(item.Protocol) && capabilityForProtocol(item.Protocol) == item.Capability {
		return false
	}
	protocol := huiQuYunProtocolForModel(item.ModelKey, endpointTypes)
	capability := capabilityForProtocol(protocol)
	contractChanged := item.Protocol != protocol || item.Capability != capability
	item.Protocol, item.Capability = protocol, capability
	configChanged := false
	if capability == "image" || capability == "video" {
		if contractChanged || strings.TrimSpace(item.CapabilityConfigJSON) == "" {
			if encoded, err := json.Marshal(DefaultModelCapabilityConfigForModel(string(protocol), item.ModelKey)); err == nil {
				configChanged = item.CapabilityConfigJSON != string(encoded)
				item.CapabilityConfigJSON = string(encoded)
				if configChanged {
					item.CapabilityVersion++
				}
			}
		}
	} else if item.CapabilityConfigJSON != "" || item.CapabilityVersion != 0 {
		item.CapabilityConfigJSON = ""
		item.CapabilityVersion = 0
		configChanged = true
	}
	return contractChanged || configChanged
}

func isHuiQuYunBaseURL(value string) bool {
	normalized := strings.ToLower(strings.TrimRight(strings.TrimSpace(value), "/"))
	return normalized == "https://api.bjhuiqu.net" || normalized == "https://api.bjhuiqu.net/v1"
}

func isAiStarsLabBaseURL(value string) bool {
	normalized := strings.ToLower(strings.TrimRight(strings.TrimSpace(value), "/"))
	return normalized == "https://api.video.aistarslab.com/openapi"
}

func huiQuYunProtocolForModel(name string, endpointTypes []string) model.ChannelInterfaceType {
	for _, endpointType := range endpointTypes {
		switch strings.ToLower(strings.TrimSpace(endpointType)) {
		case "openai-chat", "chat-completion", "chat":
			return model.ChannelInterfaceChatCompletion
		case "openai-response", "responses":
			return model.ChannelInterfaceOpenAIResponse
		case "openai-image", "image":
			return model.ChannelInterfaceOpenAIImage
		case "openai-video", "video":
			return model.ChannelInterfaceHuiQuYunVideo
		case "openai-audio", "audio":
			return model.ChannelInterfaceOpenAIAudio
		}
	}
	normalized := strings.ToLower(strings.TrimSpace(name))
	if huiQuYunModelContainsAny(normalized, "tts", "speech", "voice", "audio", "music", "sound") {
		return model.ChannelInterfaceOpenAIAudio
	}
	if huiQuYunModelContainsAny(normalized, "mj-sd", "seedance", "grok-video", "sora", "veo", "kling", "hailuo", "vidu", "wan-video", "jimeng-video", "doubao-video", "minimax-video", "video") {
		return model.ChannelInterfaceHuiQuYunVideo
	}
	if huiQuYunModelContainsAny(normalized, "gpt-image", "nano-banana", "nanobanana", "seedream", "image", "dall-e", "dalle", "imagen", "flux", "sdxl", "stable-diffusion", "midjourney", "ideogram", "recraft") {
		return model.ChannelInterfaceOpenAIImage
	}
	return model.ChannelInterfaceChatCompletion
}

func huiQuYunModelContainsAny(value string, markers ...string) bool {
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func validHuiQuYunProtocol(protocol model.ChannelInterfaceType) bool {
	switch protocol {
	case model.ChannelInterfaceChatCompletion, model.ChannelInterfaceOpenAIResponse, model.ChannelInterfaceOpenAIImage, model.ChannelInterfaceOpenAIAudio, model.ChannelInterfaceHuiQuYunVideo:
		return true
	default:
		return false
	}
}

func (s *Service) SaveAdminChannelModel(actor *model.User, channelID string, id string, req ChannelModelRequest) (*model.ChannelModel, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	channel, err := s.repo.AdminSystemChannel(channelID)
	if err != nil {
		return nil, err
	}
	modelKey, capability, protocol, err := normalizeChannelModelContract(channel, req)
	if err != nil {
		return nil, err
	}
	// 先检查同渠道重复模型，避免无关能力校验或生成无用序列号掩盖真正的冲突。
	conflict, conflictErr := s.repo.ChannelModelByKeyIncludingDisabled(channelID, modelKey)
	if conflictErr != nil && !errors.Is(conflictErr, gorm.ErrRecordNotFound) {
		return nil, conflictErr
	}
	if conflict != nil && conflict.ID != strings.TrimSpace(id) {
		return nil, BadAuthRequest("该渠道已存在模型 " + modelKey + "，请直接编辑已有模型")
	}
	if capability == "text" || capability == "image" || capability == "video" {
		if _, err := NormalizeModelCapabilityConfig(capability, string(protocol), req.CapabilityConfig); err != nil {
			return nil, err
		}
	}
	billingMode := strings.TrimSpace(req.BillingMode)
	if billingMode == "" {
		billingMode = "fixed_request"
	}
	if billingMode != "fixed_request" && billingMode != "per_second" && billingMode != "token" {
		return nil, BadAuthRequest("模型计费方式仅支持按次、按秒或 Token")
	}
	if billingMode == "per_second" && capability != "video" {
		return nil, BadAuthRequest("只有视频模型可以按秒计费")
	}
	if billingMode == "token" && !supportsTokenBilling(capability, protocol) {
		return nil, BadAuthRequest("Token 计费仅支持文本模型和火山方舟视频协议")
	}
	if req.UnitPriceMicrocredits < 0 || req.InputTokenPriceMicrocredits < 0 || req.OutputTokenPriceMicrocredits < 0 || req.CachedTokenPriceMicrocredits < 0 {
		return nil, BadAuthRequest("模型积分价格不能小于 0")
	}
	resolutionPrices, resolutionPricesJSON, priceErr := normalizeResolutionPrices(req.ResolutionPriceMicrocredits, capability, req.CapabilityConfig)
	if priceErr != nil {
		return nil, priceErr
	}
	if billingMode == "token" && req.InputTokenPriceMicrocredits == 0 && req.OutputTokenPriceMicrocredits == 0 && req.CachedTokenPriceMicrocredits == 0 {
		return nil, BadAuthRequest("Token 计费至少需要配置一项价格")
	}
	if billingMode == "token" && capability == "video" && req.OutputTokenPriceMicrocredits == 0 {
		return nil, BadAuthRequest("火山方舟视频 Token 计费需要配置每百万视频 Token 价格")
	}
	const maxTokenPriceMicrocredits = int64(1_000_000) * CreditScale
	if req.InputTokenPriceMicrocredits > maxTokenPriceMicrocredits || req.OutputTokenPriceMicrocredits > maxTokenPriceMicrocredits || req.CachedTokenPriceMicrocredits > maxTokenPriceMicrocredits {
		return nil, BadAuthRequest("Token 每百万用量价格不能超过 1,000,000 积分")
	}
	modelID, err := s.repo.NextPrefixedID("MODEL")
	if err != nil {
		return nil, err
	}
	item := &model.ChannelModel{ID: modelID, ChannelID: channelID, Enabled: true, PriceVersion: 1}
	if id != "" {
		item, err = s.repo.ChannelModelByID(channelID, id)
		if err != nil {
			return nil, err
		}
		item.PriceVersion++
	}
	item.ModelKey = modelKey
	item.DisplayName = strings.TrimSpace(req.DisplayName)
	if item.DisplayName == "" {
		item.DisplayName = modelKey
	}
	item.Capability = capability
	item.Protocol = protocol
	item.BillingMode = billingMode
	item.UnitPriceMicrocredits = req.UnitPriceMicrocredits
	item.ResolutionPricesJSON = resolutionPricesJSON
	item.ResolutionPriceMicrocredits = resolutionPrices
	item.InputTokenPriceMicrocredits = req.InputTokenPriceMicrocredits
	item.OutputTokenPriceMicrocredits = req.OutputTokenPriceMicrocredits
	item.CachedTokenPriceMicrocredits = req.CachedTokenPriceMicrocredits
	item.PriceConfigured = req.PriceConfigured
	if capability == "text" || capability == "image" || capability == "video" {
		capabilityConfig, normalizeErr := NormalizeModelCapabilityConfig(capability, string(protocol), req.CapabilityConfig)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		encoded, encodeErr := json.Marshal(capabilityConfig)
		if encodeErr != nil {
			return nil, encodeErr
		}
		if item.CapabilityConfigJSON != string(encoded) {
			item.CapabilityVersion++
		}
		item.CapabilityConfigJSON = string(encoded)
	} else {
		item.CapabilityConfigJSON = ""
		item.CapabilityVersion = 0
	}
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	}
	// 供应线路直接引用渠道模型，修改能力参数后会从这一唯一事实来源实时生成线路能力。
	// 可用配置不再保存独立能力范围；路由能力会在读取时从当前渠道模型能力实时投影。
	// 因此修改尺寸、比例或数量时无需拿旧快照做冲突校验，也不会留下第二个事实来源。
	if err := s.repo.SaveChannelModel(item); err != nil {
		return nil, err
	}
	s.invalidateRouteCatalog()
	if err := s.syncChannelModelNames(channel); err != nil {
		return nil, err
	}
	return item, nil
}

func normalizeResolutionPrices(prices map[string]int64, capability string, config *ModelCapabilityConfig) (map[string]int64, string, error) {
	if len(prices) == 0 {
		return nil, "", nil
	}
	if capability != "video" || config == nil || config.Video == nil {
		return nil, "", BadAuthRequest("只有视频模型可以配置分辨率价格")
	}
	supported := make(map[string]bool, len(config.Video.Resolutions))
	for _, resolution := range config.Video.Resolutions {
		supported[normalizeResolution(resolution)] = true
	}
	normalized := make(map[string]int64, len(prices))
	for resolution, price := range prices {
		key := normalizeResolution(resolution)
		if !supported[key] {
			return nil, "", BadAuthRequest("分辨率价格包含模型不支持的档位：" + strings.TrimSpace(resolution))
		}
		if price < 0 {
			return nil, "", BadAuthRequest("分辨率价格不能小于 0")
		}
		normalized[key] = price
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, "", err
	}
	return normalized, string(encoded), nil
}

func decodeResolutionPrices(raw string) map[string]int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	prices := map[string]int64{}
	if json.Unmarshal([]byte(raw), &prices) != nil || len(prices) == 0 {
		return nil
	}
	return prices
}

func supportsTokenBilling(capability string, protocol model.ChannelInterfaceType) bool {
	return capability == "text" || (capability == "video" && protocol == model.ChannelInterfaceVolcengineArkVideo)
}

func (s *Service) TestAdminChannelModel(ctx context.Context, actor *model.User, channelID string, req ChannelModelRequest) (*AdminChannelModelTestResult, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	channel, err := s.adminSystemChannel(channelID)
	if err != nil {
		return nil, err
	}
	modelKey, capability, protocol, err := normalizeChannelModelContract(channel, req)
	if err != nil {
		return nil, err
	}
	if capability == "text" || capability == "image" || capability == "video" {
		if _, err := NormalizeModelCapabilityConfig(capability, string(protocol), req.CapabilityConfig); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(channel.BaseURL) == "" || strings.TrimSpace(channel.APIKey) == "" {
		return nil, BadAuthRequest("请先在渠道中配置 Base URL 和 API Key")
	}
	if _, err := s.validateChannelOutboundURL(channel.BaseURL, channel.AllowLocalChannel, false); err != nil {
		return nil, err
	}
	headers, err := ParseOutboundHeadersJSON(channel.HeadersJSON)
	if err != nil {
		return nil, err
	}

	prompt := map[string]string{
		"text":  "Reply with OK.",
		"image": "A simple gray circle on a white background.",
		"video": "A static gray circle on a white background.",
		"audio": "Model test.",
	}[capability]
	videoSeconds := "6"
	videoSecondsValue := 6
	if protocol == model.ChannelInterfaceVolcengineJiMengVideo {
		videoSeconds = "5"
		videoSecondsValue = 5
	}
	imageSize, imageQuality := "", ""
	var imageProfile *ImageCapabilityConfig
	if capability == "image" {
		profile, normalizeErr := NormalizeModelCapabilityConfig(capability, string(protocol), req.CapabilityConfig)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		imageProfile = profile.Image
		imageSize, imageQuality = imageTestDefaults(imageProfile)
	}
	input := canvasGenerationInput{
		Mode:   capability,
		Prompt: prompt,
		Config: providerConfig{
			ChannelID:          channel.ID,
			APIFormat:          channel.APIFormat,
			InterfaceType:      string(protocol),
			BaseURL:            channel.BaseURL,
			AllowLocalChannel:  s.effectiveAllowLocalChannel(channel.AllowLocalChannel),
			APIKey:             channel.APIKey,
			SecretKey:          channel.SecretKey,
			Headers:            headers,
			Model:              modelKey,
			Size:               map[string]string{"image": imageSize, "video": "16:9"}[capability],
			Quality:            imageQuality,
			Count:              "1",
			VideoSeconds:       videoSeconds,
			VQuality:           "720",
			VideoGenerateAudio: "false",
			VideoWatermark:     "false",
			AudioVoice:         "alloy",
			AudioFormat:        "mp3",
			AudioSpeed:         "1",
		},
		Metadata: map[string]interface{}{},
	}
	if capability == "image" {
		input.ImageCapability = imageProfile
	}

	// 测试复用真实生成协议、运行时并发和熔断策略，但不创建用户任务或计费订单。
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	testCtx = context.WithValue(testCtx, providerAnalyticsKey{}, providerAnalyticsContext{
		Service: s, UserID: actor.ID, ChannelID: channel.ID, Capability: capability,
		Operation: "admin_model_test", Model: modelKey, VideoSeconds: videoSecondsValue,
	})
	testCtx = withProviderOutboundPolicy(testCtx, input.Config)
	startedAt := time.Now()
	switch capability {
	case "text":
		_, err = runTextTask(testCtx, input)
	case "image":
		_, err = runImageTask(testCtx, input)
	case "video":
		_, err = runVideoTask(testCtx, input)
	case "audio":
		_, err = runAudioTask(testCtx, input)
	}
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		return nil, &AuthError{Status: status, Message: "模型测试失败：" + truncateRunes(err.Error(), 1000)}
	}
	return &AdminChannelModelTestResult{DurationMs: time.Since(startedAt).Milliseconds()}, nil
}

// 模型测试必须使用当前模型声明的默认参数，避免固定分辨率 SKU 被通用 1K 测试值误伤。
func imageTestDefaults(profile *ImageCapabilityConfig) (string, string) {
	if profile == nil {
		return "1024x1024", "auto"
	}
	size := ""
	if profile.Size.Parameter != "none" {
		size = strings.TrimSpace(profile.Size.Default)
	}
	quality := ""
	if profile.Quality.Supported {
		quality = strings.TrimSpace(profile.Quality.Default)
	}
	return size, quality
}

func normalizeChannelModelContract(channel *model.ModelChannel, req ChannelModelRequest) (string, string, model.ChannelInterfaceType, error) {
	modelKey := strings.TrimPrefix(strings.TrimSpace(req.ModelKey), "models/")
	if modelKey == "" {
		return "", "", "", BadAuthRequest("请填写模型标识")
	}
	capability := normalizeCapability(req.Capability)
	if capability == "" {
		return "", "", "", BadAuthRequest("请选择模型能力")
	}
	protocol := model.ChannelInterfaceType(strings.TrimSpace(req.Protocol))
	if !validChannelInterfaceType(protocol) {
		return "", "", "", BadAuthRequest("请选择有效的模型请求协议")
	}
	if channel != nil && isHuiQuYunBaseURL(channel.BaseURL) && !validHuiQuYunProtocol(protocol) {
		return "", "", "", BadAuthRequest("汇取云仅支持文本、图片、音频和汇取云视频协议")
	}
	if expected := capabilityForProtocol(protocol); expected != "" && expected != capability {
		return "", "", "", BadAuthRequest("模型能力与请求协议不匹配")
	}
	if (protocol == model.ChannelInterfaceVolcengineJiMengImage || protocol == model.ChannelInterfaceVolcengineJiMengVideo) && (strings.TrimSpace(channel.APIKey) == "" || strings.TrimSpace(channel.SecretKey) == "") {
		return "", "", "", BadAuthRequest("即梦官方协议需要先在渠道中配置 Access Key 和 Secret Key")
	}
	return modelKey, capability, protocol, nil
}

func (s *Service) DeleteAdminChannelModel(actor *model.User, channelID string, id string) error {
	if err := s.RequireAdmin(actor); err != nil {
		return err
	}
	if _, err := s.repo.AdminSystemChannel(channelID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return BadAuthRequest("系统渠道不存在或已删除")
		}
		return err
	}
	if _, err := s.repo.ChannelModelByID(channelID, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return BadAuthRequest("渠道模型不存在或已删除")
		}
		return err
	}
	items, err := s.repo.ChannelModels(channelID, false)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		if item.ID != id {
			names = append(names, item.ModelKey)
		}
	}
	encoded, err := json.Marshal(names)
	if err != nil {
		return err
	}
	// 删除模型与渠道的兼容模型清单必须同事务提交，避免接口报错但列表已部分变化。
	err = s.repo.DeleteChannelModel(channelID, id, string(encoded), time.Now())
	if errors.Is(err, repository.ErrChannelModelInUse) {
		return BadAuthRequest("渠道模型仍被前台模型供应线路或进行中任务使用，请先移除线路并等待任务结束")
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return BadAuthRequest("渠道模型不存在或已删除")
	}
	if err == nil {
		s.invalidateRouteCatalog()
	}
	return err
}

func (s *Service) syncInitialChannelModels(channel *model.ModelChannel, names []string) error {
	existing, err := s.repo.ChannelModels(channel.ID, true)
	if err != nil {
		return err
	}
	byKey := make(map[string]*model.ChannelModel, len(existing))
	for index := range existing {
		byKey[existing[index].ModelKey] = &existing[index]
	}
	desired := make(map[string]bool, len(names))
	for _, name := range uniqueNonEmpty(names) {
		name = strings.TrimPrefix(name, "models/")
		desired[name] = true
		if item := byKey[name]; item != nil {
			if !item.Enabled {
				item.Enabled = true
				item.PriceVersion++
				if err := s.repo.SaveChannelModel(item); err != nil {
					return err
				}
			}
			continue
		}
		modelID, idErr := s.repo.NextPrefixedID("MODEL")
		if idErr != nil {
			return idErr
		}
		item := model.ChannelModel{ID: modelID, ChannelID: channel.ID, ModelKey: name, DisplayName: name, BillingMode: "fixed_request", Enabled: false, PriceVersion: 1}
		if err := s.repo.SaveChannelModel(&item); err != nil {
			return err
		}
	}
	for index := range existing {
		if existing[index].Enabled && !desired[existing[index].ModelKey] {
			existing[index].Enabled = false
			existing[index].PriceVersion++
			if err := s.repo.SaveChannelModel(&existing[index]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) ensureChannelModels(channelID string, includeDisabled bool) ([]model.ChannelModel, error) {
	items, err := s.repo.ChannelModels(channelID, includeDisabled)
	if err != nil || len(items) > 0 {
		return items, err
	}
	channel, err := s.repo.AdminSystemChannel(channelID)
	if err != nil {
		return nil, err
	}
	if err := s.syncInitialChannelModels(channel, channelModelNames(*channel)); err != nil {
		return nil, err
	}
	return s.repo.ChannelModels(channelID, includeDisabled)
}

func (s *Service) syncChannelModelNames(channel *model.ModelChannel) error {
	items, err := s.repo.ChannelModels(channel.ID, false)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.ModelKey)
	}
	encoded, err := json.Marshal(names)
	if err != nil {
		return err
	}
	channel.ModelsJSON = string(encoded)
	return s.repo.Save(channel)
}

func capabilityForProtocol(protocol model.ChannelInterfaceType) string {
	switch protocol {
	case model.ChannelInterfaceOpenAIImage, model.ChannelInterfaceGrokImage, model.ChannelInterfaceGlobalAiOpcImage, model.ChannelInterfaceVolcengineArkImage, model.ChannelInterfaceVolcengineJiMengImage, model.ChannelInterfaceGeminiImage:
		return "image"
	case model.ChannelInterfaceOpenAIAudio, model.ChannelInterfaceAsyncAudio:
		return "audio"
	case model.ChannelInterfaceNewAPIVideo, model.ChannelInterfaceNewAPIChannel1, model.ChannelInterfaceNewAPIChannel2, model.ChannelInterfaceGlobalAiOpcVideo, model.ChannelInterfaceHuiQuYunVideo, model.ChannelInterfaceAIStarsLabVideo, model.ChannelInterfaceXAIVideo, model.ChannelInterfaceVolcengineArkVideo, model.ChannelInterfaceVolcengineJiMengVideo, model.ChannelInterfaceGeminiVeo:
		return "video"
	case model.ChannelInterfaceChatCompletion, model.ChannelInterfaceOpenAIResponse:
		return "text"
	default:
		return ""
	}
}
