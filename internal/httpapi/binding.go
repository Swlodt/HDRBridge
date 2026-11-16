package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"HDRBridge/internal/apperr"

	"github.com/gin-gonic/gin"
)

type optionalString struct {
	value *string
}

// UnmarshalJSON 区分未传字段与空字符串, 拒绝将 null 静默视为未传
func (s *optionalString) UnmarshalJSON(data []byte) error {
	var value *string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if value == nil {
		return errors.New("字段必须为字符串, 不能为 null")
	}
	s.value = value
	return nil
}

func bind(c *gin.Context, v any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(v); err != nil {
		problem(c, apperr.Wrap(apperr.ErrInvalid, "JSON 数据不正确", err))
		return false
	}
	// 完整读取请求, 防止尾随 JSON 或空白绕过请求体大小限制
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			err = errors.New("只允许一个 JSON 对象")
		}
		problem(c, apperr.Wrap(apperr.ErrInvalid, "JSON 数据不正确", err))
		return false
	}
	return true
}
