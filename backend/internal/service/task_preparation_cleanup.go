package service

import (
	"context"
	"errors"
	"log"
	"time"

	"infinite-canvas/backend/internal/model"
)

const (
	taskPreparationTimeout         = 30 * time.Minute
	taskPreparationCleanupInterval = time.Minute
	taskPreparationCleanupBatch    = 100
)

func (s *Service) startTaskPreparationCleanup(ctx context.Context) {
	cleanup := func() {
		if err := s.cleanupExpiredPreparingTasks(time.Now()); err != nil {
			log.Printf("preparing task cleanup failed: %v", err)
		}
	}
	s.runWorkerLoop(func(ctx context.Context) {
		cleanup()
		ticker := time.NewTicker(taskPreparationCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cleanup()
			}
		}
	})
}

func (s *Service) cleanupExpiredPreparingTasks(now time.Time) error {
	cutoff := now.Add(-taskPreparationTimeout)
	tasks, err := s.repo.StalePreparingTasks(cutoff, taskPreparationCleanupBatch)
	if err != nil {
		return err
	}

	var resultErr error
	for _, task := range tasks {
		expired, err := s.repo.ExpirePreparingTaskIfStale(task.UserID, task.ID, cutoff, now)
		if err != nil {
			resultErr = errors.Join(resultErr, err)
			continue
		}
		if !expired {
			continue
		}

		task.Status = model.TaskStatusCancelled
		task.Stage = "准备输入超时已取消"
		task.Error = "准备生成输入超时，任务已取消"
		task.CompletedAt = &now
		s.cancelActiveTask(task.ID)

		if err := s.markSessionFailed(task, "准备生成输入超时，任务已取消。"); err != nil {
			_ = s.log(task.UserID, task.ID, "error", "准备任务超时后更新会话状态失败", err.Error())
		}
		if err := s.finalizeTaskTextReplay(task.ID, model.TaskStatusCancelled); err != nil {
			_ = s.log(task.UserID, task.ID, "error", "准备任务超时后归并文本回放失败", err.Error())
		}
		if err := s.taskBilling().RefundBilling(task.BillingOrderID, "准备生成输入超时，任务未进入执行"); err != nil {
			_ = s.log(task.UserID, task.ID, "error", "准备任务超时后处理积分失败，已保留人工核对线索", err.Error())
		}
		_ = s.log(task.UserID, task.ID, "warn", "准备生成输入超时，任务已取消", "")
	}
	return resultErr
}
