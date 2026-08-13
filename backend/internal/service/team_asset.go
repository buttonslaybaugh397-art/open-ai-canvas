package service

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"infinite-canvas/backend/internal/model"
)

type TeamAssetOwner struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type TeamAssetItem struct {
	Asset     json.RawMessage `json:"asset"`
	Owner     TeamAssetOwner  `json:"owner"`
	CanEdit   bool            `json:"canEdit"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type TeamAssetFolderItem struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Owner     TeamAssetOwner `json:"owner"`
	CanEdit   bool           `json:"canEdit"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

func (s *Service) TeamAssets(actor *model.User) ([]TeamAssetItem, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	assets, err := s.repo.TeamAssets()
	if err != nil {
		return nil, err
	}
	users, err := s.repo.Users()
	if err != nil {
		return nil, err
	}
	owners := make(map[string]TeamAssetOwner, len(users))
	for _, user := range users {
		owners[user.ID] = TeamAssetOwner{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName}
	}
	result := make([]TeamAssetItem, 0, len(assets))
	for _, asset := range assets {
		result = append(result, teamAssetItem(asset, owners[asset.OwnerUserID], actor))
	}
	return result, nil
}

func (s *Service) TeamAsset(actor *model.User, id string) (*TeamAssetItem, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	asset, err := s.repo.TeamAsset(strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	owner, err := s.repo.User(asset.OwnerUserID)
	if err != nil {
		return nil, err
	}
	item := teamAssetItem(*asset, TeamAssetOwner{ID: owner.ID, Username: owner.Username, DisplayName: owner.DisplayName}, actor)
	return &item, nil
}

func (s *Service) UpsertTeamAsset(actor *model.User, raw json.RawMessage) (*TeamAssetItem, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	parsed, err := assetFromJSON(actor.ID, raw)
	if err != nil {
		return nil, err
	}
	if err := validateTeamAsset(parsed); err != nil {
		return nil, err
	}
	existing, existingErr := s.repo.TeamAsset(parsed.ID)
	if existingErr != nil && !errors.Is(existingErr, gorm.ErrRecordNotFound) {
		return nil, existingErr
	}
	ownerUserID := actor.ID
	createdAt := time.Now()
	if existing != nil {
		if existing.OwnerUserID != actor.ID && actor.Role != model.UserRoleAdmin {
			return nil, Forbidden("只能修改自己上传的团队素材")
		}
		ownerUserID = existing.OwnerUserID
		createdAt = existing.CreatedAt
	}
	folderID, folderProvided, err := teamAssetPayloadFolderID(raw)
	if err != nil {
		return nil, err
	}
	if existing != nil && !folderProvided {
		folderID = existing.FolderID
	}
	if folderID != "" {
		if _, err := s.repo.TeamAssetFolder(folderID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, BadAuthRequest("团队素材文件夹不存在")
			}
			return nil, err
		}
	}
	now := time.Now()
	normalizedRaw, err := normalizeTeamAssetPayload(raw, parsed.ID, folderID, createdAt, now)
	if err != nil {
		return nil, err
	}
	if err := validateTeamAssetPayloadStorage(normalizedRaw); err != nil {
		return nil, err
	}
	parsed, err = assetFromJSON(ownerUserID, normalizedRaw)
	if err != nil {
		return nil, err
	}
	resourceIDs, err := teamAssetResourceIDs(normalizedRaw)
	if err != nil {
		return nil, err
	}
	existingResourceIDs := make(map[string]struct{})
	if existing != nil && existing.OwnerUserID != actor.ID && actor.Role == model.UserRoleAdmin {
		linkedResourceIDs, linkErr := s.repo.TeamAssetResourceIDs(existing.ID)
		if linkErr != nil {
			return nil, linkErr
		}
		for _, resourceID := range linkedResourceIDs {
			existingResourceIDs[resourceID] = struct{}{}
		}
	}
	for _, resourceID := range resourceIDs {
		resource, ownerErr := s.repo.ResourceForUser(actor.ID, resourceID)
		if ownerErr == nil {
			if resource.Status != model.ResourceStatusReady {
				return nil, BadAuthRequest("共享资源尚未上传完成")
			}
			continue
		}
		if _, alreadyLinked := existingResourceIDs[resourceID]; alreadyLinked {
			continue
		}
		return nil, Forbidden("团队素材只能引用当前账号已上传的资源")
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return nil, err
	}
	s.storageMu.Lock()
	defer s.storageMu.Unlock()
	usage, err := s.repo.UserStorageUsage(ownerUserID)
	if err != nil {
		return nil, err
	}
	teamCount, teamBytes, err := s.repo.TeamAssetStorageUsage(ownerUserID)
	if err != nil {
		return nil, err
	}
	usage.AssetCount += teamCount
	usage.AssetBytes += teamBytes
	existingBytes := int64(0)
	if existing != nil {
		existingBytes = int64(len([]byte(existing.PayloadJSON)))
	}
	if err := validateStructuredStorageQuotaWithPolicy(usage, "asset", existing == nil, int64(len(normalizedRaw))-existingBytes, policy.Resource); err != nil {
		return nil, err
	}
	teamAsset := model.TeamAsset{ID: parsed.ID, OwnerUserID: ownerUserID, FolderID: folderID, Kind: parsed.Kind, Category: parsed.Category, Status: parsed.Status, Title: parsed.Title, PayloadJSON: string(normalizedRaw), CreatedAt: createdAt, UpdatedAt: now}
	if err := s.repo.UpsertTeamAsset(&teamAsset, resourceIDs); err != nil {
		return nil, err
	}
	if existing == nil {
		s.recordActivity(ownerUserID, "asset", 1)
	}
	owner, err := s.repo.User(ownerUserID)
	if err != nil {
		return nil, err
	}
	item := teamAssetItem(teamAsset, TeamAssetOwner{ID: owner.ID, Username: owner.Username, DisplayName: owner.DisplayName}, actor)
	return &item, nil
}

func (s *Service) TeamAssetFolders(actor *model.User) ([]TeamAssetFolderItem, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	folders, err := s.repo.TeamAssetFolders()
	if err != nil {
		return nil, err
	}
	users, err := s.repo.Users()
	if err != nil {
		return nil, err
	}
	owners := make(map[string]TeamAssetOwner, len(users))
	for _, user := range users {
		owners[user.ID] = TeamAssetOwner{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName}
	}
	result := make([]TeamAssetFolderItem, 0, len(folders))
	for _, folder := range folders {
		result = append(result, teamAssetFolderItem(folder, owners[folder.OwnerUserID], actor))
	}
	return result, nil
}

func (s *Service) CreateTeamAssetFolder(actor *model.User, name string) (*TeamAssetFolderItem, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	normalized, err := normalizeTeamAssetFolderName(name)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	folder := model.TeamAssetFolder{ID: newID(), OwnerUserID: actor.ID, Name: normalized, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.SaveTeamAssetFolder(&folder); err != nil {
		return nil, err
	}
	item := teamAssetFolderItem(folder, TeamAssetOwner{ID: actor.ID, Username: actor.Username, DisplayName: actor.DisplayName}, actor)
	return &item, nil
}

func (s *Service) RenameTeamAssetFolder(actor *model.User, id string, name string) (*TeamAssetFolderItem, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	folder, err := s.repo.TeamAssetFolder(strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if folder.OwnerUserID != actor.ID && actor.Role != model.UserRoleAdmin {
		return nil, Forbidden("只能修改自己创建的团队文件夹")
	}
	folder.Name, err = normalizeTeamAssetFolderName(name)
	if err != nil {
		return nil, err
	}
	folder.UpdatedAt = time.Now()
	if err := s.repo.SaveTeamAssetFolder(folder); err != nil {
		return nil, err
	}
	owner, err := s.repo.User(folder.OwnerUserID)
	if err != nil {
		return nil, err
	}
	item := teamAssetFolderItem(*folder, TeamAssetOwner{ID: owner.ID, Username: owner.Username, DisplayName: owner.DisplayName}, actor)
	return &item, nil
}

func (s *Service) DeleteTeamAssetFolder(actor *model.User, id string) error {
	if actor == nil {
		return Unauthorized("请先登录")
	}
	folder, err := s.repo.TeamAssetFolder(strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if folder.OwnerUserID != actor.ID && actor.Role != model.UserRoleAdmin {
		return Forbidden("只能删除自己创建的团队文件夹")
	}
	assets, err := s.repo.TeamAssetsInFolder(folder.ID)
	if err != nil {
		return err
	}
	now := time.Now()
	for index := range assets {
		payload, payloadErr := setTeamAssetPayloadFolderID(json.RawMessage(assets[index].PayloadJSON), "", now)
		if payloadErr != nil {
			return payloadErr
		}
		assets[index].FolderID = ""
		assets[index].PayloadJSON = string(payload)
		assets[index].UpdatedAt = now
	}
	return s.repo.DeleteTeamAssetFolder(folder.ID, assets)
}

func (s *Service) MoveTeamAsset(actor *model.User, id string, folderID string) (*TeamAssetItem, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	asset, err := s.repo.TeamAsset(strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if asset.OwnerUserID != actor.ID && actor.Role != model.UserRoleAdmin {
		return nil, Forbidden("只能移动自己上传的团队素材")
	}
	folderID = strings.TrimSpace(folderID)
	if folderID != "" {
		if _, err := s.repo.TeamAssetFolder(folderID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, BadAuthRequest("团队素材文件夹不存在")
			}
			return nil, err
		}
	}
	now := time.Now()
	payload, err := setTeamAssetPayloadFolderID(json.RawMessage(asset.PayloadJSON), folderID, now)
	if err != nil {
		return nil, err
	}
	if err := s.repo.MoveTeamAsset(asset.ID, folderID, string(payload), now); err != nil {
		return nil, err
	}
	asset.FolderID, asset.PayloadJSON, asset.UpdatedAt = folderID, string(payload), now
	owner, err := s.repo.User(asset.OwnerUserID)
	if err != nil {
		return nil, err
	}
	item := teamAssetItem(*asset, TeamAssetOwner{ID: owner.ID, Username: owner.Username, DisplayName: owner.DisplayName}, actor)
	return &item, nil
}

func (s *Service) DeleteTeamAsset(actor *model.User, id string) error {
	if actor == nil {
		return Unauthorized("请先登录")
	}
	asset, err := s.repo.TeamAsset(strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if asset.OwnerUserID != actor.ID && actor.Role != model.UserRoleAdmin {
		return Forbidden("只能删除自己上传的团队素材")
	}
	return s.repo.DeleteTeamAsset(asset.ID)
}

func teamAssetItem(asset model.TeamAsset, owner TeamAssetOwner, actor *model.User) TeamAssetItem {
	return TeamAssetItem{Asset: json.RawMessage(asset.PayloadJSON), Owner: owner, CanEdit: actor != nil && (actor.ID == asset.OwnerUserID || actor.Role == model.UserRoleAdmin), CreatedAt: asset.CreatedAt, UpdatedAt: asset.UpdatedAt}
}

func teamAssetFolderItem(folder model.TeamAssetFolder, owner TeamAssetOwner, actor *model.User) TeamAssetFolderItem {
	return TeamAssetFolderItem{ID: folder.ID, Name: folder.Name, Owner: owner, CanEdit: actor != nil && (actor.ID == folder.OwnerUserID || actor.Role == model.UserRoleAdmin), CreatedAt: folder.CreatedAt, UpdatedAt: folder.UpdatedAt}
}

func normalizeTeamAssetFolderName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		return "", BadAuthRequest("团队文件夹名称不能为空")
	}
	if len([]rune(name)) > 120 {
		return "", BadAuthRequest("团队文件夹名称不能超过 120 个字符")
	}
	return name, nil
}

func teamAssetPayloadFolderID(raw json.RawMessage) (string, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", false, BadAuthRequest("团队素材数据格式错误")
	}
	value, exists := payload["folderId"]
	if !exists || value == nil {
		return "", false, nil
	}
	folderID, ok := value.(string)
	if !ok {
		return "", false, BadAuthRequest("团队素材文件夹 ID 格式错误")
	}
	return strings.TrimSpace(folderID), true, nil
}

func validateTeamAsset(asset model.Asset) error {
	switch asset.Kind {
	case "text", "image", "video", "audio", "model":
	default:
		return BadAuthRequest("团队素材类型不受支持")
	}
	if strings.TrimSpace(asset.Title) == "" {
		return BadAuthRequest("团队素材标题不能为空")
	}
	return nil
}

func validateTeamAssetPayloadStorage(raw json.RawMessage) error {
	var payload struct {
		Kind     string `json:"kind"`
		CoverURL string `json:"coverUrl"`
		Data     struct {
			StorageKey string `json:"storageKey"`
			DataURL    string `json:"dataUrl"`
			URL        string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return BadAuthRequest("团队素材数据格式错误")
	}
	if payload.Kind == "text" {
		return nil
	}
	if !strings.HasPrefix(strings.TrimSpace(payload.Data.StorageKey), "resource:") {
		return BadAuthRequest("共享媒体必须先上传到后端资源存储")
	}
	for _, mediaURL := range []string{payload.CoverURL, payload.Data.DataURL, payload.Data.URL} {
		value := strings.ToLower(strings.TrimSpace(mediaURL))
		if strings.HasPrefix(value, "blob:") || strings.HasPrefix(value, "data:") {
			return BadAuthRequest("共享媒体不能使用浏览器临时地址")
		}
	}
	return nil
}

func normalizeTeamAssetPayload(raw json.RawMessage, id string, folderID string, createdAt time.Time, updatedAt time.Time) (json.RawMessage, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, BadAuthRequest("团队素材数据格式错误")
	}
	payload["id"] = id
	payload["folderId"] = folderID
	payload["createdAt"] = createdAt.UTC().Format(time.RFC3339Nano)
	payload["updatedAt"] = updatedAt.UTC().Format(time.RFC3339Nano)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func setTeamAssetPayloadFolderID(raw json.RawMessage, folderID string, updatedAt time.Time) (json.RawMessage, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, BadAuthRequest("团队素材数据格式错误")
	}
	payload["folderId"] = folderID
	payload["updatedAt"] = updatedAt.UTC().Format(time.RFC3339Nano)
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func teamAssetResourceIDs(raw json.RawMessage) ([]string, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, BadAuthRequest("团队素材数据格式错误")
	}
	ids := make(map[string]struct{})
	collectTeamAssetResourceIDs(payload, ids)
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	return result, nil
}

func collectTeamAssetResourceIDs(value any, ids map[string]struct{}) {
	switch item := value.(type) {
	case []any:
		for _, child := range item {
			collectTeamAssetResourceIDs(child, ids)
		}
	case map[string]any:
		for key, child := range item {
			if key == "storageKey" {
				if storageKey, ok := child.(string); ok && strings.HasPrefix(storageKey, "resource:") {
					if resourceID := strings.TrimSpace(strings.TrimPrefix(storageKey, "resource:")); resourceID != "" {
						ids[resourceID] = struct{}{}
					}
				}
			}
			collectTeamAssetResourceIDs(child, ids)
		}
	}
}
