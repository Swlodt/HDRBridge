package logging

import "io"

// CloseResource 关闭资源, 失败时记录一行日志, 不替代需要向调用方返回关闭错误的操作
func CloseResource(closer io.Closer, name string) {
	if err := closer.Close(); err != nil {
		Logger().Warn("关闭资源失败", "resource", name, "error", err)
	}
}
