package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

// FileStorageSnapshot contains ownership records, not a cumulative upload counter.
type FileStorageSnapshot struct {
	Resources []model.Resource
	Sessions  []model.SessionFile
	Deletions []model.ResourceDeletionJob
}

func (r *Repository) UserFileStorageSnapshot(ctx context.Context, userID string) (FileStorageSnapshot, error) {
	var snapshot FileStorageSnapshot
	if strings.TrimSpace(userID) == "" {
		return snapshot, errors.New("storage usage requires a user")
	}
	// A resource moves to the deletion outbox atomically; observe both tables
	// in one snapshot so it cannot disappear between the two reads.
	err := r.db.WithContext(ctx).Transaction(func(db *gorm.DB) error {
		if err := db.Select("id", "user_id", "status", "provider", "endpoint", "bucket", "storage_setting_id", "object_key", "size", "ETag", "updated_at").
			Where("user_id = ? AND status IN ?", userID, []model.ResourceStatus{model.ResourceStatusReady, model.ResourceStatusFailed}).
			Order("id").Find(&snapshot.Resources).Error; err != nil {
			return err
		}
		if err := db.Select("id", "user_id", "path", "size").Where("user_id = ?", userID).
			Order("id").Find(&snapshot.Sessions).Error; err != nil {
			return err
		}
		return db.Select("id", "user_id", "resource_id", "provider", "endpoint", "bucket", "storage_setting_id", "object_key").
			Where("user_id = ? AND status IN ?", userID, []model.ResourceDeletionStatus{model.ResourceDeletionStatusPending, model.ResourceDeletionStatusProcessing}).
			Order("id").Find(&snapshot.Deletions).Error
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return snapshot, err
}
