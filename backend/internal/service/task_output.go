package service

import (
	"encoding/json"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
)

// TaskSummary 是任务列表/会话详情的读模型，不直接复用数据库 Task，避免把
// 渠道模型、供应线路和受保护输入泄露到普通用户接口。
type TaskSummary struct {
	ID                        string                     `json:"id"`
	SessionID                 string                     `json:"sessionId,omitempty"`
	ProjectID                 string                     `json:"projectId,omitempty"`
	Type                      string                     `json:"type"`
	Status                    model.TaskStatus           `json:"status"`
	Stage                     string                     `json:"stage"`
	Progress                  int                        `json:"progress"`
	Prompt                    string                     `json:"prompt"`
	Operation                 string                     `json:"operation,omitempty"`
	Provider                  string                     `json:"provider,omitempty"`
	Model                     string                     `json:"model,omitempty"`
	ProviderRequestID         string                     `json:"providerRequestId,omitempty"`
	ProviderCancelStatus      model.ProviderCancelStatus `json:"providerCancelStatus,omitempty"`
	ProviderCancelError       string                     `json:"providerCancelError,omitempty"`
	ProviderCancelAttempts    int                        `json:"providerCancelAttempts,omitempty"`
	ProviderCancelRequestedAt *time.Time                 `json:"providerCancelRequestedAt,omitempty"`
	ProviderCancelledAt       *time.Time                 `json:"providerCancelledAt,omitempty"`
	ErrorCode                 string                     `json:"errorCode,omitempty"`
	PreviewURL                string                     `json:"previewUrl,omitempty"`
	PreviewKind               string                     `json:"previewKind,omitempty"`
	PreviewStorageKey         string                     `json:"previewStorageKey,omitempty"`
	Attempts                  int                        `json:"attempts"`
	StartedAt                 *time.Time                 `json:"startedAt"`
	CompletedAt               *time.Time                 `json:"completedAt"`
	CreatedAt                 time.Time                  `json:"createdAt"`
	UpdatedAt                 time.Time                  `json:"updatedAt"`
	Billing                   *TaskBillingSummary        `json:"billing,omitempty"`
	ClientContext             *TaskClientContext         `json:"clientContext,omitempty"`
}

type TaskClientContext struct {
	ConversationID   string `json:"conversationId,omitempty"`
	MessageID        string `json:"messageId,omitempty"`
	BatchIndex       int    `json:"batchIndex,omitempty"`
	BatchCount       int    `json:"batchCount,omitempty"`
	DomainProjectID  string `json:"domainProjectId,omitempty"`
	ChapterID        string `json:"chapterId,omitempty"`
	ChapterOperation string `json:"chapterOperation,omitempty"`
	ShotID           string `json:"shotId,omitempty"`
	WorkflowStepID   string `json:"workflowStepId,omitempty"`
	ArtifactType     string `json:"artifactType,omitempty"`
}

type TaskBillingSummary struct {
	AmountMicrocredits int64               `json:"amountMicrocredits"`
	Status             model.BillingStatus `json:"status"`
}

func taskSummariesForOutput(tasks []model.Task) []TaskSummary {
	return taskSummariesForOutputWithBilling(tasks, nil)
}

func taskSummariesForOutputWithBilling(tasks []model.Task, orders map[string]model.BillingOrder) []TaskSummary {
	result := make([]TaskSummary, 0, len(tasks))
	for _, task := range tasks {
		summary := taskSummaryForOutput(task)
		if order, ok := orders[task.ID]; ok {
			summary.Billing = &TaskBillingSummary{AmountMicrocredits: order.AmountMicrocredits, Status: order.Status}
			if summary.ProviderRequestID == "" {
				summary.ProviderRequestID = order.ProviderRequestID
			}
		}
		result = append(result, summary)
	}
	return result
}

func taskBillingTaskIDs(tasks []model.Task) []string {
	ids := make([]string, 0, len(tasks))
	seen := map[string]struct{}{}
	for _, task := range tasks {
		if task.BillingOrderID == "" {
			continue
		}
		if _, ok := seen[task.ID]; ok {
			continue
		}
		seen[task.ID] = struct{}{}
		ids = append(ids, task.ID)
	}
	return ids
}

func taskSummaryForOutput(task model.Task) TaskSummary {
	errorCode := ""
	if isContentModerationFailure(task.Error) {
		errorCode = contentModerationErrorCode
	}
	previewURL, previewKind := taskMediaPreview(task.ResultJSON, task.Type)
	previewStorageKey := taskMediaPreviewStorageKey(task.ResultJSON, previewURL)
	return TaskSummary{
		ID:                        task.ID,
		SessionID:                 task.SessionID,
		ProjectID:                 task.ProjectID,
		Type:                      task.Type,
		Status:                    task.Status,
		Stage:                     task.Stage,
		Progress:                  task.Progress,
		Prompt:                    truncateRunes(task.Prompt, 500),
		Operation:                 task.Operation,
		Provider:                  task.Provider,
		Model:                     task.Model,
		ProviderRequestID:         task.ProviderRequestID,
		ProviderCancelStatus:      task.ProviderCancelStatus,
		ProviderCancelError:       task.ProviderCancelError,
		ProviderCancelAttempts:    task.ProviderCancelAttempts,
		ProviderCancelRequestedAt: task.ProviderCancelRequestedAt,
		ProviderCancelledAt:       task.ProviderCancelledAt,
		ErrorCode:                 errorCode,
		PreviewURL:                previewURL,
		PreviewKind:               previewKind,
		PreviewStorageKey:         previewStorageKey,
		Attempts:                  task.Attempts,
		StartedAt:                 task.StartedAt,
		CompletedAt:               task.CompletedAt,
		CreatedAt:                 task.CreatedAt,
		UpdatedAt:                 task.UpdatedAt,
		ClientContext:             taskClientContext(task.InputJSON),
	}
}

func taskMediaPreviewStorageKey(raw string, previewURL string) string {
	if strings.TrimSpace(raw) == "" || strings.TrimSpace(previewURL) == "" {
		return ""
	}
	var payload any
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return ""
	}
	return findTaskMediaStorageKey(payload, previewURL)
}

func findTaskMediaStorageKey(value any, previewURL string) string {
	switch item := value.(type) {
	case []any:
		for _, child := range item {
			if storageKey := findTaskMediaStorageKey(child, previewURL); storageKey != "" {
				return storageKey
			}
		}
	case map[string]any:
		storageKey := taskMediaStorageKey(item)
		if storageKey != "" && previewURL == taskMediaResourcePreviewURL(storageKey) {
			return storageKey
		}
		for _, key := range []string{"url", "videoUrl", "video_url", "imageUrl", "image_url", "outputUrl", "output_url", "mediaUrl", "media_url", "dataUrl", "data_url", "content", "coverUrl", "cover_url", "resultUrl", "result_url", "downloadUrl", "download_url", "fileUrl", "file_url", "uri", "src"} {
			if candidate, _ := item[key].(string); candidate == previewURL && storageKey != "" {
				return storageKey
			}
		}
		for _, child := range item {
			if found := findTaskMediaStorageKey(child, previewURL); found != "" {
				return found
			}
		}
	}
	return ""
}

func taskMediaStorageKey(item map[string]any) string {
	for _, key := range []string{"storageKey", "storage_key", "resourceKey", "resource_key", "providerArtifactRef", "provider_artifact_ref"} {
		if value, ok := item[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func taskMediaResourcePreviewURL(storageKey string) string {
	resourceID := canvasResourceID(storageKey)
	if resourceID == "" {
		return ""
	}
	return "/api/resources/" + resourceID + "/file"
}

// 列表只暴露页面恢复所需的非敏感关联 ID，不下发完整任务输入或其他 metadata。
func taskClientContext(raw string) *TaskClientContext {
	var input struct {
		Metadata struct {
			Source          string `json:"source"`
			ConversationID  string `json:"conversationId"`
			MessageID       string `json:"messageId"`
			BatchIndex      int    `json:"batchIndex"`
			BatchCount      int    `json:"batchCount"`
			DomainProjectID string `json:"domainProjectId"`
			ChapterID       string `json:"chapterId"`
			Operation       string `json:"operation"`
			ShotID          string `json:"shotId"`
			WorkflowStepID  string `json:"workflowStepId"`
			ArtifactType    string `json:"artifactType"`
		} `json:"metadata"`
	}
	if json.Unmarshal([]byte(raw), &input) != nil {
		return nil
	}
	metadata := input.Metadata
	if metadata.Source == "create-page" && metadata.ConversationID != "" && metadata.MessageID != "" {
		return &TaskClientContext{
			ConversationID: metadata.ConversationID,
			MessageID:      metadata.MessageID,
			BatchIndex:     metadata.BatchIndex,
			BatchCount:     metadata.BatchCount,
		}
	}
	if metadata.ShotID != "" && metadata.WorkflowStepID != "" {
		return &TaskClientContext{DomainProjectID: metadata.DomainProjectID, ShotID: metadata.ShotID, WorkflowStepID: metadata.WorkflowStepID, ArtifactType: metadata.ArtifactType}
	}
	chapterOperation := ""
	if metadata.Operation == "chapter_character_breakdown" {
		chapterOperation = "characters"
	} else if metadata.Source == "short-drama-chapter-storyboard" {
		chapterOperation = "storyboard"
	}
	if chapterOperation == "" || metadata.DomainProjectID == "" || metadata.ChapterID == "" {
		return nil
	}
	return &TaskClientContext{
		DomainProjectID:  metadata.DomainProjectID,
		ChapterID:        metadata.ChapterID,
		ChapterOperation: chapterOperation,
	}
}

// 列表只暴露首个可访问媒体地址，避免把完整生成结果和内嵌数据带回前端。
func taskMediaPreview(raw string, taskType string) (string, string) {
	if strings.TrimSpace(raw) == "" {
		return "", ""
	}
	var payload any
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return "", ""
	}
	defaultKind := "image"
	if strings.Contains(strings.ToLower(taskType), "video") {
		defaultKind = "video"
	}
	return findTaskMediaPreview(payload, defaultKind)
}

func findTaskMediaPreview(value any, hint string) (string, string) {
	switch item := value.(type) {
	case string:
		text := strings.TrimSpace(item)
		if !strings.HasPrefix(text, "/api/resources/") && !strings.HasPrefix(text, "http://") && !strings.HasPrefix(text, "https://") {
			return "", ""
		}
		kind := hint
		lower := strings.ToLower(text)
		if strings.Contains(lower, ".mp4") || strings.Contains(lower, ".webm") || strings.Contains(lower, ".mov") || strings.Contains(lower, ".m4v") || strings.Contains(lower, ".mkv") || strings.Contains(lower, ".avi") || strings.Contains(lower, ".mpeg") || strings.Contains(lower, ".mpg") || strings.Contains(lower, ".ts") {
			kind = "video"
		} else if kind != "video" {
			kind = "image"
		}
		return text, kind
	case []any:
		for _, child := range item {
			if previewURL, previewKind := findTaskMediaPreview(child, hint); previewURL != "" {
				return previewURL, previewKind
			}
		}
	case map[string]any:
		objectHint := hint
		mediaType := strings.ToLower(strings.TrimSpace(firstTaskMediaString(item, "mimeType", "mime_type", "contentType", "content_type")))
		mediaTypeHint := strings.ToLower(strings.TrimSpace(firstTaskMediaString(item, "type", "mediaType", "media_type", "kind")))
		if strings.HasPrefix(mediaType, "video/") || strings.Contains(mediaTypeHint, "video") {
			objectHint = "video"
		} else if strings.HasPrefix(mediaType, "image/") || strings.Contains(mediaTypeHint, "image") {
			objectHint = "image"
		} else if strings.HasPrefix(mediaType, "audio/") || strings.Contains(mediaTypeHint, "audio") {
			objectHint = "audio"
		}
		keys := []string{"video", "videos", "video_url", "videoUrl", "video_uri", "videoUri", "download_url", "downloadUrl", "download_uri", "downloadUri", "result", "output", "outputs", "results", "result_url", "resultUrl", "output_url", "outputUrl", "content", "media", "file", "url", "dataUrl", "data_url", "file_url", "fileUrl", "uri", "src", "images", "image", "data"}
		if objectHint != "video" {
			keys = []string{"images", "image", "video", "videos", "video_url", "videoUrl", "video_uri", "videoUri", "download_url", "downloadUrl", "download_uri", "downloadUri", "result", "output", "outputs", "results", "result_url", "resultUrl", "output_url", "outputUrl", "content", "media", "file", "url", "dataUrl", "data_url", "file_url", "fileUrl", "uri", "src", "data"}
		}
		for _, key := range keys {
			if objectHint == "video" && (key == "images" || key == "image") {
				continue
			}
			child, exists := item[key]
			if !exists {
				continue
			}
			childHint := objectHint
			if key == "video" || key == "videos" || key == "video_url" || key == "videoUrl" || key == "video_uri" || key == "videoUri" {
				childHint = "video"
			} else if key == "images" || key == "image" {
				childHint = "image"
			}
			if previewURL, previewKind := findTaskMediaPreview(child, childHint); previewURL != "" {
				return previewURL, previewKind
			}
		}
		if storageKey := taskMediaStorageKey(item); storageKey != "" && canvasResourceID(storageKey) != "" {
			return taskMediaResourcePreviewURL(storageKey), objectHint
		}
	}
	return "", ""
}

func firstTaskMediaString(item map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := item[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func truncateRunes(value string, limit int) string {
	text := []rune(value)
	if len(text) <= limit {
		return value
	}
	return string(text[:limit]) + "..."
}

func taskForOutput(task model.Task) *model.Task {
	task.InputJSON = publicTaskInputJSON(task.InputJSON)
	// 普通任务接口只暴露前台模型身份；渠道模型和供应线路属于管理员内部信息。
	task.LogicalModelRevisionID = ""
	task.RouteID = ""
	task.ChannelModelID = ""
	return &task
}

func publicTaskInputJSON(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	var input map[string]any
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return ""
	}
	public := map[string]any{}
	// 任务完成后仍需依靠这些非敏感 ID 恢复项目产物归属；密钥等配置继续被过滤。
	for _, key := range []string{"mode", "metadata", "workflowStepId", "domainProjectId", "assetVersionId", "resourceId", "mediaType", "role"} {
		if value, ok := input[key]; ok {
			public[key] = value
		}
	}
	if len(public) == 0 {
		return ""
	}
	data, _ := json.Marshal(public)
	return string(data)
}
