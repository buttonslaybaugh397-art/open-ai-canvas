package repository

import (
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) ClaimNextResourceCloudRecovery(owner string, leaseDuration time.Duration) (*model.Resource, error) {
	var resource model.Resource
	now := time.Now()
	err := r.db.Transaction(func(tx *gorm.DB) error {
		available := "provider <> ? AND local_backup_key <> '' AND cloud_sync_status IN ? AND (cloud_sync_next_attempt_at IS NULL OR cloud_sync_next_attempt_at <= ?) AND (cloud_sync_lease_expires_at IS NULL OR cloud_sync_lease_expires_at <= ?)"
		query := tx.Where(available, "local", []model.ResourceCloudSyncStatus{model.ResourceCloudSyncStatusPending, model.ResourceCloudSyncStatusRecovering}, now, now).
			Where("status = ?", model.ResourceStatusReady).Order("cloud_sync_next_attempt_at asc, updated_at asc").Limit(1)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		result := query.Find(&resource)
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		claim := tx.Model(&model.Resource{}).Where("id = ?", resource.ID)
		if r.Dialect() != "postgres" {
			claim = claim.Where(available, "local", []model.ResourceCloudSyncStatus{model.ResourceCloudSyncStatusPending, model.ResourceCloudSyncStatusRecovering}, now, now)
		}
		updated := claim.Updates(map[string]any{
			"cloud_sync_status":           model.ResourceCloudSyncStatusRecovering,
			"cloud_sync_attempts":         gorm.Expr("cloud_sync_attempts + ?", 1),
			"cloud_sync_lease_owner":      owner,
			"cloud_sync_lease_expires_at": now.Add(leaseDuration),
			"updated_at":                  now,
		})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected == 0 {
			resource = model.Resource{}
			return nil
		}
		return tx.First(&resource, "id = ?", resource.ID).Error
	})
	if err != nil || resource.ID == "" {
		return nil, err
	}
	return &resource, nil
}

func (r *Repository) CompleteResourceCloudRecovery(id string, owner string, etag string) error {
	result := r.db.Model(&model.Resource{}).Where("id = ? AND cloud_sync_lease_owner = ?", id, owner).Updates(map[string]any{
		"cloud_sync_status":           model.ResourceCloudSyncStatusSynced,
		"cloud_sync_error":            "",
		"cloud_sync_next_attempt_at":  nil,
		"cloud_sync_lease_owner":      "",
		"cloud_sync_lease_expires_at": nil,
		"e_tag":                       etag,
		"updated_at":                  time.Now(),
	})
	return result.Error
}

func (r *Repository) RetryResourceCloudRecovery(id string, owner string, lastError string, nextAttemptAt time.Time) error {
	return r.db.Model(&model.Resource{}).Where("id = ? AND cloud_sync_lease_owner = ?", id, owner).Updates(map[string]any{
		"cloud_sync_status":           model.ResourceCloudSyncStatusPending,
		"cloud_sync_error":            lastError,
		"cloud_sync_next_attempt_at":  nextAttemptAt,
		"cloud_sync_lease_owner":      "",
		"cloud_sync_lease_expires_at": nil,
		"updated_at":                  time.Now(),
	}).Error
}
