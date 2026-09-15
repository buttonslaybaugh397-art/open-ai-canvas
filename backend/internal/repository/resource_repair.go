package repository

import (
	"errors"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrResourceReferenceRepairConflict = errors.New("resource reference repair snapshot changed")

type ResourceReferenceUpdate struct {
	ID                string
	PreviousJSON      string
	PreviousUpdatedAt time.Time
	PayloadJSON       string
}

// RepairResourceReferences commits only the supplied owned document snapshots.
// Resource rows are locked/rechecked, never updated or deleted by this operation.
func (r *Repository) RepairResourceReferences(userID, oldID string, old *model.Resource, replacement model.Resource, assetUpdates, canvasUpdates []ResourceReferenceUpdate) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var resources []model.Resource
		query := tx.Where("id IN ?", []string{oldID, replacement.ID}).Order("id asc")
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.Find(&resources).Error; err != nil {
			return err
		}
		expected := map[string]*model.Resource{oldID: old, replacement.ID: &replacement}
		for _, resource := range resources {
			snapshot := expected[resource.ID]
			if snapshot == nil || resource.UserID != userID || resource.UserID != snapshot.UserID || resource.Status != snapshot.Status {
				return ErrResourceReferenceRepairConflict
			}
			delete(expected, resource.ID)
		}
		for _, snapshot := range expected {
			if snapshot != nil {
				return ErrResourceReferenceRepairConflict
			}
		}

		for _, batch := range []struct {
			model   any
			updates []ResourceReferenceUpdate
		}{{&model.Asset{}, assetUpdates}, {&model.CanvasProject{}, canvasUpdates}} {
			for _, update := range batch.updates {
				result := tx.Model(batch.model).
					Where("id = ? AND user_id = ? AND payload_json = ? AND updated_at = ?", update.ID, userID, update.PreviousJSON, update.PreviousUpdatedAt).
					UpdateColumn("payload_json", update.PayloadJSON)
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return ErrResourceReferenceRepairConflict
				}
			}
		}
		return nil
	})
}
