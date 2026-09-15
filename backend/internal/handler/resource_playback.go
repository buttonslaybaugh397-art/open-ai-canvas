package handler

import (
	"net/http"
	"time"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func registerResourcePlaybackRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.POST("/resources/:id/playback", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		policy, available := loadRuntimePolicy(c, svc)
		if !available || !enforceRateLimit(c, "resources-playback:"+user.ID, policy.Request.ResourceImportPerMinute, time.Minute) {
			return
		}
		resource, err := svc.RequestResourcePlayback(c.Request.Context(), user.ID, c.Param("id"))
		if err != nil {
			if appErr, ok := err.(*service.AppError); ok && appErr.Status == http.StatusTooManyRequests {
				c.Header("Retry-After", "5")
			}
			failService(c, err)
			return
		}
		ok(c, gin.H{"resource": resource})
	})
}
