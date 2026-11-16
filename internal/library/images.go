package library

import (
	"context"
	"strings"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/media"
	"HDRBridge/internal/store/sqlc"
)

// Image 仅暴露相册浏览所需的元数据, 不向客户端提供磁盘路径
type Image struct {
	ID              string       `json:"id"`
	AlbumID         string       `json:"album_id"`
	OriginalName    string       `json:"original_name"`
	SourceMediaType media.Format `json:"source_media_type"`
	HEICHasGainMap  *bool        `json:"heic_has_gain_map,omitempty"`
	CreatedAt       string       `json:"created_at"`
	UpdatedAt       string       `json:"updated_at"`
}

func presentImage(row sqlc.Image) Image {
	image := Image{ID: row.ID, AlbumID: row.AlbumID, OriginalName: row.OriginalName,
		SourceMediaType: media.Format(row.SourceMediaType),
		CreatedAt:       row.CreatedAt, UpdatedAt: row.UpdatedAt}
	if row.HeicHasGainMap.Valid {
		image.HEICHasGainMap = &row.HeicHasGainMap.Bool
	}
	return image
}

// GetImage 返回图片的公开元数据
func (s *Service) GetImage(ctx context.Context, id string) (Image, error) {
	row, err := s.db.Queries().GetImage(ctx, id)
	return presentImage(row), notFound(err)
}

// MoveImage 只变更相册归属, 磁盘文件由图片 ID 定位
func (s *Service) MoveImage(ctx context.Context, id, albumID string) (Image, error) {
	if strings.TrimSpace(albumID) == "" {
		return Image{}, apperr.New(apperr.ErrInvalid, "album_id 不能为空")
	}
	var row sqlc.Image
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.GetImage(ctx, id); err != nil {
			return notFound(err)
		}
		if _, err := q.GetAlbum(ctx, albumID); err != nil {
			return notFound(err)
		}
		var err error
		row, err = q.MoveImage(ctx, sqlc.MoveImageParams{ID: id, AlbumID: albumID})
		return notFound(err)
	})
	if err != nil {
		return Image{}, err
	}
	return presentImage(row), nil
}
