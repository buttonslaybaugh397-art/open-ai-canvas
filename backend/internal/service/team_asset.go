package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

var teamAuditClock struct {
	sync.Mutex
	last time.Time
}

type TeamItem struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Role        model.TeamMemberRole `json:"role"`
	CanEdit     bool                 `json:"canEdit"`
	CanManage   bool                 `json:"canManage"`
	CreatedAt   time.Time            `json:"createdAt"`
	UpdatedAt   time.Time            `json:"updatedAt"`
}

type TeamUsageItem struct {
	MemberCount       int64 `json:"memberCount"`
	AssetCount        int64 `json:"assetCount"`
	AssetLimit        int64 `json:"assetLimit"`
	StorageBytes      int64 `json:"storageBytes"`
	StorageLimitBytes int64 `json:"storageLimitBytes"`
}

type TeamDetailItem struct {
	Team  TeamItem      `json:"team"`
	Usage TeamUsageItem `json:"usage"`
}

const defaultTeamAssetLimit int64 = 5000
const defaultTeamStorageLimit int64 = 100 << 30
const maxTeamAssetLimit int64 = 1_000_000
const maxTeamStorageLimit int64 = 10 << 40

type TeamMemberItem struct {
	UserID      string               `json:"userId"`
	Username    string               `json:"username"`
	DisplayName string               `json:"displayName"`
	Role        model.TeamMemberRole `json:"role"`
	JoinedAt    time.Time            `json:"joinedAt"`
}

type TeamAssetOwner struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type TeamAssetItem struct {
	ID            string               `json:"id"`
	SourceAssetID string               `json:"sourceAssetId"`
	FolderID      string               `json:"folderId,omitempty"`
	Asset         json.RawMessage      `json:"asset"`
	Owner         TeamAssetOwner       `json:"owner"`
	CanEdit       bool                 `json:"canEdit"`
	CanDelete     bool                 `json:"canDelete"`
	MemberRole    model.TeamMemberRole `json:"memberRole"`
	CreatedAt     time.Time            `json:"createdAt"`
	UpdatedAt     time.Time            `json:"updatedAt"`
}

type TeamAssetPage struct {
	Assets   []TeamAssetItem `json:"assets"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
	Total    int64           `json:"total"`
}

type TeamAssetFolderItem struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CanEdit   bool      `json:"canEdit"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ImportedTeamAssetItem struct {
	TeamAssetID string          `json:"teamAssetId"`
	Asset       json.RawMessage `json:"asset"`
}

type TeamAuditItem struct {
	ID               string    `json:"id"`
	Action           string    `json:"action"`
	TargetType       string    `json:"targetType,omitempty"`
	TargetID         string    `json:"targetId,omitempty"`
	Summary          string    `json:"summary"`
	ActorUserID      string    `json:"actorUserId"`
	ActorUsername    string    `json:"actorUsername"`
	ActorDisplayName string    `json:"actorDisplayName"`
	CreatedAt        time.Time `json:"createdAt"`
}

type TeamAuditPage struct {
	Events   []TeamAuditItem `json:"events"`
	Page     int             `json:"page"`
	PageSize int             `json:"pageSize"`
	Total    int64           `json:"total"`
}

type TeamInvitationItem struct {
	ID        string               `json:"id"`
	TeamID    string               `json:"teamId"`
	Role      model.TeamMemberRole `json:"role"`
	ExpiresAt time.Time            `json:"expiresAt"`
	CreatedAt time.Time            `json:"createdAt"`
}

type TeamInvitationPreview struct {
	TeamName  string               `json:"teamName"`
	Role      model.TeamMemberRole `json:"role"`
	ExpiresAt time.Time            `json:"expiresAt"`
	Available bool                 `json:"available"`
}

func (s *Service) Teams(actor *model.User) ([]TeamItem, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	teams, err := s.repo.TeamsForUser(actor.ID)
	if err != nil {
		return nil, err
	}
	result := make([]TeamItem, 0, len(teams))
	for _, team := range teams {
		member, memberErr := s.repo.TeamMember(team.ID, actor.ID)
		if memberErr != nil {
			return nil, memberErr
		}
		result = append(result, teamItem(team, member.Role))
	}
	return result, nil
}

func (s *Service) CreateTeam(actor *model.User, name string) (*TeamItem, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 120 {
		return nil, BadAuthRequest("团队名称不能为空且不能超过 120 个字符")
	}
	now := time.Now().UTC()
	team := model.Team{ID: newID(), Name: name, AssetLimit: defaultTeamAssetLimit, StorageLimit: defaultTeamStorageLimit, CreatedByUserID: actor.ID, CreatedAt: now, UpdatedAt: now}
	owner := model.TeamMember{TeamID: team.ID, UserID: actor.ID, Role: model.TeamMemberRoleOwner, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now}
	if err := s.repo.CreateTeam(&team, &owner, newTeamAuditEvent(actor, team.ID, "team.created", "team", team.ID, "创建团队")); err != nil {
		return nil, err
	}
	item := teamItem(team, owner.Role)
	return &item, nil
}

func (s *Service) TeamDetail(actor *model.User, teamID string) (*TeamDetailItem, error) {
	member, err := s.requireTeamMember(actor, teamID)
	if err != nil {
		return nil, err
	}
	team, err := s.repo.Team(strings.TrimSpace(teamID))
	if err != nil {
		return nil, err
	}
	usage, err := s.repo.TeamUsage(team.ID)
	if err != nil {
		return nil, err
	}
	return &TeamDetailItem{
		Team:  teamItem(*team, member.Role),
		Usage: TeamUsageItem{MemberCount: usage.MemberCount, AssetCount: usage.AssetCount, AssetLimit: team.AssetLimit, StorageBytes: usage.StorageBytes, StorageLimitBytes: team.StorageLimit},
	}, nil
}

func (s *Service) UpdateTeamSettings(actor *model.User, teamID string, name string, description string, assetLimit int64, storageLimitBytes int64) (*TeamDetailItem, error) {
	member, err := s.requireTeamMember(actor, teamID)
	if err != nil {
		return nil, err
	}
	if member.Role != model.TeamMemberRoleOwner {
		return nil, Forbidden("只有团队所有者可以修改团队设置")
	}
	name, description = strings.TrimSpace(name), strings.TrimSpace(description)
	if name == "" || len([]rune(name)) > 120 {
		return nil, BadAuthRequest("团队名称不能为空且不能超过 120 个字符")
	}
	if len([]rune(description)) > 500 {
		return nil, BadAuthRequest("团队描述不能超过 500 个字符")
	}
	if assetLimit < 1 || assetLimit > maxTeamAssetLimit {
		return nil, BadAuthRequest("团队素材上限必须在 1 到 1000000 之间")
	}
	if storageLimitBytes < 1<<30 || storageLimitBytes > maxTeamStorageLimit {
		return nil, BadAuthRequest("团队存储上限必须在 1 GB 到 10 TB 之间")
	}
	usage, err := s.repo.TeamUsage(strings.TrimSpace(teamID))
	if err != nil {
		return nil, err
	}
	if assetLimit < usage.AssetCount {
		return nil, BadAuthRequest(fmt.Sprintf("素材上限不能低于当前使用量 %d", usage.AssetCount))
	}
	if storageLimitBytes < usage.StorageBytes {
		return nil, BadAuthRequest(fmt.Sprintf("存储上限不能低于当前使用量 %s", formatStorageLimit(usage.StorageBytes)))
	}
	now := time.Now().UTC()
	if err := s.repo.UpdateTeamSettings(strings.TrimSpace(teamID), name, description, assetLimit, storageLimitBytes, now, newTeamAuditEvent(actor, strings.TrimSpace(teamID), "team.settings_updated", "team", strings.TrimSpace(teamID), "更新团队设置与配额")); err != nil {
		if errors.Is(err, repository.ErrTeamAssetQuotaExceeded) {
			return nil, BadAuthRequest(fmt.Sprintf("素材上限不能低于当前使用量 %d", usage.AssetCount))
		}
		if errors.Is(err, repository.ErrTeamStorageQuotaExceeded) {
			return nil, BadAuthRequest(fmt.Sprintf("存储上限不能低于当前使用量 %s", formatStorageLimit(usage.StorageBytes)))
		}
		return nil, err
	}
	return s.TeamDetail(actor, teamID)
}

func (s *Service) TeamAuditEvents(actor *model.User, teamID string, page int, pageSize int) (TeamAuditPage, error) {
	member, err := s.requireTeamManager(actor, teamID)
	if err != nil {
		return TeamAuditPage{}, err
	}
	if !teamRoleCanManage(member.Role) {
		return TeamAuditPage{}, Forbidden("当前团队角色无权查看审计记录")
	}
	page, pageSize = normalizeTeamAssetPage(page, pageSize)
	records, err := s.repo.TeamAuditEvents(strings.TrimSpace(teamID), page, pageSize)
	if err != nil {
		return TeamAuditPage{}, err
	}
	result := TeamAuditPage{Events: make([]TeamAuditItem, 0, len(records.Events)), Page: records.Page, PageSize: records.PageSize, Total: records.Total}
	for _, event := range records.Events {
		result.Events = append(result.Events, TeamAuditItem{ID: event.ID, Action: event.Action, TargetType: event.TargetType, TargetID: event.TargetID, Summary: event.Summary, ActorUserID: event.ActorUserID, ActorUsername: event.ActorUsername, ActorDisplayName: event.ActorDisplayName, CreatedAt: event.CreatedAt})
	}
	return result, nil
}

func (s *Service) TeamMembers(actor *model.User, teamID string) ([]TeamMemberItem, error) {
	if _, err := s.requireTeamMember(actor, teamID); err != nil {
		return nil, err
	}
	members, err := s.repo.TeamMembers(strings.TrimSpace(teamID))
	if err != nil {
		return nil, err
	}
	result := make([]TeamMemberItem, 0, len(members))
	for _, member := range members {
		result = append(result, teamMemberItem(member))
	}
	return result, nil
}

func (s *Service) TeamInvitations(actor *model.User, teamID string) ([]TeamInvitationItem, error) {
	if _, err := s.requireTeamManager(actor, teamID); err != nil {
		return nil, err
	}
	records, err := s.repo.TeamInvitations(strings.TrimSpace(teamID))
	if err != nil {
		return nil, err
	}
	items := make([]TeamInvitationItem, 0, len(records))
	for _, record := range records {
		items = append(items, TeamInvitationItem{ID: record.ID, TeamID: record.TeamID, Role: record.Role, ExpiresAt: record.ExpiresAt, CreatedAt: record.CreatedAt})
	}
	return items, nil
}

func (s *Service) CreateTeamInvitation(actor *model.User, teamID string, role model.TeamMemberRole, validHours int) (*TeamInvitationItem, string, error) {
	manager, err := s.requireTeamManager(actor, teamID)
	if err != nil {
		return nil, "", err
	}
	if role != model.TeamMemberRoleAdmin && role != model.TeamMemberRoleEditor && role != model.TeamMemberRoleViewer {
		return nil, "", BadAuthRequest("邀请角色无效")
	}
	if manager.Role != model.TeamMemberRoleOwner && role == model.TeamMemberRoleAdmin {
		return nil, "", Forbidden("管理员不能邀请其他管理员")
	}
	if validHours != 24 && validHours != 72 && validHours != 168 && validHours != 720 {
		return nil, "", BadAuthRequest("邀请有效期无效")
	}
	var tokenBytes [32]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return nil, "", fmt.Errorf("生成邀请令牌：%w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes[:])
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	invitation := model.TeamInvitation{ID: newID(), TeamID: strings.TrimSpace(teamID), Role: role, TokenHash: fmt.Sprintf("%x", hash[:]), CreatedByUserID: actor.ID, ExpiresAt: now.Add(time.Duration(validHours) * time.Hour), CreatedAt: now}
	if err := s.repo.CreateTeamInvitation(&invitation, newTeamAuditEvent(actor, invitation.TeamID, "invitation.created", "invitation", invitation.ID, fmt.Sprintf("创建%s邀请", teamRoleLabel(role)))); err != nil {
		return nil, "", err
	}
	item := &TeamInvitationItem{ID: invitation.ID, TeamID: invitation.TeamID, Role: invitation.Role, ExpiresAt: invitation.ExpiresAt, CreatedAt: invitation.CreatedAt}
	return item, token, nil
}

func (s *Service) RevokeTeamInvitation(actor *model.User, teamID string, invitationID string) error {
	if _, err := s.requireTeamManager(actor, teamID); err != nil {
		return err
	}
	invitationID = strings.TrimSpace(invitationID)
	if invitationID == "" {
		return BadAuthRequest("邀请 ID 不能为空")
	}
	err := s.repo.RevokeTeamInvitation(strings.TrimSpace(teamID), invitationID, time.Now().UTC(), newTeamAuditEvent(actor, strings.TrimSpace(teamID), "invitation.revoked", "invitation", invitationID, "撤销团队邀请"))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NotFound("邀请不存在或已失效")
	}
	return err
}

func (s *Service) TeamInvitationPreview(token string) (*TeamInvitationPreview, error) {
	tokenHash, err := invitationTokenHash(token)
	if err != nil {
		return nil, err
	}
	invitation, err := s.repo.TeamInvitationByHash(tokenHash)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NotFound("邀请链接无效")
	}
	if err != nil {
		return nil, err
	}
	team, err := s.repo.Team(invitation.TeamID)
	if err != nil {
		return nil, err
	}
	available := invitation.RevokedAt == nil && invitation.ConsumedAt == nil && invitation.ExpiresAt.After(time.Now().UTC())
	return &TeamInvitationPreview{TeamName: team.Name, Role: invitation.Role, ExpiresAt: invitation.ExpiresAt, Available: available}, nil
}

func (s *Service) AcceptTeamInvitation(actor *model.User, token string) (*TeamItem, error) {
	if actor == nil || actor.Status != model.UserStatusActive {
		return nil, Forbidden("当前账号不可加入团队")
	}
	tokenHash, err := invitationTokenHash(token)
	if err != nil {
		return nil, err
	}
	preview, err := s.repo.TeamInvitationByHash(tokenHash)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NotFound("邀请链接无效")
	}
	if err != nil {
		return nil, err
	}
	audit := newTeamAuditEvent(actor, preview.TeamID, "invitation.accepted", "invitation", preview.ID, "通过邀请加入团队")
	accepted, err := s.repo.AcceptTeamInvitation(tokenHash, actor.ID, time.Now().UTC(), audit)
	if errors.Is(err, repository.ErrTeamInvitationUnavailable) {
		return nil, BadAuthRequest("邀请已过期、已撤销或已被使用")
	}
	if errors.Is(err, repository.ErrTeamInvitationAlreadyMember) {
		return nil, BadAuthRequest("你已经是该团队成员")
	}
	if err != nil {
		return nil, err
	}
	team, err := s.repo.Team(accepted.TeamID)
	if err != nil {
		return nil, err
	}
	item := teamItem(*team, accepted.Role)
	return &item, nil
}

func invitationTokenHash(token string) (string, error) {
	token = strings.TrimSpace(token)
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(decoded) != 32 {
		return "", BadAuthRequest("邀请链接无效")
	}
	hash := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", hash[:]), nil
}

func (s *Service) AddTeamMember(actor *model.User, teamID string, username string, role model.TeamMemberRole) (*TeamMemberItem, error) {
	manager, err := s.requireTeamManager(actor, teamID)
	if err != nil {
		return nil, err
	}
	if err := validateAssignableTeamRole(manager.Role, role); err != nil {
		return nil, err
	}
	username = strings.TrimSpace(username)
	if username == "" || len([]rune(username)) > 80 {
		return nil, BadAuthRequest("用户名不能为空且不能超过 80 个字符")
	}
	target, err := s.repo.UserByUsername(username)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NotFound("未找到该用户名对应的账号")
	}
	if err != nil {
		return nil, err
	}
	if target.Status != model.UserStatusActive {
		return nil, BadAuthRequest("该账号当前不可加入团队")
	}
	if target.ID == actor.ID {
		return nil, BadAuthRequest("不能通过添加成员修改自己的团队角色")
	}
	existing, memberErr := s.repo.TeamMemberRecord(strings.TrimSpace(teamID), target.ID)
	if memberErr == nil {
		if existing.Status == model.TeamMemberStatusActive {
			return nil, BadAuthRequest("该用户已经是团队成员")
		}
	} else if !errors.Is(memberErr, gorm.ErrRecordNotFound) {
		return nil, memberErr
	}
	now := time.Now().UTC()
	member := model.TeamMember{TeamID: strings.TrimSpace(teamID), UserID: target.ID, Role: role, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now}
	if existing != nil {
		member.CreatedAt = existing.CreatedAt
	}
	if err := s.repo.SaveTeamMember(&member, newTeamAuditEvent(actor, member.TeamID, "member.added", "member", target.ID, fmt.Sprintf("添加成员为%s", teamRoleLabel(role)))); err != nil {
		return nil, err
	}
	item := TeamMemberItem{UserID: target.ID, Username: target.Username, DisplayName: target.DisplayName, Role: role, JoinedAt: member.CreatedAt}
	return &item, nil
}

func (s *Service) UpdateTeamMemberRole(actor *model.User, teamID string, userID string, role model.TeamMemberRole) (*TeamMemberItem, error) {
	manager, err := s.requireTeamManager(actor, teamID)
	if err != nil {
		return nil, err
	}
	if err := validateAssignableTeamRole(manager.Role, role); err != nil {
		return nil, err
	}
	teamID, userID = strings.TrimSpace(teamID), strings.TrimSpace(userID)
	if userID == "" {
		return nil, BadAuthRequest("成员 ID 不能为空")
	}
	targetMember, err := s.repo.TeamMember(teamID, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NotFound("团队成员不存在")
	}
	if err != nil {
		return nil, err
	}
	if targetMember.Role == model.TeamMemberRoleOwner {
		return nil, Forbidden("不能修改团队所有者的角色")
	}
	if targetMember.UserID == actor.ID {
		return nil, Forbidden("不能修改自己的团队角色")
	}
	if manager.Role == model.TeamMemberRoleAdmin && targetMember.Role == model.TeamMemberRoleAdmin {
		return nil, Forbidden("管理员不能管理其他管理员")
	}
	if err := s.repo.UpdateTeamMemberRole(teamID, userID, role, time.Now().UTC(), newTeamAuditEvent(actor, teamID, "member.role_updated", "member", userID, fmt.Sprintf("将成员角色调整为%s", teamRoleLabel(role)))); err != nil {
		return nil, err
	}
	user, err := s.repo.User(userID)
	if err != nil {
		return nil, err
	}
	item := TeamMemberItem{UserID: user.ID, Username: user.Username, DisplayName: user.DisplayName, Role: role, JoinedAt: targetMember.CreatedAt}
	return &item, nil
}

func (s *Service) RemoveTeamMember(actor *model.User, teamID string, userID string) error {
	actorMember, err := s.requireTeamMember(actor, teamID)
	if err != nil {
		return err
	}
	teamID, userID = strings.TrimSpace(teamID), strings.TrimSpace(userID)
	targetMember, err := s.repo.TeamMember(teamID, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NotFound("团队成员不存在")
	}
	if err != nil {
		return err
	}
	if targetMember.Role == model.TeamMemberRoleOwner {
		return Forbidden("团队所有者不能退出或被移除")
	}
	if targetMember.UserID != actor.ID {
		if !teamRoleCanManage(actorMember.Role) {
			return Forbidden("当前团队角色没有管理权限")
		}
		if actorMember.Role == model.TeamMemberRoleAdmin && targetMember.Role == model.TeamMemberRoleAdmin {
			return Forbidden("管理员不能移除其他管理员")
		}
	}
	action, summary := "member.removed", "移除团队成员"
	if userID == actor.ID {
		action, summary = "member.left", "退出团队"
	}
	return s.repo.RemoveTeamMember(teamID, userID, time.Now().UTC(), newTeamAuditEvent(actor, teamID, action, "member", userID, summary))
}

func (s *Service) TeamAssets(actor *model.User, teamID string, filter repository.TeamAssetFilter) (TeamAssetPage, error) {
	member, err := s.requireTeamMember(actor, teamID)
	if err != nil {
		return TeamAssetPage{}, err
	}
	filter.Page, filter.PageSize = normalizeTeamAssetPage(filter.Page, filter.PageSize)
	if len([]rune(strings.TrimSpace(filter.Query))) > 120 {
		return TeamAssetPage{}, BadAuthRequest("搜索关键词不能超过 120 个字符")
	}
	if filter.Kind != "" && !validTeamAssetKind(filter.Kind) {
		return TeamAssetPage{}, BadAuthRequest("团队素材类型无效")
	}
	assets, total, err := s.repo.TeamAssetsPage(strings.TrimSpace(teamID), filter)
	if err != nil {
		return TeamAssetPage{}, err
	}
	items := make([]TeamAssetItem, 0, len(assets))
	for _, asset := range assets {
		item, itemErr := s.teamAssetItem(asset, actor, member.Role)
		if itemErr != nil {
			return TeamAssetPage{}, itemErr
		}
		items = append(items, item)
	}
	return TeamAssetPage{Assets: items, Page: filter.Page, PageSize: filter.PageSize, Total: total}, nil
}

func (s *Service) ShareTeamAssets(actor *model.User, teamID string, assetIDs []string, folderID string) ([]TeamAssetItem, error) {
	member, err := s.requireTeamEditor(actor, teamID)
	if err != nil {
		return nil, err
	}
	if len(assetIDs) == 0 || len(assetIDs) > 50 {
		return nil, BadAuthRequest("请选择 1 到 50 个个人素材进行共享")
	}
	teamID, folderID = strings.TrimSpace(teamID), strings.TrimSpace(folderID)
	if folderID != "" {
		if _, err := s.repo.TeamAssetFolder(teamID, folderID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, NotFound("团队素材文件夹不存在")
			}
			return nil, err
		}
	}
	seen := make(map[string]struct{}, len(assetIDs))
	sharedAssets := make([]model.TeamAsset, 0, len(assetIDs))
	for _, sourceID := range assetIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" {
			return nil, BadAuthRequest("个人素材 ID 不能为空")
		}
		if _, exists := seen[sourceID]; exists {
			continue
		}
		seen[sourceID] = struct{}{}
		source, sourceErr := s.repo.AssetForUser(actor.ID, sourceID)
		if sourceErr != nil {
			if errors.Is(sourceErr, gorm.ErrRecordNotFound) {
				return nil, Forbidden("只能共享当前账号拥有的个人素材")
			}
			return nil, sourceErr
		}
		if err := validateTeamAsset(*source); err != nil {
			return nil, err
		}
		resourceIDs, resourceErr := teamAssetResourceIDs(json.RawMessage(source.PayloadJSON))
		if resourceErr != nil {
			return nil, resourceErr
		}
		for _, resourceID := range resourceIDs {
			resource, ownerErr := s.repo.ResourceForUser(actor.ID, resourceID)
			if ownerErr != nil {
				return nil, Forbidden("团队素材只能引用当前账号拥有的资源")
			}
			if resource.Status != model.ResourceStatusReady {
				return nil, BadAuthRequest("共享资源尚未上传完成")
			}
		}
		now, createdAt, id := time.Now().UTC(), time.Now().UTC(), newID()
		if existing, existingErr := s.repo.TeamAssetBySource(teamID, sourceID); existingErr == nil {
			id, createdAt = existing.ID, existing.CreatedAt
		} else if !errors.Is(existingErr, gorm.ErrRecordNotFound) {
			return nil, existingErr
		}
		payload, payloadErr := normalizeTeamAssetPayload(json.RawMessage(source.PayloadJSON), id, folderID, createdAt, now)
		if payloadErr != nil {
			return nil, payloadErr
		}
		if err := validateTeamAssetPayloadStorage(payload); err != nil {
			return nil, err
		}
		shared := model.TeamAsset{ID: id, TeamID: teamID, OwnerUserID: actor.ID, SourceAssetID: sourceID, FolderID: folderID, Kind: source.Kind, Category: source.Category, Status: source.Status, Title: source.Title, PayloadJSON: string(payload), CreatedAt: createdAt, UpdatedAt: now}
		sharedAssets = append(sharedAssets, shared)
	}
	saves := make([]repository.TeamAssetSave, 0, len(sharedAssets))
	for index := range sharedAssets {
		resourceIDs, err := teamAssetResourceIDs(json.RawMessage(sharedAssets[index].PayloadJSON))
		if err != nil {
			return nil, err
		}
		saves = append(saves, repository.TeamAssetSave{Asset: &sharedAssets[index], ResourceIDs: resourceIDs})
	}
	if err := s.repo.SaveTeamAssets(teamID, saves, newTeamAuditEvent(actor, teamID, "asset.shared", "asset_batch", "", fmt.Sprintf("共享或更新 %d 个团队素材", len(saves)))); err != nil {
		if errors.Is(err, repository.ErrTeamAssetQuotaExceeded) {
			return nil, BadAuthRequest("团队素材数量已达到上限，请清理素材或调整团队配额")
		}
		if errors.Is(err, repository.ErrTeamStorageQuotaExceeded) {
			return nil, BadAuthRequest("团队存储空间已达到上限，请清理素材或调整团队配额")
		}
		return nil, err
	}
	result := make([]TeamAssetItem, 0, len(sharedAssets))
	for _, shared := range sharedAssets {
		item, itemErr := s.teamAssetItem(shared, actor, member.Role)
		if itemErr != nil {
			return nil, itemErr
		}
		result = append(result, item)
	}
	return result, nil
}

// ImportTeamAssets materializes team snapshots and their media as independent
// personal assets. The copies remain usable after unsharing or membership loss.
func (s *Service) ImportTeamAssets(actor *model.User, teamID string, assetIDs []string) ([]ImportedTeamAssetItem, error) {
	if _, err := s.requireTeamMember(actor, teamID); err != nil {
		return nil, err
	}
	if len(assetIDs) == 0 || len(assetIDs) > 50 {
		return nil, BadAuthRequest("请选择 1 到 50 个团队素材进行复制")
	}
	teamID = strings.TrimSpace(teamID)
	seen := make(map[string]struct{}, len(assetIDs))
	sources := make([]model.TeamAsset, 0, len(assetIDs))
	resourceByID := make(map[string]*model.Resource)
	resourceOrder := make([]string, 0)
	for _, rawID := range assetIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			return nil, BadAuthRequest("团队素材 ID 不能为空")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		source, err := s.repo.TeamAsset(teamID, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, NotFound("团队素材不存在或已取消共享")
		}
		if err != nil {
			return nil, err
		}
		if err := validateTeamAssetPayloadStorage(json.RawMessage(source.PayloadJSON)); err != nil {
			return nil, err
		}
		resourceIDs, err := s.repo.TeamAssetResourceIDs(source.ID)
		if err != nil {
			return nil, err
		}
		for _, resourceID := range resourceIDs {
			if _, exists := resourceByID[resourceID]; exists {
				continue
			}
			resource, err := s.repo.TeamSharedResourceForUser(actor.ID, resourceID)
			if err != nil {
				return nil, Forbidden("团队素材引用的资源不可访问")
			}
			if resource.Status != model.ResourceStatusReady || resource.Size <= 0 {
				return nil, BadAuthRequest("团队素材引用的资源尚未就绪")
			}
			resourceByID[resourceID] = resource
			resourceOrder = append(resourceOrder, resourceID)
		}
		sources = append(sources, *source)
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		return nil, err
	}
	s.storageMu.Lock()
	usage, err := s.repo.UserStorageUsage(actor.ID)
	if err == nil {
		incomingBytes := int64(0)
		for _, source := range sources {
			incomingBytes += int64(len([]byte(source.PayloadJSON)))
		}
		if usage.AssetCount+int64(len(sources)) > policy.Resource.AssetCount {
			err = BadAuthRequest(fmt.Sprintf("账号素材数量不能超过 %d 个", policy.Resource.AssetCount))
		} else {
			err = validateStructuredStorageQuotaWithPolicy(usage, "asset", false, incomingBytes, policy.Resource)
		}
	}
	s.storageMu.Unlock()
	if err != nil {
		return nil, err
	}

	totalResourceBytes := int64(0)
	for _, resource := range resourceByID {
		totalResourceBytes += resource.Size
	}
	quotaDay := ""
	if totalResourceBytes > 0 {
		quotaDay, err = s.reserveUserStoredFileQuota(actor.ID, totalResourceBytes, totalResourceBytes+1, megabytes(policy.Resource.DailyUploadMB), gigabytes(policy.Resource.StoredFileGB), "团队素材副本超过单次复制限制")
		if err != nil {
			return nil, err
		}
	}
	quotaCommitted := false
	defer func() {
		if totalResourceBytes > 0 && !quotaCommitted {
			s.releaseUserUploadQuota(actor.ID, quotaDay, totalResourceBytes)
		}
	}()

	createdResources := make([]*model.Resource, 0, len(resourceOrder))
	resourceIDMap := make(map[string]string, len(resourceOrder))
	cleanupResources := func() error {
		var cleanupErr error
		for index := len(createdResources) - 1; index >= 0; index-- {
			resource := createdResources[index]
			if err := s.deleteStoredResourceObject(actor.ID, resource); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
				continue
			}
			if err := s.repo.DeleteResource(actor.ID, resource.ID); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
		}
		return cleanupErr
	}
	failAfterCopy := func(err error) ([]ImportedTeamAssetItem, error) {
		if cleanupErr := cleanupResources(); cleanupErr != nil {
			return nil, errors.Join(err, fmt.Errorf("清理团队素材副本失败：%w", cleanupErr))
		}
		return nil, err
	}
	for _, oldID := range resourceOrder {
		sourceResource := resourceByID[oldID]
		_, body, err := s.OpenResource(actor.ID, oldID)
		if err != nil {
			return failAfterCopy(err)
		}
		copied, _, copyErr := s.storeResource(actor.ID, sourceResource.Kind, path.Base(sourceResource.ObjectKey), sourceResource.MimeType, sourceResource.Size, sourceResource.Width, sourceResource.Height, sourceResource.DurationMs, io.LimitReader(body, sourceResource.Size+1), nil)
		closeErr := body.Close()
		if copyErr != nil {
			return failAfterCopy(copyErr)
		}
		if closeErr != nil {
			createdResources = append(createdResources, copied)
			return failAfterCopy(closeErr)
		}
		createdResources = append(createdResources, copied)
		resourceIDMap[oldID] = copied.ID
	}

	now := time.Now().UTC()
	assets := make([]model.Asset, 0, len(sources))
	result := make([]ImportedTeamAssetItem, 0, len(sources))
	for _, source := range sources {
		id := newID()
		payload, err := importedTeamAssetPayload(json.RawMessage(source.PayloadJSON), id, now, resourceIDMap)
		if err != nil {
			return failAfterCopy(err)
		}
		asset := model.Asset{ID: id, UserID: actor.ID, Kind: source.Kind, Category: source.Category, Status: source.Status, Title: source.Title, PayloadJSON: string(payload), CreatedAt: now, UpdatedAt: now}
		assets = append(assets, asset)
		result = append(result, ImportedTeamAssetItem{TeamAssetID: source.ID, Asset: payload})
	}
	s.storageMu.Lock()
	usage, usageErr := s.repo.UserStorageUsage(actor.ID)
	if usageErr == nil {
		incomingBytes := int64(0)
		for _, asset := range assets {
			incomingBytes += int64(len([]byte(asset.PayloadJSON)))
		}
		if usage.AssetCount+int64(len(assets)) > policy.Resource.AssetCount {
			usageErr = BadAuthRequest(fmt.Sprintf("账号素材数量不能超过 %d 个", policy.Resource.AssetCount))
		} else {
			usageErr = validateStructuredStorageQuotaWithPolicy(usage, "asset", false, incomingBytes, policy.Resource)
		}
	}
	if usageErr == nil {
		usageErr = s.repo.CreateImportedTeamAssets(assets, newTeamAuditEvent(actor, teamID, "asset.imported", "asset_batch", "", fmt.Sprintf("复制 %d 个团队素材到个人素材", len(assets))))
	}
	s.storageMu.Unlock()
	if usageErr != nil {
		return failAfterCopy(usageErr)
	}
	if totalResourceBytes > 0 {
		s.commitUserUploadQuota(actor.ID, totalResourceBytes)
	}
	quotaCommitted = true
	s.recordActivity(actor.ID, "asset", len(assets))
	return result, nil
}

func (s *Service) TeamAssetFolders(actor *model.User, teamID string) ([]TeamAssetFolderItem, error) {
	member, err := s.requireTeamMember(actor, teamID)
	if err != nil {
		return nil, err
	}
	folders, err := s.repo.TeamAssetFolders(strings.TrimSpace(teamID))
	if err != nil {
		return nil, err
	}
	result := make([]TeamAssetFolderItem, 0, len(folders))
	for _, folder := range folders {
		result = append(result, teamAssetFolderItem(folder, teamRoleCanEdit(member.Role)))
	}
	return result, nil
}

func (s *Service) CreateTeamAssetFolder(actor *model.User, teamID string, name string) (*TeamAssetFolderItem, error) {
	member, err := s.requireTeamEditor(actor, teamID)
	if err != nil {
		return nil, err
	}
	name, err = normalizeTeamAssetFolderName(name)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	folder := model.TeamAssetFolder{ID: newID(), TeamID: strings.TrimSpace(teamID), OwnerUserID: actor.ID, Name: name, NameKey: strings.ToLower(name), CreatedAt: now, UpdatedAt: now}
	if err := s.repo.SaveTeamAssetFolder(&folder, newTeamAuditEvent(actor, folder.TeamID, "folder.created", "folder", folder.ID, "创建团队素材文件夹")); err != nil {
		return nil, err
	}
	item := teamAssetFolderItem(folder, teamRoleCanEdit(member.Role))
	return &item, nil
}

func (s *Service) RenameTeamAssetFolder(actor *model.User, teamID string, id string, name string) (*TeamAssetFolderItem, error) {
	member, err := s.requireTeamEditor(actor, teamID)
	if err != nil {
		return nil, err
	}
	folder, err := s.repo.TeamAssetFolder(strings.TrimSpace(teamID), strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	folder.Name, err = normalizeTeamAssetFolderName(name)
	if err != nil {
		return nil, err
	}
	folder.NameKey, folder.UpdatedAt = strings.ToLower(folder.Name), time.Now().UTC()
	if err := s.repo.SaveTeamAssetFolder(folder, newTeamAuditEvent(actor, folder.TeamID, "folder.renamed", "folder", folder.ID, "重命名团队素材文件夹")); err != nil {
		return nil, err
	}
	item := teamAssetFolderItem(*folder, teamRoleCanEdit(member.Role))
	return &item, nil
}

func (s *Service) DeleteTeamAssetFolder(actor *model.User, teamID string, id string) error {
	if _, err := s.requireTeamManager(actor, teamID); err != nil {
		return err
	}
	folder, err := s.repo.TeamAssetFolder(strings.TrimSpace(teamID), strings.TrimSpace(id))
	if err != nil {
		return err
	}
	assets, err := s.repo.TeamAssetsInFolder(folder.TeamID, folder.ID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for index := range assets {
		payload, payloadErr := setTeamAssetPayloadFolderID(json.RawMessage(assets[index].PayloadJSON), "", now)
		if payloadErr != nil {
			return payloadErr
		}
		assets[index].FolderID, assets[index].PayloadJSON, assets[index].UpdatedAt = "", string(payload), now
	}
	return s.repo.DeleteTeamAssetFolder(folder.TeamID, folder.ID, assets, newTeamAuditEvent(actor, folder.TeamID, "folder.deleted", "folder", folder.ID, "删除团队素材文件夹"))
}

func (s *Service) MoveTeamAsset(actor *model.User, teamID string, id string, folderID string) (*TeamAssetItem, error) {
	member, err := s.requireTeamEditor(actor, teamID)
	if err != nil {
		return nil, err
	}
	teamID, folderID = strings.TrimSpace(teamID), strings.TrimSpace(folderID)
	asset, err := s.repo.TeamAsset(teamID, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if folderID != "" {
		if _, err := s.repo.TeamAssetFolder(teamID, folderID); err != nil {
			return nil, NotFound("团队素材文件夹不存在")
		}
	}
	now := time.Now().UTC()
	payload, err := setTeamAssetPayloadFolderID(json.RawMessage(asset.PayloadJSON), folderID, now)
	if err != nil {
		return nil, err
	}
	if err := s.repo.MoveTeamAsset(teamID, asset.ID, folderID, string(payload), now, newTeamAuditEvent(actor, teamID, "asset.moved", "asset", asset.ID, "移动团队素材")); err != nil {
		return nil, err
	}
	asset.FolderID, asset.PayloadJSON, asset.UpdatedAt = folderID, string(payload), now
	item, err := s.teamAssetItem(*asset, actor, member.Role)
	return &item, err
}

func (s *Service) DeleteTeamAsset(actor *model.User, teamID string, id string) error {
	member, err := s.requireTeamEditor(actor, teamID)
	if err != nil {
		return err
	}
	asset, err := s.repo.TeamAsset(strings.TrimSpace(teamID), strings.TrimSpace(id))
	if err != nil {
		return err
	}
	if asset.OwnerUserID != actor.ID && !teamRoleCanManage(member.Role) {
		return Forbidden("只能取消自己共享的素材")
	}
	return s.repo.DeleteTeamAsset(asset.TeamID, asset.ID, newTeamAuditEvent(actor, asset.TeamID, "asset.unshared", "asset", asset.ID, "取消共享团队素材"))
}

func newTeamAuditEvent(actor *model.User, teamID string, action string, targetType string, targetID string, summary string) *model.TeamAuditEvent {
	teamAuditClock.Lock()
	now := time.Now().UTC()
	if !now.After(teamAuditClock.last) {
		now = teamAuditClock.last.Add(time.Nanosecond)
	}
	teamAuditClock.last = now
	teamAuditClock.Unlock()
	return &model.TeamAuditEvent{ID: newID(), TeamID: strings.TrimSpace(teamID), ActorUserID: actor.ID, Action: action, TargetType: targetType, TargetID: strings.TrimSpace(targetID), Summary: summary, CreatedAt: now}
}

func teamRoleLabel(role model.TeamMemberRole) string {
	switch role {
	case model.TeamMemberRoleOwner:
		return "所有者"
	case model.TeamMemberRoleAdmin:
		return "管理员"
	case model.TeamMemberRoleEditor:
		return "编辑者"
	default:
		return "查看者"
	}
}

func (s *Service) requireTeamMember(actor *model.User, teamID string) (*model.TeamMember, error) {
	if actor == nil {
		return nil, Unauthorized("请先登录")
	}
	member, err := s.repo.TeamMember(strings.TrimSpace(teamID), actor.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, Forbidden("无权访问该团队")
	}
	return member, err
}

func (s *Service) requireTeamEditor(actor *model.User, teamID string) (*model.TeamMember, error) {
	member, err := s.requireTeamMember(actor, teamID)
	if err != nil {
		return nil, err
	}
	if !teamRoleCanEdit(member.Role) {
		return nil, Forbidden("当前团队角色没有编辑权限")
	}
	return member, nil
}
func (s *Service) requireTeamManager(actor *model.User, teamID string) (*model.TeamMember, error) {
	member, err := s.requireTeamMember(actor, teamID)
	if err != nil {
		return nil, err
	}
	if !teamRoleCanManage(member.Role) {
		return nil, Forbidden("当前团队角色没有管理权限")
	}
	return member, nil
}

func (s *Service) teamAssetItem(asset model.TeamAsset, actor *model.User, role model.TeamMemberRole) (TeamAssetItem, error) {
	owner, err := s.repo.User(asset.OwnerUserID)
	if err != nil {
		return TeamAssetItem{}, err
	}
	return TeamAssetItem{ID: asset.ID, SourceAssetID: asset.SourceAssetID, FolderID: asset.FolderID, Asset: json.RawMessage(asset.PayloadJSON), Owner: TeamAssetOwner{ID: owner.ID, Username: owner.Username, DisplayName: owner.DisplayName}, CanEdit: teamRoleCanEdit(role), CanDelete: actor != nil && (actor.ID == asset.OwnerUserID || teamRoleCanManage(role)), MemberRole: role, CreatedAt: asset.CreatedAt, UpdatedAt: asset.UpdatedAt}, nil
}

func teamItem(team model.Team, role model.TeamMemberRole) TeamItem {
	return TeamItem{ID: team.ID, Name: team.Name, Description: team.Description, Role: role, CanEdit: teamRoleCanEdit(role), CanManage: teamRoleCanManage(role), CreatedAt: team.CreatedAt, UpdatedAt: team.UpdatedAt}
}
func teamMemberItem(member repository.TeamMemberRecord) TeamMemberItem {
	return TeamMemberItem{UserID: member.UserID, Username: member.Username, DisplayName: member.DisplayName, Role: member.Role, JoinedAt: member.JoinedAt}
}
func validateAssignableTeamRole(actorRole model.TeamMemberRole, role model.TeamMemberRole) error {
	if role != model.TeamMemberRoleAdmin && role != model.TeamMemberRoleEditor && role != model.TeamMemberRoleViewer {
		return BadAuthRequest("团队成员角色无效")
	}
	if actorRole == model.TeamMemberRoleAdmin && role == model.TeamMemberRoleAdmin {
		return Forbidden("只有团队所有者可以任命管理员")
	}
	return nil
}
func teamAssetFolderItem(folder model.TeamAssetFolder, canEdit bool) TeamAssetFolderItem {
	return TeamAssetFolderItem{ID: folder.ID, Name: folder.Name, CanEdit: canEdit, CreatedAt: folder.CreatedAt, UpdatedAt: folder.UpdatedAt}
}
func teamRoleCanEdit(role model.TeamMemberRole) bool {
	return role == model.TeamMemberRoleOwner || role == model.TeamMemberRoleAdmin || role == model.TeamMemberRoleEditor
}
func teamRoleCanManage(role model.TeamMemberRole) bool {
	return role == model.TeamMemberRoleOwner || role == model.TeamMemberRoleAdmin
}
func normalizeTeamAssetPage(page int, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 24
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}

func normalizeTeamAssetFolderName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || len([]rune(name)) > 120 {
		return "", BadAuthRequest("团队文件夹名称不能为空且不能超过 120 个字符")
	}
	return name, nil
}
func validTeamAssetKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "text", "image", "video", "audio", "model":
		return true
	default:
		return false
	}
}
func validateTeamAsset(asset model.Asset) error {
	if !validTeamAssetKind(asset.Kind) {
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
	payload["id"], payload["folderId"] = id, folderID
	payload["createdAt"], payload["updatedAt"] = createdAt.UTC().Format(time.RFC3339Nano), updatedAt.UTC().Format(time.RFC3339Nano)
	encoded, err := json.Marshal(payload)
	return json.RawMessage(encoded), err
}
func setTeamAssetPayloadFolderID(raw json.RawMessage, folderID string, updatedAt time.Time) (json.RawMessage, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, BadAuthRequest("团队素材数据格式错误")
	}
	payload["folderId"], payload["updatedAt"] = folderID, updatedAt.UTC().Format(time.RFC3339Nano)
	encoded, err := json.Marshal(payload)
	return json.RawMessage(encoded), err
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
					if id := strings.TrimSpace(strings.TrimPrefix(storageKey, "resource:")); id != "" {
						ids[id] = struct{}{}
					}
				}
			}
			collectTeamAssetResourceIDs(child, ids)
		}
	}
}

func importedTeamAssetPayload(raw json.RawMessage, id string, now time.Time, resourceIDs map[string]string) (json.RawMessage, error) {
	var payload any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, BadAuthRequest("团队素材数据格式错误")
	}
	rewriteImportedTeamAssetValue(payload, "", resourceIDs)
	root, ok := payload.(map[string]any)
	if !ok {
		return nil, BadAuthRequest("团队素材数据格式错误")
	}
	root["id"] = id
	delete(root, "folderId")
	root["createdAt"], root["updatedAt"] = now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)
	encoded, err := json.Marshal(root)
	return json.RawMessage(encoded), err
}

func rewriteImportedTeamAssetValue(value any, key string, resourceIDs map[string]string) {
	switch item := value.(type) {
	case []any:
		for _, child := range item {
			rewriteImportedTeamAssetValue(child, "", resourceIDs)
		}
	case map[string]any:
		for childKey, child := range item {
			if text, ok := child.(string); ok {
				item[childKey] = rewriteImportedTeamAssetString(text, childKey, resourceIDs)
				continue
			}
			rewriteImportedTeamAssetValue(child, childKey, resourceIDs)
		}
	}
}

func rewriteImportedTeamAssetString(value string, key string, resourceIDs map[string]string) string {
	if key == "storageKey" && strings.HasPrefix(value, "resource:") {
		if id := strings.TrimSpace(strings.TrimPrefix(value, "resource:")); resourceIDs[id] != "" {
			return "resource:" + resourceIDs[id]
		}
	}
	for oldID, newID := range resourceIDs {
		value = strings.ReplaceAll(value, "/api/resources/"+oldID+"/file", "/api/resources/"+newID+"/file")
	}
	return value
}
