package library

import (
	"context"
	"database/sql"
	"strings"
	"unicode/utf8"
	"uuid"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/store/sqlc"
)

const uncategorizedID = "uncategorized"

// ErrProtectedAlbum 表示不能修改或删除内置未分类相册
var ErrProtectedAlbum = apperr.New(apperr.ErrConflict, "不能修改或删除内置未分类相册")

// Album 是相册的业务视图, 不向 HTTP 层暴露 sqlc 生成类型
type Album struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// AlbumUpdate 使用 nil 保留原字段, 空描述则显式清空描述
type AlbumUpdate struct {
	Name        *string
	Description *string
}

// CreateAlbum 校验并创建相册, 名称允许重复
func (s *Service) CreateAlbum(ctx context.Context, name, desc string) (Album, error) {
	name, desc, err := checkFields(name, desc)
	if err != nil {
		return Album{}, err
	}
	row, err := s.db.Queries().CreateAlbum(ctx, sqlc.CreateAlbumParams{
		ID:          uuid.NewV7().String(),
		Name:        name,
		Description: desc,
	})
	return presentAlbum(row), err
}

// ListAlbums 返回按创建时间排列的相册, 未分类固定在首位
func (s *Service) ListAlbums(ctx context.Context) ([]Album, error) {
	albums, err := s.db.Queries().Albums(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Album, 0, len(albums))
	for _, row := range albums {
		result = append(result, presentAlbum(row))
	}
	return result, nil
}

// GetAlbum 返回指定相册, 不存在时返回 apperr.ErrNotFound
func (s *Service) GetAlbum(ctx context.Context, id string) (Album, error) {
	res, err := s.db.Queries().GetAlbum(ctx, id)
	return presentAlbum(res), notFound(err)
}

// UpdateAlbum 原子更新显式提供的字段, 保留其他字段的并发修改
func (s *Service) UpdateAlbum(ctx context.Context, id string, in AlbumUpdate) (Album, error) {
	if err := checkAlbumMutable(id); err != nil {
		return Album{}, err
	}
	if in.Name == nil && in.Description == nil {
		return Album{}, apperr.New(apperr.ErrInvalid, "至少提供 name 或 description")
	}
	params := sqlc.UpdateAlbumParams{ID: id}
	if in.Name != nil {
		name, err := checkName(*in.Name)
		if err != nil {
			return Album{}, err
		}
		params.Name = sql.NullString{String: name, Valid: true}
	}
	if in.Description != nil {
		desc, err := checkDescription(*in.Description)
		if err != nil {
			return Album{}, err
		}
		params.Description = sql.NullString{String: desc, Valid: true}
	}
	res, err := s.db.Queries().UpdateAlbum(ctx, params)
	return presentAlbum(res), notFound(err)
}

func checkAlbumMutable(id string) error {
	if id == uncategorizedID {
		return ErrProtectedAlbum
	}
	return nil
}

func checkFields(name, desc string) (string, string, error) {
	name, err := checkName(name)
	if err != nil {
		return "", "", err
	}
	desc, err = checkDescription(desc)
	return name, desc, err
}

func checkName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 120 {
		return "", apperr.New(apperr.ErrInvalid, "名称为 1–120 字")
	}
	if strings.ContainsRune(name, 0) {
		return "", apperr.New(apperr.ErrInvalid, "名称不能包含 NUL 字符")
	}
	return name, nil
}

func checkDescription(desc string) (string, error) {
	desc = strings.TrimSpace(desc)
	if utf8.RuneCountInString(desc) > 2000 {
		return "", apperr.New(apperr.ErrInvalid, "描述最多 2000 字")
	}
	return desc, nil
}

func presentAlbum(row sqlc.Album) Album {
	return Album{
		ID: row.ID, Name: row.Name, Description: row.Description,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}
