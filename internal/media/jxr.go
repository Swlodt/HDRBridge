package media

import (
	"context"
	"errors"
	"fmt"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"HDRBridge/internal/config"
	"HDRBridge/internal/logging"
)

// JXR 在调用方的暂存目录中同步生成产物
type JXR struct {
	tools   config.Tools
	timeout time.Duration
}

// NewJXR 解析工具的可执行路径, 不启动任何子进程
func NewJXR(tools config.Tools, settings config.Convert) (*JXR, error) {
	for _, path := range []*string{&tools.JXRDec, &tools.Magick, &tools.Encoder} {
		resolved, err := exec.LookPath(*path)
		if err != nil {
			return nil, fmt.Errorf("查找转换工具 %q: %w", *path, err)
		}
		*path = resolved
	}
	return &JXR{tools: tools, timeout: settings.Timeout}, nil
}

// Convert 在独占的暂存目录中同步生成 HEIC 和可选的 SDR 缩略图
func (p *JXR) Convert(ctx context.Context, dir string) (err error) {
	requestCtx := ctx
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	work := filepath.Join(dir, workDir)
	if err := os.Mkdir(work, 0700); err != nil {
		return err
	}
	defer func() {
		if cleanupErr := os.RemoveAll(work); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("清理转换临时目录: %w", cleanupErr))
		}
	}()
	tiff, exr := filepath.Join(work, "intermediate.tif"), filepath.Join(work, "intermediate.exr")
	if _, err := runTool(ctx, p.tools.JXRDec, "-i", filepath.Join(dir, SourceName), "-o", tiff); err != nil {
		return fmt.Errorf("JXR → TIFF: %w", err)
	}
	// scRGB 浮点数保留线性亮度, 避免写 EXR 前发生 SDR gamma 转换或裁切
	if _, err := runTool(ctx, p.tools.Magick, tiff, "-set", "colorspace", "RGB", "-define", "quantum:format=floating-point", "-depth", "16", exr); err != nil {
		return fmt.Errorf("TIFF → EXR: %w", err)
	}
	if _, err := runTool(ctx, p.tools.Encoder, exr, work, "-q", "1", "-d", "10", "-r", "10", "-R", "14"); err != nil {
		return fmt.Errorf("EXR → HEIC: %w", err)
	}
	output := filepath.Join(work, "intermediate.heic")
	info, err := os.Stat(output)
	if err != nil {
		return fmt.Errorf("检查 HEIC: %w", err)
	}
	if info.Size() == 0 {
		return errors.New("HEIC 文件为空")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(output, filepath.Join(dir, HDRName)); err != nil {
		return fmt.Errorf("保存 HEIC: %w", err)
	}
	thumbnail := filepath.Join(work, ThumbnailName)
	// EXR 读回时可能被标记为 sRGB, 需恢复线性 RGB 标记以正确编码 JPEG 亮度
	// 从 0.75 开始平滑压缩高光, 以像素峰值等比缩放各通道来减少色偏
	_, err = runTool(ctx, p.tools.Magick, exr,
		"-set", "colorspace", "RGB", "-resize", "512x512>",
		"-channel", "RGB", "-fx", "peak=max(0.75,max(u.r,max(u.g,u.b))); excess=peak-0.75; max(0,u)*(0.75+excess/(1+4*excess))/peak", "+channel",
		"-colorspace", "sRGB", "-quality", "85", thumbnail)
	if err == nil {
		err = validateJPEG(thumbnail)
	}
	if err == nil {
		err = os.Rename(thumbnail, filepath.Join(dir, ThumbnailName))
	}
	// 主产物已完成, 处理时限耗尽只影响可选缩略图, 请求取消仍须终止上传
	if requestCtx.Err() != nil {
		return requestCtx.Err()
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		logging.Logger().Warn("HEIC 已生成, 缩略图生成失败", "image_id", filepath.Base(dir), "error", err)
	}
	return nil
}

func validateJPEG(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer logging.CloseResource(f, path)
	_, err = jpeg.Decode(f)
	return err
}
