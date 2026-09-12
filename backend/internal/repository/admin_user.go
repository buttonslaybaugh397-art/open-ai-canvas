package repository

import (
	"errors"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

var ErrUsernameExists = errors.New("username already exists")
var ErrUserChanged = errors.New("user changed concurrently")

func (r *Repository) UpdateAdminUser(user *model.User, previousUsername string, resetSessions bool, audit *model.AdminAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if user.Username != previousUsername {
			// Restored databases may only have a case-sensitive username index.
			// Serialize renames with other user writes before checking login-name uniqueness.
			if r.Dialect() == "postgres" {
				if err := tx.Exec("LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
					return err
				}
			}
			var count int64
			if err := tx.Model(&model.User{}).Where("lower(username) = lower(?) AND id <> ?", user.Username, user.ID).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return ErrUsernameExists
			}
		}
		updates := map[string]any{
			"username": user.Username, "display_name": user.DisplayName, "email": user.Email,
			"role": user.Role, "status": user.Status, "updated_at": user.UpdatedAt,
		}
		if resetSessions {
			updates["password_hash"] = user.PasswordHash
			if err := tx.Delete(&model.AuthSession{}, "user_id = ?", user.ID).Error; err != nil {
				return err
			}
		}
		result := tx.Model(&model.User{}).Where("id = ? AND username = ?", user.ID, previousUsername).Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrUserChanged
		}
		return tx.Create(audit).Error
	})
}
