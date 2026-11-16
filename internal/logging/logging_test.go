package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestConfigureAndRuntimeLevel(t *testing.T) {
	previous, previousLevel := Logger(), level.Level()
	t.Cleanup(func() { slog.SetDefault(previous); SetLevel(previousLevel) })
	var output, errorOutput bytes.Buffer
	ConfigureStreams(&output, &errorOutput, slog.LevelInfo)
	Logger().Debug("hidden")
	if output.Len() != 0 {
		t.Fatalf("INFO 级别不应输出 DEBUG: %s", &output)
	}
	SetLevel(slog.LevelDebug)
	logger := Logger().WithGroup("image").With("id", "photo")
	logger.Debug("visible")
	logger.Error("failed")
	if text := output.String(); strings.Count(text, "\n") != 1 || !strings.Contains(text, "level=DEBUG") || !strings.Contains(text, "image.id=photo") {
		t.Fatalf("动态级别或结构化字段未生效: %s", text)
	}
	if text := errorOutput.String(); strings.Count(text, "\n") != 1 || !strings.Contains(text, "level=ERROR") || !strings.Contains(text, "image.id=photo") {
		t.Fatalf("错误日志应单独输出并保留结构化字段: %s", text)
	}
}

func TestAdaptersUseCurrentLoggerAndSingleLineRecords(t *testing.T) {
	previous, previousLevel := Logger(), level.Level()
	t.Cleanup(func() { slog.SetDefault(previous); SetLevel(previousLevel) })
	var first, output, errorOutput bytes.Buffer
	Configure(&first, slog.LevelInfo)
	writer := Writer(slog.LevelWarn)
	standard := StdLogger(slog.LevelError)
	ConfigureStreams(&output, &errorOutput, slog.LevelInfo)
	message := "panic detail\nstack detail\n"
	n, err := writer.Write([]byte(message))
	if err != nil || n != len(message) {
		t.Fatalf("日志适配器写入结果错误: %d, %v", n, err)
	}
	standard.Print("server error")
	if first.Len() != 0 {
		t.Fatalf("适配器仍使用旧 logger: %s", &first)
	}
	if text := output.String(); strings.Count(text, "\n") != 1 || !strings.Contains(text, "level=WARN") || !strings.Contains(text, `panic detail\nstack detail`) {
		t.Fatalf("WARN 日志应在普通输出中保持单行: %s", text)
	}
	if text := errorOutput.String(); strings.Count(text, "\n") != 1 || !strings.Contains(text, "level=ERROR") || !strings.Contains(text, "server error") {
		t.Fatalf("标准库适配器应将 ERROR 写入错误输出: %s", text)
	}
}
