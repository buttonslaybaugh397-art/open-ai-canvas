package protocol

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

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
	result = append(result, aiStarsLabImageAdapter(), aiStarsLabVideoAdapter())
	for _, info := range items {
		if info.ID == "aistarslab-image" || info.ID == "aistarslab-video" {
			continue
		}
		result = append(result, builtinAdapter{info: info})
	}
	return result
}

func aiStarsLabImageAdapter() Adapter {
	info := customChannelMetadataByIDMust("aistarslab-image")
	return builtinAdapter{
		info: info,
		create: func(request GenerationRequest) (RequestSpec, error) {
			return jsonSpec(http.MethodPost, "/generation/create/image", aiStarsLabImageBody(request)), nil
		},
		parseCreate: aiStarsLabCreate,
		poll:        aiStarsLabPoll,
		parsePoll: func(c PollContext, payload map[string]any) (PollResult, error) {
			return aiStarsLabPollMediaResult(c, payload, CapabilityImage)
		},
	}
}

func aiStarsLabVideoAdapter() Adapter {
	info := customChannelMetadataByIDMust("aistarslab-video")
	return builtinAdapter{
		info: info,
		create: func(request GenerationRequest) (RequestSpec, error) {
			return jsonSpec(http.MethodPost, "/generation/create/video", aiStarsLabVideoBody(request)), nil
		},
		parseCreate: aiStarsLabCreate,
		poll:        aiStarsLabPoll,
		parsePoll: func(c PollContext, payload map[string]any) (PollResult, error) {
			return aiStarsLabPollMediaResult(c, payload, CapabilityVideo)
		},
	}
}

func customChannelMetadataByIDMust(id string) Metadata {
	info, ok := customChannelMetadataByID(id)
	if !ok {
		panic(fmt.Sprintf("custom channel metadata %q is missing", id))
	}
	return info
}

func aiStarsLabExtra(request GenerationRequest, key string) string {
	if request.Extra == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(request.Extra[key]))
}

func aiStarsLabMediaURLs(values []MediaReference) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		if candidate := strings.TrimSpace(defaultValue(value.URL, value.DataURL)); candidate != "" {
			result = append(result, candidate)
		}
	}
	return result
}

func aiStarsLabImageBody(request GenerationRequest) map[string]any {
	modelName := aiStarsLabExtra(request, "aistarslabModel")
	if modelName == "" {
		modelName = request.Model
	}
	body := map[string]any{
		"channel":     aiStarsLabExtra(request, "aistarslabChannel"),
		"model":       modelName,
		"prompt":      request.Prompt,
		"aspectRatio": defaultValue(request.AspectRatio, "1:1"),
		"inputImages": aiStarsLabMediaURLs(request.Images),
		"n":           maxInt(request.ImageCount, 1),
	}
	if request.Quality != "" {
		body["quality"] = request.Quality
	}
	return body
}

func aiStarsLabVideoBody(request GenerationRequest) map[string]any {
	modelName := aiStarsLabExtra(request, "aistarslabModel")
	if modelName == "" {
		modelName = request.Model
	}
	body := map[string]any{
		"channel":     aiStarsLabExtra(request, "aistarslabChannel"),
		"model":       modelName,
		"prompt":      request.Prompt,
		"aspectRatio": request.AspectRatio,
		"duration":    request.Duration,
		"inputImages": aiStarsLabMediaURLs(request.Images),
		"inputVideos": aiStarsLabMediaURLs(request.Videos),
		"inputAudios": aiStarsLabMediaURLs(request.Audios),
	}
	if mode := aiStarsLabExtra(request, "aistarslabMode"); mode != "" {
		body["mode"] = mode
	}
	quality := defaultValue(request.Quality, request.Resolution)
	if quality != "" {
		body["quality"] = quality
	}
	return body
}

func maxInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func aiStarsLabPoll(c PollContext) (RequestSpec, error) {
	return RequestSpec{Method: http.MethodGet, Path: "/generation/status?taskId=" + url.QueryEscape(c.TaskID)}, nil
}

func aiStarsLabStatus(payload map[string]any) (Status, string) {
	state := payload
	if data := object(payload["data"]); data != nil {
		state = data
	}
	raw := strings.TrimSpace(fmt.Sprint(state["status"]))
	switch raw {
	case "1":
		return StatusPending, ""
	case "2":
		return StatusProcessing, ""
	case "3":
		return StatusSucceeded, ""
	case "4":
		return StatusFailed, firstString(state, "errorMessage", "errorCode", "msg", "message")
	default:
		return normalizeStatus(raw), firstString(state, "errorMessage", "errorCode", "msg", "message")
	}
}

func aiStarsLabTaskIDFromPayload(payload map[string]any) string {
	id := firstString(payload, "taskId", "task_id", "id")
	if data := object(payload["data"]); data != nil {
		id = defaultValue(firstString(data, "taskId", "task_id", "id"), id)
	}
	return id
}

func aiStarsLabResponseFailure(payload map[string]any) string {
	code := strings.TrimSpace(fmt.Sprint(payload["code"]))
	if code == "" || code == "<nil>" || code == "0" || strings.EqualFold(code, "ok") || strings.EqualFold(code, "success") {
		return ""
	}
	return firstString(payload, "msg", "message", "errorMessage", "errorCode", "code")
}

func aiStarsLabCreate(payload map[string]any) (CreateResult, error) {
	if message := aiStarsLabResponseFailure(payload); message != "" {
		return CreateResult{Status: StatusFailed, Message: message}, nil
	}
	id := aiStarsLabTaskIDFromPayload(payload)
	if id == "" {
		return CreateResult{}, fmt.Errorf("AIStarsLab response has no task id")
	}
	status, message := aiStarsLabStatus(payload)
	if status == "" {
		status = StatusPending
	}
	return CreateResult{TaskID: id, Status: status, Message: message}, nil
}

func aiStarsLabPollResult(c PollContext, payload map[string]any) (PollResult, error) {
	status, message := aiStarsLabStatus(payload)
	id := defaultValue(aiStarsLabTaskIDFromPayload(payload), c.TaskID)
	if status == StatusFailed {
		return PollResult{TaskID: id, Status: status, Message: message}, nil
	}
	if status == StatusSucceeded {
		state := payload
		if data := object(payload["data"]); data != nil {
			state = data
		}
		outputs, _ := state["outputs"].([]any)
		videos := make([]MediaReference, 0, len(outputs))
		for _, output := range outputs {
			if value, ok := output.(string); ok && strings.TrimSpace(value) != "" {
				videos = append(videos, MediaReference{URL: strings.TrimSpace(value), Kind: "video"})
			}
		}
		return PollResult{TaskID: id, Status: status, Result: &Result{Videos: videos}, Message: message}, nil
	}
	return PollResult{TaskID: id, Status: status, Message: message}, nil
}

func aiStarsLabPollMediaResult(c PollContext, payload map[string]any, capability Capability) (PollResult, error) {
	result, err := aiStarsLabPollResult(c, payload)
	if err != nil || result.Status != StatusSucceeded {
		return result, err
	}
	if result.Result == nil {
		return result, nil
	}
	if capability == CapabilityImage {
		for index := range result.Result.Videos {
			result.Result.Videos[index].Kind = string(CapabilityImage)
		}
		result.Result.Images = result.Result.Videos
		result.Result.Videos = nil
	}
	return result, nil
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
		provider := ManifestProvider{
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
			Response:                aiStarsLabManifestResponse(info.ID),
		}
		if info.ID == "aistarslab-image" {
			provider.Create.Fields = map[string]string{
				"channel":     "request.extra.aistarslabChannel",
				"model":       "request.extra.aistarslabModel",
				"prompt":      "request.prompt",
				"aspectRatio": "request.aspectRatio|trim",
				"quality":     "request.quality|omit_auto",
				"inputImages": "request.images|media_urls",
				"n":           "request.imageCount|int",
			}
		} else if info.ID == "aistarslab-video" {
			provider.Create.Fields = map[string]string{
				"channel":     "request.extra.aistarslabChannel",
				"model":       "request.extra.aistarslabModel",
				"prompt":      "request.prompt",
				"aspectRatio": "request.aspectRatio|trim",
				"quality":     "request.quality|omit_auto",
				"duration":    "request.duration|int",
				"mode":        "request.extra.aistarslabMode|omit_empty",
				"inputImages": "request.images|media_urls",
				"inputVideos": "request.videos|media_urls",
				"inputAudios": "request.audios|media_urls",
			}
		}
		providers = append(providers, provider)
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

func aiStarsLabManifestResponse(providerID string) ManifestResponse {
	response := ManifestResponse{
		TaskIDPaths:  []string{"data.taskId", "data.task_id", "taskId", "task_id", "id"},
		StatusPaths:  []string{"data.status", "status"},
		ErrorPaths:   []string{"code"},
		MessagePaths: []string{"data.errorMessage", "data.errorCode", "data.msg", "msg", "message"},
		ResultPaths:  []string{"data.outputs", "outputs"},
		ResultKind:   "video",
	}
	if providerID == "aistarslab-image" {
		response.ResultKind = "image"
	}
	return response
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
