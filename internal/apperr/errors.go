// Package apperr 定义跨业务包的错误分类, 不依赖 HTTP 或存储实现
package apperr

import "errors"

var (
	// ErrInvalid 表示调用参数不满足接口或业务约束
	ErrInvalid = errors.New("参数无效")
	// ErrNotFound 表示请求的业务资源不存在
	ErrNotFound = errors.New("资源不存在")
	// ErrConflict 表示资源当前状态不允许执行请求的操作
	ErrConflict = errors.New("操作冲突")
	// ErrTooLarge 表示上传文件或请求体超过允许大小
	ErrTooLarge = errors.New("上传大小超限")
	// ErrInternal 允许内部错误附带可公开的提示, 底层原因仅写入日志
	ErrInternal = errors.New("服务器内部错误")
)

// publicError 将面向用户的提示与底层原因分开, 外层包装不会改变公开文案
type publicError struct {
	kind    error
	message string
	cause   error
}

// New 创建带稳定公开提示的分类错误, message 不得包含内部路径或底层错误
func New(kind error, message string) error {
	return &publicError{kind: kind, message: message}
}

// Wrap 保留底层错误供日志排障, 只公开指定的 message
func Wrap(kind error, message string, cause error) error {
	return &publicError{kind: kind, message: message, cause: cause}
}

// Error 保留公开提示与底层原因, 仅供内部诊断
func (e *publicError) Error() string {
	message := e.kind.Error() + ": " + e.message
	if e.cause != nil {
		return message + ": " + e.cause.Error()
	}
	return message
}

// Unwrap 保留底层错误链
func (e *publicError) Unwrap() error { return e.cause }

// Is 使分类错误可通过 errors.Is 判断
func (e *publicError) Is(target error) bool { return errors.Is(e.kind, target) }

// Message 返回公开提示, 忽略错误链中的内部包装, 未分类错误使用通用文案
func Message(err error) string {
	if public, ok := errors.AsType[*publicError](err); ok {
		return public.kind.Error() + ": " + public.message
	}
	for _, kind := range []error{ErrTooLarge, ErrInvalid, ErrNotFound, ErrConflict} {
		if errors.Is(err, kind) {
			return kind.Error()
		}
	}
	return ErrInternal.Error()
}
