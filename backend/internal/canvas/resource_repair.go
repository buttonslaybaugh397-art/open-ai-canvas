package canvas

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"infinite-canvas/backend/internal/assets"
	"infinite-canvas/backend/internal/kernel"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

func (s *Service) RepairResourceReferences(userID, oldID, replacementID string) error {
	userID = strings.TrimSpace(userID)
	oldID = strings.TrimSpace(oldID)
	replacementID = strings.TrimSpace(replacementID)
	if userID == "" {
		return kernel.Unauthorized("请先登录")
	}
	if oldID == "" || replacementID == "" || assets.ValidID(oldID) != oldID || assets.ValidID(replacementID) != replacementID || oldID == replacementID {
		return kernel.BadAuthRequest("原资源与替换资源 ID 必须有效且不同")
	}
	return s.host.WithStorageLock(func() error {
		replacement, err := s.repo.ResourceForUser(userID, replacementID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return kernel.NotFound("替换资源不存在或不属于当前用户")
		}
		if err != nil {
			return err
		}
		if replacement.Status != model.ResourceStatusReady {
			return kernel.NewAppError(409, "替换资源尚未就绪")
		}
		// Global lookup distinguishes a missing row from another account's resource.
		old, err := s.repo.Resource(oldID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if old != nil {
			if old.UserID != userID {
				return kernel.Forbidden("无权修复该资源的引用")
			}
			if old.Status != model.ResourceStatusFailed && old.Status != model.ResourceStatusDeleted {
				return kernel.NewAppError(409, "原资源仍可用或正在上传，不能替换引用")
			}
		}
		ownedAssets, err := s.repo.Assets(userID)
		if err != nil {
			return err
		}
		canvases, err := s.repo.CanvasProjects(userID)
		if err != nil {
			return err
		}
		var assetUpdates, canvasUpdates []repository.ResourceReferenceUpdate
		var assetBytes, canvasBytes int64
		appendUpdate := func(updates *[]repository.ResourceReferenceUpdate, bytes *int64, id, raw string, updatedAt time.Time) error {
			next, changed, err := assets.RebindDocumentResource(raw, oldID, replacementID)
			if err != nil {
				return kernel.BadAuthRequest("已有素材或画布数据无法解析，已停止修复引用")
			}
			if !changed {
				return nil
			}
			if err := ValidateSyncedPayload(json.RawMessage(next), "资源引用"); err != nil {
				return err
			}
			*updates = append(*updates, repository.ResourceReferenceUpdate{
				ID: id, PreviousJSON: raw, PreviousUpdatedAt: updatedAt, PayloadJSON: next,
			})
			*bytes += int64(len(next) - len(raw))
			return nil
		}
		for _, asset := range ownedAssets {
			if err := appendUpdate(&assetUpdates, &assetBytes, asset.ID, asset.PayloadJSON, asset.UpdatedAt); err != nil {
				return err
			}
		}
		for _, project := range canvases {
			if err := appendUpdate(&canvasUpdates, &canvasBytes, project.ID, project.PayloadJSON, project.UpdatedAt); err != nil {
				return err
			}
		}
		for _, quota := range []struct {
			kind  string
			bytes int64
		}{{"asset", assetBytes}, {"canvas", canvasBytes}} {
			if quota.bytes > 0 {
				if err := s.host.StructuredQuota(userID, quota.kind, false, quota.bytes); err != nil {
					return err
				}
			}
		}
		if err := s.repo.RepairResourceReferences(userID, oldID, old, *replacement, assetUpdates, canvasUpdates); err != nil {
			if errors.Is(err, repository.ErrResourceReferenceRepairConflict) {
				return kernel.WrapAppError(409, "资源或文档已被并发修改，请重新读取后重试", err)
			}
			return err
		}
		return nil
	})
}
