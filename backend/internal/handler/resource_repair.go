package handler

import (
	"net/http"
	"time"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func registerResourceRepairRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.POST("/resources/:id/repair-references", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		policy, available := loadRuntimePolicy(c, svc)
		if !available || !enforceRateLimit(c, "resources-repair:"+user.ID, policy.Request.AssetWritePerMinute, time.Minute) {
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
		var req struct {
			ReplacementResourceID string `json:"replacementResourceId"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			fail(c, http.StatusBadRequest, service.BadAuthRequest("资源引用修复请求格式错误"))
			return
		}
		if err := svc.RepairResourceReferences(user.ID, c.Param("id"), req.ReplacementResourceID); err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"repaired": true})
	})
}
