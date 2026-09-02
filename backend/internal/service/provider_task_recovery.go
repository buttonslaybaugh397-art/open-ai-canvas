package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/protocol"
	"infinite-canvas/backend/internal/repository"
)

const providerTaskRecoveryLeaseDuration = 10 * time.Minute

type ProviderTaskQueryResult struct {
	Task           *model.Task `json:"task"`
	ProviderStatus string      `json:"providerStatus"`
	Recovered      bool        `json:"recovered"`
	Refetched      bool        `json:"refetched"`
	BillingSettled bool        `json:"billingSettled"`
}

func (s *Service) QueryFailedVideoTask(ctx context.Context, userID string, taskID string) (*ProviderTaskQueryResult, error) {
	task, err := s.repo.TaskForUser(strings.TrimSpace(userID), strings.TrimSpace(taskID))
	if err != nil {
		return nil, err
	}
	return s.queryVideoTaskProvider(ctx, task, strings.TrimSpace(userID))
}

func (s *Service) AdminQueryFailedVideoTask(ctx context.Context, actor *model.User, logID string) (*ProviderTaskQueryResult, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	log, err := s.repo.APICallLog(strings.TrimSpace(logID))
	if err != nil {
		return nil, err
	}
	if log.Capability != "video" || strings.TrimSpace(log.TaskID) == "" {
		return nil, BadAuthRequest("该请求没有可查询的视频任务")
	}
	task, err := s.repo.Task(log.TaskID)
	if err != nil {
		return nil, err
	}
	if task.UserID != log.UserID {
		return nil, BadAuthRequest("请求与任务归属不一致")
	}
	if task.ProviderRequestID == "" {
		task.ProviderRequestID = strings.TrimSpace(log.ProviderRequestID)
	}
	result, err := s.queryVideoTaskProvider(ctx, task, "")
	if err != nil {
		return nil, err
	}
	if err := s.appendAdminAudit(actor, "api_log.query_provider_task", "task", task.ID, "人工查询视频任务", map[string]any{
		"apiCallLogId": log.ID, "providerRequestId": task.ProviderRequestID, "providerStatus": result.ProviderStatus, "recovered": result.Recovered, "refetched": result.Refetched,
	}); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) queryVideoTaskProvider(ctx context.Context, task *model.Task, claimUserID string) (*ProviderTaskQueryResult, error) {
	billing := s.taskBilling()
	ctx = withProtocolRegistry(ctx, s.protocolRegistry())
	if task == nil || task.ID == "" {
		return nil, BadAuthRequest("任务不存在")
	}
	wasSucceeded := task.Status == model.TaskStatusSucceeded
	if task.Status != model.TaskStatusFailed && !wasSucceeded {
		return nil, BadAuthRequest("只能人工查询失败或已完成的视频任务")
	}
	if !strings.HasPrefix(task.Type, "canvas_video") && !strings.HasPrefix(task.Type, "video_") {
		return nil, BadAuthRequest("该任务不是视频生成任务")
	}

	s.hydrateTaskProviderRequestID(task)
	providerRequestID := strings.TrimSpace(task.ProviderRequestID)
	if providerRequestID == "" {
		return nil, BadAuthRequest("该任务没有可恢复的上游任务 ID")
	}
	wasRefunded := false
	if !wasSucceeded && task.BillingOrderID != "" {
		order, err := s.repo.BillingOrder(task.BillingOrderID)
		if err != nil {
			return nil, err
		}
		if order.UserID != task.UserID || order.TaskID != task.ID {
			return nil, BadAuthRequest("任务与计费订单归属不一致")
		}
		wasRefunded = order.Status == model.BillingStatusRefunded
	}

	decryptedInput, err := s.decryptTaskInputJSON(task.InputJSON)
	if err != nil {
		return nil, fmt.Errorf("读取任务配置失败：%w", err)
	}
	var input canvasGenerationInput
	if err := json.Unmarshal([]byte(decryptedInput), &input); err != nil {
		return nil, fmt.Errorf("任务输入解析失败：%w", err)
	}
	if wasSucceeded && strings.TrimSpace(input.Config.BaseURL) == "" && strings.TrimSpace(input.Config.ChannelID) == "" {
		if strings.TrimSpace(task.ChannelModelID) == "" {
			return nil, BadAuthRequest("历史任务缺少原渠道配置，无法重新获取视频")
		}
		channelModel, modelErr := s.repo.ChannelModel(task.ChannelModelID)
		if modelErr != nil || !channelModel.Enabled {
			return nil, BadAuthRequest("历史任务原模型服务已失效，无法重新获取视频")
		}
		input.Config.ChannelID = channelModel.ChannelID
		input.Config.ChannelModelKey = channelModel.ModelKey
		input.Config.Model = channelModel.ModelKey
	}
	config, err := s.resolveProviderConfig(input.Config)
	if err != nil {
		return nil, err
	}
	adapter, declarative := declarativeProtocolAdapterForContext(ctx, config.InterfaceType)
	if !declarative && config.InterfaceType != string(model.ChannelInterfaceNewAPIChannel2) {
		return nil, BadAuthRequest("该任务的请求协议不支持安全查询上游状态")
	}
	input.Config = config
	if err := s.hydrateGenerationMedia(task.UserID, &input, generationRequiresPublicReferenceURL(ctx, input)); err != nil {
		return nil, err
	}
	if !wasSucceeded {
		task.InputJSON = decryptedInput
	}
	task.ProviderRequestID = providerRequestID
	if err := s.repo.UpdateTaskProviderState(task.ID, providerRequestID, task.PollStage, task.NextPollAt); err != nil {
		return nil, err
	}

	owner := "manual-recovery:" + newID()
	if err := s.repo.ClaimTaskProviderRecovery(task.ID, claimUserID, owner, providerTaskRecoveryLeaseDuration); err != nil {
		if errors.Is(err, repository.ErrTaskProviderRecoveryConflict) {
			return nil, &AuthError{Status: 409, Message: "该任务正在查询上游状态，请稍后再试"}
		}
		return nil, err
	}
	task.LeaseOwner = owner
	defer func() {
		if releaseErr := s.repo.ReleaseTaskProviderRecovery(task.ID, owner); releaseErr != nil {
			_ = s.log(task.UserID, task.ID, "error", "人工查询租约释放失败", releaseErr.Error())
		}
	}()

	// 上游已成功后的完整媒体下载和本地入库可能持续数十秒。浏览器关闭抽屉、
	// 页面刷新或代理断开都不应中断这项运维恢复，否则任务会再次停在
	// failed/refunded。保留请求值用于审计，但把执行生命周期交给恢复租约控制。
	recoveryCtx, cancelRecovery := providerTaskRecoveryContext(ctx)
	defer cancelRecovery()
	queryCtx := withProviderAnalytics(recoveryCtx, s, *task)
	queryCtx = withProviderOutboundPolicy(queryCtx, input.Config)
	result, providerStatus, err := queryExistingVideoTask(queryCtx, input, adapter, declarative, providerRequestID)
	if err != nil {
		_ = s.log(task.UserID, task.ID, "error", "人工查询上游视频任务失败", err.Error())
		return nil, err
	}
	if result == nil {
		_ = s.log(task.UserID, task.ID, "info", "人工查询完成，上游任务仍在处理", providerStatus)
		return &ProviderTaskQueryResult{Task: taskForOutput(*task), ProviderStatus: providerStatus, Recovered: false}, nil
	}

	result, err = s.persistGeneratedMediaResult(task.UserID, result)
	if err != nil {
		_ = s.log(task.UserID, task.ID, "error", "人工查询已取得视频，但结果保存失败", err.Error())
		return nil, err
	}
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	task.Error = ""
	task.PollStage = strings.ToLower(providerStatus)
	task.NextPollAt = nil
	if wasSucceeded {
		if err := s.replaceSucceededTaskResultWithinStorageQuota(task, resultJSON, owner); err != nil {
			_ = s.log(task.UserID, task.ID, "error", "已取得上游视频，但二次获取结果替换失败", err.Error())
			return nil, err
		}
		if err := s.RegisterTaskOutputFromTask(*task); err != nil {
			_ = s.log(task.UserID, task.ID, "error", "视频已重新获取，但项目产物更新失败", err.Error())
			return nil, fmt.Errorf("视频已重新获取，但项目产物更新失败：%w", err)
		}
		_ = s.log(task.UserID, task.ID, "info", "已从上游重新获取视频并更新任务产物，积分未变", providerStatus)
		return &ProviderTaskQueryResult{Task: taskForOutput(*task), ProviderStatus: providerStatus, Recovered: true, Refetched: true, BillingSettled: true}, nil
	}
	if err := s.saveTaskCompletionWithinStorageQuota(task, resultJSON, nil, false); err != nil {
		uncertainErr := billing.MarkBillingUncertain(task.BillingOrderID, "人工查询确认上游成功，但任务结果未保存："+err.Error())
		_ = s.log(task.UserID, task.ID, "error", "人工查询已取得视频，但任务恢复失败", err.Error())
		if uncertainErr != nil {
			return nil, errors.Join(err, fmt.Errorf("记录任务结果未保存的计费待核对状态失败：%w", uncertainErr))
		}
		return nil, err
	}
	billingSettled := true
	var billingErr error
	if wasRefunded {
		billingErr = billing.RestoreRefundedBilling(task.BillingOrderID, providerRequestID)
	} else {
		billingErr = billing.SettleBilling(task.BillingOrderID, providerRequestID)
	}
	if billingErr != nil {
		billingSettled = false
		uncertainErr := billing.MarkBillingUncertain(task.BillingOrderID, "人工查询确认生成成功，但积分结算失败："+billingErr.Error())
		_ = s.log(task.UserID, task.ID, "error", "任务恢复成功但积分结算失败，已进入待核对", billingErr.Error())
		if uncertainErr != nil {
			return nil, errors.Join(billingErr, fmt.Errorf("记录任务恢复后的计费待核对状态失败：%w", uncertainErr))
		}
		return nil, billingErr
	}
	if err := s.RegisterTaskOutputFromTask(*task); err != nil {
		_ = s.log(task.UserID, task.ID, "error", "任务恢复成功但项目产物登记失败", err.Error())
		return nil, fmt.Errorf("任务已恢复并完成扣费，但项目素材登记失败：%w", err)
	}
	_ = s.log(task.UserID, task.ID, "info", "人工查询确认生成成功，任务已恢复、完成结算并登记项目产物", providerStatus)
	return &ProviderTaskQueryResult{Task: taskForOutput(*task), ProviderStatus: providerStatus, Recovered: true, BillingSettled: billingSettled}, nil
}

// Existing NewAPI Video Generations tasks need the host parser because it
// supports the nested result shapes returned by NewAPI-compatible gateways.
// A declarative plugin with the same protocol ID must not shadow that parser.
func queryExistingVideoTask(ctx context.Context, input canvasGenerationInput, adapter protocol.Adapter, declarative bool, providerRequestID string) (map[string]interface{}, string, error) {
	if input.Config.InterfaceType == string(model.ChannelInterfaceNewAPIChannel2) {
		return queryNewAPIChannel2VideoTask(ctx, input, providerRequestID)
	}
	if declarative {
		return queryProtocolAdapterVideoTask(ctx, input, adapter, providerRequestID)
	}
	return nil, "", BadAuthRequest("该任务的请求协议不支持安全查询上游状态")
}

func providerTaskRecoveryContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), providerTaskRecoveryLeaseDuration)
}
