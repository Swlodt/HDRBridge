package httpapi

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"HDRBridge/internal/logging"

	"github.com/gin-gonic/gin"
)

func logRequest(c *gin.Context) {
	start := time.Now()
	path := c.Request.URL.Path
	if query := c.Request.URL.RawQuery; query != "" {
		path += "?" + query
	}
	c.Next()
	duration := time.Since(start)

	attrs := []slog.Attr{
		slog.String("method", c.Request.Method),
		slog.String("path", path),
		slog.Int("status", c.Writer.Status()),
		slog.Duration("duration", duration),
		slog.String("client_ip", c.ClientIP()),
	}
	level := slog.LevelInfo
	if requestErrors := c.Errors.ByType(gin.ErrorTypePrivate).Errors(); len(requestErrors) > 0 {
		// Gin 在连接中断时将错误交给 c.Errors, 不调用自定义 recovery 回调
		attrs = append(attrs, slog.Any("errors", requestErrors))
		level = slog.LevelError
	}
	logging.Logger().LogAttrs(c.Request.Context(), level, "HTTP", attrs...)
}

func logRecoveredPanic(c *gin.Context, recovered any) {
	logging.Logger().LogAttrs(c.Request.Context(), slog.LevelError, "HTTP 请求发生 panic",
		slog.String("method", c.Request.Method),
		slog.String("path", c.Request.URL.Path),
		slog.Any("panic", recovered),
		slog.String("stack", string(debug.Stack())),
	)
	c.AbortWithStatus(http.StatusInternalServerError)
}
