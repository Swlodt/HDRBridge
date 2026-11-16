package httpapi

import (
	"HDRBridge/internal/apperr"
	"HDRBridge/internal/config"
	"HDRBridge/internal/library"
	"HDRBridge/web"

	"github.com/gin-gonic/gin"
)

type api struct {
	catalog *library.Service
	upload  config.Upload
}

// New 组装 API 和嵌入的 Web 路由, 不启动监听或后台 goroutine
func New(catalog *library.Service, upload config.Upload) *gin.Engine {
	service := api{catalog: catalog, upload: upload}

	r := gin.New()
	r.Use(logRequest, gin.CustomRecoveryWithWriter(nil, logRecoveredPanic))
	_ = r.SetTrustedProxies(nil)

	api := r.Group("/api")

	api.GET("/albums", service.listAlbums)
	api.POST("/albums", service.createAlbum)
	api.GET("/albums/:id", service.getAlbum)
	api.PATCH("/albums/:id", service.updateAlbum)
	api.DELETE("/albums/:id", service.deleteAlbum)
	api.GET("/albums/:id/images", service.listImages)
	api.POST("/albums/:id/images", service.uploadImages)
	api.GET("/images", service.listImages)
	api.GET("/images/:id", service.getImage)
	api.PATCH("/images/:id", service.moveImage)
	api.DELETE("/images/:id", service.deleteImage)
	api.GET("/images/:id/thumbnail", service.thumbnail)
	api.GET("/images/:id/preview", service.preview)
	api.GET("/images/:id/download", service.download)

	var frontend web.Handler
	r.NoRoute(func(c *gin.Context) {
		if frontend.Serve(c.Writer, c.Request) {
			return
		}
		problem(c, apperr.ErrNotFound)
	})

	return r
}
