package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"infinite-canvas/backend/internal/kernel"
	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/gorm"
)

type UpdateOwnDisplayNameRequest struct {
	DisplayName string `json:"displayName"`
}

func (s *Service) UpdateOwnDisplayName(actor *model.User, req UpdateOwnDisplayNameRequest) (AuthUser, error) {
	if actor == nil || strings.TrimSpace(actor.ID) == "" {
		return AuthUser{}, kernel.Unauthorized("请先登录")
	}
	user, err := s.repo.User(actor.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AuthUser{}, kernel.Unauthorized("登录状态已失效")
	}
	if err != nil {
		return AuthUser{}, err
	}
	if user.Status != model.UserStatusActive {
		return AuthUser{}, kernel.Forbidden("该账号已被禁用")
	}
	if user.DisplayName != actor.DisplayName {
		return AuthUser{}, kernel.NewAppError(http.StatusConflict, "显示名称已变化，请刷新后重试")
	}
	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" || !utf8.ValidString(displayName) || utf8.RuneCountInString(displayName) > 40 {
		return AuthUser{}, kernel.BadAuthRequest("显示名称须为 1-40 个字符")
	}
	if strings.ContainsAny(displayName, "\u2028\u2029") || strings.IndexFunc(displayName, unicode.IsControl) >= 0 {
		return AuthUser{}, kernel.BadAuthRequest("显示名称不能包含换行或控制字符")
	}
	publicUser, err := s.PublicAuthUser(user)
	if err != nil {
		return AuthUser{}, err
	}
	if displayName == user.DisplayName {
		return publicUser, nil
	}
	metadata, err := json.Marshal(map[string]string{"previousDisplayName": user.DisplayName, "displayName": displayName})
	if err != nil {
		return AuthUser{}, err
	}
	now := time.Now()
	audit := &model.AdminAuditEvent{
		ID: kernel.NewID(), ActorUserID: user.ID, Action: "user.display_name.update_self", TargetType: "user", TargetID: user.ID,
		Summary: "用户修改自己的显示名称", MetadataJSON: string(metadata), CreatedAt: now,
	}
	updated, err := s.repo.UpdateOwnDisplayName(user.ID, user.DisplayName, displayName, now, audit)
	if errors.Is(err, repository.ErrUserChanged) {
		return AuthUser{}, kernel.NewAppError(http.StatusConflict, "账号资料已变化，请刷新后重试")
	}
	if err != nil {
		return AuthUser{}, err
	}
	publicUser.User = *updated
	return publicUser, nil
}
