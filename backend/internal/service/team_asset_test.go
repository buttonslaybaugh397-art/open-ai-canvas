package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTeamAssetsAreIsolatedAndPlatformAdminHasNoImplicitAccess(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	ownerA := testTeamUser("owner-a")
	ownerB := testTeamUser("owner-b")
	platformAdmin := testTeamUser("platform-admin")
	platformAdmin.Role = model.UserRoleAdmin
	if err := db.Create(&[]model.User{ownerA, ownerB, platformAdmin}).Error; err != nil {
		t.Fatal(err)
	}
	teamA := createTestTeam(t, svc, &ownerA, "Team A")
	teamB := createTestTeam(t, svc, &ownerB, "Team B")
	createTestPersonalAsset(t, db, ownerA.ID, "asset-a", "Asset A")
	if _, err := svc.ShareTeamAssets(&ownerA, teamA.ID, []string{"asset-a"}, ""); err != nil {
		t.Fatal(err)
	}

	pageA, err := svc.TeamAssets(&ownerA, teamA.ID, repository.TeamAssetFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if pageA.Total != 1 || len(pageA.Assets) != 1 {
		t.Fatalf("team A page = %#v", pageA)
	}
	pageB, err := svc.TeamAssets(&ownerB, teamB.ID, repository.TeamAssetFilter{Page: 1, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if pageB.Total != 0 {
		t.Fatalf("team B saw team A assets: %#v", pageB)
	}
	if _, err := svc.TeamAssets(&ownerB, teamA.ID, repository.TeamAssetFilter{}); err == nil {
		t.Fatal("non-member accessed another team")
	}
	if _, err := svc.TeamAssets(&platformAdmin, teamA.ID, repository.TeamAssetFilter{}); err == nil {
		t.Fatal("platform admin received implicit team access")
	}
}

func TestTeamRolesAndPersonalAssetOwnership(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("owner")
	viewer := testTeamUser("viewer")
	editor := testTeamUser("editor")
	outsider := testTeamUser("outsider")
	if err := db.Create(&[]model.User{owner, viewer, editor, outsider}).Error; err != nil {
		t.Fatal(err)
	}
	team := createTestTeam(t, svc, &owner, "Production")
	now := time.Now().UTC()
	members := []model.TeamMember{
		{TeamID: team.ID, UserID: viewer.ID, Role: model.TeamMemberRoleViewer, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now},
		{TeamID: team.ID, UserID: editor.ID, Role: model.TeamMemberRoleEditor, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&members).Error; err != nil {
		t.Fatal(err)
	}
	createTestPersonalAsset(t, db, outsider.ID, "foreign-asset", "Foreign")
	createTestPersonalAsset(t, db, editor.ID, "editor-asset", "Editor")

	if _, err := svc.CreateTeamAssetFolder(&viewer, team.ID, "Blocked"); err == nil {
		t.Fatal("viewer created a folder")
	}
	if _, err := svc.ShareTeamAssets(&viewer, team.ID, []string{"foreign-asset"}, ""); err == nil {
		t.Fatal("viewer shared an asset")
	}
	if _, err := svc.ShareTeamAssets(&editor, team.ID, []string{"foreign-asset"}, ""); err == nil {
		t.Fatal("editor shared another user's asset")
	}
	shared, err := svc.ShareTeamAssets(&editor, team.ID, []string{"editor-asset"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(shared) != 1 || !shared[0].CanEdit || shared[0].CanDelete == false {
		t.Fatalf("unexpected shared item: %#v", shared)
	}
	if err := svc.DeleteTeamAsset(&viewer, team.ID, shared[0].ID); err == nil {
		t.Fatal("viewer deleted an asset")
	}
	if err := svc.DeleteTeamAsset(&owner, team.ID, shared[0].ID); err != nil {
		t.Fatalf("owner could not manage team asset: %v", err)
	}
}

func TestTeamMemberManagementPermissionsAndReactivation(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("member-owner")
	admin := testTeamUser("member-admin")
	editor := testTeamUser("member-editor")
	viewer := testTeamUser("member-viewer")
	outsider := testTeamUser("member-outsider")
	platformAdmin := testTeamUser("member-platform-admin")
	platformAdmin.Role = model.UserRoleAdmin
	if err := db.Create(&[]model.User{owner, admin, editor, viewer, outsider, platformAdmin}).Error; err != nil {
		t.Fatal(err)
	}
	team := createTestTeam(t, svc, &owner, "Members")

	addedAdmin, err := svc.AddTeamMember(&owner, team.ID, admin.Username, model.TeamMemberRoleAdmin)
	if err != nil || addedAdmin.Role != model.TeamMemberRoleAdmin {
		t.Fatalf("owner add admin = %#v, %v", addedAdmin, err)
	}
	if _, err := svc.AddTeamMember(&admin, team.ID, editor.Username, model.TeamMemberRoleEditor); err != nil {
		t.Fatalf("admin add editor: %v", err)
	}
	if _, err := svc.AddTeamMember(&admin, team.ID, viewer.Username, model.TeamMemberRoleViewer); err != nil {
		t.Fatalf("admin add viewer: %v", err)
	}
	if _, err := svc.AddTeamMember(&admin, team.ID, outsider.Username, model.TeamMemberRoleAdmin); err == nil {
		t.Fatal("admin appointed another admin")
	}
	if _, err := svc.UpdateTeamMemberRole(&admin, team.ID, editor.ID, model.TeamMemberRoleViewer); err != nil {
		t.Fatalf("admin could not manage editor: %v", err)
	}
	if _, err := svc.UpdateTeamMemberRole(&admin, team.ID, owner.ID, model.TeamMemberRoleViewer); err == nil {
		t.Fatal("admin demoted owner")
	}
	if _, err := svc.UpdateTeamMemberRole(&admin, team.ID, admin.ID, model.TeamMemberRoleEditor); err == nil {
		t.Fatal("admin changed own role")
	}
	if _, err := svc.AddTeamMember(&viewer, team.ID, outsider.Username, model.TeamMemberRoleViewer); err == nil {
		t.Fatal("viewer added a member")
	}
	if err := svc.RemoveTeamMember(&viewer, team.ID, editor.ID); err == nil {
		t.Fatal("viewer removed another member")
	}
	if _, err := svc.TeamMembers(&platformAdmin, team.ID); err == nil {
		t.Fatal("platform admin received implicit member-list access")
	}

	members, err := svc.TeamMembers(&editor, team.ID)
	if err != nil || len(members) != 4 {
		t.Fatalf("member list = %#v, %v", members, err)
	}
	if err := svc.RemoveTeamMember(&viewer, team.ID, viewer.ID); err != nil {
		t.Fatalf("viewer could not leave: %v", err)
	}
	if _, err := svc.TeamAssets(&viewer, team.ID, repository.TeamAssetFilter{}); err == nil {
		t.Fatal("removed viewer retained team access")
	}
	if _, err := svc.AddTeamMember(&owner, team.ID, viewer.Username, model.TeamMemberRoleEditor); err != nil {
		t.Fatalf("owner could not reactivate viewer: %v", err)
	}
	var stored []model.TeamMember
	if err := db.Find(&stored, "team_id = ? AND user_id = ?", team.ID, viewer.ID).Error; err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].Status != model.TeamMemberStatusActive || stored[0].Role != model.TeamMemberRoleEditor {
		t.Fatalf("reactivated membership = %#v", stored)
	}
}

func TestTeamOwnerCannotBeDemotedRemovedOrLeave(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("protected-owner")
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	team := createTestTeam(t, svc, &owner, "Protected")
	if _, err := svc.UpdateTeamMemberRole(&owner, team.ID, owner.ID, model.TeamMemberRoleViewer); err == nil {
		t.Fatal("owner demoted self")
	}
	if err := svc.RemoveTeamMember(&owner, team.ID, owner.ID); err == nil {
		t.Fatal("owner left team")
	}
	members, err := svc.TeamMembers(&owner, team.ID)
	if err != nil || len(members) != 1 || members[0].Role != model.TeamMemberRoleOwner {
		t.Fatalf("owner membership changed = %#v, %v", members, err)
	}
}

func TestTeamAuditEventsAreManagerOnlyAndTenantIsolated(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	ownerA, ownerB := testTeamUser("audit-owner-a"), testTeamUser("audit-owner-b")
	admin, viewer := testTeamUser("audit-admin"), testTeamUser("audit-viewer")
	if err := db.Create(&[]model.User{ownerA, ownerB, admin, viewer}).Error; err != nil {
		t.Fatal(err)
	}
	teamA := createTestTeam(t, svc, &ownerA, "Audit A")
	teamB := createTestTeam(t, svc, &ownerB, "Audit B")
	if _, err := svc.AddTeamMember(&ownerA, teamA.ID, admin.Username, model.TeamMemberRoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddTeamMember(&ownerA, teamA.ID, viewer.Username, model.TeamMemberRoleViewer); err != nil {
		t.Fatal(err)
	}

	page, err := svc.TeamAuditEvents(&admin, teamA.ID, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 3 || len(page.Events) != 2 || page.Page != 1 || page.PageSize != 2 {
		t.Fatalf("unexpected team audit page: %#v", page)
	}
	if page.Events[0].Action != "member.added" || page.Events[0].ActorUserID != ownerA.ID {
		t.Fatalf("audit ordering or actor is incorrect: %#v", page.Events)
	}
	if _, err := svc.TeamAuditEvents(&viewer, teamA.ID, 1, 20); err == nil {
		t.Fatal("viewer read team audit events")
	}
	if _, err := svc.TeamAuditEvents(&ownerB, teamA.ID, 1, 20); err == nil {
		t.Fatal("cross-team owner read team audit events")
	}
	other, err := svc.TeamAuditEvents(&ownerB, teamB.ID, 1, 20)
	if err != nil || other.Total != 1 || len(other.Events) != 1 || other.Events[0].ActorUserID != ownerB.ID {
		t.Fatalf("team audit events crossed tenant boundary: %#v, %v", other, err)
	}
}

func TestTeamAuditFailureRollsBackSettingsUpdate(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("audit-rollback-owner")
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	team := createTestTeam(t, svc, &owner, "Original")
	if err := db.Migrator().DropTable(&model.TeamAuditEvent{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.UpdateTeamSettings(&owner, team.ID, "Changed", "", 100, 2<<30); err == nil {
		t.Fatal("settings update succeeded without its audit event")
	}
	stored, err := svc.repo.Team(team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Name != "Original" {
		t.Fatalf("settings escaped failed audit transaction: %#v", stored)
	}
}

func TestTeamInvitationLifecycleStoresOnlyHash(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("invite-owner")
	guest := testTeamUser("invite-guest")
	if err := db.Create(&[]model.User{owner, guest}).Error; err != nil {
		t.Fatal(err)
	}
	team := createTestTeam(t, svc, &owner, "Invite team")
	invitation, token, err := svc.CreateTeamInvitation(&owner, team.ID, model.TeamMemberRoleEditor, 72)
	if err != nil || invitation.Role != model.TeamMemberRoleEditor || token == "" {
		t.Fatalf("create invitation = %#v, token=%q, err=%v", invitation, token, err)
	}
	var stored model.TeamInvitation
	if err := db.First(&stored, "id = ?", invitation.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.TokenHash == token || strings.Contains(stored.TokenHash, token) || len(stored.TokenHash) != 64 {
		t.Fatalf("plaintext token escaped into storage: %#v", stored)
	}
	var audit model.TeamAuditEvent
	if err := db.First(&audit, "target_id = ?", invitation.ID).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(audit.Summary, token) {
		t.Fatal("plaintext token escaped into audit")
	}
	preview, err := svc.TeamInvitationPreview(token)
	if err != nil || !preview.Available || preview.TeamName != "Invite team" {
		t.Fatalf("preview = %#v, %v", preview, err)
	}
	joined, err := svc.AcceptTeamInvitation(&guest, token)
	if err != nil || joined.ID != team.ID || joined.Role != model.TeamMemberRoleEditor {
		t.Fatalf("accept = %#v, %v", joined, err)
	}
	member, err := svc.repo.TeamMember(team.ID, guest.ID)
	if err != nil || member.Role != model.TeamMemberRoleEditor {
		t.Fatalf("accepted membership = %#v, %v", member, err)
	}
	if _, err := svc.AcceptTeamInvitation(&guest, token); err == nil {
		t.Fatal("consumed invitation was accepted twice")
	}
}

func TestTeamInvitationPermissionsRevocationAndExpiry(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("invite-policy-owner")
	admin := testTeamUser("invite-policy-admin")
	viewer := testTeamUser("invite-policy-viewer")
	if err := db.Create(&[]model.User{owner, admin, viewer}).Error; err != nil {
		t.Fatal(err)
	}
	team := createTestTeam(t, svc, &owner, "Invite policy")
	if _, err := svc.AddTeamMember(&owner, team.ID, admin.Username, model.TeamMemberRoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddTeamMember(&owner, team.ID, viewer.Username, model.TeamMemberRoleViewer); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CreateTeamInvitation(&viewer, team.ID, model.TeamMemberRoleViewer, 24); err == nil {
		t.Fatal("viewer created invitation")
	}
	if _, _, err := svc.CreateTeamInvitation(&admin, team.ID, model.TeamMemberRoleAdmin, 24); err == nil {
		t.Fatal("admin invited another admin")
	}
	invitation, token, err := svc.CreateTeamInvitation(&admin, team.ID, model.TeamMemberRoleViewer, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RevokeTeamInvitation(&owner, team.ID, invitation.ID); err != nil {
		t.Fatal(err)
	}
	newGuest := testTeamUser("new-guest")
	if _, err := svc.AcceptTeamInvitation(&newGuest, token); err == nil {
		t.Fatal("revoked invitation was accepted")
	}
	expired, expiredToken, err := svc.CreateTeamInvitation(&owner, team.ID, model.TeamMemberRoleViewer, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.TeamInvitation{}).Where("id = ?", expired.ID).Update("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	expiredGuest := testTeamUser("expired-guest")
	if _, err := svc.AcceptTeamInvitation(&expiredGuest, expiredToken); err == nil {
		t.Fatal("expired invitation was accepted")
	}
}

func TestTeamDetailAndSettingsPermissions(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("settings-owner")
	viewer := testTeamUser("settings-viewer")
	if err := db.Create(&[]model.User{owner, viewer}).Error; err != nil {
		t.Fatal(err)
	}
	team := createTestTeam(t, svc, &owner, "Settings")
	now := time.Now().UTC()
	if err := db.Create(&model.TeamMember{TeamID: team.ID, UserID: viewer.ID, Role: model.TeamMemberRoleViewer, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	detail, err := svc.TeamDetail(&viewer, team.ID)
	if err != nil || detail.Usage.MemberCount != 2 || detail.Usage.AssetLimit != defaultTeamAssetLimit || detail.Usage.StorageLimitBytes != defaultTeamStorageLimit {
		t.Fatalf("viewer detail = %#v, %v", detail, err)
	}
	if _, err := svc.UpdateTeamSettings(&viewer, team.ID, "Blocked", "", 100, 2<<30); err == nil {
		t.Fatal("viewer updated team settings")
	}
	updated, err := svc.UpdateTeamSettings(&owner, team.ID, "Renamed", "Shared production assets", 100, 2<<30)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Team.Name != "Renamed" || updated.Team.Description != "Shared production assets" || updated.Usage.AssetLimit != 100 || updated.Usage.StorageLimitBytes != 2<<30 {
		t.Fatalf("updated detail = %#v", updated)
	}
}

func TestTeamUsageDeduplicatesResourcesAndRejectsLowerLimits(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("usage-owner")
	viewer := testTeamUser("usage-viewer")
	team, shared := createSharedImageTeamAssets(t, svc, db, &owner, &viewer, []string{"usage-a", "usage-b"})
	detail, err := svc.TeamDetail(&owner, team.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Usage.AssetCount != int64(len(shared)) || detail.Usage.StorageBytes != int64(len([]byte("shared-image-content"))) {
		t.Fatalf("deduplicated usage = %#v", detail.Usage)
	}
	if _, err := svc.UpdateTeamSettings(&owner, team.ID, team.Name, "", 1, 1<<30); err == nil || !strings.Contains(err.Error(), "当前使用量") {
		t.Fatalf("lower asset limit error = %v", err)
	}
	if err := db.Model(&model.Resource{}).Where("user_id = ?", owner.ID).Update("size", int64(2)<<30).Error; err != nil {
		t.Fatal(err)
	}
	detail, err = svc.TeamDetail(&owner, team.ID)
	if err != nil || detail.Usage.StorageBytes != int64(2)<<30 {
		t.Fatalf("updated storage usage = %#v, %v", detail, err)
	}
	if _, err := svc.UpdateTeamSettings(&owner, team.ID, team.Name, "", 10, detail.Usage.StorageBytes-1); err == nil || !strings.Contains(err.Error(), "当前使用量") {
		t.Fatalf("lower storage limit error = %v", err)
	}
}

func TestShareTeamAssetsRollsBackWholeBatchWhenQuotaExceeded(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("batch-quota-owner")
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	team := createTestTeam(t, svc, &owner, "Atomic batch")
	createTestPersonalAsset(t, db, owner.ID, "batch-a", "A")
	createTestPersonalAsset(t, db, owner.ID, "batch-b", "B")
	if err := db.Model(&model.Team{}).Where("id = ?", team.ID).Update("asset_limit", 1).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ShareTeamAssets(&owner, team.ID, []string{"batch-a", "batch-b"}, ""); err == nil || !strings.Contains(err.Error(), "数量已达到上限") {
		t.Fatalf("batch quota error = %v", err)
	}
	var assetCount int64
	var linkCount int64
	if err := db.Model(&model.TeamAsset{}).Where("team_id = ?", team.ID).Count(&assetCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.TeamAssetResource{}).Count(&linkCount).Error; err != nil {
		t.Fatal(err)
	}
	if assetCount != 0 || linkCount != 0 {
		t.Fatalf("failed batch persisted assets=%d links=%d", assetCount, linkCount)
	}
}

func TestShareTeamAssetsRollsBackWholeBatchWhenStorageExceeded(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("batch-storage-owner")
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	team := createTestTeam(t, svc, &owner, "Atomic storage batch")
	now := time.Now().UTC()
	resource := model.Resource{ID: "batch-storage-resource", UserID: owner.ID, Kind: "image", Status: model.ResourceStatusReady, Provider: "local", Size: 2 << 30, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	for _, assetID := range []string{"storage-a", "storage-b"} {
		payload := `{"id":"` + assetID + `","kind":"image","category":"other","status":"confirmed","title":"` + assetID + `","data":{"dataUrl":"/api/resources/` + resource.ID + `/file","storageKey":"resource:` + resource.ID + `"}}`
		asset := model.Asset{ID: assetID, UserID: owner.ID, Kind: "image", Category: model.AssetCategoryOther, Status: model.AssetVersionStatusConfirmed, Title: assetID, PayloadJSON: payload, CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&asset).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Model(&model.Team{}).Where("id = ?", team.ID).Update("storage_limit", int64(1)<<30).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ShareTeamAssets(&owner, team.ID, []string{"storage-a", "storage-b"}, ""); err == nil || !strings.Contains(err.Error(), "存储空间已达到上限") {
		t.Fatalf("batch storage error = %v", err)
	}
	var assetCount int64
	var linkCount int64
	if err := db.Model(&model.TeamAsset{}).Where("team_id = ?", team.ID).Count(&assetCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.TeamAssetResource{}).Count(&linkCount).Error; err != nil {
		t.Fatal(err)
	}
	if assetCount != 0 || linkCount != 0 {
		t.Fatalf("failed storage batch persisted assets=%d links=%d", assetCount, linkCount)
	}
}

func TestTeamSharedResourceAccessRequiresActiveMembership(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("resource-owner")
	viewer := testTeamUser("resource-viewer")
	outsider := testTeamUser("resource-outsider")
	if err := db.Create(&[]model.User{owner, viewer, outsider}).Error; err != nil {
		t.Fatal(err)
	}
	team := createTestTeam(t, svc, &owner, "Resources")
	now := time.Now().UTC()
	if err := db.Create(&model.TeamMember{TeamID: team.ID, UserID: viewer.ID, Role: model.TeamMemberRoleViewer, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	resource := model.Resource{ID: "resource-1", UserID: owner.ID, Kind: "image", Status: model.ResourceStatusReady, Provider: "local", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	asset := model.TeamAsset{ID: "shared-asset", TeamID: team.ID, OwnerUserID: owner.ID, SourceAssetID: "source-1", Kind: "image", Title: "Shared", PayloadJSON: "{}", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.TeamAssetResource{TeamAssetID: asset.ID, ResourceID: resource.ID, CreatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.repo.TeamSharedResourceForUser(viewer.ID, resource.ID); err != nil {
		t.Fatalf("member could not read shared resource: %v", err)
	}
	if _, err := svc.repo.TeamSharedResourceForUser(outsider.ID, resource.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("outsider resource lookup = %v", err)
	}
}

func TestTeamAssetPageUsesStablePaginationAndFilters(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("paging-owner")
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	team := createTestTeam(t, svc, &owner, "Paging")
	now := time.Now().UTC()
	assets := []model.TeamAsset{
		{ID: "a-1", TeamID: team.ID, OwnerUserID: owner.ID, SourceAssetID: "s-1", Kind: "image", Title: "Alpha", PayloadJSON: "{}", CreatedAt: now, UpdatedAt: now},
		{ID: "a-2", TeamID: team.ID, OwnerUserID: owner.ID, SourceAssetID: "s-2", Kind: "text", Title: "Beta", PayloadJSON: "{}", CreatedAt: now, UpdatedAt: now},
		{ID: "a-3", TeamID: team.ID, OwnerUserID: owner.ID, SourceAssetID: "s-3", Kind: "image", Title: "Gamma", PayloadJSON: "{}", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.Create(&assets).Error; err != nil {
		t.Fatal(err)
	}
	page, err := svc.TeamAssets(&owner, team.ID, repository.TeamAssetFilter{Page: 1, PageSize: 1, Kind: "image"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Assets) != 1 {
		t.Fatalf("filtered page = %#v", page)
	}
}

func TestViewerImportsIndependentTeamAssetResource(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("import-owner")
	viewer := testTeamUser("import-viewer")
	if err := db.Create(&[]model.User{owner, viewer}).Error; err != nil {
		t.Fatal(err)
	}
	team := createTestTeam(t, svc, &owner, "Import")
	now := time.Now().UTC()
	if err := db.Create(&model.TeamMember{TeamID: team.ID, UserID: viewer.ID, Role: model.TeamMemberRoleViewer, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	objectKey := filepath.ToSlash(filepath.Join("users", owner.ID, "image", "source.png"))
	filePath := filepath.Join(svc.dataDir, "resources", filepath.FromSlash(objectKey))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o750); err != nil {
		t.Fatal(err)
	}
	content := []byte("team-image-content")
	if err := os.WriteFile(filePath, content, 0o640); err != nil {
		t.Fatal(err)
	}
	resource := model.Resource{ID: "source-resource", UserID: owner.ID, Kind: "image", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: objectKey, MimeType: "image/png", Size: int64(len(content)), Width: 20, Height: 10, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	payload := `{"id":"source-asset","kind":"image","category":"character","status":"confirmed","title":"Shared image","coverUrl":"/api/resources/source-resource/file","data":{"dataUrl":"/api/resources/source-resource/file","storageKey":"resource:source-resource","bytes":18,"mimeType":"image/png","width":20,"height":10},"createdAt":"2026-01-01T00:00:00Z","updatedAt":"2026-01-01T00:00:00Z"}`
	asset := model.Asset{ID: "source-asset", UserID: owner.ID, Kind: "image", Category: model.AssetCategoryCharacter, Status: model.AssetVersionStatusConfirmed, Title: "Shared image", PayloadJSON: payload, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	shared, err := svc.ShareTeamAssets(&owner, team.ID, []string{asset.ID}, "")
	if err != nil {
		t.Fatal(err)
	}

	imported, err := svc.ImportTeamAssets(&viewer, team.ID, []string{shared[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 1 {
		t.Fatalf("imported = %#v", imported)
	}
	if strings.Contains(string(imported[0].Asset), resource.ID) {
		t.Fatalf("import retained source resource: %s", imported[0].Asset)
	}
	resourceIDs, err := teamAssetResourceIDs(imported[0].Asset)
	if err != nil || len(resourceIDs) != 1 {
		t.Fatalf("import resource ids = %#v, %v", resourceIDs, err)
	}
	copiedResource, err := svc.repo.ResourceForUser(viewer.ID, resourceIDs[0])
	if err != nil {
		t.Fatalf("viewer does not own copied resource: %v", err)
	}
	if copiedResource.ObjectKey == resource.ObjectKey {
		t.Fatal("import reused the source object key")
	}
	if err := svc.DeleteTeamAsset(&owner, team.ID, shared[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.TeamMember{}).Where("team_id = ? AND user_id = ?", team.ID, viewer.ID).Update("status", model.TeamMemberStatus("removed")).Error; err != nil {
		t.Fatal(err)
	}
	storedAsset, err := svc.UserAsset(viewer.ID, importedAssetID(t, imported[0].Asset))
	if err != nil || len(storedAsset) == 0 {
		t.Fatalf("personal copy missing after unshare: %s, %v", storedAsset, err)
	}
	_, body, err := svc.OpenResource(viewer.ID, copiedResource.ID)
	if err != nil {
		t.Fatalf("copied resource unavailable after membership removal: %v", err)
	}
	defer body.Close()
}

func TestImportTeamAssetsRejectsCrossTeamID(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	ownerA := testTeamUser("import-a")
	ownerB := testTeamUser("import-b")
	if err := db.Create(&[]model.User{ownerA, ownerB}).Error; err != nil {
		t.Fatal(err)
	}
	teamA := createTestTeam(t, svc, &ownerA, "A")
	teamB := createTestTeam(t, svc, &ownerB, "B")
	createTestPersonalAsset(t, db, ownerA.ID, "asset-a", "Asset A")
	shared, err := svc.ShareTeamAssets(&ownerA, teamA.ID, []string{"asset-a"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ImportTeamAssets(&ownerB, teamB.ID, []string{shared[0].ID}); err == nil {
		t.Fatal("cross-team asset id was imported")
	}
}

func TestImportTeamAssetsChecksAssetQuotaBeforeCopyingResources(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("quota-owner")
	viewer := testTeamUser("quota-viewer")
	team, shared := createSharedImageTeamAssets(t, svc, db, &owner, &viewer, []string{"quota-source"})
	createTestPersonalAsset(t, db, viewer.ID, "existing-personal", "Existing")
	policy := defaultRuntimePolicy()
	policy.Resource.AssetCount = 1
	encoded, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: runtimePolicySettingKey, ValueJSON: string(encoded), UpdatedAt: time.Now().UTC()}).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.ImportTeamAssets(&viewer, team.ID, []string{shared[0].ID}); err == nil || !strings.Contains(err.Error(), "素材数量") {
		t.Fatalf("quota import error = %v", err)
	}
	assertUserHasNoImportedResources(t, svc, db, viewer.ID)
}

func TestImportTeamAssetsDeduplicatesAssetIDsAndSharedResources(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("dedupe-owner")
	viewer := testTeamUser("dedupe-viewer")
	team, shared := createSharedImageTeamAssets(t, svc, db, &owner, &viewer, []string{"dedupe-source-a", "dedupe-source-b"})

	imported, err := svc.ImportTeamAssets(&viewer, team.ID, []string{shared[0].ID, shared[0].ID, shared[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(imported) != 2 {
		t.Fatalf("imported = %#v", imported)
	}
	firstIDs, firstErr := teamAssetResourceIDs(imported[0].Asset)
	secondIDs, secondErr := teamAssetResourceIDs(imported[1].Asset)
	if firstErr != nil || secondErr != nil || len(firstIDs) != 1 || len(secondIDs) != 1 || firstIDs[0] != secondIDs[0] {
		t.Fatalf("deduplicated resource ids = %#v / %#v, errors = %v / %v", firstIDs, secondIDs, firstErr, secondErr)
	}
	var resourceCount int64
	if err := db.Model(&model.Resource{}).Where("user_id = ?", viewer.ID).Count(&resourceCount).Error; err != nil {
		t.Fatal(err)
	}
	if resourceCount != 1 {
		t.Fatalf("viewer resource count = %d, want 1", resourceCount)
	}
}

func TestImportTeamAssetsCleansCopiedResourcesWhenAssetInsertFails(t *testing.T) {
	svc, db := newTeamAssetTestService(t)
	owner := testTeamUser("cleanup-owner")
	viewer := testTeamUser("cleanup-viewer")
	team, shared := createSharedImageTeamAssets(t, svc, db, &owner, &viewer, []string{"cleanup-source"})
	if err := db.Exec(`CREATE TRIGGER fail_imported_asset BEFORE INSERT ON assets
		WHEN NEW.user_id = 'cleanup-viewer'
		BEGIN SELECT RAISE(FAIL, 'forced import failure'); END`).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := svc.ImportTeamAssets(&viewer, team.ID, []string{shared[0].ID}); err == nil {
		t.Fatal("asset insertion failure was not returned")
	}
	assertUserHasNoImportedResources(t, svc, db, viewer.ID)
}

func importedAssetID(t *testing.T, raw []byte) string {
	t.Helper()
	var identity struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &identity); err != nil || identity.ID == "" {
		t.Fatalf("invalid imported asset: %s, %v", raw, err)
	}
	return identity.ID
}

func newTeamAssetTestService(t *testing.T) (*Service, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+newID()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.UserDailyUploadUsage{}, &model.UserOSSSetting{}, &model.StorageLocation{}, &model.User{}, &model.Asset{}, &model.Resource{}, &model.SessionFile{}, &model.CanvasProject{}, &model.Session{}, &model.Message{}, &model.Task{}, &model.TaskLog{}, &model.Result{}, &model.TaskTextDelta{}, &model.ApiCallLog{}, &model.Team{}, &model.TeamMember{}, &model.TeamAsset{}, &model.TeamAssetFolder{}, &model.TeamAssetResource{}, &model.TeamAuditEvent{}, &model.TeamInvitation{}); err != nil {
		t.Fatal(err)
	}
	return &Service{repo: repository.New(db), dataDir: t.TempDir()}, db
}

func testTeamUser(id string) model.User {
	now := time.Now().UTC()
	return model.User{ID: id, Username: id, DisplayName: id, Role: model.UserRoleUser, Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}
}
func createTestTeam(t *testing.T, svc *Service, owner *model.User, name string) *TeamItem {
	t.Helper()
	team, err := svc.CreateTeam(owner, name)
	if err != nil {
		t.Fatal(err)
	}
	return team
}
func createTestPersonalAsset(t *testing.T, db *gorm.DB, userID string, id string, title string) {
	t.Helper()
	now := time.Now().UTC()
	asset := model.Asset{ID: id, UserID: userID, Kind: "text", Category: model.AssetCategoryOther, Status: model.AssetVersionStatusConfirmed, Title: title, PayloadJSON: "{\"id\":\"" + id + "\",\"kind\":\"text\",\"title\":\"" + title + "\",\"data\":{\"content\":\"hello\"}}", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
}

func createSharedImageTeamAssets(t *testing.T, svc *Service, db *gorm.DB, owner *model.User, viewer *model.User, assetIDs []string) (*TeamItem, []TeamAssetItem) {
	t.Helper()
	if err := db.Create(&[]model.User{*owner, *viewer}).Error; err != nil {
		t.Fatal(err)
	}
	team := createTestTeam(t, svc, owner, "Import safeguards")
	now := time.Now().UTC()
	if err := db.Create(&model.TeamMember{TeamID: team.ID, UserID: viewer.ID, Role: model.TeamMemberRoleViewer, Status: model.TeamMemberStatusActive, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	content := []byte("shared-image-content")
	resourceID := "shared-resource-" + owner.ID
	objectKey := filepath.ToSlash(filepath.Join("users", owner.ID, "image", "source.png"))
	filePath := filepath.Join(svc.dataDir, "resources", filepath.FromSlash(objectKey))
	if err := os.MkdirAll(filepath.Dir(filePath), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, content, 0o640); err != nil {
		t.Fatal(err)
	}
	resource := model.Resource{ID: resourceID, UserID: owner.ID, Kind: "image", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: objectKey, MimeType: "image/png", Size: int64(len(content)), Width: 20, Height: 10, CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&resource).Error; err != nil {
		t.Fatal(err)
	}
	for _, assetID := range assetIDs {
		payload := `{"id":"` + assetID + `","kind":"image","category":"character","status":"confirmed","title":"` + assetID + `","coverUrl":"/api/resources/` + resourceID + `/file","data":{"dataUrl":"/api/resources/` + resourceID + `/file","storageKey":"resource:` + resourceID + `","bytes":20,"mimeType":"image/png","width":20,"height":10}}`
		asset := model.Asset{ID: assetID, UserID: owner.ID, Kind: "image", Category: model.AssetCategoryCharacter, Status: model.AssetVersionStatusConfirmed, Title: assetID, PayloadJSON: payload, CreatedAt: now, UpdatedAt: now}
		if err := db.Create(&asset).Error; err != nil {
			t.Fatal(err)
		}
	}
	shared, err := svc.ShareTeamAssets(owner, team.ID, assetIDs, "")
	if err != nil {
		t.Fatal(err)
	}
	return team, shared
}

func assertUserHasNoImportedResources(t *testing.T, svc *Service, db *gorm.DB, userID string) {
	t.Helper()
	var resourceCount int64
	if err := db.Model(&model.Resource{}).Where("user_id = ?", userID).Count(&resourceCount).Error; err != nil {
		t.Fatal(err)
	}
	if resourceCount != 0 {
		t.Fatalf("user resource count = %d, want 0", resourceCount)
	}
	resourceDir := filepath.Join(svc.dataDir, "resources", "users", userID)
	files := 0
	walkErr := filepath.WalkDir(resourceDir, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files++
		}
		return nil
	})
	if walkErr != nil && !os.IsNotExist(walkErr) {
		t.Fatalf("inspect imported resource directory: %v", walkErr)
	}
	if files != 0 {
		t.Fatalf("imported resource files still exist under %s", resourceDir)
	}
}
