// Package logging 统一应用日志入口、输出格式和运行时级别
package logging

import (
	"context"
	"io"
	"log"
	"log/slog"
	"strings"
)

var level slog.LevelVar

// Configure 在启动阶段设置全局日志输出, 运行期间调整级别使用 SetLevel
func Configure(output io.Writer, minimum slog.Level) {
	level.Set(minimum)
	slog.SetDefault(slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: &level})))
}

// ConfigureStreams 将 ERROR 及以上日志写入 errorOutput, 其余日志写入 output
func ConfigureStreams(output, errorOutput io.Writer, minimum slog.Level) {
	level.Set(minimum)
	options := &slog.HandlerOptions{Level: &level}
	slog.SetDefault(slog.New(streamHandler{
		output:      slog.NewTextHandler(output, options),
		errorOutput: slog.NewTextHandler(errorOutput, options),
	}))
}

// Logger 返回当前全局 slog 实例, 未配置时使用标准库默认实例
func Logger() *slog.Logger { return slog.Default() }

// SetLevel 并发安全地调整日志处理器的最低级别
func SetLevel(minimum slog.Level) { level.Set(minimum) }

// Writer 将第三方组件的文本日志转入当前全局日志, 每次 Write 生成一条记录
func Writer(level slog.Level) io.Writer { return logWriter{level: level} }

// StdLogger 为 net/http 等使用 log.Logger 的组件提供统一日志入口
func StdLogger(level slog.Level) *log.Logger { return log.New(Writer(level), "", 0) }

type streamHandler struct {
	output      slog.Handler
	errorOutput slog.Handler
}

func (h streamHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handlerFor(level).Enabled(ctx, level)
}

func (h streamHandler) Handle(ctx context.Context, record slog.Record) error {
	return h.handlerFor(record.Level).Handle(ctx, record)
}

func (h streamHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return streamHandler{output: h.output.WithAttrs(attrs), errorOutput: h.errorOutput.WithAttrs(attrs)}
}

func (h streamHandler) WithGroup(name string) slog.Handler {
	return streamHandler{output: h.output.WithGroup(name), errorOutput: h.errorOutput.WithGroup(name)}
}

func (h streamHandler) handlerFor(level slog.Level) slog.Handler {
	if level >= slog.LevelError {
		return h.errorOutput
	}
	return h.output
}

type logWriter struct{ level slog.Level }

// Write 保留组件日志内容, 换行由全局 handler 转义为单条记录
func (w logWriter) Write(p []byte) (int, error) {
	message := strings.TrimSuffix(string(p), "\n")
	if message != "" {
		Logger().Log(context.Background(), w.level, message)
	}
	return len(p), nil
}
