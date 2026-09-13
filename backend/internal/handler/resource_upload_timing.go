package handler

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

// Only fixed stage names and elapsed times are exposed, never storage credentials or URLs.
func appendUploadTiming(c *gin.Context, stage string, started time.Time) {
	c.Writer.Header().Add("Server-Timing", fmt.Sprintf("%s;dur=%.1f", stage, float64(time.Since(started).Microseconds())/1000))
}
