package handler

import (
	"net/http"
	"strconv"
	"time"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/repository"
	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterTeamAssetRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/teams", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		teams, err := svc.Teams(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"teams": teams})
	})
	r.POST("/teams", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "teams-write:"+user.ID, 12, time.Minute) {
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		team, err := svc.CreateTeam(user, req.Name)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"team": team})
	})
	r.GET("/teams/:teamId", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		detail, err := svc.TeamDetail(user, c.Param("teamId"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, detail)
	})
	r.PATCH("/teams/:teamId", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "teams-write:"+user.ID, 12, time.Minute) {
			return
		}
		var req struct {
			Name              string `json:"name"`
			Description       string `json:"description"`
			AssetLimit        int64  `json:"assetLimit"`
			StorageLimitBytes int64  `json:"storageLimitBytes"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		detail, err := svc.UpdateTeamSettings(user, c.Param("teamId"), req.Name, req.Description, req.AssetLimit, req.StorageLimitBytes)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, detail)
	})
	r.GET("/teams/:teamId/members", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		members, err := svc.TeamMembers(user, c.Param("teamId"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"members": members})
	})
	r.GET("/teams/:teamId/audit-events", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
		result, err := svc.TeamAuditEvents(user, c.Param("teamId"), page, pageSize)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
	r.GET("/teams/:teamId/invitations", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		invitations, err := svc.TeamInvitations(user, c.Param("teamId"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"invitations": invitations})
	})
	r.POST("/teams/:teamId/invitations", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "team-invitations-write:"+user.ID, 20, time.Minute) {
			return
		}
		var req struct {
			Role       model.TeamMemberRole `json:"role"`
			ValidHours int                  `json:"validHours"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		invitation, token, err := svc.CreateTeamInvitation(user, c.Param("teamId"), req.Role, req.ValidHours)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"invitation": invitation, "inviteUrl": "/teams/join/" + token})
	})
	r.DELETE("/teams/:teamId/invitations/:id", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.RevokeTeamInvitation(user, c.Param("teamId"), c.Param("id")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"id": c.Param("id")})
	})
	r.GET("/team-invitations/:token", func(c *gin.Context) {
		preview, err := svc.TeamInvitationPreview(c.Param("token"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, preview)
	})
	r.POST("/team-invitations/:token/accept", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "team-invitations-accept:"+user.ID, 20, time.Minute) {
			return
		}
		team, err := svc.AcceptTeamInvitation(user, c.Param("token"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"team": team})
	})
	r.POST("/teams/:teamId/members", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "team-members-write:"+user.ID, 30, time.Minute) {
			return
		}
		var req struct {
			Username string               `json:"username"`
			Role     model.TeamMemberRole `json:"role"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		member, err := svc.AddTeamMember(user, c.Param("teamId"), req.Username, req.Role)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"member": member})
	})
	r.PATCH("/teams/:teamId/members/:userId", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "team-members-write:"+user.ID, 30, time.Minute) {
			return
		}
		var req struct {
			Role model.TeamMemberRole `json:"role"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		member, err := svc.UpdateTeamMemberRole(user, c.Param("teamId"), c.Param("userId"), req.Role)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"member": member})
	})
	r.DELETE("/teams/:teamId/members/:userId", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if !enforceRateLimit(c, "team-members-write:"+user.ID, 30, time.Minute) {
			return
		}
		if err := svc.RemoveTeamMember(user, c.Param("teamId"), c.Param("userId")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"userId": c.Param("userId")})
	})

	r.GET("/teams/:teamId/assets", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
		pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "24"))
		var folderID *string
		if value, exists := c.GetQuery("folderId"); exists {
			folderID = &value
		}
		result, err := svc.TeamAssets(user, c.Param("teamId"), repository.TeamAssetFilter{Page: page, PageSize: pageSize, Query: c.Query("query"), Kind: c.Query("kind"), FolderID: folderID})
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})
	r.POST("/teams/:teamId/assets/share", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		policy, available := loadRuntimePolicy(c, svc)
		if !available || !enforceRateLimit(c, "team-assets-write:"+user.ID, policy.Request.AssetWritePerMinute, time.Minute) {
			return
		}
		var req struct {
			AssetIDs []string `json:"assetIds"`
			FolderID string   `json:"folderId"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		assets, err := svc.ShareTeamAssets(user, c.Param("teamId"), req.AssetIDs, req.FolderID)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"assets": assets})
	})
	r.POST("/teams/:teamId/assets/import", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		policy, available := loadRuntimePolicy(c, svc)
		if !available || !enforceRateLimit(c, "team-assets-import:"+user.ID, policy.Request.AssetWritePerMinute, time.Minute) {
			return
		}
		var req struct {
			AssetIDs []string `json:"assetIds"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		assets, err := svc.ImportTeamAssets(user, c.Param("teamId"), req.AssetIDs)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"imported": assets})
	})
	r.PATCH("/teams/:teamId/assets/:id/folder", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req struct {
			FolderID string `json:"folderId"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		asset, err := svc.MoveTeamAsset(user, c.Param("teamId"), c.Param("id"), req.FolderID)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"asset": asset})
	})
	r.DELETE("/teams/:teamId/assets/:id", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.DeleteTeamAsset(user, c.Param("teamId"), c.Param("id")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"id": c.Param("id")})
	})

	r.GET("/teams/:teamId/asset-folders", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		folders, err := svc.TeamAssetFolders(user, c.Param("teamId"))
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"folders": folders})
	})
	r.POST("/teams/:teamId/asset-folders", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		folder, err := svc.CreateTeamAssetFolder(user, c.Param("teamId"), req.Name)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"folder": folder})
	})
	r.PATCH("/teams/:teamId/asset-folders/:id", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req struct {
			Name string `json:"name"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		folder, err := svc.RenameTeamAssetFolder(user, c.Param("teamId"), c.Param("id"), req.Name)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"folder": folder})
	})
	r.DELETE("/teams/:teamId/asset-folders/:id", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		if err := svc.DeleteTeamAssetFolder(user, c.Param("teamId"), c.Param("id")); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"id": c.Param("id")})
	})
}
