package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/library"

	"github.com/gin-gonic/gin"
)

func TestProblemClassifiesApplicationErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"内部包装", fmt.Errorf("save /private/data: %w", apperr.New(apperr.ErrInvalid, "名称不能为空")), 400, "参数无效: 名称不能为空"},
		{"底层原因", apperr.Wrap(apperr.ErrInvalid, "JSON 数据不正确", errors.New("internal decoder detail")), 400, "参数无效: JSON 数据不正确"},
		{"普通分类包装", fmt.Errorf("internal detail: %w", apperr.ErrConflict), 409, "操作冲突"},
		{"参数错误", apperr.New(apperr.ErrInvalid, "名称不能为空"), 400, "参数无效: 名称不能为空"},
		{"资源不存在", fmt.Errorf("查询相册: %w", apperr.ErrNotFound), 404, "资源不存在"},
		{"业务冲突", apperr.New(apperr.ErrConflict, "当前状态不允许操作"), 409, "操作冲突: 当前状态不允许操作"},
		{"相册保护", fmt.Errorf("删除相册: %w", library.ErrProtectedAlbum), 409, "操作冲突: 不能修改或删除内置未分类相册"},
		{"转换失败", apperr.Wrap(apperr.ErrInternal, "JXR 转换失败", errors.New("/private/encoder")), 500, "服务器内部错误: JXR 转换失败"},
		{"未经转换的 SQL 错误", sql.ErrNoRows, 500, "服务器内部错误"},
		{"包装的 SQL 错误", fmt.Errorf("读取内部记录: %w", sql.ErrNoRows), 500, "服务器内部错误"},
		{"未经转换的文件错误", fs.ErrNotExist, 500, "服务器内部错误"},
		{"存储目录丢失", &fs.PathError{Op: "open", Path: "/data/images", Err: fs.ErrNotExist}, 500, "服务器内部错误"},
		{"同文案不同错误", errors.New(apperr.ErrNotFound.Error()), 500, "服务器内部错误"},
		{"未知错误", errors.New("数据库连接失败"), 500, "服务器内部错误"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/albums/test", nil)
			problem(c, tc.err)
			if response.Code != tc.status {
				t.Fatalf("应返回 %d, 得到 %d: %s", tc.status, response.Code, response.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 1 || body["error"] != tc.message {
				t.Fatalf("错误响应为 %v, 期望 %q", body, tc.message)
			}
		})
	}
}
