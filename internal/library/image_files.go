package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/logging"
	"HDRBridge/internal/media"

	"golang.org/x/sys/unix"
)

const (
	originalName      = "source"
	rasterPreviewName = "preview.png"
)

type imageFile uint8

const (
	// hdrFile 是发布的 HEIC 产物
	hdrFile imageFile = iota + 1
	// thumbnailFile 是 JXR 转换生成的 SDR 缩略图
	thumbnailFile
	// originalFile 是未经 HDR 转换的普通图片原件
	originalFile
	// rasterPreviewFile 是普通位图的 PNG 预览
	rasterPreviewFile
)

// filename 返回固定文件名, 拒绝未知类型以免误打开其他产物
func (k imageFile) filename() (string, error) {
	switch k {
	case hdrFile:
		return media.HDRName, nil
	case thumbnailFile:
		return media.ThumbnailName, nil
	case originalFile:
		return originalName, nil
	case rasterPreviewFile:
		return rasterPreviewName, nil
	default:
		return "", fmt.Errorf("无效的图片文件类型: %d", k)
	}
}

// validImageID 限制 ID 为单层本地目录名, 不要求历史记录使用 UUID
func validImageID(id string) bool {
	return filepath.IsLocal(id) && id != "." && !strings.ContainsAny(id, `/\`) && !strings.ContainsRune(id, 0)
}

// Artifact 包含可用于 HTTP 响应的文件和元数据, 调用方负责关闭 File
type Artifact struct {
	File      *os.File
	Name      string
	MediaType string
}

// OpenPreview 按请求的尺寸和浏览器可显示的格式选择预览
func (s *Service) OpenPreview(ctx context.Context, id string, thumbnail bool) (Artifact, error) {
	row, err := s.db.Queries().GetImage(ctx, id)
	if err != nil {
		return Artifact{}, notFound(err)
	}
	source := media.Format(row.SourceMediaType)
	switch {
	case source == media.JPEGXR:
		return s.openArtifact(id, thumbnailFile, "", "image/jpeg")
	case source == media.SVG || (!thumbnail && source.BrowserPreview()):
		return s.openArtifact(id, originalFile, "", string(source))
	default:
		return s.openArtifact(id, rasterPreviewFile, "", "image/png")
	}
}

// OpenDownload 保留普通图片原件, JXR 仅提供成功发布的 HEIC
func (s *Service) OpenDownload(ctx context.Context, id string) (Artifact, error) {
	row, err := s.db.Queries().GetImage(ctx, id)
	if err != nil {
		return Artifact{}, notFound(err)
	}
	name := path.Base(strings.ReplaceAll(row.OriginalName, `\`, "/"))
	if media.Format(row.SourceMediaType) != media.JPEGXR {
		return s.openArtifact(id, originalFile, name, row.SourceMediaType)
	}
	name = strings.TrimSuffix(name, path.Ext(name)) + ".heic"
	return s.openArtifact(id, hdrFile, name, "image/heic")
}

func (s *Service) openArtifact(id string, kind imageFile, name, mediaType string) (Artifact, error) {
	f, err := s.openFile(id, kind)
	return Artifact{File: f, Name: name, MediaType: mediaType}, err
}

func (s *Service) openFile(id string, kind imageFile) (*os.File, error) {
	if !validImageID(id) {
		return nil, fmt.Errorf("无效的图片目录 ID: %q", id)
	}
	name, err := kind.filename()
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(s.db.ImagesRoot())
	if err != nil {
		return nil, err
	}
	defer logging.CloseResource(root, s.db.ImagesRoot())
	// 普通文件不受 O_NONBLOCK 影响, 意外出现的 FIFO 则不会在 Stat 前永久阻塞
	f, err := root.OpenFile(filepath.Join(id, name), os.O_RDONLY|unix.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, apperr.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		logging.CloseResource(f, f.Name())
		if err == nil {
			err = errors.New("图片文件不是普通文件")
		}
		return nil, err
	}
	return f, nil
}
