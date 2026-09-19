package handler

import (
	"io"
	"net/http"
	"strings"

	"infinite-canvas/backend/internal/service"

	"github.com/gin-gonic/gin"
)

func RegisterAdminStorageRoutes(r *gin.RouterGroup, svc *service.Service) {
	r.POST("/admin/resources/delete", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		var req service.AdminResourceDeleteRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			failService(c, service.BadAuthRequest("删除资源请求无效"))
			return
		}
		result, err := svc.DeleteAdminResources(user, req)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.GET("/admin/storage/stats", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		stats, err := svc.AdminStorageStats(user)
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, gin.H{"stats": stats})
	})

	r.GET("/admin/resources", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		page, limit, err := parsePaginationQuery(c, 20)
		if err != nil {
			fail(c, http.StatusBadRequest, err)
			return
		}
		result, err := svc.AdminResourcePage(user, service.AdminResourceQuery{
			Keyword: c.Query("keyword"), Kind: c.Query("kind"), Status: c.Query("status"),
			Provider: c.Query("provider"), UserID: c.Query("userId"), Page: page, Limit: limit,
		})
		if err != nil {
			failService(c, err)
			return
		}
		ok(c, result)
	})

	r.GET("/admin/resources/:id/file", func(c *gin.Context) {
		user, err := currentUser(c, svc)
		if err != nil {
			failService(c, err)
			return
		}
		downloadFileName := ""
		if c.Query("download") == "1" {
			downloadFileName = strings.TrimSpace(c.Query("filename"))
			if downloadFileName == "" {
				downloadFileName = c.Param("id")
			}
		}
		delivery, err := svc.PrepareResourceDeliveryAsAdmin(user, c.Param("id"), service.ResourceDeliveryOptions{
			Context:          c.Request.Context(),
			ForceDirect:      c.Query("direct") == "1",
			ForceProxy:       c.Query("proxy") == "1",
			DownloadFileName: downloadFileName,
		})
		if err != nil {
			failService(c, err)
			return
		}
		if c.Query("resolve") == "1" {
			c.Header("Cache-Control", "private, no-store")
			c.Header("Referrer-Policy", "no-referrer")
			ok(c, gin.H{"url": delivery.RedirectURL})
			return
		}
		if delivery.RedirectURL != "" {
			c.Header("Cache-Control", "private, no-store")
			c.Header("Vary", "Cookie")
			c.Header("Referrer-Policy", "no-referrer")
			c.Header("X-Content-Type-Options", "nosniff")
			c.Redirect(http.StatusTemporaryRedirect, delivery.RedirectURL)
			return
		}
		stream, err := svc.OpenResourceRangeAsAdmin(user, c.Param("id"), c.GetHeader("Range"))
		if err != nil {
			failService(c, err)
			return
		}
		defer stream.Body.Close()
		mimeType := stream.Resource.MimeType
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		c.Header("Cache-Control", "private, no-cache")
		c.Header("Accept-Ranges", stream.AcceptRanges)
		c.Header("X-Content-Type-Options", "nosniff")
		if stream.ContentRange != "" {
			c.Header("Content-Range", stream.ContentRange)
		}
		if downloadFileName != "" {
			c.Header("Content-Disposition", attachmentContentDisposition(downloadFileName))
		}
		if seeker, ok := stream.Body.(io.ReadSeeker); ok {
			c.Header("Content-Type", mimeType)
			http.ServeContent(c.Writer, c.Request, stream.Resource.ID, stream.Resource.UpdatedAt, seeker)
			return
		}
		c.DataFromReader(stream.StatusCode, stream.ContentLength, mimeType, stream.Body, nil)
	})
}
