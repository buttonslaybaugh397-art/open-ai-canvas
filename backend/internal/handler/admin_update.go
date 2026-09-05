package handler

import (
	"fmt"
	"mime"
	"net/http"
	"strings"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

const maxMigrationImportBytes int64 = 20<<30 + 1

func RegisterAdminUpdateRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.GET("/admin/system-update", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		status, err := svc.AdminUpdateStatus(c.Request.Context(), actor)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, status)
	})
	r.POST("/admin/system-update/check", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		status, err := svc.AdminCheckUpdate(c.Request.Context(), actor)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, status)
	})
	r.POST("/admin/system-update/start", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		var request struct {
			TargetVersion string `json:"targetVersion" binding:"required"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		status, err := svc.AdminStartUpdate(c.Request.Context(), actor, request.TargetVersion)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, status)
	})
	r.POST("/admin/system-update/rollback", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		var request struct {
			Reason string `json:"reason" binding:"required"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		status, err := svc.AdminRollbackUpdate(c.Request.Context(), actor, request.Reason)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, status)
	})
	r.POST("/admin/system-update/migration/export", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		status, err := svc.AdminStartMigrationExport(c.Request.Context(), actor)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, status)
	})
	r.POST("/admin/system-update/migration/import", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		contentLength := c.Request.ContentLength
		if contentLength <= 0 {
			failService(c, service.NewAppError(http.StatusBadRequest, "迁移包必须使用固定文件长度"))
			return
		}
		contentType := strings.TrimSpace(strings.Split(c.GetHeader("Content-Type"), ";")[0])
		if contentType != "application/zip" && contentType != "application/octet-stream" {
			failService(c, service.NewAppError(http.StatusUnsupportedMediaType, "迁移包必须是 ZIP 文件"))
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxMigrationImportBytes)
		status, err := svc.AdminImportMigration(c.Request.Context(), actor, contentLength, c.Request.Body)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, status)
	})
	r.GET("/admin/system-update/migration/download", func(c *gin.Context) {
		actor, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		archive, stream, err := svc.AdminOpenMigrationExport(c.Request.Context(), actor)
		if err != nil {
			failService(c, err)
			return
		}
		defer stream.Close()
		c.Header("Cache-Control", "private, no-store")
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Length", fmt.Sprintf("%d", archive.Size))
		c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": archive.ID + ".zip"}))
		c.Header("X-Migration-ID", archive.ID)
		c.Header("X-Migration-SHA256", archive.Checksum)
		c.Header("X-Migration-Version", archive.Version)
		c.DataFromReader(http.StatusOK, archive.Size, "application/zip", stream, nil)
	})
}
