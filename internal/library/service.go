// Package library 管理相册、图片及其持久化文件的生命周期
package library

import (
	"context"
	"database/sql"
	"errors"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/store"
)

// Service 通过同一存储协调相册、图片和文件清理
type Service struct {
	db          *store.Store
	converter   Converter
	previewer   Previewer
	wake        chan struct{}
	cleanupGate chan struct{}
}

// Converter 在独占的暂存目录内生成 HEIC, 返回前必须停止写入
type Converter interface {
	Convert(ctx context.Context, dir string) error
}

// Previewer 在上传暂存目录中生成位图预览, 返回前必须停止写入
type Previewer interface {
	MakePreview(ctx context.Context, source, output string) error
}

// NewService 复用调用方存储, 上传需要提供对应格式的处理器
func NewService(db *store.Store, converter Converter, previewer Previewer) *Service {
	s := &Service{
		db: db, converter: converter, previewer: previewer,
		wake: make(chan struct{}, 1), cleanupGate: make(chan struct{}, 1),
	}
	s.cleanupGate <- struct{}{}
	return s
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return apperr.ErrNotFound
	}
	return err
}
