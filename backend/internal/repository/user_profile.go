package repository

import (
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

func (r *Repository) UpdateOwnDisplayName(userID, previousDisplayName, displayName string, now time.Time, audit *model.AdminAuditEvent) (*model.User, error) {
	var user model.User
	err := r.db.Transaction(func(tx *gorm.DB) error {
		// A self-service profile change must never write login credentials or permissions.
		result := tx.Model(&model.User{}).
			Where("id = ? AND display_name = ? AND status = ?", userID, previousDisplayName, model.UserStatusActive).
			Updates(map[string]any{"display_name": displayName, "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrUserChanged
		}
		if err := tx.Create(audit).Error; err != nil {
			return err
		}
		return tx.First(&user, "id = ?", userID).Error
	})
	if err != nil {
		return nil, err
	}
	return &user, nil
}
