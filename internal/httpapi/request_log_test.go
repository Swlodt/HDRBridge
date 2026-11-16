package httpapi

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"

	"HDRBridge/internal/config"

	"github.com/gin-gonic/gin"
)

func TestRequestLogging(t *testing.T) {
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
	var output bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	router := New(nil, config.Upload{})
	router.GET("/log-test", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	router.GET("/log-panic", func(c *gin.Context) { panic("test panic") })
	router.GET("/log-disconnect", func(c *gin.Context) { panic(syscall.EPIPE) })

	for _, tc := range []struct {
		name   string
		target string
		status int
		fields [][]string
	}{
		{
			name:   "访问日志",
			target: "/log-test?page=2",
			status: http.StatusNoContent,
			fields: [][]string{{"level=INFO", "method=GET", `path="/log-test?page=2"`, "status=204", "duration=", "client_ip=192.0.2.1"}},
		},
		{
			name:   "panic 恢复",
			target: "/log-panic",
			status: http.StatusInternalServerError,
			fields: [][]string{
				{"level=ERROR", "path=/log-panic", `panic="test panic"`, "stack=", "request_log_test.go"},
				{"level=INFO", "path=/log-panic", "status=500"},
			},
		},
		{
			name:   "连接中断",
			target: "/log-disconnect",
			status: http.StatusOK,
			fields: [][]string{{"level=ERROR", "path=/log-disconnect", "errors=", syscall.EPIPE.Error()}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output.Reset()
			request := httptest.NewRequest(http.MethodGet, tc.target, nil)
			request.RemoteAddr = "192.0.2.1:1234"
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.status {
				t.Fatalf("HTTP 状态码 = %d, 期望 %d", response.Code, tc.status)
			}
			lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
			if len(lines) != len(tc.fields) {
				t.Fatalf("日志行数 = %d, 期望 %d: %s", len(lines), len(tc.fields), &output)
			}
			for i, line := range lines {
				if strings.Count(line, "time=") != 1 || strings.Contains(line, "[GIN]") || strings.Contains(line, "[Recovery]") {
					t.Errorf("每条日志应仅由 slog 格式化一次: %s", line)
				}
				for _, field := range tc.fields[i] {
					if !strings.Contains(line, field) {
						t.Errorf("缺少日志字段 %q: %s", field, line)
					}
				}
			}
		})
	}
}
