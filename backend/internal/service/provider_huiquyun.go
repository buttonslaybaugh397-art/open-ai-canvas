package service

import (
	"strconv"
	"strings"
)

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

func isHuiQuYunMJSD933Model(modelName string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(modelName)), "mj-sd2.0-933-720p")
}

func isHuiQuYunMX933VideoModel(modelName string) bool {
	normalized := strings.ToLower(strings.TrimSpace(modelName))
	return strings.HasPrefix(normalized, "sd2-mx933-720-") || strings.HasPrefix(normalized, "sd2-mx933-720-fast-")
}

func isHuiQuYun933MultipartVideoModel(modelName string) bool {
	return isHuiQuYunMX933VideoModel(modelName) || isHuiQuYunMJSD933Model(modelName)
}

func huiQuYunFixedVideoDuration(modelName string) int {
	normalized := strings.ToLower(strings.TrimSpace(modelName))
	index := strings.LastIndex(normalized, "-")
	if index < 0 || index == len(normalized)-1 || !strings.HasSuffix(normalized, "s") {
		return 0
	}
	seconds, err := strconv.Atoi(strings.TrimSuffix(normalized[index+1:], "s"))
	if err != nil || seconds <= 0 {
		return 0
	}
	return seconds
}
