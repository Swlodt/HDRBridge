package library

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/store/sqlc"
)

// PageInput 指定文件名搜索词和游标
type PageInput struct {
	Search string
	Cursor string
	Limit  int
}

// ImagePage 返回当前页、搜索匹配数量和相册或全库的图片总数
type ImagePage struct {
	Images     []Image `json:"images"`
	NextCursor *string `json:"next_cursor"`
	Matched    int64   `json:"matched"`
	Total      int64   `json:"total"`
}

type pageCursor struct {
	Time string `json:"t"`
	ID   string `json:"i"`
}

// PageByAlbum 在同一快照中读取相册的图片和数量
func (s *Service) PageByAlbum(ctx context.Context, id string, in PageInput) (ImagePage, error) {
	return s.page(ctx, &id, in)
}

// PageAll 在同一快照中读取全库的图片和数量
func (s *Service) PageAll(ctx context.Context, in PageInput) (ImagePage, error) {
	return s.page(ctx, nil, in)
}

func (s *Service) page(ctx context.Context, albumID *string, in PageInput) (ImagePage, error) {
	if in.Limit == 0 {
		in.Limit = 50
	}
	if in.Limit < 1 || in.Limit > 100 {
		return ImagePage{}, apperr.New(apperr.ErrInvalid, "limit 必须为 1–100")
	}
	in.Search = strings.TrimSpace(in.Search)
	if utf8.RuneCountInString(in.Search) > 512 || len(in.Cursor) > 1024 {
		return ImagePage{}, apperr.New(apperr.ErrInvalid, "搜索或游标参数过长")
	}
	var cursor pageCursor
	if in.Cursor != "" {
		data, err := base64.RawURLEncoding.DecodeString(in.Cursor)
		if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.ID == "" || len(cursor.ID) > 256 {
			return ImagePage{}, apperr.New(apperr.ErrInvalid, "无效的分页游标")
		}
		if _, err := time.Parse(time.RFC3339Nano, cursor.Time); err != nil {
			return ImagePage{}, apperr.New(apperr.ErrInvalid, "无效的分页游标")
		}
	}
	result := ImagePage{Images: []Image{}}
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		var err error
		if albumID == nil {
			result.Total, err = q.CountImages(ctx)
		} else {
			if _, err := q.GetAlbum(ctx, *albumID); err != nil {
				return notFound(err)
			}
			result.Total, err = q.CountAlbumImages(ctx, *albumID)
		}
		if err != nil {
			return err
		}
		if in.Search == "" {
			result.Matched = result.Total
		} else if albumID == nil {
			result.Matched, err = q.CountMatchingImages(ctx, in.Search)
		} else {
			result.Matched, err = q.CountMatchingAlbumImages(ctx, sqlc.CountMatchingAlbumImagesParams{AlbumID: *albumID, Search: in.Search})
		}
		if err != nil {
			return err
		}
		rows, err := readImagePage(ctx, q, albumID, in, cursor)
		if err != nil {
			return err
		}
		if len(rows) > in.Limit {
			rows = rows[:in.Limit]
			last := rows[len(rows)-1]
			data, err := json.Marshal(pageCursor{Time: last.CreatedAt, ID: last.ID})
			if err != nil {
				return err
			}
			next := base64.RawURLEncoding.EncodeToString(data)
			result.NextCursor = &next
		}
		for _, row := range rows {
			result.Images = append(result.Images, presentImage(row))
		}
		return nil
	})
	if err != nil {
		return ImagePage{}, err
	}
	return result, nil
}

func readImagePage(ctx context.Context, q *sqlc.Queries, albumID *string, in PageInput, cursor pageCursor) ([]sqlc.Image, error) {
	limit := int64(in.Limit + 1)
	if albumID == nil {
		if in.Cursor == "" {
			return q.PageImages(ctx, sqlc.PageImagesParams{Search: in.Search, PageLimit: limit})
		}
		return q.PageImagesBefore(ctx, sqlc.PageImagesBeforeParams{Search: in.Search, CursorTime: cursor.Time, CursorID: cursor.ID, PageLimit: limit})
	}
	if in.Cursor == "" {
		return q.PageAlbumImages(ctx, sqlc.PageAlbumImagesParams{AlbumID: *albumID, Search: in.Search, PageLimit: limit})
	}
	return q.PageAlbumImagesBefore(ctx, sqlc.PageAlbumImagesBeforeParams{AlbumID: *albumID, Search: in.Search, CursorTime: cursor.Time, CursorID: cursor.ID, PageLimit: limit})
}
