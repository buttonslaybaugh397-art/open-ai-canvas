package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"infinite-canvas/backend/internal/model"
)

type ChannelModelsRequest struct {
	BaseURL   string           `json:"baseUrl"`
	APIKey    string           `json:"apiKey"`
	APIFormat string           `json:"apiFormat"`
	Headers   []OutboundHeader `json:"headers"`
}

type channelModelsPayload struct {
	Data   []channelModelItem `json:"data"`
	Models []channelModelItem `json:"models"`
	Error  *providerError     `json:"error"`
	Code   *int               `json:"code"`
	Msg    string             `json:"msg"`
}

type channelModelItem struct {
	ID                     string                        `json:"id"`
	Name                   string                        `json:"name"`
	DisplayName            string                        `json:"display_name"`
	ModelType              string                        `json:"model_type"`
	SupportedEndpointTypes []string                      `json:"supported_endpoint_types"`
	DefaultParameters      channelModelCatalogParameters `json:"default_parameters"`
	Options                channelModelCatalogOptions    `json:"options"`
	SupportsImages         *bool                         `json:"supports_images"`
	MinImages              *int                          `json:"min_images"`
	MaxImages              *int                          `json:"max_images"`
}

type channelModelCatalogParameters struct {
	AspectRatio     string `json:"aspect_ratio"`
	DurationSeconds string `json:"duration_seconds"`
	Resolution      string `json:"resolution"`
}

type channelModelCatalogOptions struct {
	AspectRatio     []ChannelModelCatalogOption `json:"aspect_ratio"`
	DurationSeconds []ChannelModelCatalogOption `json:"duration_seconds"`
	Resolution      []ChannelModelCatalogOption `json:"resolution"`
}

type aiStarsLabConfig struct {
	ImageConfig []aiStarsLabChannel `json:"imageConfig"`
	VideoConfig []aiStarsLabChannel `json:"videoConfig"`
}

type aiStarsLabChannel struct {
	Channel       string            `json:"channel"`
	Title         string            `json:"title"`
	DefaultOption aiStarsLabBool    `json:"defaultOption"`
	Models        []aiStarsLabModel `json:"models"`
}

type aiStarsLabBool bool

func (value *aiStarsLabBool) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var boolean bool
	if err := json.Unmarshal(trimmed, &boolean); err == nil {
		*value = aiStarsLabBool(boolean)
		return nil
	}
	var text string
	if err := json.Unmarshal(trimmed, &text); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "true":
		*value = true
	case "false":
		*value = false
	default:
		return fmt.Errorf("invalid boolean value %q", text)
	}
	return nil
}

type aiStarsLabModel struct {
	Model          string              `json:"model"`
	Label          string              `json:"label"`
	Qualities      []aiStarsLabQuality `json:"qualities"`
	AspectRatios   []string            `json:"aspectRatios"`
	Duration       aiStarsLabDuration  `json:"duration"`
	Modes          []string            `json:"modes"`
	InputImagesMax int                 `json:"inputImagesMax"`
	InputVideosMax int                 `json:"inputVideosMax"`
	InputAudiosMax int                 `json:"inputAudiosMax"`
}

type aiStarsLabDuration struct {
	Min     int
	Max     int
	Options []int
}

func (value *aiStarsLabDuration) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	if trimmed[0] == '[' {
		var options []int
		if err := json.Unmarshal(trimmed, &options); err != nil {
			return err
		}
		value.Options = normalizePositiveInts(options)
		return nil
	}
	var config struct {
		Min     int   `json:"min"`
		Max     int   `json:"max"`
		Options []int `json:"options"`
	}
	if err := json.Unmarshal(trimmed, &config); err != nil {
		return err
	}
	value.Min = config.Min
	value.Max = config.Max
	value.Options = normalizePositiveInts(config.Options)
	return nil
}

type aiStarsLabQuality struct {
	Quality string `json:"quality"`
}

func (s *Service) FetchChannelModels(ctx context.Context, actor *model.User, input ChannelModelsRequest) ([]string, error) {
	items, err := s.FetchChannelModelCatalog(ctx, actor, input)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(items))
	models := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(item.ID)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		models = append(models, name)
	}
	sort.Strings(models)
	return models, nil
}

// ChannelModelCatalogItem 是前端自定义渠道拉取模型目录后的最小合同；
// 协议、能力和可选参数均来自上游公开元数据，不展开供应商内部兼容模型。
type ChannelModelCatalogItem struct {
	ID                     string                               `json:"id"`
	DisplayName            string                               `json:"displayName,omitempty"`
	ModelType              string                               `json:"modelType,omitempty"`
	SupportedEndpointTypes []string                             `json:"supportedEndpointTypes,omitempty"`
	DefaultParameters      ChannelModelCatalogDefaultParameters `json:"defaultParameters,omitempty"`
	Options                ChannelModelCatalogOptions           `json:"options,omitempty"`
	SupportsImages         *bool                                `json:"supportsImages,omitempty"`
	MinImages              *int                                 `json:"minImages,omitempty"`
	MaxImages              *int                                 `json:"maxImages,omitempty"`
	AIStarsLab             *AIStarsLabCatalogRoute              `json:"aistarslab,omitempty"`
}

type AIStarsLabCatalogRoute struct {
	Channel        string   `json:"channel"`
	Capability     string   `json:"capability"`
	Model          string   `json:"model"`
	Qualities      []string `json:"qualities,omitempty"`
	AspectRatios   []string `json:"aspectRatios,omitempty"`
	Duration       []int    `json:"duration,omitempty"`
	DurationMin    int      `json:"durationMin,omitempty"`
	DurationMax    int      `json:"durationMax,omitempty"`
	Modes          []string `json:"modes,omitempty"`
	InputImagesMax int      `json:"inputImagesMax,omitempty"`
	InputVideosMax int      `json:"inputVideosMax,omitempty"`
	InputAudiosMax int      `json:"inputAudiosMax,omitempty"`
}

type ChannelModelCatalogDefaultParameters struct {
	AspectRatio     string `json:"aspectRatio,omitempty"`
	DurationSeconds string `json:"durationSeconds,omitempty"`
	Resolution      string `json:"resolution,omitempty"`
}

type ChannelModelCatalogOptions struct {
	AspectRatio     []ChannelModelCatalogOption `json:"aspectRatio,omitempty"`
	DurationSeconds []ChannelModelCatalogOption `json:"durationSeconds,omitempty"`
	Resolution      []ChannelModelCatalogOption `json:"resolution,omitempty"`
}

type ChannelModelCatalogOption struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

func (s *Service) FetchChannelModelCatalog(ctx context.Context, actor *model.User, input ChannelModelsRequest) ([]ChannelModelCatalogItem, error) {
	if actor == nil || strings.TrimSpace(actor.ID) == "" {
		return nil, Unauthorized("请先登录")
	}
	baseURL := strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	apiKey := strings.TrimSpace(input.APIKey)
	if baseURL == "" {
		return nil, BadAuthRequest("请填写 Base URL")
	}
	if apiKey == "" {
		return nil, BadAuthRequest("请填写 API Key")
	}
	apiFormat := strings.ToLower(strings.TrimSpace(input.APIFormat))
	if apiFormat == "" {
		apiFormat = "openai"
	}
	if apiFormat != "openai" && apiFormat != "gemini" {
		return nil, BadAuthRequest("接口协议不支持拉取模型")
	}
	headers, err := NormalizeOutboundHeaders(input.Headers)
	if err != nil {
		return nil, err
	}
	if isAIStarsLabBaseURL(baseURL) {
		if !s.protocolIsSelectable(string(model.ChannelInterfaceAIStarsLabImage)) && !s.protocolIsSelectable(string(model.ChannelInterfaceAIStarsLabVideo)) {
			return nil, BadAuthRequest("AIStarsLab 渠道插件未启用，请先在插件中心启用")
		}
		return s.fetchAIStarsLabCatalog(ctx, baseURL, apiKey, headers)
	}

	target := apiURL(baseURL, "/models")
	if apiFormat == "gemini" {
		if !strings.HasSuffix(strings.ToLower(baseURL), "/v1beta") {
			baseURL += "/v1beta"
		}
		target = baseURL + "/models"
	}
	if _, err := ValidateOutboundURL(target); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, BadAuthRequest("模型服务地址无效")
	}
	if apiFormat == "gemini" {
		request.Header.Set("x-goog-api-key", apiKey)
	} else {
		request.Header.Set("Authorization", "Bearer "+apiKey)
	}
	ApplyOutboundHeaders(request, headers)

	// 只代理固定的模型目录 GET；用户密钥仅用于本次请求，不写入数据库或日志。
	data, _, err := doBinary(request)
	if err != nil {
		return nil, channelModelsUpstreamError(err)
	}
	var payload channelModelsPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, WrapAppError(http.StatusBadGateway, "模型服务返回的不是有效 JSON", err)
	}
	if payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
		return nil, NewAppError(http.StatusBadGateway, "模型服务返回失败，请检查渠道配置")
	}
	if payload.Code != nil && *payload.Code != 0 {
		return nil, NewAppError(http.StatusBadGateway, "模型服务返回失败，请检查渠道配置")
	}

	items := payload.Data
	if apiFormat == "gemini" {
		items = payload.Models
	}
	seen := make(map[string]bool, len(items))
	catalog := make([]ChannelModelCatalogItem, 0, len(items))
	for _, item := range items {
		name := strings.TrimPrefix(strings.TrimSpace(firstNonEmpty(item.ID, item.Name)), "models/")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		catalog = append(catalog, ChannelModelCatalogItem{
			ID:                     name,
			DisplayName:            strings.TrimSpace(item.DisplayName),
			ModelType:              normalizeCatalogModelType(item.ModelType),
			SupportedEndpointTypes: normalizeCatalogEndpointTypes(item.SupportedEndpointTypes),
			DefaultParameters: ChannelModelCatalogDefaultParameters{
				AspectRatio:     strings.TrimSpace(item.DefaultParameters.AspectRatio),
				DurationSeconds: strings.TrimSpace(item.DefaultParameters.DurationSeconds),
				Resolution:      strings.TrimSpace(item.DefaultParameters.Resolution),
			},
			Options: ChannelModelCatalogOptions{
				AspectRatio:     normalizeCatalogOptions(item.Options.AspectRatio),
				DurationSeconds: normalizeCatalogOptions(item.Options.DurationSeconds),
				Resolution:      normalizeCatalogOptions(item.Options.Resolution),
			},
			SupportsImages: item.SupportsImages,
			MinImages:      item.MinImages,
			MaxImages:      item.MaxImages,
		})
	}
	sort.Slice(catalog, func(left int, right int) bool {
		return catalog[left].ID < catalog[right].ID
	})
	if s.isPluginEnabled() {
		catalog = extendChannelModelCatalog(baseURL, apiFormat, headers, catalog)
	}
	return catalog, nil
}

func isAIStarsLabBaseURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	path := strings.TrimRight(strings.ToLower(strings.TrimSpace(parsed.Path)), "/")
	return host == "api.video.aistarslab.com" && path == "/openapi"
}

func (s *Service) fetchAIStarsLabCatalog(ctx context.Context, baseURL, apiKey string, headers []OutboundHeader) ([]ChannelModelCatalogItem, error) {
	target := strings.TrimRight(baseURL, "/") + "/generation/config"
	if _, err := ValidateOutboundURL(target); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, BadAuthRequest("模型服务地址无效")
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Accept", "application/json")
	ApplyOutboundHeaders(request, headers)
	request.Header.Del("Accept-Encoding")

	data, _, err := doBinary(request)
	if err != nil {
		return nil, channelModelsUpstreamError(err)
	}
	var envelope struct {
		Code int              `json:"code"`
		Msg  string           `json:"msg"`
		Data aiStarsLabConfig `json:"data"`
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	trimmed := bytes.TrimSpace(data)
	if !json.Valid(trimmed) {
		return nil, NewAppError(http.StatusBadGateway, "AIStarsLab 返回的不是有效 JSON，请检查渠道地址和请求头")
	}
	if err := json.Unmarshal(trimmed, &envelope); err != nil {
		return nil, WrapAppError(http.StatusBadGateway, "AIStarsLab 模型目录字段格式不兼容，请检查渠道接口版本", err)
	}
	if envelope.Code != 0 {
		return nil, NewAppError(http.StatusBadGateway, firstNonEmpty(strings.TrimSpace(envelope.Msg), "AIStarsLab 配置读取失败"))
	}

	items := make([]ChannelModelCatalogItem, 0, 16)
	appendChannels := func(capability string, channels []aiStarsLabChannel) {
		for _, channel := range channels {
			channelID := strings.TrimSpace(channel.Channel)
			if channelID == "" {
				continue
			}
			for _, upstreamModel := range channel.Models {
				name := strings.TrimSpace(upstreamModel.Model)
				if name == "" {
					continue
				}
				qualities := make([]string, 0, len(upstreamModel.Qualities))
				for _, quality := range upstreamModel.Qualities {
					value := strings.TrimSpace(quality.Quality)
					if value == "" {
						continue
					}
					if capability == "video" {
						value = normalizeResolution(value)
					}
					qualities = append(qualities, value)
				}
				items = append(items, ChannelModelCatalogItem{
					ID:                     channelID + ":" + name,
					DisplayName:            aiStarsLabCatalogDisplayName(upstreamModel.Label, name, firstNonEmpty(strings.TrimSpace(channel.Title), channelID)),
					ModelType:              capability,
					SupportedEndpointTypes: []string{"aistarslab-" + capability},
					AIStarsLab: &AIStarsLabCatalogRoute{
						Channel: channelID, Capability: capability, Model: name, Qualities: qualities,
						AspectRatios: upstreamModel.AspectRatios, Duration: upstreamModel.Duration.Options,
						DurationMin: upstreamModel.Duration.Min, DurationMax: upstreamModel.Duration.Max,
						Modes: upstreamModel.Modes, InputImagesMax: upstreamModel.InputImagesMax,
						InputVideosMax: upstreamModel.InputVideosMax, InputAudiosMax: upstreamModel.InputAudiosMax,
					},
				})
			}
		}
	}
	appendChannels("image", envelope.Data.ImageConfig)
	appendChannels("video", envelope.Data.VideoConfig)
	sort.SliceStable(items, func(left, right int) bool { return items[left].ID < items[right].ID })
	return items, nil
}

func aiStarsLabCatalogDisplayName(label, modelName, routeTitle string) string {
	base := strings.TrimSpace(label)
	if base == "" {
		base = strings.TrimSpace(modelName)
	}
	suffix := strings.TrimSpace(routeTitle)
	if suffix == "" || strings.Contains(base, suffix) {
		return base
	}
	return base + "（" + suffix + "）"
}

func normalizePositiveInts(values []int) []int {
	seen := make(map[int]bool, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func normalizeCatalogModelType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	switch normalized {
	case "text", "image", "video", "audio":
		return normalized
	default:
		return ""
	}
}

func normalizeCatalogOptions(options []ChannelModelCatalogOption) []ChannelModelCatalogOption {
	seen := make(map[string]bool, len(options))
	normalized := make([]ChannelModelCatalogOption, 0, len(options))
	for _, option := range options {
		value := strings.TrimSpace(option.Value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		normalized = append(normalized, ChannelModelCatalogOption{Value: value, Label: strings.TrimSpace(option.Label)})
	}
	return normalized
}

func normalizeCatalogEndpointTypes(values []string) []string {
	seen := make(map[string]bool, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		item := strings.TrimSpace(value)
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		normalized = append(normalized, item)
	}
	return normalized
}

func channelModelsUpstreamError(err error) error {
	var authErr *AuthError
	if errors.As(err, &authErr) {
		return authErr
	}
	var httpErr providerHTTPError
	if !errors.As(err, &httpErr) {
		return WrapAppError(http.StatusBadGateway, "连接模型服务失败，请检查渠道地址和网络", err)
	}
	switch httpErr.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return NewAppError(http.StatusBadGateway, "模型服务鉴权失败，请检查 API Key")
	case http.StatusNotFound:
		return NewAppError(http.StatusBadGateway, "模型服务未提供 /models 接口")
	case http.StatusTooManyRequests:
		return NewAppError(http.StatusBadGateway, "模型服务请求过于频繁或额度不足")
	default:
		return WrapAppError(http.StatusBadGateway, httpErr.Error(), err)
	}
}
