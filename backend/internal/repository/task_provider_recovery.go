package repository

import (
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"
)

// ResumeFailedTaskProviderPolling transfers the manual lease to the durable poll
// queue. Billing and provider identity are retained; this is not a new submission.
func (r *Repository) ResumeFailedTaskProviderPolling(task *model.Task, providerStatus string) (*model.Task, error) {
	if task == nil || strings.TrimSpace(task.ProviderRequestID) == "" || !strings.HasPrefix(task.LeaseOwner, "manual-recovery:") {
		return nil, ErrTaskStateConflict
	}
	now := time.Now()
	result := taskLeaseWriter(r.db.Model(&model.Task{}), task.LeaseOwner).
		Where("id = ? AND user_id = ? AND status = ?", task.ID, task.UserID, model.TaskStatusFailed).
		Updates(map[string]any{
			"status": model.TaskStatusRunning, "stage": "已恢复自动查询上游任务",
			"error": "", "completed_at": nil, "provider_request_id": task.ProviderRequestID,
			"provider_recovery_at": now, "poll_stage": providerStatus, "next_poll_at": now.Add(5 * time.Second),
			"lease_owner": "", "lease_expires_at": nil, "updated_at": now,
		})
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, ErrTaskStateConflict
	}
	return r.TaskForUser(task.UserID, task.ID)
}
