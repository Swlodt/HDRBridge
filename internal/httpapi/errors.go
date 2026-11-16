package httpapi

import (
	"errors"
	"net/http"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/logging"

	"github.com/gin-gonic/gin"
)

func problem(c *gin.Context, err error) {
	code, msg := errorResponse(err)
	if code == http.StatusInternalServerError {
		logging.Logger().Error("请求失败", "path", c.Request.URL.Path, "error", err)
	}
	c.JSON(code, gin.H{"error": msg})
}

func errorResponse(err error) (int, string) {
	code := http.StatusInternalServerError
	msg := "服务器内部错误"
	switch {
	case errors.Is(err, apperr.ErrTooLarge):
		code = http.StatusRequestEntityTooLarge
		msg = apperr.Message(err)
	case errors.Is(err, apperr.ErrInvalid):
		code = http.StatusBadRequest
		msg = apperr.Message(err)
	case errors.Is(err, apperr.ErrNotFound):
		code = http.StatusNotFound
		msg = apperr.ErrNotFound.Error()
	case errors.Is(err, apperr.ErrConflict):
		code = http.StatusConflict
		msg = apperr.Message(err)
	case errors.Is(err, apperr.ErrInternal):
		msg = apperr.Message(err)
	}
	return code, msg
}
