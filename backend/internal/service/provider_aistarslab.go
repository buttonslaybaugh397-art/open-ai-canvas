package service

import (
	"errors"
	"fmt"
	"strings"
)

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
