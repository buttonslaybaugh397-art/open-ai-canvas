package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const contentModerationErrorCode = "sensitive_words_detected"

const contentModerationRetryMessage = "内容审核未通过，请修改提示词后重新生成；原任务不能直接重试"

// 只提取供应商明确返回的错误码和短消息，避免把完整响应或用户输入复制到调用日志。
func providerFailureDetails(payload map[string]any) (string, string) {
	candidates := make([]map[string]any, 0, 3)
	for _, key := range []string{"error", "data"} {
		if nested, ok := payload[key].(map[string]any); ok {
			candidates = append(candidates, nested)
		}
	}
	// 内层通常是供应商业务错误，外层 code 可能只是 HTTP 包装码。
	candidates = append(candidates, payload)
	code := ""
	message := ""
	for _, candidate := range candidates {
		if code == "" {
			code = normalizedProviderErrorCode(candidate["code"])
		}
		if message == "" {
			message = strings.TrimSpace(stringField(candidate, "message"))
			if message == "" {
				message = strings.TrimSpace(stringField(candidate, "msg"))
			}
		}
	}
	return code, truncateRunes(message, 500)
}

func providerResponseBusinessFailure(responseBody []byte) (string, string, bool) {
	if len(responseBody) == 0 {
		return "", "", false
	}
	var payload map[string]any
	if json.Unmarshal(responseBody, &payload) != nil {
		return "", "", false
	}
	return providerPayloadBusinessFailure(payload)
}

func providerPayloadBusinessFailure(payload map[string]any) (string, string, bool) {
	if errorValue, ok := payload["error"].(map[string]any); ok {
		code, message := providerFailureDetails(map[string]any{"error": errorValue})
		if code != "" || message != "" {
			return code, message, true
		}
	}
	if !providerBusinessCodeFailed(payload["code"]) {
		return "", "", false
	}
	code, message := providerFailureDetails(payload)
	return code, message, true
}

// providerPayloadIndicatesAcceptedTask prevents a gateway's wrapper/business
// code from hiding a real asynchronous submission. Once a task identifier and
// an in-flight status are present, the caller must be allowed to poll it.
func providerPayloadIndicatesAcceptedTask(payload map[string]any) bool {
	if payload == nil || providerRequestIDFromPayload(payload) == "" {
		return false
	}
	status := providerStatusFromPayload(payload, 0)
	if status != "" {
		switch status {
		case "accepted", "submitted", "queued", "pending", "processing", "running", "in_progress", "in-progress", "not_start", "created":
			return true
		case "failed", "failure", "error", "cancelled", "canceled", "expired", "succeeded", "success", "completed", "done":
			return false
		}
	}
	// Some wrappers omit status but return a custom success code alongside the
	// task id. An explicit non-success business code still wins over the ID.
	if _, hasError := payload["error"]; hasError {
		return false
	}
	for key, value := range payload {
		if !strings.EqualFold(key, "code") && !strings.EqualFold(key, "statusCode") && !strings.EqualFold(key, "status_code") {
			continue
		}
		code := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
		if code == "" || code == "<nil>" || code == "0" || code == "success" || code == "succeeded" || code == "ok" || code == "accepted" || code == "queued" || code == "pending" || code == "processing" {
			continue
		}
		if numeric, err := strconv.Atoi(code); err == nil && numeric >= 200 && numeric < 300 {
			continue
		}
		return false
	}
	return true
}

func providerStatusFromPayload(value any, depth int) string {
	if depth > 12 {
		return ""
	}
	switch current := value.(type) {
	case map[string]any:
		for key, candidate := range current {
			if strings.EqualFold(key, "status") || strings.EqualFold(key, "state") {
				if text, ok := candidate.(string); ok {
					return strings.ToLower(strings.TrimSpace(text))
				}
			}
		}
		for _, nested := range current {
			if status := providerStatusFromPayload(nested, depth+1); status != "" {
				return status
			}
		}
	case []any:
		for _, nested := range current {
			if status := providerStatusFromPayload(nested, depth+1); status != "" {
				return status
			}
		}
	}
	return ""
}

func providerBusinessCodeFailed(value any) bool {
	code := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
	switch code {
	case "", "0", "success", "succeeded", "ok", "accepted", "queued", "pending", "processing", "in_progress", "in-progress", "<nil>":
		return false
	default:
		// Some gateways expose the HTTP result as a business code.
		if numeric, err := strconv.Atoi(code); err == nil && numeric >= 200 && numeric < 300 {
			return false
		}
		return true
	}
}

func normalizedProviderErrorCode(value any) string {
	var code string
	switch current := value.(type) {
	case string:
		code = current
	case fmt.Stringer:
		code = current.String()
	case float64:
		if current != 0 {
			code = fmt.Sprintf("%g", current)
		}
	case int:
		if current != 0 {
			code = fmt.Sprintf("%d", current)
		}
	case int64:
		if current != 0 {
			code = fmt.Sprintf("%d", current)
		}
	}
	code = strings.TrimSpace(code)
	if code == "0" {
		return ""
	}
	return truncateRunes(code, 80)
}

func isContentModerationFailure(value string) bool {
	return strings.Contains(strings.ToLower(value), contentModerationErrorCode)
}
