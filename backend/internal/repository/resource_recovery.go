package repository

import (
	"time"

	"infinite-canvas/backend/internal/model"
)

func (r *Repository) MarkResourceCloudRecovered(userID, id, provider, objectKey string, size int64, etag string) (bool, error) {
	result := r.db.Model(&model.Resource{}).
		Where("id = ? AND user_id = ? AND provider = ? AND object_key = ? AND status = ?", id, userID, provider, objectKey, model.ResourceStatusReady).
		Updates(map[string]any{"size": size, "e_tag": etag, "error": "", "updated_at": time.Now()})
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) MarkReadyResourceFailed(userID, id, provider, objectKey, reason string) (bool, error) {
	result := r.db.Model(&model.Resource{}).
		Where("id = ? AND user_id = ? AND provider = ? AND object_key = ? AND status = ?", id, userID, provider, objectKey, model.ResourceStatusReady).
		Updates(map[string]any{"status": model.ResourceStatusFailed, "error": reason, "updated_at": time.Now()})
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) PromoteLocalResource(userID, id, localObjectKey, provider, endpoint, bucket, storageSettingID, objectKey string, size int64, etag string) (bool, error) {
	result := r.db.Model(&model.Resource{}).
		Where("id = ? AND user_id = ? AND provider = ? AND object_key = ? AND status = ?", id, userID, "local", localObjectKey, model.ResourceStatusReady).
		Updates(map[string]any{
			"provider": provider, "endpoint": endpoint, "bucket": bucket, "storage_setting_id": storageSettingID,
			"object_key": objectKey, "size": size, "e_tag": etag, "error": "", "updated_at": time.Now(),
		})
	return result.RowsAffected == 1, result.Error
}
