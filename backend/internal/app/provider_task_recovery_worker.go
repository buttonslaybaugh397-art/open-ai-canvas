package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
)

const providerRecoveryMaxAge = 24 * time.Hour

func (s *Service) processRecoveredVideoTask(ctx context.Context, task *model.Task) error {
	fail := func(err error) error {
		return s.terminalCoordinator().handleExecutionFailure(task, err, false, false)
	}
	if task.ProviderRecoveryAt == nil || strings.TrimSpace(task.ProviderRequestID) == "" ||
		(task.Type != "canvas_video" && !strings.HasPrefix(task.Type, "video_")) {
		return fail(errors.New("恢复任务缺少可安全查询的上游任务信息"))
	}
	if time.Since(*task.ProviderRecoveryAt) >= providerRecoveryMaxAge {
		return fail(errors.New("恢复查询已超过 24 小时，请确认上游状态后再次人工查询"))
	}
	if _, err := s.recoveredTaskBillingRefunded(task); err != nil {
		// A mismatched order must never reach billing cleanup.
		task.Status, task.Stage, task.Error = model.TaskStatusFailed, "恢复任务计费校验失败", s.UserFacingErrorMessage(err)
		return errors.Join(err, s.terminalCoordinator().markTerminalState(task))
	}
	raw, err := s.decryptTaskInputJSON(task.InputJSON)
	if err != nil {
		return fail(err)
	}
	var input canvasGenerationInput
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return fail(err)
	}
	input.Config, err = s.resolveProviderConfig(input.Config)
	if err != nil {
		return fail(err)
	}
	ctx = ensureOfficialProtocolAdapter(withProtocolRegistry(ctx, s.protocolRegistry()), input.Config.InterfaceType)
	adapter, err := generationProtocolAdapterForContext(ctx, input.Config.InterfaceType)
	if err != nil {
		return fail(err)
	}
	if adapter == nil {
		return fail(errors.New("任务协议不支持安全恢复查询"))
	}
	ctx = withProviderOutboundPolicy(withProviderAnalytics(ctx, s, *task), input.Config)
	result, status, err := queryProtocolAdapterVideoTask(ctx, input, adapter, task.ProviderRequestID)
	if err != nil {
		if status != "" {
			// An explicit upstream failure (or an invalid result) is not a poll retry.
			return fail(err)
		}
		_ = s.log(task.UserID, task.ID, "warn", "恢复查询暂时失败，将继续查询原上游任务", s.UserFacingErrorMessage(err))
		return s.repo.DeferRunningTaskForProviderPoll(task.ID, task.LeaseOwner, "上游查询暂不可用，等待自动重查", 30*time.Second)
	}
	if result == nil {
		return s.repo.DeferRunningTaskForProviderPoll(task.ID, task.LeaseOwner, "后台仍在生成，已恢复自动查询", 5*time.Second)
	}
	_, err = s.completeRecoveredVideoTask(task, result, status)
	if err != nil && task.Status == model.TaskStatusRunning {
		_, terminalErr := s.terminalCoordinator().handleResultPersistenceFailure(task, err)
		return terminalErr
	}
	return err
}
