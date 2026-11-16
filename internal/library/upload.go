package library

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/logging"
	"HDRBridge/internal/media"
	"HDRBridge/internal/store/sqlc"
)

var errImageExists = apperr.New(apperr.ErrConflict, "图片已存在")

// Upload 在 staging 中完成转换或预览生成, 再发布文件和元数据
func (s *Service) Upload(ctx context.Context, albumID, filename string, source io.Reader, maxBytes int64) (Image, error) {
	filename = path.Base(strings.ReplaceAll(filename, `\`, "/"))
	if filename == "." || strings.ContainsRune(filename, 0) {
		return Image{}, apperr.New(apperr.ErrInvalid, "文件名无效")
	}
	if maxBytes < 4 || maxBytes >= 1<<62 {
		return Image{}, fmt.Errorf("无效的上传大小配置")
	}
	if _, err := s.db.Queries().GetAlbum(ctx, albumID); err != nil {
		return Image{}, notFound(err)
	}
	id := uuid.NewV7().String()
	dir := filepath.Join(s.db.StagingRoot(), id)
	if err := os.Mkdir(dir, 0700); err != nil {
		return Image{}, err
	}
	defer s.cleanupUpload(ctx, "staging", id, dir)
	part := filepath.Join(dir, originalName) + ".part"
	f, err := os.OpenFile(part, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return Image{}, err
	}
	closed := false
	defer func() {
		if !closed {
			logging.CloseResource(f, part)
		}
	}()
	checksum := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, checksum), io.LimitReader(contextReader{ctx, source}, maxBytes+1))
	if err != nil {
		return Image{}, err
	}
	if n > maxBytes {
		return Image{}, apperr.New(apperr.ErrTooLarge, fmt.Sprintf("单个文件最多 %d 字节", maxBytes))
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Image{}, err
	}
	format, err := media.DetectFormat(f)
	if err != nil {
		return Image{}, err
	}
	if format == "" {
		return Image{}, apperr.New(apperr.ErrInvalid, "无法识别图片格式，或该格式暂不支持")
	}
	if !format.MatchesExtension(path.Ext(filename)) {
		return Image{}, apperr.New(apperr.ErrInvalid, "文件扩展名与图片内容不匹配")
	}
	digest := checksum.Sum(nil)
	exists, err := s.db.Queries().ImageChecksumExists(ctx, digest)
	if err != nil {
		return Image{}, err
	}
	if exists {
		return Image{}, errImageExists
	}
	if err := f.Sync(); err != nil {
		return Image{}, err
	}
	closeErr := f.Close()
	closed = true
	if closeErr != nil {
		return Image{}, closeErr
	}
	sourceName := originalName
	if format == media.JPEGXR {
		sourceName = media.SourceName
	}
	saved := filepath.Join(dir, sourceName)
	if err := os.Rename(part, saved); err != nil {
		return Image{}, err
	}
	var gainMap sql.NullBool
	if format == media.JPEGXR {
		if s.converter == nil {
			return Image{}, errors.New("未配置 JXR 转换器")
		}
		if err := s.converter.Convert(ctx, dir); err != nil {
			if ctx.Err() != nil {
				return Image{}, ctx.Err()
			}
			message := "JXR 转换失败，请查看服务日志"
			if errors.Is(err, context.DeadlineExceeded) {
				message = "JXR 转换超时，请重试或调整转换超时设置"
			}
			return Image{}, apperr.Wrap(apperr.ErrInternal, message, err)
		}
		hasGainMap, err := media.HEICHasGainMap(filepath.Join(dir, media.HDRName))
		if err != nil {
			logging.Logger().Warn("无法识别 HEIC 的增益图标记", "image_id", id, "error", err)
		} else {
			gainMap = sql.NullBool{Bool: hasGainMap, Valid: true}
		}
	} else if format != media.SVG {
		if s.previewer == nil {
			return Image{}, errors.New("未配置图片预览器")
		}
		if err := s.previewer.MakePreview(ctx, saved, filepath.Join(dir, rasterPreviewName)); err != nil {
			return Image{}, err
		}
	}
	finalDir := filepath.Join(s.db.ImagesRoot(), id)
	published := false
	moved := false
	defer func() {
		if moved && !published {
			s.cleanupUpload(ctx, "images", id, finalDir)
		}
	}()
	var row sqlc.Image
	err = s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.GetAlbum(ctx, albumID); err != nil {
			return notFound(err)
		}
		if err := os.Rename(dir, finalDir); err != nil {
			return err
		}
		moved = true
		var err error
		// 另一请求可能在转换期间已发布同一图片, 由唯一索引拦截重复写入
		row, err = q.CreateImage(ctx, sqlc.CreateImageParams{
			ID: id, AlbumID: albumID, OriginalName: filename, SourceMediaType: string(format),
			SourceSha256: digest, HeicHasGainMap: gainMap,
		})
		if errors.Is(err, sql.ErrNoRows) {
			return errImageExists
		}
		return err
	})
	if err != nil {
		return Image{}, err
	}
	published = true
	return presentImage(row), nil
}

func (s *Service) cleanupUpload(ctx context.Context, area, id, dir string) {
	if err := os.RemoveAll(dir); err != nil {
		// 请求取消后仍需保存清理任务, 但不能因数据库故障无限延长请求生命周期
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if queueErr := s.queueOrphan(cleanupCtx, area, id); queueErr != nil {
			logging.Logger().Warn("清理上传残留及登记重试失败", "path", dir, "error", err, "queue_error", queueErr)
			return
		}
		logging.Logger().Warn("上传残留已加入清理队列", "path", dir, "error", err)
	}
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
