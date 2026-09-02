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
		bundledChannelManifest(
			"weijin-channel",
			"维今 ONE API 渠道",
			"维今 ONE API",
			"维今 ONE 视频生成渠道，支持动态模型能力、全模态参考素材和严格任务状态。",
			"https://www.weijinapi.top",
			[]string{"weijin-video"},
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
	result = append(result, aiStarsLabImageAdapter(), aiStarsLabVideoAdapter(), weijinVideoAdapter())
	for _, info := range items {
		if info.ID == "aistarslab-image" || info.ID == "aistarslab-video" || info.ID == "weijin-video" {
			continue
		}
		result = append(result, builtinAdapter{info: info})
	}
	return result
}

func weijinVideoAdapter() Adapter {
	info := customChannelMetadataByIDMust("weijin-video")
	return builtinAdapter{
		info: info,
		create: func(request GenerationRequest) (RequestSpec, error) {
			body := map[string]any{
				"model":        strings.TrimSpace(request.Model),
				"prompt":       strings.TrimSpace(request.Prompt),
				"seconds":      request.Duration,
				"aspect_ratio": strings.TrimSpace(request.AspectRatio),
			}
			if resolution := weijinRequestResolution(request.Model, defaultValue(request.Resolution, request.Quality)); resolution != "" {
				body["resolution"] = resolution
			}
			if values := mediaValues(request.Images); len(values) > 0 {
				body["images"] = values
			}
			if values := mediaValues(request.Videos); len(values) > 0 {
				body["videos"] = values
			}
			if values := mediaValues(request.Audios); len(values) > 0 {
				body["audios"] = values
			}
			return jsonSpec(http.MethodPost, "/v1/videos", compactMap(body)), nil
		},
		parseCreate: weijinCreateResult,
		poll: func(c PollContext) (RequestSpec, error) {
			return RequestSpec{Method: http.MethodGet, Path: "/v1/videos/" + url.PathEscape(c.TaskID)}, nil
		},
		parsePoll: weijinPollResult,
	}
}

func weijinRequestResolution(modelID, resolution string) string {
	resolution = strings.TrimSpace(resolution)
	if resolution == "" || strings.EqualFold(resolution, "auto") {
		return ""
	}
	normalizedModel := strings.ToLower(strings.TrimSpace(modelID))
	for _, marker := range []string{"480p", "720p", "1080p", "2160p", "4k"} {
		if strings.Contains(normalizedModel, marker) {
			return ""
		}
	}
	return resolution
}

func weijinStatus(payload map[string]any) (Status, error) {
	raw := strings.ToLower(strings.TrimSpace(firstString(payload, "status")))
	switch raw {
	case "queued":
		return StatusPending, nil
	case "in_progress":
		return StatusProcessing, nil
	case "completed":
		return StatusSucceeded, nil
	case "failed":
		return StatusFailed, nil
	default:
		return "", fmt.Errorf("Weijin response has unknown status %q", raw)
	}
}

func weijinTaskID(payload map[string]any) string {
	return firstString(payload, "task_id", "id")
}

func weijinFailureMessage(payload map[string]any) string {
	if failure := object(payload["error"]); failure != nil {
		return firstString(failure, "message", "code")
	}
	return firstString(payload, "message", "error")
}

func weijinCreateResult(payload map[string]any) (CreateResult, error) {
	id := weijinTaskID(payload)
	if id == "" {
		return CreateResult{}, fmt.Errorf("Weijin response has no task id")
	}
	status, err := weijinStatus(payload)
	if err != nil {
		return CreateResult{}, err
	}
	return CreateResult{TaskID: id, Status: status, Message: weijinFailureMessage(payload)}, nil
}

func weijinPollResult(c PollContext, payload map[string]any) (PollResult, error) {
	status, err := weijinStatus(payload)
	if err != nil {
		return PollResult{}, err
	}
	id := defaultValue(weijinTaskID(payload), c.TaskID)
	if status == StatusFailed {
		return PollResult{TaskID: id, Status: status, Message: weijinFailureMessage(payload)}, nil
	}
	if status != StatusSucceeded {
		return PollResult{TaskID: id, Status: status}, nil
	}
	resultURL := firstString(payload, "result_url", "video_url", "url", "content")
	if resultURL == "" {
		return PollResult{}, fmt.Errorf("Weijin task %s completed without a result URL", id)
	}
	return PollResult{TaskID: id, Status: status, Result: &Result{Videos: []MediaReference{{URL: resultURL, Kind: string(CapabilityVideo), Ephemeral: true}}}}, nil
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
	if message := aiStarsLabResponseFailure(payload); message != "" {
		return PollResult{TaskID: defaultValue(aiStarsLabTaskIDFromPayload(payload), c.TaskID), Status: StatusFailed, Message: message}, nil
	}
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
	weijinVideo := metadata("weijin-video", "维今 ONE 视频", "维今 ONE API", CapabilityVideo, "POST /v1/videos", "GET /v1/videos/{task_id}", "application/json")
	weijinVideo.Parameters = []Parameter{
		{Name: "model", Type: "string", Required: true, Mapping: "model"},
		{Name: "prompt", Type: "string", Required: true, Mapping: "prompt"},
		{Name: "duration", Type: "integer", Mapping: "seconds"},
		{Name: "aspectRatio", Type: "string", Mapping: "aspect_ratio"},
		{Name: "resolution", Type: "string", Mapping: "resolution"},
		{Name: "images", Type: "media[]", Mapping: "images"},
		{Name: "videos", Type: "media[]", Mapping: "videos"},
		{Name: "audios", Type: "media[]", Mapping: "audios"},
	}
	weijinVideo.RequiresPublicMediaURLs = true
	return []Metadata{
		globalImage,
		globalVideo,
		huiQuYunVideo,
		aiStarsLabImage,
		aiStarsLabVideo,
		weijinVideo,
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
			Response:                bundledChannelManifestResponse(info.ID),
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
		} else if info.ID == "weijin-video" {
			provider.Create.Fields = map[string]string{
				"model":        "request.model",
				"prompt":       "request.prompt",
				"seconds":      "request.duration|int",
				"aspect_ratio": "request.aspectRatio|omit_empty",
				"resolution":   "request.resolution|omit_auto",
				"images":       "request.images|media_urls",
				"videos":       "request.videos|media_urls",
				"audios":       "request.audios|media_urls",
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

func bundledChannelManifestResponse(providerID string) ManifestResponse {
	if providerID == "weijin-video" {
		return ManifestResponse{
			TaskIDPaths: []string{"task_id", "id"}, StatusPaths: []string{"status"},
			MessagePaths:   []string{"error.message", "error.code", "message"},
			ResultURLPaths: []string{"result_url", "video_url", "url", "content"}, ResultKind: "video", ResultEphemeral: true,
		}
	}
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
