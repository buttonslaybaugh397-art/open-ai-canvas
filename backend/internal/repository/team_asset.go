package repository

import (
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) TeamAssets() ([]model.TeamAsset, error) {
	var assets []model.TeamAsset
	err := r.db.Order("updated_at desc, created_at desc").Find(&assets).Error
	return assets, err
}

func (r *Repository) TeamAsset(id string) (*model.TeamAsset, error) {
	var asset model.TeamAsset
	if err := r.db.First(&asset, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *Repository) TeamAssetFolder(id string) (*model.TeamAssetFolder, error) {
	var folder model.TeamAssetFolder
	if err := r.db.First(&folder, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &folder, nil
}

func (r *Repository) TeamAssetResourceIDs(teamAssetID string) ([]string, error) {
	var links []model.TeamAssetResource
	if err := r.db.Where("team_asset_id = ?", teamAssetID).Order("resource_id asc").Find(&links).Error; err != nil {
		return nil, err
	}
	resourceIDs := make([]string, 0, len(links))
	for _, link := range links {
		resourceIDs = append(resourceIDs, link.ResourceID)
	}
	return resourceIDs, nil
}

func (r *Repository) TeamAssetStorageUsage(ownerUserID string) (int64, int64, error) {
	var assets []model.TeamAsset
	if err := r.db.Select("payload_json").Find(&assets, "owner_user_id = ?", ownerUserID).Error; err != nil {
		return 0, 0, err
	}
	var bytes int64
	for _, asset := range assets {
		bytes += int64(len([]byte(asset.PayloadJSON)))
	}
	return int64(len(assets)), bytes, nil
}

func (r *Repository) UpsertTeamAsset(asset *model.TeamAsset, resourceIDs []string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"folder_id",
				"kind",
				"category",
				"status",
				"title",
				"payload_json",
				"updated_at",
			}),
		}).Create(asset).Error; err != nil {
			return err
		}
		if err := tx.Where("team_asset_id = ?", asset.ID).Delete(&model.TeamAssetResource{}).Error; err != nil {
			return err
		}
		links := make([]model.TeamAssetResource, 0, len(resourceIDs))
		seen := make(map[string]struct{}, len(resourceIDs))
		for _, resourceID := range resourceIDs {
			resourceID = strings.TrimSpace(resourceID)
			if resourceID == "" {
				continue
			}
			if _, exists := seen[resourceID]; exists {
				continue
			}
			seen[resourceID] = struct{}{}
			links = append(links, model.TeamAssetResource{TeamAssetID: asset.ID, ResourceID: resourceID, CreatedAt: asset.UpdatedAt})
		}
		if len(links) == 0 {
			return nil
		}
		return tx.Create(&links).Error
	})
}

func (r *Repository) TeamAssetFolders() ([]model.TeamAssetFolder, error) {
	var folders []model.TeamAssetFolder
	err := r.db.Order("updated_at desc, created_at desc").Find(&folders).Error
	return folders, err
}

func (r *Repository) SaveTeamAssetFolder(folder *model.TeamAssetFolder) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name",
			"updated_at",
		}),
	}).Create(folder).Error
}

func (r *Repository) TeamAssetsInFolder(folderID string) ([]model.TeamAsset, error) {
	var assets []model.TeamAsset
	err := r.db.Where("folder_id = ?", folderID).Order("updated_at desc, created_at desc").Find(&assets).Error
	return assets, err
}

func (r *Repository) DeleteTeamAssetFolder(folderID string, assets []model.TeamAsset) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		for _, asset := range assets {
			result := tx.Model(&model.TeamAsset{}).
				Where("id = ? AND folder_id = ?", asset.ID, folderID).
				Updates(map[string]any{
					"folder_id":    "",
					"payload_json": asset.PayloadJSON,
					"updated_at":   asset.UpdatedAt,
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
		}
		result := tx.Delete(&model.TeamAssetFolder{}, "id = ?", folderID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func (r *Repository) MoveTeamAsset(id string, folderID string, payloadJSON string, updatedAt time.Time) error {
	result := r.db.Model(&model.TeamAsset{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"folder_id":    folderID,
			"payload_json": payloadJSON,
			"updated_at":   updatedAt,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *Repository) DeleteTeamAsset(id string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("team_asset_id = ?", id).Delete(&model.TeamAssetResource{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&model.TeamAsset{}, "id = ?", id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}
