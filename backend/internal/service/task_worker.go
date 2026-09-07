package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
)

// taskWorkerCoordinator 收敛任务领取、租约维护和执行结果落库，避免 Service 同时承担 worker 生命周期与业务命令。
type taskWorkerCoordinator struct {
	service *Service
}

func newTaskWorkerCoordinator(service *Service) *taskWorkerCoordinator {
	return &taskWorkerCoordinator{service: service}
}

func (s *Service) taskWorker() *taskWorkerCoordinator {
	if s.taskWorkerCoordinator != nil {
		return s.taskWorkerCoordinator
	}
	// 部分单元测试直接构造 Service 字面量；延迟创建保持这些测试和内部工具兼容。
	return newTaskWorkerCoordinator(s)
}

func (w *taskWorkerCoordinator) start(ctx context.Context) {
	s := w.service
	s.startTextReplayCleanup(ctx)
	s.startTaskPreparationCleanup(ctx)
	s.startProviderCancellationReconciliation(ctx)
	s.startBillingReviewAudit(ctx)
	s.runWorkerLoop(func(ctx context.Context) {
		slots := make(chan struct{}, maxChannelConcurrencyLimit)
		dispatch := func() {
			if ctx.Err() != nil || s.IsDraining() {
				return
			}
			setting, err := s.runtimeConcurrencySetting()
			if err != nil {
				return
			}
			workerConcurrency := setting.WorkerConcurrency
			for len(slots) < workerConcurrency {
				releaseGlobal, acquired, err := s.coordinator.acquire(ctx, "workers", workerConcurrency, 45*time.Minute)
				if err != nil || !acquired {
					return
				}
				task, err := s.repo.ClaimNextTask(s.workerID, 45*time.Second)
				if err != nil || task == nil {
					releaseGlobal()
					return
				}
				slots <- struct{}{}
				started := s.runWorkerTask(func() {
					defer func() { <-slots; releaseGlobal() }()
					if err := w.processClaimedTask(task); err != nil {
						_ = s.log(task.UserID, task.ID, "error", "后台任务处理失败", err.Error())
					}
				})
				if !started {
					<-slots
					releaseGlobal()
					_ = s.repo.ReleaseTaskLease(task.ID, s.workerID)
					return
				}
			}
		}

		dispatch()
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				dispatch()
			}
		}
	})
}

func (w *taskWorkerCoordinator) processNextTask() error {
	s := w.service
	task, err := s.repo.ClaimNextTask(s.workerID, 45*time.Second)
	if err != nil || task == nil {
		return err
	}
	return w.processClaimedTask(task)
}

func (w *taskWorkerCoordinator) processClaimedTask(task *model.Task) error {
	s := w.service
	terminal := s.terminalCoordinator()
	_ = s.log(task.UserID, task.ID, "info", "后端任务开始处理", "")
	policy, err := s.RuntimePolicy()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskExecutionTimeoutWithPolicy(task.Type, policy.Task))
	defer cancel()
	leaseDone := make(chan struct{})
	leaseLost := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := s.repo.RenewTaskLease(task.ID, s.workerID, 45*time.Second); err != nil {
					leaseLost <- err
					cancel()
					return
				}
			case <-leaseDone:
				return
			}
		}
	}()
	defer close(leaseDone)
	s.registerActiveTask(task.ID, cancel)
	defer s.unregisterActiveTask(task.ID)
	// 取消请求可能在任务领取和注册 worker context 之间到达；再次读取终态
	// 可以避免这种极窄窗口仍然向上游发起调用。
	if latest, latestErr := s.repo.Task(task.ID); latestErr == nil && latest.Status == model.TaskStatusCancelled {
		return terminal.handleAlreadyCancelled(*latest)
	}

	task.Stage = "调用生成模型"
	task.Progress = 35
	if taskUsesUpstreamReportedProgress(task.Type) {
		// 图片/视频百分比只能来自供应商状态响应。连接和提交阶段只展示文案，
		// 不能再用统一的 35% 冒充真实生成进度。
		task.Stage = "正在连接上游"
		task.Progress = 0
	}
	if err := s.repo.UpdateTaskProgress(task.ID, task.Stage, task.Progress); err != nil {
		return fmt.Errorf("更新任务进度失败，任务暂未调用上游：%w", err)
	}
	routeAttempt, err := s.beginTaskRouteAttempt(task)
	if err != nil {
		return terminal.markPreparationFailure(task, "路由准备失败", err, isRouteDispatchUncertain(err), "路由准备失败，上游请求未发出")
	}
	if err := s.taskBilling().MarkBillingRunning(task.BillingOrderID); err != nil {
		return terminal.markPreparationFailure(task, "计费准备失败", err, false, "计费准备失败，上游请求未发出")
	}
	routeResult, stateErr := s.routeExecutor().execute(ctx, task, routeAttempt)
	if stateErr != nil {
		return stateErr
	}
	result, canvasOps, err := routeResult.result, routeResult.canvasOps, routeResult.err
	providerSucceeded := routeResult.providerSucceeded
	providerAccepted := routeResult.providerAccepted || (strings.TrimSpace(task.ProviderRequestID) != "" && task.PollStage != "create")
	providerErr := routeResult.err
	if err == nil {
		result, err = s.persistGeneratedMediaResult(task.UserID, result)
	}
	if err == nil {
		_, err = s.finalizeCharacterTurnaroundTask(*task, result)
	}
	if err != nil {
		channelSlotFailedBeforeRequest := false
		if code, _ := ChannelSlotFailureDetails(err); code != "" {
			channelSlotFailedBeforeRequest = true
		}
		select {
		case leaseErr := <-leaseLost:
			_ = s.log(task.UserID, task.ID, "warn", "任务租约失效，等待其他 worker 恢复", leaseErr.Error())
			return leaseErr
		default:
		}
		if errors.Is(err, context.DeadlineExceeded) {
			decryptedInput, decryptErr := s.decryptTaskInputJSON(task.InputJSON)
			if decryptErr == nil && s.shouldDeferVideoProviderTask(*task, decryptedInput, err) {
				if deferErr := s.repo.DeferRunningTaskForProviderPoll(task.ID, task.LeaseOwner, "后台仍在生成", 15*time.Second); deferErr != nil {
					return deferErr
				}
				_ = s.log(task.UserID, task.ID, "info", "前台等待结束，上游视频仍在生成，将继续回查原任务", task.PollStage)
				return nil
			}
			err = errors.New(taskTimeoutMessage(task.Type))
		}
		if providerErr != nil && providerAccepted && isRecoverableVideoProviderError(*task, err) {
			if deferErr := s.deferAcceptedVideoTask(task, err); deferErr != nil {
				return deferErr
			}
			return nil
		}
		return terminal.handleExecutionFailure(task, err, providerSucceeded, channelSlotFailedBeforeRequest)
	}
	latest, err := s.repo.Task(task.ID)
	if err != nil {
		return err
	}
	if latest.Status == model.TaskStatusCancelled {
		return terminal.handleCancelledResult(*latest)
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		_, terminalErr := terminal.handleResultPersistenceFailure(task, fmt.Errorf("序列化任务结果失败：%w", err))
		return terminalErr
	}
	opsJSON, err := json.Marshal(canvasOps)
	if err != nil {
		_, terminalErr := terminal.handleResultPersistenceFailure(task, fmt.Errorf("序列化画布操作失败：%w", err))
		return terminalErr
	}
	if err := s.saveTaskCompletionWithinStorageQuota(task, resultJSON, opsJSON, len(canvasOps) > 0); err != nil {
		_, terminalErr := terminal.handleResultPersistenceFailure(task, err)
		return terminalErr
	}
	return terminal.handleSuccess(task)
}

// Accepted asynchronous video jobs must remain recoverable when a provider
// transiently rejects a poll/download request. The next worker run rehydrates
// the provider ID and resumes polling instead of entering local failed state.
func (s *Service) deferAcceptedVideoTask(task *model.Task, cause error) error {
	if task == nil || strings.TrimSpace(task.ProviderRequestID) == "" {
		return errors.New("已提交视频任务缺少上游任务 ID")
	}
	stage := "上游任务已提交，等待结果"
	if cause != nil {
		stage = "上游任务已提交，稍后继续查询"
	}
	if err := s.repo.DeferRunningTaskForProviderPoll(task.ID, task.LeaseOwner, stage, 15*time.Second); err != nil {
		return fmt.Errorf("保留已提交视频任务失败：%w", err)
	}
	_ = s.log(task.UserID, task.ID, "warn", stage, safeProviderLogError(cause))
	return nil
}

func isRecoverableVideoProviderError(task model.Task, err error) bool {
	if err == nil || strings.TrimSpace(task.ProviderRequestID) == "" || (!strings.HasPrefix(task.Type, "canvas_video") && !strings.HasPrefix(task.Type, "video_")) {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var httpErr providerHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == 400 || httpErr.StatusCode == 404 || httpErr.StatusCode == 409 || httpErr.StatusCode == 422 || httpErr.StatusCode == 429 || httpErr.StatusCode >= 500
	}
	// Result parsing/download errors are recoverable while the upstream job ID
	// is present; explicit provider terminal failures are handled normally.
	message := strings.ToLower(err.Error())
	for _, terminal := range []string{"视频生成失败", "任务失败", "cancelled", "canceled", "expired", "上游返回失败"} {
		if strings.Contains(message, terminal) {
			return false
		}
	}
	return true
}

func taskUsesUpstreamReportedProgress(taskType string) bool {
	return taskType == "canvas_image" || taskType == "canvas_video" || strings.HasPrefix(taskType, "video_")
}

func taskFailureMessage(err error) string {
	if err == nil {
		return "任务处理失败"
	}
	return truncateRunes(err.Error(), 2_000)
}

func taskExecutionTimeoutWithPolicy(taskType string, policy RuntimeTaskPolicy) time.Duration {
	switch {
	case taskType == "agent_storyboard" || taskType == "agent_storyboard_rows":
		return time.Duration(policy.StoryboardTimeoutMinutes) * time.Minute
	case strings.HasPrefix(taskType, "canvas_video") || strings.HasPrefix(taskType, "video_"):
		return max(time.Duration(policy.VideoTimeoutMinutes)*time.Minute, 5*time.Minute)
	case strings.HasPrefix(taskType, "canvas_image"):
		return time.Duration(policy.ImageTimeoutMinutes) * time.Minute
	case strings.HasPrefix(taskType, "canvas_audio"):
		return time.Duration(policy.AudioTimeoutMinutes) * time.Minute
	case strings.HasPrefix(taskType, "canvas_text"):
		return time.Duration(policy.TextTimeoutMinutes) * time.Minute
	default:
		return time.Duration(policy.DefaultTimeoutMinutes) * time.Minute
	}
}

func (s *Service) shouldDeferVideoProviderTask(task model.Task, decryptedInput string, err error) bool {
	if !errors.Is(err, context.DeadlineExceeded) || strings.TrimSpace(task.ProviderRequestID) == "" || (!strings.HasPrefix(task.Type, "canvas_video") && !strings.HasPrefix(task.Type, "video_")) {
		return false
	}
	var input canvasGenerationInput
	if json.Unmarshal([]byte(decryptedInput), &input) != nil {
		return false
	}
	resolved, resolveErr := s.resolveProviderConfig(input.Config)
	if resolveErr != nil {
		return false
	}
	return resolved.InterfaceType == string(model.ChannelInterfaceNewAPIChannel1) || resolved.InterfaceType == string(model.ChannelInterfaceNewAPIChannel2)
}

func taskTimeoutMessage(taskType string) string {
	if strings.HasPrefix(taskType, "canvas_video") || strings.HasPrefix(taskType, "video_") {
		return "视频生成等待超时，请稍后到任务中心查看或重试。"
	}
	if strings.HasPrefix(taskType, "canvas_image") {
		return "图片生成等待超时，请稍后重试。"
	}
	return "任务执行超时，请稍后重试。"
}
