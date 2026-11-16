package library

import (
	"errors"
	"slices"
	"testing"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/testutil"
)

func TestPaginationKeepsCountsAcrossSearchAndCursors(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	s := NewService(db, nil, nil)
	if _, err := testutil.Database(t, db).ExecContext(t.Context(), `
 INSERT INTO albums(id, name) VALUES ('other', '其他');
 WITH seeds(id, album_id, original_name, created_at) AS (VALUES
 ('a', 'uncategorized', 'match.jxr', '2026-01-01T00:00:00.000Z'),
 ('b', 'uncategorized', 'other.jxr', '2026-01-01T00:00:00.000Z'),
 ('c', 'uncategorized', 'MATCH.jxr', '2026-01-01T00:00:00.000Z'),
 ('d', 'uncategorized', 'other.jxr', '2026-01-01T00:00:00.000Z'),
 ('e', 'uncategorized', 'other.jxr', '2026-01-02T00:00:00.000Z'),
 ('f', 'uncategorized', 'match.jxr', '2026-01-02T00:00:00.000Z'),
 ('g', 'uncategorized', 'other.jxr', '2026-01-02T00:00:00.000Z'),
 ('h', 'uncategorized', 'Match.jxr', '2026-01-02T00:00:00.000Z'),
 ('z', 'other', 'match.jxr', '2026-01-03T00:00:00.000Z')
 )
 INSERT INTO images (id, album_id, original_name, created_at, source_sha256)
 SELECT id, album_id, original_name, created_at, CAST(printf('%032s', id) AS BLOB) FROM seeds;`); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		search string
		want   []string
	}{
		{"", []string{"h", "g", "f", "e", "d", "c", "b", "a"}},
		{" match ", []string{"h", "f", "c", "a"}},
		{"missing", nil},
	} {
		t.Run(tc.search, func(t *testing.T) {
			var got []string
			input := PageInput{Search: tc.search, Limit: 1}
			for pages := 0; ; pages++ {
				if pages > len(tc.want) {
					t.Fatal("游标未结束")
				}
				page, err := s.PageByAlbum(t.Context(), "uncategorized", input)
				if err != nil {
					t.Fatal(err)
				}
				if page.Matched != int64(len(tc.want)) || page.Total != 8 {
					t.Fatalf("分页或搜索改变了统计: %+v", page)
				}
				for _, item := range page.Images {
					got = append(got, item.ID)
				}
				if page.NextCursor == nil {
					break
				}
				input.Cursor = *page.NextCursor
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("分页重复、遗漏或排序错误: %v, 期望 %v", got, tc.want)
			}
		})
	}
}

func TestListByAlbumRequiresExistingAlbum(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	s := NewService(db, nil, nil)
	if _, err := s.PageByAlbum(t.Context(), "missing", PageInput{}); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("不存在的相册不能返回空图片列表: %v", err)
	}
	page, err := s.PageByAlbum(t.Context(), "uncategorized", PageInput{})
	images := page.Images
	if err != nil || images == nil || len(images) != 0 {
		t.Fatalf("空相册应返回空切片: %+v, %v", images, err)
	}
}
