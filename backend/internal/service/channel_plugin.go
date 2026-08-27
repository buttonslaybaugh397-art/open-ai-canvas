package service

import (
	"context"
	"strings"
)

// ChannelPlugin 描述渠道在统一中转和配置层的稳定身份。
// 生成协议仍由各接口实现负责，插件只提供可复用的渠道边界，避免渠道判断散落在 handler 和页面。
type ChannelPlugin struct {
	ID            string
	ProtocolID    string
	Label         string
	BaseURL       string
	RelayFormat   string
	Dedicated     bool
	StaticCatalog func() []ChannelModelCatalogItem
	FetchCatalog  func(*Service, context.Context, string, string, bool, []OutboundHeader) ([]ChannelModelCatalogItem, error)
}

var channelPlugins = []ChannelPlugin{
	{ID: "globalaiopc", ProtocolID: "globalaiopc-image", Label: "GlobalAiOpc", BaseURL: "https://zcbservice.aizfw.cn/kyyReactApiServer", RelayFormat: "globalaiopc", Dedicated: true, StaticCatalog: globalAiOpcCatalog},
	{ID: "huiquyun", ProtocolID: "huiquyun-video", Label: "汇取云", BaseURL: "https://api.bjhuiqu.net/v1", RelayFormat: "openai", Dedicated: true},
	{ID: "aistarslab", ProtocolID: "aistarslab-image", Label: "AIStarsLab", BaseURL: "https://api.video.aistarslab.com/openapi", RelayFormat: "aistarslab", Dedicated: true, FetchCatalog: func(s *Service, ctx context.Context, baseURL, apiKey string, allowLocal bool, headers []OutboundHeader) ([]ChannelModelCatalogItem, error) {
		return s.fetchAiStarsLabCatalog(ctx, baseURL, apiKey, allowLocal, headers)
	}},
}

func globalAiOpcCatalog() []ChannelModelCatalogItem {
	imageModels := []string{"seedream_5.0Pro", "seedream-5.0"}
	videoModels := []string{"seedance-2.5-c1", "seedance-2.5-c2", "seedance-2.5-c3", "sd_2.0_fast_special", "sd_2.0_special", "sd_2.0_discount", "sd_2.0_fast_discount", "seedance_1_5_pro_1080p", "seedance_1_5_pro_720p", "seedance_1_5_pro_480p", "MiniMax-H3-c4", "videos_933_c1", "videos_fast_933_c1", "videos_stable", "videos_stable_fast"}
	catalog := make([]ChannelModelCatalogItem, 0, len(imageModels)+len(videoModels))
	for _, id := range imageModels {
		catalog = append(catalog, ChannelModelCatalogItem{ID: id, SupportedEndpointTypes: []string{"globalaiopc-image"}})
	}
	for _, id := range videoModels {
		catalog = append(catalog, ChannelModelCatalogItem{ID: id, SupportedEndpointTypes: []string{"globalaiopc-video"}})
	}
	return catalog
}

func ListChannelPlugins() []ChannelPlugin {
	return append([]ChannelPlugin(nil), channelPlugins...)
}

func ChannelPluginFor(baseURL, connectionType string) *ChannelPlugin {
	normalizedConnection := strings.ToLower(strings.TrimSpace(connectionType))
	normalizedBaseURL := strings.ToLower(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	for index := range channelPlugins {
		plugin := &channelPlugins[index]
		if normalizedConnection == plugin.ID || normalizedBaseURL == strings.ToLower(strings.TrimRight(plugin.BaseURL, "/")) {
			return plugin
		}
	}
	return nil
}

// NormalizeCustomRelayFormat 将专用渠道格式映射到中转安全策略使用的协议族。
// 专用格式可以保留在请求头中作为扩展标识，但路径和内容类型仍遵循对应协议族。
func NormalizeCustomRelayFormat(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "openai", "huiquyun", "globalaiopc", "aistarslab":
		return "openai", true
	case "gemini":
		return "gemini", true
	default:
		return "", false
	}
}
