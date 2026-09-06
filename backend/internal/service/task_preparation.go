package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
)

// FinalizePreparingTask fills the input prepared in the browser and atomically
// moves the task into the worker queue. The initial admission input remains
// authoritative so the late upload step cannot alter routing or billing.
func (s *Service) FinalizePreparingTask(userID string, id string, input map[string]any) (*model.Task, error) {
	task, err := s.repo.TaskForUser(userID, id)
	if err != nil {
		return nil, err
	}
	if task.Status == model.TaskStatusQueued {
		return taskForOutput(*task), nil
	}
	if task.Status != model.TaskStatusPreparing {
		return nil, fmt.Errorf("任务当前状态为 %s，无法补齐生成输入", task.Status)
	}

	draftJSON, err := s.decryptTaskInputJSON(task.InputJSON)
	if err != nil {
		return nil, fmt.Errorf("读取准备态任务失败：%w", err)
	}
	var draft map[string]any
	if err := json.Unmarshal([]byte(draftJSON), &draft); err != nil {
		return nil, fmt.Errorf("准备态任务输入无效：%w", err)
	}
	normalizedInput, err := normalizeTaskInput(input)
	if err != nil {
		return nil, err
	}
	merged := mergePreparingTaskInput(draft, normalizedInput)
	if err := s.requireCustomChannelsForTaskInput(merged); err != nil {
		return nil, err
	}
	if err := s.ValidateTaskCapability(merged); err != nil {
		return nil, err
	}
	if containsInlineMediaDataURL(merged) {
		return nil, BadAuthRequest("任务输入不能包含内嵌媒体，请先上传到资源存储")
	}
	if err := s.protectTaskSecrets(merged); err != nil {
		return nil, err
	}
	inputJSON, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("序列化任务输入失败：%w", err)
	}

	policy, err := s.RuntimePolicy()
	if err != nil {
		return nil, err
	}
	s.storageMu.Lock()
	usage, usageErr := s.repo.UserStorageUsage(userID)
	if usageErr == nil {
		delta := int64(len([]byte(inputJSON)) - len([]byte(task.InputJSON)))
		if delta > 0 {
			usageErr = validateTaskDataGrowthQuotaWithPolicy(usage, delta, policy.Resource)
		}
	}
	var finalized *model.Task
	if usageErr == nil {
		finalized, usageErr = s.repo.FinalizePreparingTask(userID, id, string(inputJSON), time.Now())
	}
	s.storageMu.Unlock()
	if errors.Is(usageErr, repository.ErrTaskStateConflict) {
		latest, latestErr := s.repo.TaskForUser(userID, id)
		if latestErr == nil && latest.Status == model.TaskStatusQueued {
			return taskForOutput(*latest), nil
		}
	}
	if usageErr != nil {
		return nil, usageErr
	}
	_ = s.log(userID, id, "info", "任务输入已准备完成，已进入队列", "")
	return taskForOutput(*finalized), nil
}

func mergePreparingTaskInput(draft map[string]any, prepared map[string]any) map[string]any {
	merged := make(map[string]any, len(draft))
	for key, value := range draft {
		merged[key] = value
	}
	for key := range preparingTaskAttachmentKeys {
		if value, ok := prepared[key]; ok {
			merged[key] = value
		}
	}
	return merged
}

// The browser may only replace media references after a preparation task has
// passed admission and reserved quota. All generation settings remain frozen.
var preparingTaskAttachmentKeys = map[string]struct{}{
	"referenceImages": {}, "referenceVideos": {}, "referenceAudios": {}, "mask": {},
}
