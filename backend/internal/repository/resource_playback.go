package repository

import (
	"time"

	"infinite-canvas/backend/internal/model"
)

// The unique output key also identifies the worker claim, without a schema change.
func (r *Repository) ClaimResourcePlayback(userID, id, outputKey string) (bool, error) {
	result := r.db.Model(&model.Resource{}).
		Where("id = ? AND user_id = ? AND kind = ? AND status = ? AND (playback_status = '' OR playback_status IS NULL OR playback_status = ?)",
			id, userID, "video", model.ResourceStatusReady, model.PlaybackStatusNone).
		Updates(map[string]any{"playback_status": model.PlaybackStatusProcessing, "playback_object_key": outputKey, "playback_error": "", "updated_at": time.Now()})
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) CompleteResourcePlayback(userID, id, claimKey, status, message string) (bool, error) {
	outputKey := claimKey
	if status != model.PlaybackStatusReady {
		outputKey = ""
	}
	result := r.db.Model(&model.Resource{}).
		Where("id = ? AND user_id = ? AND kind = ? AND status = ? AND playback_status = ? AND playback_object_key = ?",
			id, userID, "video", model.ResourceStatusReady, model.PlaybackStatusProcessing, claimKey).
		Updates(map[string]any{"playback_status": status, "playback_object_key": outputKey, "playback_error": message, "updated_at": time.Now()})
	return result.RowsAffected == 1, result.Error
}

func (r *Repository) MarkResourcePlaybackNone(userID, id string) (bool, error) {
	result := r.db.Model(&model.Resource{}).
		Where("id = ? AND user_id = ? AND kind = ? AND status = ? AND (playback_status = '' OR playback_status IS NULL)",
			id, userID, "video", model.ResourceStatusReady).
		Updates(map[string]any{"playback_status": model.PlaybackStatusNone, "updated_at": time.Now()})
	return result.RowsAffected == 1, result.Error
}

// Do not reset live work in other instances when this process starts.
func (r *Repository) ExpireResourcePlaybacks(before time.Time) error {
	return r.db.Model(&model.Resource{}).
		Where("playback_status = ? AND updated_at < ?", model.PlaybackStatusProcessing, before).
		Updates(map[string]any{"playback_status": model.PlaybackStatusFailed, "playback_object_key": "", "playback_error": "播放副本处理超时，请下载原件", "updated_at": time.Now()}).Error
}

func (r *Repository) ExpireResourcePlayback(userID, id string, before time.Time) error {
	return r.db.Model(&model.Resource{}).
		Where("id = ? AND user_id = ? AND playback_status = ? AND updated_at < ?", id, userID, model.PlaybackStatusProcessing, before).
		Updates(map[string]any{"playback_status": model.PlaybackStatusFailed, "playback_object_key": "", "playback_error": "播放副本处理超时，请下载原件", "updated_at": time.Now()}).Error
}
