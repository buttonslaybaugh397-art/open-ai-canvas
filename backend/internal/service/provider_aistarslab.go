package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
)

func generationRequiresPublicReferenceURL(ctx context.Context, input canvasGenerationInput) bool {
	requirePublicURL := protocolRequiresPublicReferenceURL(input.Config.InterfaceType) || input.Config.InterfaceType == string(model.ChannelInterfaceMiniMaxVideo)
	if adapter, ok := protocolAdapterForContext(ctx, input.Config.InterfaceType); ok {
		requirePublicURL = requirePublicURL || adapter.Metadata().RequiresPublicMediaURLs
	}
	if input.Config.InterfaceType == string(model.ChannelInterfaceHuiQuYunVideo) && huiQuYunUsesMultipartVideoRequest(input) {
		return false
	}
	return requirePublicURL
}

func protocolRequiresPublicReferenceURL(interfaceType string) bool {
	switch interfaceType {
	case "newapi-channel-1", "newapi-channel-2", string(model.ChannelInterfaceGlobalAiOpcImage), string(model.ChannelInterfaceGlobalAiOpcVideo), string(model.ChannelInterfaceHuiQuYunVideo), string(model.ChannelInterfaceVolcengineArkVideo), string(model.ChannelInterfaceAIStarsLabImage), string(model.ChannelInterfaceAIStarsLabVideo), string(model.ChannelInterfaceWeijinVideo):
		return true
	default:
		return false
	}
}

func firstStringInList(value interface{}) string {
	items, ok := value.([]interface{})
	if !ok {
		return ""
	}
	for _, item := range items {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func runAiStarsLabImageTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	if input.Mask != nil {
		return nil, errors.New("AIStarsLab 图片协议不支持蒙版编辑，请移除蒙版后重试")
	}
	id := resumedProviderRequestID(ctx)
	if id == "" {
		body, err := aiStarsLabImageRequestBody(input)
		if err != nil {
			return nil, err
		}
		var created map[string]interface{}
		if err := postJSON(ctx, input.Config, "/generation/create/image", body, &created); err != nil {
			return nil, err
		}
		if message := aiStarsLabResponseError(created); message != "" {
			return nil, errors.New(message)
		}
		id = aiStarsLabTaskID(created)
	}
	if id == "" {
		return nil, errors.New("AIStarsLab 图片接口没有返回任务 ID")
	}
	resultURL, err := pollAiStarsLabTask(ctx, input, id, "图片")
	if err != nil {
		return nil, err
	}
	data, mimeType, err := getProviderExternalBinary(withProviderRequestKind(ctx, "download"), input.Config, resultURL)
	if err != nil {
		return nil, fmt.Errorf("AIStarsLab 图片结果下载失败（任务 %s）：%w", id, err)
	}
	mimeType = normalizedMediaMimeType(mimeType, data)
	return map[string]interface{}{"mode": "image", "images": []map[string]interface{}{{"dataUrl": dataURL(mimeType, data), "mimeType": mimeType}}}, nil
}

func runAiStarsLabVideoTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	id := resumedProviderRequestID(ctx)
	if id == "" {
		body, err := aiStarsLabVideoRequestBody(input)
		if err != nil {
			return nil, err
		}
		var created map[string]interface{}
		if err := postJSON(ctx, input.Config, "/generation/create/video", body, &created); err != nil {
			return nil, err
		}
		if message := aiStarsLabResponseError(created); message != "" {
			return nil, errors.New(message)
		}
		id = aiStarsLabTaskID(created)
	}
	if id == "" {
		return nil, errors.New("AIStarsLab 视频接口没有返回任务 ID")
	}
	resultURL, err := pollAiStarsLabTask(ctx, input, id, "视频")
	if err != nil {
		return nil, err
	}
	data, mimeType, err := getProviderExternalBinary(withProviderRequestKind(ctx, "download"), input.Config, resultURL)
	if err != nil {
		return nil, fmt.Errorf("AIStarsLab 视频结果下载失败（任务 %s）：%w", id, err)
	}
	mimeType = normalizedMediaMimeType(mimeType, data)
	return videoResult(resultURL, mimeType, data), nil
}

func aiStarsLabImageRequestBody(input canvasGenerationInput) (map[string]interface{}, error) {
	route := aiStarsLabRoute(input.Config.CapabilityConfig, input.Config.Model)
	if route == nil || strings.TrimSpace(route.Channel) == "" {
		return nil, errors.New("AIStarsLab 模型缺少线路编码，请在后台重新拉取该渠道模型")
	}
	images, err := aiStarsLabMediaURLs(input.ReferenceImages)
	if err != nil {
		return nil, err
	}
	if route.InputImagesMax >= 0 && len(images) > route.InputImagesMax {
		return nil, fmt.Errorf("AIStarsLab 当前模型最多支持 %d 张参考图，当前连接了 %d 张", route.InputImagesMax, len(images))
	}
	quality, err := aiStarsLabRequestQuality(route, nil, input.Config.Quality)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"channel": strings.TrimSpace(route.Channel), "model": aiStarsLabRequestModel(route, input.Config.Model),
		"prompt": withSystemPrompt(input.Config, input.Prompt), "aspectRatio": aiStarsLabImageRatio(input.Config.Size, route),
		"quality": quality, "inputImages": images, "n": 1,
	}, nil
}

func aiStarsLabRoute(config *ModelCapabilityConfig, modelKey string) *AIStarsLabCapabilityConfig {
	if config != nil && config.AIStarsLab != nil && strings.TrimSpace(config.AIStarsLab.Channel) != "" {
		return config.AIStarsLab
	}
	channelID, modelName, found := strings.Cut(strings.TrimSpace(modelKey), ":")
	if !found || strings.TrimSpace(channelID) == "" || strings.TrimSpace(modelName) == "" {
		return nil
	}
	derived := AIStarsLabCapabilityConfig{Channel: strings.TrimSpace(channelID), Model: strings.TrimSpace(modelName), InputImagesMax: -1, InputVideosMax: -1, InputAudiosMax: -1}
	if config != nil && config.AIStarsLab != nil {
		existing := config.AIStarsLab
		derived.Capability = existing.Capability
		derived.Qualities, derived.AspectRatios, derived.Modes = existing.Qualities, existing.AspectRatios, existing.Modes
		derived.Duration, derived.DurationMin, derived.DurationMax = existing.Duration, existing.DurationMin, existing.DurationMax
	}
	return &derived
}

func aiStarsLabRequestModel(route *AIStarsLabCapabilityConfig, fallback string) string {
	if route != nil && strings.TrimSpace(route.Model) != "" {
		return strings.TrimSpace(route.Model)
	}
	return strings.TrimSpace(fallback)
}

func aiStarsLabImageRatio(value string, route *AIStarsLabCapabilityConfig) string {
	normalized := strings.TrimSpace(value)
	if route != nil {
		for _, ratio := range route.AspectRatios {
			if strings.EqualFold(strings.TrimSpace(ratio), normalized) {
				return strings.TrimSpace(ratio)
			}
		}
		if len(route.AspectRatios) > 0 {
			return strings.TrimSpace(route.AspectRatios[0])
		}
	}
	if normalized == "" || strings.EqualFold(normalized, "auto") {
		return "1:1"
	}
	return normalized
}

func aiStarsLabTaskID(payload map[string]interface{}) string {
	id := firstNonEmptyString(stringField(payload, "taskId"), stringField(payload, "task_id"), stringField(payload, "id"))
	if data, ok := payload["data"].(map[string]interface{}); ok && id == "" {
		id = firstNonEmptyString(stringField(data, "taskId"), stringField(data, "task_id"), stringField(data, "id"))
	}
	return id
}

func aiStarsLabResponseError(payload map[string]interface{}) string {
	data, _ := payload["data"].(map[string]interface{})
	code := strings.TrimSpace(fmt.Sprint(payload["code"]))
	nestedCode := strings.TrimSpace(fmt.Sprint(data["code"]))
	for _, candidate := range []string{code, nestedCode} {
		if candidate != "" && candidate != "<nil>" && candidate != "0" && !strings.EqualFold(candidate, "success") && !strings.EqualFold(candidate, "ok") {
			return firstNonEmptyString(stringField(payload, "msg"), stringField(payload, "message"), stringField(payload, "errorMessage"), stringField(payload, "errorCode"), stringField(data, "msg"), stringField(data, "message"), stringField(data, "errorMessage"), stringField(data, "errorCode"), candidate)
		}
	}
	return firstNonEmptyString(stringField(payload, "errorMessage"), stringField(payload, "errorCode"), stringField(data, "errorMessage"), stringField(data, "errorCode"))
}

func pollAiStarsLabTask(ctx context.Context, input canvasGenerationInput, id string, label string) (string, error) {
	for deadline := providerPollingDeadline(ctx); time.Now().Before(deadline); {
		var payload map[string]interface{}
		if err := getJSON(withProviderRequestKind(ctx, "poll"), input.Config, "/generation/status?taskId="+url.QueryEscape(id), &payload); err != nil {
			return "", err
		}
		state := payload
		if data, ok := payload["data"].(map[string]interface{}); ok {
			state = data
		}
		if message := aiStarsLabResponseError(payload); message != "" {
			return "", fmt.Errorf("AIStarsLab %s查询失败（任务 %s）：%s", label, id, message)
		}
		switch fmt.Sprint(state["status"]) {
		case "1", "2":
			// Created and running are both pending from the host's perspective.
		case "3":
			resultURL := firstStringInList(state["outputs"])
			if resultURL == "" {
				return "", fmt.Errorf("AIStarsLab %s任务 %s 已完成但没有返回结果地址", label, id)
			}
			return resultURL, nil
		case "4":
			return "", fmt.Errorf("AIStarsLab %s生成失败（任务 %s）：%s", label, id, firstNonEmptyString(stringField(state, "errorMessage"), stringField(state, "errorCode"), "上游返回失败"))
		}
		if err := sleepContext(ctx, 10*time.Second); err != nil {
			return "", err
		}
	}
	return "", context.DeadlineExceeded
}

func aiStarsLabMediaURLs(items []providerMedia) ([]string, error) {
	urls := make([]string, 0, len(items))
	for _, item := range items {
		value, err := videoGenerationsMediaURL(item)
		if err != nil || !isPublicMediaURL(value) {
			return nil, errors.New("AIStarsLab 参考素材仅支持公网 URL；请先保存到对象存储")
		}
		urls = append(urls, value)
	}
	return urls, nil
}

func aiStarsLabRequestQuality(route *AIStarsLabCapabilityConfig, config *VideoCapabilityConfig, requested string) (string, error) {
	supported := route.Qualities
	if len(supported) == 0 && config != nil {
		supported = config.Resolutions
	}
	normalized := strings.TrimSpace(requested)
	if len(supported) == 0 {
		return normalized, nil
	}
	if normalized == "" || strings.EqualFold(normalized, "auto") || strings.EqualFold(normalized, "default") {
		return strings.TrimSpace(supported[0]), nil
	}
	requestedKey := normalizeResolution(normalized)
	for _, value := range supported {
		candidate := strings.TrimSpace(value)
		if strings.EqualFold(candidate, normalized) || normalizeResolution(candidate) == requestedKey {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("AIStarsLab 当前线路不支持 %s 画质，可选：%s", normalized, strings.Join(supported, "、"))
}

func aiStarsLabVideoRequestBody(input canvasGenerationInput) (map[string]interface{}, error) {
	if len(input.ReferenceAudios) > 0 && len(input.ReferenceImages) == 0 {
		return nil, errors.New("AIStarsLab 参考音频必须同时提供至少 1 张参考图片")
	}
	route := aiStarsLabRoute(input.Config.CapabilityConfig, input.Config.Model)
	if route == nil || strings.TrimSpace(route.Channel) == "" {
		return nil, errors.New("AIStarsLab 模型缺少线路编码，请在后台重新拉取该渠道模型")
	}
	config := DefaultModelCapabilityConfigForModel(string(model.ChannelInterfaceAIStarsLabVideo), input.Config.Model).Video
	if input.VideoCapability != nil {
		config = input.VideoCapability
	}
	images, err := aiStarsLabMediaURLs(input.ReferenceImages)
	if err != nil {
		return nil, err
	}
	videos, err := aiStarsLabMediaURLs(input.ReferenceVideos)
	if err != nil {
		return nil, err
	}
	audios, err := aiStarsLabMediaURLs(input.ReferenceAudios)
	if err != nil {
		return nil, err
	}
	if route.InputVideosMax >= 0 && len(videos) > route.InputVideosMax {
		return nil, fmt.Errorf("AIStarsLab 当前模型最多支持 %d 个参考视频，当前连接了 %d 个", route.InputVideosMax, len(videos))
	}
	if route.InputAudiosMax >= 0 && len(audios) > route.InputAudiosMax {
		return nil, fmt.Errorf("AIStarsLab 当前模型最多支持 %d 个参考音频，当前连接了 %d 个", route.InputAudiosMax, len(audios))
	}
	duration := config.Duration.Default
	if value, parseErr := strconv.Atoi(strings.TrimSpace(input.Config.VideoSeconds)); parseErr == nil && value > 0 {
		duration = value
	}
	mode, err := aiStarsLabVideoMode(route, len(images))
	if err != nil {
		return nil, err
	}
	quality, err := aiStarsLabRequestQuality(route, config, input.Config.VQuality)
	if err != nil {
		return nil, err
	}
	body := map[string]interface{}{
		"channel": strings.TrimSpace(route.Channel), "model": aiStarsLabRequestModel(route, input.Config.Model),
		"prompt": strings.TrimSpace(input.Prompt), "aspectRatio": strings.TrimSpace(input.Config.Size),
		"quality": quality, "duration": duration, "inputImages": images, "inputVideos": videos, "inputAudios": audios,
	}
	if mode != "" {
		body["mode"] = mode
	}
	return body, nil
}

func aiStarsLabVideoMode(route *AIStarsLabCapabilityConfig, imageCount int) (string, error) {
	if route == nil {
		return "", errors.New("AIStarsLab 模型缺少线路编码，请在后台重新拉取该渠道模型")
	}
	if len(route.Modes) == 0 {
		switch imageCount {
		case 0:
			return "text2video", nil
		case 2:
			return "frames2video", nil
		default:
			return "image2video", nil
		}
	}
	supports := func(name string) bool {
		for _, value := range route.Modes {
			if strings.EqualFold(strings.TrimSpace(value), name) {
				return true
			}
		}
		return false
	}
	if imageCount == 0 {
		if supports("text2video") {
			return "text2video", nil
		}
		return "", errors.New("AIStarsLab 当前模型不支持文生视频，请至少提供 1 张参考图片")
	}
	if imageCount == 2 && supports("frames2video") {
		return "frames2video", nil
	}
	if supports("image2video") {
		return "image2video", nil
	}
	return "", errors.New("AIStarsLab 当前模型不支持参考图片生成，请改用文生视频")
}
