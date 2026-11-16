package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type failingCloser struct {
	calls int
	err   error
}

func (c *failingCloser) Close() error { c.calls++; return c.err }

func TestCloseLogsOnlyFailures(t *testing.T) {
	previous := Logger()
	t.Cleanup(func() { slog.SetDefault(previous) })
	var output bytes.Buffer
	Configure(&output, slog.LevelInfo)
	success := &failingCloser{}
	func() { defer CloseResource(success, "successful") }()
	if success.calls != 1 || output.Len() != 0 {
		t.Fatalf("正常关闭不应输出日志: calls=%d, %s", success.calls, &output)
	}
	failure := &failingCloser{err: errors.New("close failed\ndetail")}
	func() { defer CloseResource(failure, "upload.part") }()
	text := output.String()
	if failure.calls != 1 || strings.Count(text, "\n") != 1 || !strings.Contains(text, "level=WARN") || !strings.Contains(text, "resource=upload.part") || !strings.Contains(text, `close failed\ndetail`) {
		t.Fatalf("关闭失败应仅记录一行并保留资源与原因: calls=%d, %s", failure.calls, text)
	}
}
