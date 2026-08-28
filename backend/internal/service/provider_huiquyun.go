package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"mime/multipart"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func huiQuYunVideoRequestBody(input canvasGenerationInput) (map[string]interface{}, error) {
	normalizeHuiQuYunVideoInput(&input)
	if len(input.ReferenceAudios) > 0 && len(input.ReferenceImages) == 0 && len(input.ReferenceVideos) == 0 {
		return nil, errors.New("汇取云参考音频必须搭配参考图片或参考视频")
	}
	images, err := huiQuYunMediaURLs(input.ReferenceImages)
	if err != nil {
		return nil, err
	}
	videos, err := huiQuYunMediaURLs(input.ReferenceVideos)
	if err != nil {
		return nil, err
	}
	audios, err := huiQuYunMediaURLs(input.ReferenceAudios)
	if err != nil {
		return nil, err
	}
	seconds, _ := strconv.Atoi(strings.TrimSpace(input.Config.VideoSeconds))
	ratio := input.Config.Size
	if isHuiQuYun933MultipartVideoModel(input.Config.Model) {
		if hasHuiQuYunVideoReferences(input) {
			return nil, errors.New("汇取云 MX933 参考素材必须使用 multipart 文件上传")
		}
		return map[string]interface{}{
			"model": input.Config.Model, "prompt": strings.TrimSpace(input.Prompt), "seconds": seconds,
			"resolution": input.Config.VQuality, "aspect_ratio": ratio,
			"generate_audio": parseBool(input.Config.VideoGenerateAudio, true),
		}, nil
	}
	body := map[string]interface{}{
		"model": input.Config.Model, "prompt": strings.TrimSpace(input.Prompt), "seconds": seconds,
		"resolution": "720P", "aspect_ratio": normalizeHuiQuYunVideoRatio(ratio),
		"audio": parseBool(input.Config.VideoGenerateAudio, true),
	}
	switch len(images) {
	case 1:
		body["reference_image"] = images[0]
	case 2:
		body["start_frame"], body["end_frame"] = images[0], images[1]
	default:
		if len(images) > 2 {
			body["reference_images"] = images
		}
	}
	if len(videos) > 0 {
		body["video_references"] = videos
	}
	if len(audios) > 0 {
		body["audio_reference"] = audios[0]
	}
	return body, nil
}

func hasHuiQuYunVideoReferences(input canvasGenerationInput) bool {
	return len(input.ReferenceImages)+len(input.ReferenceVideos)+len(input.ReferenceAudios) > 0
}

func huiQuYunMX933MultipartBody(input canvasGenerationInput) (*bytes.Buffer, string, error) {
	normalizeHuiQuYunVideoInput(&input)
	if len(input.ReferenceAudios) > 0 && len(input.ReferenceImages) == 0 && len(input.ReferenceVideos) == 0 {
		return nil, "", errors.New("汇取云参考音频必须搭配参考图片或参考视频")
	}
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writeField(writer, "model", input.Config.Model)
	writeField(writer, "prompt", strings.TrimSpace(input.Prompt))
	writeField(writer, "seconds", input.Config.VideoSeconds)
	writeField(writer, "resolution", input.Config.VQuality)
	writeField(writer, "aspect_ratio", input.Config.Size)
	writeField(writer, "generate_audio", strconv.FormatBool(parseBool(input.Config.VideoGenerateAudio, true)))

	images := append([]providerMedia(nil), input.ReferenceImages...)
	writeFrame := func(metadataKey string, field string) error {
		frameID := metadataString(input.Metadata, metadataKey)
		if frameID == "" {
			return nil
		}
		for index, image := range images {
			if image.ID != frameID {
				continue
			}
			if err := writeMediaPart(writer, field, image); err != nil {
				return err
			}
			images = append(images[:index], images[index+1:]...)
			return nil
		}
		return fmt.Errorf("汇取云 MX933 %s参考图未包含在当前任务素材中", map[string]string{"first_frame": "首帧", "last_frame": "尾帧"}[field])
	}
	if err := writeFrame("videoStartFrameNodeId", "first_frame"); err != nil {
		return nil, "", err
	}
	if err := writeFrame("videoEndFrameNodeId", "last_frame"); err != nil {
		return nil, "", err
	}
	for _, image := range images {
		if err := writeMediaPart(writer, "images", image); err != nil {
			return nil, "", err
		}
	}
	for _, video := range input.ReferenceVideos {
		if err := writeMediaPart(writer, "videos", video); err != nil {
			return nil, "", err
		}
	}
	for _, audio := range input.ReferenceAudios {
		if err := writeMediaPart(writer, "audios", audio); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body, writer.FormDataContentType(), nil
}

func normalizeHuiQuYunVideoInput(input *canvasGenerationInput) {
	seconds := huiQuYunFixedVideoDuration(input.Config.Model)
	if seconds == 0 {
		seconds, _ = strconv.Atoi(strings.TrimSpace(input.Config.VideoSeconds))
		if seconds < 4 || seconds > 15 {
			seconds = 8
		}
	}
	input.Config.VideoSeconds = strconv.Itoa(seconds)
	if isHuiQuYun933MultipartVideoModel(input.Config.Model) {
		input.Config.Size = normalizeHuiQuYunMX933VideoRatio(input.Config.Size)
		quality := strings.ToLower(strings.TrimSpace(input.Config.VQuality))
		if quality != "480p" && quality != "720p" {
			quality = "720p"
		}
		input.Config.VQuality = quality
		return
	}
	input.Config.Size = normalizeHuiQuYunVideoRatio(input.Config.Size)
	input.Config.VQuality = "720p"
}

func huiQuYunMediaURLs(items []providerMedia) ([]string, error) {
	urls := make([]string, 0, len(items))
	for _, item := range items {
		value, err := videoGenerationsMediaURL(item)
		if err != nil || !isPublicMediaURL(value) {
			return nil, errors.New("汇取云参考素材需要公网 URL；请先保存到对象存储")
		}
		urls = append(urls, value)
	}
	return urls, nil
}

func normalizeHuiQuYunVideoRatio(value string) string {
	normalized := strings.TrimSpace(value)
	if strings.Contains(normalized, "x") {
		parts := strings.SplitN(normalized, "x", 2)
		width, widthErr := strconv.Atoi(parts[0])
		height, heightErr := strconv.Atoi(parts[1])
		if widthErr == nil && heightErr == nil && width > 0 && height > 0 {
			switch {
			case width == height:
				normalized = "1:1"
			case width > height:
				normalized = "16:9"
			default:
				normalized = "9:16"
			}
		}
	}
	switch normalized {
	case "21:9", "4:3", "16:9", "1:1", "3:4", "9:16":
		return normalized
	default:
		return "16:9"
	}
}

func normalizeHuiQuYunMX933VideoRatio(value string) string {
	switch strings.TrimSpace(value) {
	case "3:2", "2:3":
		return strings.TrimSpace(value)
	default:
		return normalizeHuiQuYunVideoRatio(value)
	}
}

func huiQuYunVideoError(state map[string]interface{}) string {
	if value, ok := state["error"].(map[string]interface{}); ok {
		return firstNonEmptyString(stringField(value, "message"), stringField(value, "code"), "上游返回失败")
	}
	return firstNonEmptyString(stringField(state, "error"), stringField(state, "message"), stringField(state, "msg"), "上游返回失败")
}

func huiQuYunUsesMultipartVideoRequest(input canvasGenerationInput) bool {
	return isHuiQuYun933MultipartVideoModel(input.Config.Model) && hasHuiQuYunVideoReferences(input)
}

func runHuiQuYunVideoTask(ctx context.Context, input canvasGenerationInput) (map[string]interface{}, error) {
	id := resumedProviderRequestID(ctx)
	if id == "" {
		var created map[string]interface{}
		if huiQuYunUsesMultipartVideoRequest(input) {
			body, contentType, err := huiQuYunMX933MultipartBody(input)
			if err != nil {
				return nil, err
			}
			if err := postForm(ctx, input.Config, "/videos/generations", contentType, body, &created); err != nil {
				return nil, err
			}
		} else {
			body, err := huiQuYunVideoRequestBody(input)
			if err != nil {
				return nil, err
			}
			if err := postJSON(ctx, input.Config, "/videos/generations", body, &created); err != nil {
				return nil, err
			}
		}
		id = firstNonEmptyString(stringField(created, "id"), stringField(created, "task_id"), stringField(created, "request_id"))
		if id == "" {
			if data, ok := created["data"].(map[string]interface{}); ok {
				id = firstNonEmptyString(stringField(data, "id"), stringField(data, "task_id"), stringField(data, "request_id"))
			}
		}
	}
	if id == "" {
		return nil, errors.New("汇取云接口没有返回任务 ID")
	}
	for deadline := providerPollingDeadline(ctx); time.Now().Before(deadline); {
		var state map[string]interface{}
		if err := getJSON(withProviderRequestKind(ctx, "poll"), input.Config, "/videos/"+url.PathEscape(id), &state); err != nil {
			return nil, err
		}
		if data, ok := state["data"].(map[string]interface{}); ok {
			state = data
		}
		status := strings.ToLower(strings.TrimSpace(stringField(state, "status")))
		switch status {
		case "completed", "succeeded":
			if videoURL := newAPIVideoResultURL(state); videoURL != "" {
				data, mimeType, err := getProviderExternalBinary(withProviderRequestKind(ctx, "download"), input.Config, videoURL)
				if err != nil {
					return nil, fmt.Errorf("汇取云视频结果下载失败（任务 %s）：%w", id, err)
				}
				mimeType = normalizedMediaMimeType(mimeType, data)
				return map[string]interface{}{"mode": "video", "video": map[string]interface{}{"dataUrl": dataURL(mimeType, data), "mimeType": mimeType}}, nil
			}
			data, mimeType, err := getBinary(withProviderRequestKind(ctx, "download"), input.Config, "/videos/"+url.PathEscape(id)+"/content")
			if err != nil {
				return nil, err
			}
			mimeType = normalizedMediaMimeType(mimeType, data)
			return map[string]interface{}{"mode": "video", "video": map[string]interface{}{"dataUrl": dataURL(mimeType, data), "mimeType": mimeType}}, nil
		case "failed", "cancelled", "canceled":
			return nil, fmt.Errorf("汇取云视频生成失败（任务 %s）：%s", id, huiQuYunVideoError(state))
		case "queued", "in_progress", "processing", "":
		default:
			return nil, fmt.Errorf("汇取云任务 %s 返回未知状态：%s", id, status)
		}
		if err := sleepContext(ctx, 5*time.Second); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("汇取云视频生成超时（任务 %s）", id)
}
