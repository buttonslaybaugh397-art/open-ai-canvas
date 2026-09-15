package protocol

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func tianYueChannelManifest() Manifest {
	manifest := bundledChannelManifest(
		"tianyue-channel", "天悦渠道", "天悦",
		"天悦视频生成渠道，区分按次与按秒计费用量，支持图片、视频与音频参考。",
		"https://api.tianyue.xyz", []string{"tianyue-video"},
	)
	info := tianYueVideoMetadata()
	attachDocumentation(&info)
	manifest.Metadata.Documentation = info.Documentation
	manifest.Contributes.Providers[0].Create.ContentType = "application/json"
	return manifest
}

func tianYueVideoMetadata() Metadata {
	info := metadata("tianyue-video", "天悦视频", "天悦", CapabilityVideo, "POST /v1/videos", "GET /v1/videos/{task_id}", "application/json")
	info.RequiresPublicMediaURLs = true
	info.Parameters = []Parameter{
		{Name: "model", Type: "string", Required: true, Mapping: "model", Description: "按天悦模型广场配置填写实际模型名，不预置模型列表。"},
		{Name: "prompt", Type: "string", Required: true, Mapping: "prompt"},
		{Name: "duration", Type: "integer", Required: true, Mapping: "video_duration", Description: "真实时长（秒）。额外的上游 duration 为计费用量：-se 结尾模型取真实秒数，其余固定为 1；不允许透传覆盖。"},
		{Name: "aspectRatio", Type: "string", Mapping: "aspect_ratio", Values: []string{"16:9", "9:16", "4:3", "3:4", "1:1"}},
		{Name: "resolution", Type: "string", Required: true, Mapping: "resolution", Values: []string{"480p", "720p", "1080p"}},
		{Name: "images", Type: "media[]", Mapping: "image_urls", Description: "始终发送数组；数量按模型能力配置校验。"},
		{Name: "videos", Type: "media[]", Mapping: "video_urls"},
		{Name: "audios", Type: "media[]", Mapping: "audio_urls"},
	}
	return info
}

func tianYueVideoAdapter() Adapter {
	return builtinAdapter{
		info: tianYueVideoMetadata(),
		create: func(request GenerationRequest) (RequestSpec, error) {
			body, err := tianYueVideoBody(request)
			if err != nil {
				return RequestSpec{}, err
			}
			return jsonSpec(http.MethodPost, "/v1/videos", body), nil
		},
		parseCreate: func(payload map[string]any) (CreateResult, error) {
			id := firstString(payload, "task_id", "id")
			if id == "" {
				return CreateResult{}, fmt.Errorf("天悦创建响应缺少任务 ID")
			}
			state, err := tianYueVideoPollResult(PollContext{TaskID: id}, payload)
			if err != nil {
				return CreateResult{}, err
			}
			return CreateResult{TaskID: id, Status: state.Status, Result: state.Result, Message: state.Message}, nil
		},
		poll: func(c PollContext) (RequestSpec, error) {
			if strings.TrimSpace(c.TaskID) == "" {
				return RequestSpec{}, fmt.Errorf("天悦查询任务 ID 不能为空")
			}
			return RequestSpec{Method: http.MethodGet, Path: "/v1/videos/" + url.PathEscape(c.TaskID)}, nil
		},
		parsePoll: tianYueVideoPollResult,
	}
}

func tianYueVideoBody(request GenerationRequest) (map[string]any, error) {
	modelID := strings.TrimSpace(request.Model)
	prompt := strings.TrimSpace(request.Prompt)
	if modelID == "" || prompt == "" {
		return nil, fmt.Errorf("天悦视频需要模型名和提示词")
	}
	if request.Duration <= 0 {
		return nil, fmt.Errorf("天悦视频需要明确的正整数时长，请按模型配置填写")
	}
	resolution := strings.ToLower(strings.TrimSpace(request.Resolution))
	switch resolution {
	case "480", "720", "1080":
		resolution += "p"
	case "480p", "720p", "1080p":
	default:
		return nil, fmt.Errorf("天悦视频需要明确的分辨率：480p、720p 或 1080p")
	}
	ratio := strings.TrimSpace(request.AspectRatio)
	switch ratio {
	case "", "16:9", "9:16", "4:3", "3:4", "1:1":
	default:
		return nil, fmt.Errorf("天悦视频不支持该画幅比例")
	}
	// duration is upstream billing usage, not playback duration. Never merge
	// caller extras into this body: a per-request model must always send 1.
	billingDuration := 1
	if strings.HasSuffix(modelID, "-se") {
		billingDuration = request.Duration
	}
	body := map[string]any{
		"model": modelID, "prompt": prompt,
		"duration": billingDuration, "video_duration": request.Duration,
		"resolution": resolution, "image_urls": mediaValues(request.Images),
	}
	if ratio != "" {
		body["aspect_ratio"] = ratio
	}
	if values := mediaValues(request.Videos); len(values) > 0 {
		body["video_urls"] = values
	}
	if values := mediaValues(request.Audios); len(values) > 0 {
		body["audio_urls"] = values
	}
	return body, nil
}

func tianYueVideoPollResult(c PollContext, payload map[string]any) (PollResult, error) {
	id := firstString(payload, "task_id", "id")
	if id != "" && c.TaskID != "" && id != c.TaskID {
		return PollResult{}, fmt.Errorf("天悦查询响应的任务 ID 与请求不一致")
	}
	result := PollResult{TaskID: defaultValue(id, c.TaskID)}
	switch firstString(payload, "status") {
	case "queued":
		result.Status = StatusPending
	case "in_progress":
		result.Status = StatusProcessing
	case "failed":
		result.Status, result.Message = StatusFailed, channelFailure(payload)
	case "completed":
		resultURL := firstString(payload, "video_url", "url")
		if resultURL == "" {
			resultURL = firstString(object(payload["metadata"]), "url")
		}
		if resultURL == "" {
			return PollResult{}, fmt.Errorf("天悦任务已完成但没有返回视频地址")
		}
		result.Status = StatusSucceeded
		result.Result = &Result{Videos: []MediaReference{{URL: resultURL, Kind: "video", Ephemeral: true}}}
	default:
		return PollResult{}, fmt.Errorf("天悦响应包含未知任务状态")
	}
	return result, nil
}
