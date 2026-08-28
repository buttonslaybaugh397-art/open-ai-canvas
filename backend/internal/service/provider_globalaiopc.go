package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
)

func runGlobalAiOpcTask(ctx context.Context, input canvasGenerationInput, mode string) (map[string]interface{}, error) {
	if input.Mask != nil {
		return nil, errors.New("GlobalAiOpc 图片任务不支持蒙版编辑，请移除蒙版后重试")
	}
	id := resumedProviderRequestID(ctx)
	if id == "" {
		body, err := globalAiOpcTaskBody(input, mode)
		if err != nil {
			return nil, err
		}
		var created map[string]interface{}
		if err := postGlobalAiOpcJSON(ctx, input.Config, body, &created); err != nil {
			return nil, err
		}
		created, err = unwrapGlobalAiOpcTask(created)
		if err != nil {
			return nil, err
		}
		id = firstNonEmptyString(stringField(created, "id"), stringField(created, "task_id"), stringField(created, "request_id"))
		if id == "" {
			return nil, errors.New("GlobalAiOpc 接口没有返回任务 ID")
		}
	}
	for deadline := providerPollingDeadline(ctx); time.Now().Before(deadline); {
		var state map[string]interface{}
		if err := getGlobalAiOpcJSON(withProviderRequestKind(ctx, "poll"), input.Config, id, &state); err != nil {
			return nil, err
		}
		state, err := unwrapGlobalAiOpcTask(state)
		if err != nil {
			return nil, err
		}
		status := strings.ToLower(strings.TrimSpace(stringField(state, "status")))
		switch status {
		case "completed", "succeeded", "success":
			resultURL := firstNonEmptyString(stringField(state, mode+"_url"), stringField(state, "result_url"), stringField(state, "url"))
			if resultURL == "" {
				return nil, fmt.Errorf("GlobalAiOpc 任务 %s 已完成但没有返回结果地址", id)
			}
			data, mimeType, err := getProviderExternalBinary(withProviderRequestKind(ctx, "download"), input.Config, resultURL)
			if err != nil {
				return nil, fmt.Errorf("GlobalAiOpc 结果下载失败（任务 %s）：%w", id, err)
			}
			mimeType = normalizedMediaMimeType(mimeType, data)
			media := map[string]interface{}{"dataUrl": dataURL(mimeType, data), "mimeType": mimeType, "url": resultURL}
			if mode == "image" {
				return map[string]interface{}{"mode": "image", "images": []map[string]interface{}{media}}, nil
			}
			return map[string]interface{}{"mode": "video", "video": media}, nil
		case "failed", "cancelled", "canceled", "expired":
			return nil, fmt.Errorf("GlobalAiOpc 任务 %s 失败：%s", id, globalAiOpcTaskError(state))
		case "queued", "processing", "running", "pending", "":
			if err := sleepContext(ctx, 5*time.Second); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("GlobalAiOpc 任务 %s 返回未知状态：%s", id, status)
		}
	}
	return nil, fmt.Errorf("GlobalAiOpc 生成超时（任务 %s）", id)
}

func globalAiOpcTaskBody(input canvasGenerationInput, mode string) (map[string]interface{}, error) {
	body := map[string]interface{}{"model": input.Config.Model, "prompt": strings.TrimSpace(input.Prompt)}
	imageURLs, err := globalAiOpcMediaURLs(input.ReferenceImages)
	if err != nil {
		return nil, err
	}
	if mode == "image" {
		if len(imageURLs) > 0 {
			body["reference_images"] = imageURLs
		}
		body["aspect_ratio"] = normalizeGlobalAiOpcImageRatio(input.Config.Size)
		body["resolution"] = normalizeGlobalAiOpcImageResolution(input.Config.Model, input.Config.Quality)
		body["watermark"] = parseBool(input.Config.VideoWatermark, false)
		return body, nil
	}
	modelName := strings.ToLower(strings.TrimSpace(input.Config.Model))
	profile := input.VideoCapability
	if profile == nil {
		profile = DefaultModelCapabilityConfigForModel(string(model.ChannelInterfaceGlobalAiOpcVideo), input.Config.Model).Video
	}
	if profile == nil {
		return nil, errors.New("GlobalAiOpc 视频模型缺少能力配置")
	}
	seconds, secondsErr := strconv.Atoi(strings.TrimSpace(input.Config.VideoSeconds))
	if secondsErr != nil || seconds <= 0 {
		seconds = profile.Duration.Default
	}
	body["duration"] = seconds
	ratio := strings.TrimSpace(input.Config.Size)
	if !containsCapabilityString(profile.Ratios, ratio) {
		ratio = profile.DefaultRatio
	}
	isSeedance15 := strings.HasPrefix(modelName, "seedance_1_5_pro_")
	if isSeedance15 {
		body["size"] = ratio
	} else {
		body["aspect_ratio"] = ratio
		body["resolution"] = normalizeGlobalAiOpcVideoResolution(modelName, input.Config.VQuality, profile.DefaultResolution)
	}
	if profile.GenerateAudio.Supported {
		body["generate_audio"] = parseBool(input.Config.VideoGenerateAudio, profile.GenerateAudio.Default)
	}
	if profile.Watermark.Supported {
		body["watermark"] = parseBool(input.Config.VideoWatermark, profile.Watermark.Default)
	}
	if len(imageURLs) > 0 && !isSeedance15 {
		body["reference_images"] = imageURLs
	}
	if startID := metadataString(input.Metadata, "videoStartFrameNodeId"); startID != "" {
		firstImage := globalAiOpcFrameURL(input.ReferenceImages, imageURLs, startID)
		if firstImage == "" {
			return nil, errors.New("GlobalAiOpc 视频首帧未包含在参考图片中")
		}
		body["first_image"] = firstImage
	} else if isSeedance15 && len(imageURLs) > 0 {
		body["first_image"] = imageURLs[0]
	}
	if endID := metadataString(input.Metadata, "videoEndFrameNodeId"); endID != "" {
		lastImage := globalAiOpcFrameURL(input.ReferenceImages, imageURLs, endID)
		if lastImage == "" {
			return nil, errors.New("GlobalAiOpc 视频尾帧未包含在参考图片中")
		}
		body["last_image"] = lastImage
	} else if isSeedance15 && len(imageURLs) > 1 {
		body["last_image"] = imageURLs[1]
	}
	videoURLs, err := globalAiOpcMediaURLs(input.ReferenceVideos)
	if err != nil {
		return nil, err
	}
	if len(videoURLs) > 0 {
		body["reference_videos"] = videoURLs
	}
	audioURLs, err := globalAiOpcMediaURLs(input.ReferenceAudios)
	if err != nil {
		return nil, err
	}
	if len(audioURLs) > 0 {
		body["reference_audios"] = audioURLs
	}
	return body, nil
}

func globalAiOpcMediaURLs(items []providerMedia) ([]string, error) {
	values := make([]string, 0, len(items))
	for _, item := range items {
		value, err := globalAiOpcMediaURL(item)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func globalAiOpcFrameURL(images []providerMedia, urls []string, frameID string) string {
	for index, image := range images {
		if image.ID == frameID && index < len(urls) {
			return urls[index]
		}
	}
	return ""
}

func globalAiOpcMediaURL(media providerMedia) (string, error) {
	value := strings.TrimSpace(media.URL)
	if !isPublicMediaURL(value) {
		return "", errors.New("GlobalAiOpc 参考素材必须使用公网 HTTP(S) URL，请先上传到 OSS")
	}
	if _, err := ValidateOutboundURL(value); err != nil {
		return "", err
	}
	return value, nil
}

func normalizeGlobalAiOpcImageRatio(value string) string {
	switch strings.TrimSpace(value) {
	case "1:1", "3:4", "4:3", "16:9", "9:16", "3:2", "2:3", "21:9":
		return strings.TrimSpace(value)
	default:
		return "1:1"
	}
}

func normalizeGlobalAiOpcImageResolution(modelName, value string) string {
	resolution := strings.ToUpper(strings.TrimSpace(value))
	if strings.EqualFold(strings.TrimSpace(modelName), "seedream_5.0Pro") {
		if resolution == "2K" {
			return "2K"
		}
		return "1K"
	}
	switch resolution {
	case "3K", "4K":
		return resolution
	default:
		return "2K"
	}
}

func normalizeGlobalAiOpcVideoResolution(modelName, value, fallback string) string {
	if modelName == "minimax-h3-c4" {
		return "1440P"
	}
	resolution := strings.ToLower(strings.TrimSpace(value))
	if resolution == "" {
		resolution = strings.ToLower(strings.TrimSpace(fallback))
	}
	switch resolution {
	case "low":
		resolution = "480p"
	case "auto", "medium", "high", "":
		resolution = "720p"
	case "4k", "2160":
		resolution = "2160p"
	default:
		if _, err := strconv.Atoi(resolution); err == nil {
			resolution += "p"
		}
	}
	if modelName == "sd_2.0_special" && resolution == "2160p" {
		return "4k"
	}
	return resolution
}

func globalAiOpcTaskError(state map[string]interface{}) string {
	if value, ok := state["error"].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	if value, ok := state["error"].(map[string]interface{}); ok {
		return firstNonEmptyString(stringField(value, "message"), stringField(value, "code"), "上游返回失败")
	}
	return firstNonEmptyString(stringField(state, "message"), stringField(state, "msg"), "上游返回失败")
}

func unwrapGlobalAiOpcTask(payload map[string]interface{}) (map[string]interface{}, error) {
	if code, ok := payload["code"]; ok && globalAiOpcResponseCodeFailed(code) {
		return nil, errors.New(firstNonEmptyString(stringField(payload, "msg"), stringField(payload, "message"), "GlobalAiOpc 请求失败"))
	}
	if data, ok := payload["data"].(map[string]interface{}); ok {
		return data, nil
	}
	if _, wrapped := payload["data"]; wrapped {
		return nil, errors.New("GlobalAiOpc 接口没有返回任务数据")
	}
	return payload, nil
}

func globalAiOpcResponseCodeFailed(value interface{}) bool {
	switch code := value.(type) {
	case nil:
		return false
	case float64:
		return code != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(code)) {
		case "", "0", "ok", "success", "succeeded", "completed":
			return false
		default:
			return true
		}
	default:
		return true
	}
}

func postGlobalAiOpcJSON(ctx context.Context, config providerConfig, body interface{}, target interface{}) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, globalAiOpcTaskURL(config.BaseURL, ""), bytes.NewReader(data))
	if err != nil {
		return err
	}
	applyProviderAuth(req, config)
	req.Header.Set("Content-Type", "application/json")
	ApplyOutboundHeaders(req, config.Headers)
	return doJSON(req, target)
}

func getGlobalAiOpcJSON(ctx context.Context, config providerConfig, taskID string, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, globalAiOpcTaskURL(config.BaseURL, taskID), nil)
	if err != nil {
		return err
	}
	applyProviderAuth(req, config)
	ApplyOutboundHeaders(req, config.Headers)
	return doJSON(req, target)
}

func globalAiOpcTaskURL(baseURL, taskID string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/") + "/v2/model-center/tasks"
	if strings.TrimSpace(taskID) != "" {
		base += "/" + url.PathEscape(strings.TrimSpace(taskID))
	}
	return base
}
