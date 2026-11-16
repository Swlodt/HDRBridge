package library

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/testutil"
)

func TestCreateFieldLimits(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	s := NewService(db, nil, nil)
	name := strings.Repeat("旅", 120)
	desc := strings.Repeat("📷", 2000)
	created, err := s.CreateAlbum(t.Context(), name, desc)
	if err != nil {
		t.Fatalf("名称和描述恰好达到上限时创建失败: %v", err)
	}
	if created.Name != name || created.Description != desc {
		t.Fatal("达到上限的名称或描述未完整保留")
	}

	before, err := s.ListAlbums(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ label, name, desc string }{
		{"名称全为空白", " \t\n", ""},
		{"名称超过上限", name + "旅", ""},
		{"描述超过上限", "旅行", desc + "📷"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			if _, err := s.CreateAlbum(t.Context(), tc.name, tc.desc); !errors.Is(err, apperr.ErrInvalid) {
				t.Errorf("无效输入应返回参数错误: %v", err)
			}
			after, err := s.ListAlbums(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(after, before) {
				t.Errorf("拒绝创建后列表发生变化, 原有 %d 条, 现有 %d 条", len(before), len(after))
			}
		})
	}
}

func TestListOrder(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	s := NewService(db, nil, nil)
	// 固定时间和 ID, 让插入顺序与预期顺序不同, 同时覆盖时间相同的情况
	_, err := testutil.Database(t, db).ExecContext(t.Context(), `
		INSERT INTO albums (id, name, created_at) VALUES
			('b', '乙', '2000-01-02T00:00:00.000Z'),
			('z', '早', '2000-01-01T00:00:00.000Z'),
			('a', '甲', '2000-01-02T00:00:00.000Z')`)
	if err != nil {
		t.Fatal(err)
	}
	albums, err := s.ListAlbums(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, a := range albums {
		ids = append(ids, a.ID)
	}
	want := []string{"uncategorized", "z", "a", "b"}
	if !slices.Equal(ids, want) {
		t.Errorf("相册顺序为 %v, 期望 %v", ids, want)
	}
}
