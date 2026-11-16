package media

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"strings"
	"time"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/logging"
)

// Raster 同步生成 PNG 预览, 并发准入由调用层负责
type Raster struct {
	program string
	timeout time.Duration
}

// NewRaster 使用系统 sips, timeout 包括生成预览和失败诊断的时间
func NewRaster(timeout time.Duration) *Raster {
	return &Raster{program: "/usr/bin/sips", timeout: timeout}
}

// MakePreview 生成最长边不超过 512 像素的 PNG, 仅明确的输入问题返回参数错误
func (r *Raster) MakePreview(ctx context.Context, source, output string) error {
	requestCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	diagnostic, err := runTool(ctx, r.program, "-s", "format", "png", "-Z", "512", source, "--out", output)
	if err != nil {
		return r.failure(requestCtx, ctx, source, err)
	}
	// sips 跳过无法解码的文件时可能仍返回成功, 不能只依赖退出状态
	f, err := os.Open(output)
	if err != nil {
		return r.failure(requestCtx, ctx, source, fmt.Errorf("打开 PNG 预览: %w: %s", err, strings.TrimSpace(diagnostic)))
	}
	defer logging.CloseResource(f, output)
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return previewFailure(fmt.Errorf("校验 PNG 预览: %w", err))
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 512 || cfg.Height > 512 {
		return previewFailure(errors.New("生成的 PNG 预览尺寸无效"))
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return previewFailure(err)
	}
	if _, err := png.Decode(f); err != nil {
		return previewFailure(fmt.Errorf("解码 PNG 预览: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return previewContextError(requestCtx, err)
	}
	if err := f.Sync(); err != nil {
		return previewFailure(fmt.Errorf("同步 PNG 预览: %w", err))
	}
	return nil
}

func (r *Raster) failure(requestCtx, ctx context.Context, source string, cause error) error {
	if err := ctx.Err(); err != nil {
		return previewContextError(requestCtx, errors.Join(err, cause))
	}
	// 文件不可访问或工具异常属于服务端故障, 不能误报为用户上传了坏图
	f, err := os.Open(source)
	if err != nil {
		return previewFailure(errors.Join(cause, err))
	}
	logging.CloseResource(f, source)
	// 仅在失败路径查询输入元数据, 避免为正常上传额外启动进程
	metadata, err := runTool(ctx, r.program, "-g", "pixelWidth", "-g", "pixelHeight", source)
	if ctx.Err() != nil {
		return previewContextError(requestCtx, errors.Join(ctx.Err(), cause, err))
	}
	if err != nil {
		return previewFailure(errors.Join(cause, err))
	}
	for line := range strings.SplitSeq(metadata, "\n") {
		switch strings.TrimSpace(line) {
		case "pixelWidth: <nil>", "pixelHeight: <nil>":
			return apperr.Wrap(apperr.ErrInvalid, "无法读取图片以生成预览，文件可能已损坏或编码不受支持", cause)
		}
	}
	return previewFailure(cause)
}

func previewContextError(requestCtx context.Context, cause error) error {
	if err := requestCtx.Err(); err != nil {
		return err
	}
	return apperr.Wrap(apperr.ErrInternal, "生成图片预览超时，请稍后重试", cause)
}

func previewFailure(cause error) error {
	return apperr.Wrap(apperr.ErrInternal, "生成图片预览失败，请查看服务日志", cause)
}
