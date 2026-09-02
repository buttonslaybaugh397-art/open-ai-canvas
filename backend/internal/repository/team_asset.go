package repository

import (
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrTeamAssetQuotaExceeded = errors.New("team asset quota exceeded")
var ErrTeamStorageQuotaExceeded = errors.New("team storage quota exceeded")
var ErrTeamInvitationUnavailable = errors.New("team invitation unavailable")
var ErrTeamInvitationAlreadyMember = errors.New("user is already an active team member")

type TeamUsage struct {
	MemberCount  int64 `json:"memberCount"`
	AssetCount   int64 `json:"assetCount"`
	StorageBytes int64 `json:"storageBytes"`
}

type TeamAssetSave struct {
	Asset       *model.TeamAsset
	ResourceIDs []string
}

type TeamAssetFilter struct {
	Page     int
	PageSize int
	Query    string
	Kind     string
	FolderID *string
}

type TeamAuditPage struct {
	Events   []TeamAuditRecord
	Page     int
	PageSize int
	Total    int64
}

type TeamAuditRecord struct {
	model.TeamAuditEvent
	ActorUsername    string `gorm:"column:actor_username"`
	ActorDisplayName string `gorm:"column:actor_display_name"`
}

type TeamMemberRecord struct {
	TeamID      string               `gorm:"column:team_id"`
	UserID      string               `gorm:"column:user_id"`
	Username    string               `gorm:"column:username"`
	DisplayName string               `gorm:"column:display_name"`
	Role        model.TeamMemberRole `gorm:"column:role"`
	JoinedAt    time.Time            `gorm:"column:joined_at"`
}

func (r *Repository) TeamInvitations(teamID string) ([]model.TeamInvitation, error) {
	var invitations []model.TeamInvitation
	err := r.db.Where("team_id = ? AND consumed_at IS NULL AND revoked_at IS NULL", teamID).Order("created_at DESC").Find(&invitations).Error
	return invitations, err
}

func (r *Repository) TeamInvitationByHash(tokenHash string) (*model.TeamInvitation, error) {
	var invitation model.TeamInvitation
	if err := r.db.First(&invitation, "token_hash = ?", tokenHash).Error; err != nil {
		return nil, err
	}
	return &invitation, nil
}

func (r *Repository) CreateTeamInvitation(invitation *model.TeamInvitation, audit *model.TeamAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(invitation).Error; err != nil {
			return err
		}
		return appendTeamAudit(tx, audit)
	})
}

func (r *Repository) RevokeTeamInvitation(teamID string, invitationID string, revokedAt time.Time, audit *model.TeamAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.TeamInvitation{}).
			Where("id = ? AND team_id = ? AND consumed_at IS NULL AND revoked_at IS NULL", invitationID, teamID).
			Update("revoked_at", revokedAt)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return appendTeamAudit(tx, audit)
	})
}

func (r *Repository) AcceptTeamInvitation(tokenHash string, userID string, acceptedAt time.Time, audit *model.TeamAuditEvent) (*model.TeamInvitation, error) {
	var accepted model.TeamInvitation
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&accepted, "token_hash = ?", tokenHash).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTeamInvitationUnavailable
			}
			return err
		}
		if accepted.RevokedAt != nil || accepted.ConsumedAt != nil || !accepted.ExpiresAt.After(acceptedAt) {
			return ErrTeamInvitationUnavailable
		}
		var existing model.TeamMember
		existingErr := tx.First(&existing, "team_id = ? AND user_id = ?", accepted.TeamID, userID).Error
		if existingErr == nil && existing.Status == model.TeamMemberStatusActive {
			return ErrTeamInvitationAlreadyMember
		}
		if existingErr != nil && !errors.Is(existingErr, gorm.ErrRecordNotFound) {
			return existingErr
		}
		claim := tx.Model(&model.TeamInvitation{}).
			Where("id = ? AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at > ?", accepted.ID, acceptedAt).
			Updates(map[string]any{"consumed_at": acceptedAt, "consumed_by_user_id": userID})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return ErrTeamInvitationUnavailable
		}
		member := model.TeamMember{TeamID: accepted.TeamID, UserID: userID, Role: accepted.Role, Status: model.TeamMemberStatusActive, CreatedAt: acceptedAt, UpdatedAt: acceptedAt}
		if errors.Is(existingErr, gorm.ErrRecordNotFound) {
			if err := tx.Create(&member).Error; err != nil {
				return err
			}
		} else if err := tx.Model(&existing).Updates(map[string]any{"role": accepted.Role, "status": model.TeamMemberStatusActive, "updated_at": acceptedAt}).Error; err != nil {
			return err
		}
		return appendTeamAudit(tx, audit)
	})
	return &accepted, err
}

func (r *Repository) TeamsForUser(userID string) ([]model.Team, error) {
	var teams []model.Team
	err := r.db.Table("teams").Joins("JOIN team_members ON team_members.team_id = teams.id").Where("team_members.user_id = ? AND team_members.status = ?", userID, model.TeamMemberStatusActive).Order("teams.updated_at DESC, teams.created_at DESC").Find(&teams).Error
	return teams, err
}

func (r *Repository) Team(teamID string) (*model.Team, error) {
	var team model.Team
	if err := r.db.First(&team, "id = ?", teamID).Error; err != nil {
		return nil, err
	}
	return &team, nil
}

func (r *Repository) TeamUsage(teamID string) (TeamUsage, error) {
	return teamUsage(r.db, teamID)
}

func (r *Repository) UpdateTeamSettings(teamID string, name string, description string, assetLimit int64, storageLimit int64, updatedAt time.Time, audit *model.TeamAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var team model.Team
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&team, "id = ?", teamID).Error; err != nil {
			return err
		}
		usage, err := teamUsage(tx, teamID)
		if err != nil {
			return err
		}
		if assetLimit < usage.AssetCount {
			return ErrTeamAssetQuotaExceeded
		}
		if storageLimit < usage.StorageBytes {
			return ErrTeamStorageQuotaExceeded
		}
		if err := tx.Model(&team).Updates(map[string]any{"name": name, "description": description, "asset_limit": assetLimit, "storage_limit": storageLimit, "updated_at": updatedAt}).Error; err != nil {
			return err
		}
		return appendTeamAudit(tx, audit)
	})
}

func (r *Repository) TeamMember(teamID string, userID string) (*model.TeamMember, error) {
	var member model.TeamMember
	if err := r.db.First(&member, "team_id = ? AND user_id = ? AND status = ?", teamID, userID, model.TeamMemberStatusActive).Error; err != nil {
		return nil, err
	}
	return &member, nil
}

func (r *Repository) TeamMemberRecord(teamID string, userID string) (*model.TeamMember, error) {
	var member model.TeamMember
	if err := r.db.First(&member, "team_id = ? AND user_id = ?", teamID, userID).Error; err != nil {
		return nil, err
	}
	return &member, nil
}

func (r *Repository) TeamMembers(teamID string) ([]TeamMemberRecord, error) {
	var members []TeamMemberRecord
	err := r.db.Table("team_members").
		Select("team_members.team_id, team_members.user_id, users.username, users.display_name, team_members.role, team_members.created_at AS joined_at").
		Joins("JOIN users ON users.id = team_members.user_id").
		Where("team_members.team_id = ? AND team_members.status = ?", teamID, model.TeamMemberStatusActive).
		Order("CASE team_members.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 WHEN 'editor' THEN 2 ELSE 3 END, users.username ASC").
		Scan(&members).Error
	return members, err
}

func (r *Repository) SaveTeamMember(member *model.TeamMember, audit *model.TeamAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.TeamMember{}).Where("team_id = ? AND user_id = ?", member.TeamID, member.UserID).Updates(map[string]any{"role": member.Role, "status": member.Status, "updated_at": member.UpdatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			if err := tx.Create(member).Error; err != nil {
				return err
			}
		}
		return appendTeamAudit(tx, audit)
	})
}

func (r *Repository) UpdateTeamMemberRole(teamID string, userID string, role model.TeamMemberRole, updatedAt time.Time, audit *model.TeamAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.TeamMember{}).Where("team_id = ? AND user_id = ? AND status = ?", teamID, userID, model.TeamMemberStatusActive).Updates(map[string]any{"role": role, "updated_at": updatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return appendTeamAudit(tx, audit)
	})
}

func (r *Repository) RemoveTeamMember(teamID string, userID string, updatedAt time.Time, audit *model.TeamAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.TeamMember{}).Where("team_id = ? AND user_id = ? AND status = ?", teamID, userID, model.TeamMemberStatusActive).Updates(map[string]any{"status": model.TeamMemberStatusRemoved, "updated_at": updatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return appendTeamAudit(tx, audit)
	})
}

func (r *Repository) CreateTeam(team *model.Team, owner *model.TeamMember, audit *model.TeamAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(team).Error; err != nil {
			return err
		}
		if err := tx.Create(owner).Error; err != nil {
			return err
		}
		return appendTeamAudit(tx, audit)
	})
}

func (r *Repository) TeamAuditEvents(teamID string, page int, pageSize int) (TeamAuditPage, error) {
	query := r.db.Model(&model.TeamAuditEvent{}).Where("team_id = ?", teamID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return TeamAuditPage{}, err
	}
	var events []TeamAuditRecord
	err := r.db.Table("team_audit_events").
		Select("team_audit_events.*, users.username AS actor_username, users.display_name AS actor_display_name").
		Joins("LEFT JOIN users ON users.id = team_audit_events.actor_user_id").
		Where("team_audit_events.team_id = ?", teamID).
		Order("team_audit_events.created_at DESC, team_audit_events.id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Scan(&events).Error
	return TeamAuditPage{Events: events, Page: page, PageSize: pageSize, Total: total}, err
}

func (r *Repository) TeamAssetsPage(teamID string, filter TeamAssetFilter) ([]model.TeamAsset, int64, error) {
	query := r.db.Model(&model.TeamAsset{}).Where("team_id = ?", teamID)
	if value := strings.TrimSpace(filter.Query); value != "" {
		query = query.Where("LOWER(title) LIKE ?", "%"+strings.ToLower(value)+"%")
	}
	if value := strings.TrimSpace(filter.Kind); value != "" {
		query = query.Where("kind = ?", value)
	}
	if filter.FolderID != nil {
		query = query.Where("folder_id = ?", strings.TrimSpace(*filter.FolderID))
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var assets []model.TeamAsset
	err := query.Order("updated_at DESC, id DESC").Offset((filter.Page - 1) * filter.PageSize).Limit(filter.PageSize).Find(&assets).Error
	return assets, total, err
}

func (r *Repository) TeamAsset(teamID string, id string) (*model.TeamAsset, error) {
	var asset model.TeamAsset
	if err := r.db.First(&asset, "team_id = ? AND id = ?", teamID, id).Error; err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *Repository) TeamAssetBySource(teamID string, sourceAssetID string) (*model.TeamAsset, error) {
	var asset model.TeamAsset
	if err := r.db.First(&asset, "team_id = ? AND source_asset_id = ?", teamID, sourceAssetID).Error; err != nil {
		return nil, err
	}
	return &asset, nil
}

func (r *Repository) TeamAssetFolder(teamID string, id string) (*model.TeamAssetFolder, error) {
	var folder model.TeamAssetFolder
	if err := r.db.First(&folder, "team_id = ? AND id = ?", teamID, id).Error; err != nil {
		return nil, err
	}
	return &folder, nil
}

func (r *Repository) TeamAssetResourceIDs(teamAssetID string) ([]string, error) {
	var resourceIDs []string
	err := r.db.Model(&model.TeamAssetResource{}).Where("team_asset_id = ?", teamAssetID).Order("resource_id ASC").Pluck("resource_id", &resourceIDs).Error
	return resourceIDs, err
}

func (r *Repository) CreateImportedTeamAssets(assets []model.Asset, audit *model.TeamAuditEvent) error {
	if len(assets) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&assets).Error; err != nil {
			return err
		}
		return appendTeamAudit(tx, audit)
	})
}

func (r *Repository) SaveTeamAsset(asset *model.TeamAsset, resourceIDs []string) error {
	return r.SaveTeamAssets(asset.TeamID, []TeamAssetSave{{Asset: asset, ResourceIDs: resourceIDs}}, nil)
}

func (r *Repository) SaveTeamAssets(teamID string, saves []TeamAssetSave, audit *model.TeamAuditEvent) error {
	if len(saves) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		var team model.Team
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&team, "id = ?", teamID).Error; err != nil {
			return err
		}
		for _, save := range saves {
			if save.Asset == nil || save.Asset.TeamID != teamID {
				return errors.New("team asset batch contains an invalid team")
			}
			asset := save.Asset
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "team_id"}, {Name: "source_asset_id"}}, DoUpdates: clause.AssignmentColumns([]string{"owner_user_id", "folder_id", "kind", "category", "status", "title", "payload_json", "updated_at"})}).Create(asset).Error; err != nil {
				return err
			}
			var stored model.TeamAsset
			if err := tx.First(&stored, "team_id = ? AND source_asset_id = ?", teamID, asset.SourceAssetID).Error; err != nil {
				return err
			}
			asset.ID, asset.CreatedAt = stored.ID, stored.CreatedAt
			if err := tx.Where("team_asset_id = ?", stored.ID).Delete(&model.TeamAssetResource{}).Error; err != nil {
				return err
			}
			seen := make(map[string]struct{}, len(save.ResourceIDs))
			links := make([]model.TeamAssetResource, 0, len(save.ResourceIDs))
			for _, resourceID := range save.ResourceIDs {
				resourceID = strings.TrimSpace(resourceID)
				if resourceID == "" {
					continue
				}
				if _, exists := seen[resourceID]; exists {
					continue
				}
				seen[resourceID] = struct{}{}
				links = append(links, model.TeamAssetResource{TeamAssetID: stored.ID, ResourceID: resourceID, CreatedAt: asset.UpdatedAt})
			}
			if len(links) > 0 {
				if err := tx.Create(&links).Error; err != nil {
					return err
				}
			}
		}
		usage, err := teamUsage(tx, teamID)
		if err != nil {
			return err
		}
		if usage.AssetCount > team.AssetLimit {
			return ErrTeamAssetQuotaExceeded
		}
		if usage.StorageBytes > team.StorageLimit {
			return ErrTeamStorageQuotaExceeded
		}
		return appendTeamAudit(tx, audit)
	})
}

func teamUsage(db *gorm.DB, teamID string) (TeamUsage, error) {
	var usage TeamUsage
	query := `SELECT
		(SELECT COUNT(*) FROM team_members WHERE team_id = ? AND status = ?) AS member_count,
		(SELECT COUNT(*) FROM team_assets WHERE team_id = ?) AS asset_count,
		(SELECT COALESCE(SUM(resources.size), 0) FROM resources WHERE resources.id IN (
			SELECT DISTINCT team_asset_resources.resource_id FROM team_asset_resources
			JOIN team_assets ON team_assets.id = team_asset_resources.team_asset_id
			WHERE team_assets.team_id = ?
		)) AS storage_bytes`
	err := db.Raw(query, teamID, model.TeamMemberStatusActive, teamID, teamID).Scan(&usage).Error
	return usage, err
}

func (r *Repository) TeamAssetFolders(teamID string) ([]model.TeamAssetFolder, error) {
	var folders []model.TeamAssetFolder
	err := r.db.Where("team_id = ?", teamID).Order("name_key ASC, id ASC").Find(&folders).Error
	return folders, err
}

func (r *Repository) SaveTeamAssetFolder(folder *model.TeamAssetFolder, audit *model.TeamAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.TeamAssetFolder{}).Where("team_id = ? AND id = ?", folder.TeamID, folder.ID).Updates(map[string]any{"name": folder.Name, "name_key": folder.NameKey, "updated_at": folder.UpdatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			if err := tx.Create(folder).Error; err != nil {
				return err
			}
		}
		return appendTeamAudit(tx, audit)
	})
}

func (r *Repository) TeamAssetsInFolder(teamID string, folderID string) ([]model.TeamAsset, error) {
	var assets []model.TeamAsset
	err := r.db.Where("team_id = ? AND folder_id = ?", teamID, folderID).Find(&assets).Error
	return assets, err
}

func (r *Repository) DeleteTeamAssetFolder(teamID string, folderID string, assets []model.TeamAsset, audit *model.TeamAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		for _, asset := range assets {
			result := tx.Model(&model.TeamAsset{}).Where("team_id = ? AND id = ? AND folder_id = ?", teamID, asset.ID, folderID).Updates(map[string]any{"folder_id": "", "payload_json": asset.PayloadJSON, "updated_at": asset.UpdatedAt})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
		}
		result := tx.Delete(&model.TeamAssetFolder{}, "team_id = ? AND id = ?", teamID, folderID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return appendTeamAudit(tx, audit)
	})
}

func (r *Repository) MoveTeamAsset(teamID string, id string, folderID string, payloadJSON string, updatedAt time.Time, audit *model.TeamAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.TeamAsset{}).Where("team_id = ? AND id = ?", teamID, id).Updates(map[string]any{"folder_id": folderID, "payload_json": payloadJSON, "updated_at": updatedAt})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
		return appendTeamAudit(tx, audit)
	})
}

func (r *Repository) DeleteTeamAsset(teamID string, id string, audit *model.TeamAuditEvent) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var asset model.TeamAsset
		if err := tx.First(&asset, "team_id = ? AND id = ?", teamID, id).Error; err != nil {
			return err
		}
		if err := tx.Where("team_asset_id = ?", id).Delete(&model.TeamAssetResource{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&asset).Error; err != nil {
			return err
		}
		return appendTeamAudit(tx, audit)
	})
}

func (r *Repository) AppendTeamAudit(event *model.TeamAuditEvent) error {
	return appendTeamAudit(r.db, event)
}

func appendTeamAudit(db *gorm.DB, event *model.TeamAuditEvent) error {
	if event == nil {
		return nil
	}
	return db.Create(event).Error
}

func (r *Repository) TeamSharedResourceForUser(userID string, resourceID string) (*model.Resource, error) {
	var resource model.Resource
	err := r.db.Table("resources").Joins("JOIN team_asset_resources ON team_asset_resources.resource_id = resources.id").Joins("JOIN team_assets ON team_assets.id = team_asset_resources.team_asset_id").Joins("JOIN team_members ON team_members.team_id = team_assets.team_id").Where("resources.id = ? AND team_members.user_id = ? AND team_members.status = ?", resourceID, userID, model.TeamMemberStatusActive).First(&resource).Error
	if err != nil {
		return nil, err
	}
	return &resource, nil
}
