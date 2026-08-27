package protocol

import "strings"

const bundledHostProvidersRuntime = "host:providers"

// BundledHostManifests groups related host-backed providers into one plugin
// lifecycle unit. The provider IDs remain the stable model protocol IDs.
func BundledHostManifests() []Manifest {
	return []Manifest{
		bundledChannelManifest(
			"globalaiopc-channel",
			"GlobalAiOpc 渠道",
			"GlobalAiOpc",
			"GlobalAiOpc 图片与视频生成渠道。",
			"https://zcbservice.aizfw.cn/kyyReactApiServer",
			[]string{"globalaiopc-image", "globalaiopc-video"},
		),
		bundledChannelManifest(
			"huiquyun-channel",
			"汇取云渠道",
			"汇取云",
			"汇取云视频生成渠道；文本、图片与音频模型继续使用对应的通用协议插件。",
			"https://api.bjhuiqu.net/v1",
			[]string{"huiquyun-video"},
		),
		bundledChannelManifest(
			"aistarslab-channel",
			"AIStarsLab 渠道",
			"AIStarsLab",
			"AIStarsLab 图片与视频生成渠道，保留模型线路编码和能力目录。",
			"https://api.video.aistarslab.com/openapi",
			[]string{"aistarslab-image", "aistarslab-video"},
		),
	}
}

func BundledHostProviderIDs() map[string]struct{} {
	result := make(map[string]struct{})
	for _, manifest := range BundledHostManifests() {
		for _, provider := range manifest.Contributes.Providers {
			result[provider.ID] = struct{}{}
		}
	}
	return result
}

func customChannelAdapters() []Adapter {
	items := customChannelMetadata()
	result := make([]Adapter, 0, len(items))
	for _, info := range items {
		result = append(result, builtinAdapter{info: info})
	}
	return result
}

func customChannelMetadata() []Metadata {
	globalImage := metadata("globalaiopc-image", "GlobalAiOpc 图片", "GlobalAiOpc", CapabilityImage, "POST /v2/model-center/tasks", "GET /v2/model-center/tasks/{task_id}", "application/json")
	globalImage.Parameters = mediaParams()
	globalImage.RequiresPublicMediaURLs = true
	globalVideo := metadata("globalaiopc-video", "GlobalAiOpc 视频", "GlobalAiOpc", CapabilityVideo, "POST /v2/model-center/tasks", "GET /v2/model-center/tasks/{task_id}", "application/json")
	globalVideo.Parameters = videoParams()
	globalVideo.RequiresPublicMediaURLs = true
	huiQuYunVideo := metadata("huiquyun-video", "汇取云视频", "汇取云", CapabilityVideo, "POST /videos/generations", "GET /videos/{task_id}", "application/json or multipart/form-data")
	huiQuYunVideo.Parameters = videoParams()
	aiStarsLabImage := metadata("aistarslab-image", "AIStarsLab 图片", "AIStarsLab", CapabilityImage, "POST /generation/create/image", "GET /generation/status?taskId={task_id}", "application/json")
	aiStarsLabImage.Parameters = mediaParams()
	aiStarsLabImage.RequiresPublicMediaURLs = true
	aiStarsLabVideo := metadata("aistarslab-video", "AIStarsLab 视频", "AIStarsLab", CapabilityVideo, "POST /generation/create/video", "GET /generation/status?taskId={task_id}", "application/json")
	aiStarsLabVideo.Parameters = videoParams()
	aiStarsLabVideo.RequiresPublicMediaURLs = true
	return []Metadata{
		globalImage,
		globalVideo,
		huiQuYunVideo,
		aiStarsLabImage,
		aiStarsLabVideo,
	}
}

func bundledChannelManifest(id, name, vendor, description, baseURL string, providerIDs []string) Manifest {
	providers := make([]ManifestProvider, 0, len(providerIDs))
	for _, providerID := range providerIDs {
		info, ok := customChannelMetadataByID(providerID)
		if !ok {
			continue
		}
		providers = append(providers, ManifestProvider{
			ID:                      info.ID,
			Label:                   info.Name,
			Capabilities:            info.Categories,
			Scopes:                  info.Scopes,
			BaseURL:                 baseURL,
			RequiresPublicMediaURLs: info.RequiresPublicMediaURLs,
			Auth:                    ManifestAuth{Type: "bearer", Field: "apiKey", Header: "Authorization"},
			Parameters:              info.Parameters,
			Create:                  manifestOperation(info.Create),
			Poll:                    manifestOperationPtr(info.Poll),
			Response:                ManifestResponse{},
		})
	}
	return Manifest{
		APIVersion: "yingce.plugin/v1",
		Metadata: Metadata{
			ID:            id,
			Version:       "1.0.0",
			Name:          name,
			Vendor:        vendor,
			Description:   description,
			Documentation: bundledChannelDocumentation(name, description),
			Enabled:       true,
			Installable:   true,
		},
		Runtime:     ManifestRuntime{Backend: bundledHostProvidersRuntime},
		Permissions: []string{"generation.run"},
		Contributes: ManifestContributions{Providers: providers},
	}
}

func customChannelMetadataByID(id string) (Metadata, bool) {
	for _, info := range customChannelMetadata() {
		if info.ID == id {
			return info, true
		}
	}
	return Metadata{}, false
}

func manifestOperation(summary string) ManifestOperation {
	parts := strings.SplitN(strings.TrimSpace(summary), " ", 2)
	operation := ManifestOperation{Method: "POST", Path: "/__host__"}
	if len(parts) == 2 {
		operation.Method = parts[0]
		operation.Path = strings.ReplaceAll(parts[1], "{task_id}", "{{taskId}}")
	}
	return operation
}

func manifestOperationPtr(summary string) *ManifestOperation {
	if strings.TrimSpace(summary) == "" {
		return nil
	}
	operation := manifestOperation(summary)
	return &operation
}

func bundledChannelDocumentation(name, description string) string {
	return "# " + name + "\n\n" + description + "\n\n## 影策运行时合同\n\n- 插件只负责该渠道的协议选择、请求映射、异步轮询与结果解析。\n- API Key 由后端渠道配置读取，不写入插件清单、浏览器 URL 或任务日志。\n- 停用此插件后，其贡献的全部模型请求协议立即不可选，已保存配置也不能继续发起新任务。\n- 参考素材按协议要求转换为上游可访问的公网 URL；任务、计费、重试和资源保存仍由影策宿主负责。\n"
}
