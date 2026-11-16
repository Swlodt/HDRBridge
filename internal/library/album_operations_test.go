package library

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/store"
	"HDRBridge/internal/testutil"
)

func TestAlbumPartialUpdate(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	s := NewService(db, nil, nil)
	created := createTestAlbum(t, s)
	if _, err := testutil.Database(t, db).ExecContext(t.Context(), "UPDATE albums SET updated_at = '2000-01-01T00:00:00.000Z' WHERE id = ?", created.ID); err != nil {
		t.Fatal(err)
	}
	name := "  新名称  "
	updated, err := s.UpdateAlbum(t.Context(), created.ID, AlbumUpdate{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Name != "新名称" || updated.Description != created.Description || updated.CreatedAt != created.CreatedAt || updated.UpdatedAt == "2000-01-01T00:00:00.000Z" {
		t.Fatalf("重命名未保留字段或未更新时间: %+v", updated)
	}
	desc := "  新描述  "
	updated, err = s.UpdateAlbum(t.Context(), created.ID, AlbumUpdate{Description: &desc})
	if err != nil || updated.Name != "新名称" || updated.Description != "新描述" {
		t.Fatalf("单独更新描述失败: %+v, %v", updated, err)
	}
	desc = ""
	updated, err = s.UpdateAlbum(t.Context(), created.ID, AlbumUpdate{Description: &desc})
	if err != nil || updated.Description != "" || updated.Name != "新名称" {
		t.Fatalf("清空描述失败: %+v, %v", updated, err)
	}
	got, err := s.GetAlbum(t.Context(), created.ID)
	if err != nil || got != updated {
		t.Fatalf("更新结果未持久化: %+v, %v", got, err)
	}
	name, desc = strings.Repeat("旅", 120), strings.Repeat("图", 2000)
	if _, err := s.UpdateAlbum(t.Context(), created.ID, AlbumUpdate{Name: &name, Description: &desc}); err != nil {
		t.Fatalf("字段长度上限应合法: %v", err)
	}
}

func TestAlbumInvalidOperations(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	s := NewService(db, nil, nil)
	created := createTestAlbum(t, s)
	blank, longName, longDesc := " \t\n", strings.Repeat("旅", 121), strings.Repeat("图", 2001)
	for _, input := range []AlbumUpdate{
		{}, {Name: &blank}, {Name: &longName}, {Description: &longDesc},
	} {
		if _, err := s.UpdateAlbum(t.Context(), created.ID, input); !errors.Is(err, apperr.ErrInvalid) {
			t.Fatalf("无效更新应被拒绝: %v", err)
		}
	}
	got, err := s.GetAlbum(t.Context(), created.ID)
	if err != nil || got != created {
		t.Fatalf("无效更新改变了相册: %+v, %v", got, err)
	}
	if _, err := s.UpdateAlbum(t.Context(), uncategorizedID, AlbumUpdate{Name: &created.Name}); !errors.Is(err, ErrProtectedAlbum) || !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("内置相册应禁止更新: %v", err)
	}
	for _, deleteImages := range []bool{false, true} {
		if err := s.DeleteAlbum(t.Context(), uncategorizedID, deleteImages); !errors.Is(err, ErrProtectedAlbum) || !errors.Is(err, apperr.ErrConflict) {
			t.Fatalf("内置相册应禁止删除: %v", err)
		}
		if err := s.DeleteAlbum(t.Context(), "missing", deleteImages); !errors.Is(err, apperr.ErrNotFound) {
			t.Fatalf("删除不存在的相册应返回不存在: %v", err)
		}
	}
	if _, err := s.GetAlbum(t.Context(), "missing"); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("查询不存在的相册: %v", err)
	}
	if _, err := s.UpdateAlbum(t.Context(), "missing", AlbumUpdate{Name: &created.Name}); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("更新不存在的相册: %v", err)
	}
}

func TestConcurrentPartialUpdates(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	s := NewService(db, nil, nil)
	created := createTestAlbum(t, s)
	name, description := "新名称", "新描述"
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, input := range []AlbumUpdate{{Name: &name}, {Description: &description}} {
		go func() {
			<-start
			_, err := s.UpdateAlbum(t.Context(), created.ID, input)
			results <- err
		}()
	}
	close(start)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.GetAlbum(t.Context(), created.ID)
	if err != nil || got.Name != name || got.Description != description {
		t.Fatalf("并发部分更新丢失字段: %+v, %v", got, err)
	}
}

func TestDeleteAlbumMovesImages(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	s := NewService(db, nil, nil)
	created := createTestAlbum(t, s)
	other := createTestAlbum(t, s)
	seedTestImage(t, db, other.ID, "other")
	seedTestImage(t, db, uncategorizedID, "existing")
	for _, id := range []string{"a", "b", "c", "d"} {
		seedTestImage(t, db, created.ID, id)
	}
	if err := s.DeleteAlbum(t.Context(), created.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAlbum(t.Context(), created.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("相册未删除: %v", err)
	}
	page, err := s.PageByAlbum(t.Context(), uncategorizedID, PageInput{})
	images := page.Images
	if err != nil || len(images) != 5 {
		t.Fatalf("图片未完整转入未分类: %+v, %v", images, err)
	}
	for _, item := range images {
		if item.AlbumID != uncategorizedID {
			t.Errorf("图片未移入未分类: %+v", item)
		}
		assertTestImageFile(t, db, item.ID, true)
	}
	page, err = s.PageByAlbum(t.Context(), other.ID, PageInput{})
	remaining := page.Images
	if err != nil || len(remaining) != 1 || remaining[0].ID != "other" {
		t.Fatalf("删除影响了其他相册: %+v, %v", remaining, err)
	}
	assertNoPendingDeletions(t, db)
}

func TestDeleteAlbumWithImages(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	s := NewService(db, nil, nil)
	created := createTestAlbum(t, s)
	seedTestImage(t, db, uncategorizedID, "keep")
	for _, id := range []string{"a", "b", "c"} {
		seedTestImage(t, db, created.ID, id)
	}
	if err := s.DeleteAlbum(t.Context(), created.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAlbum(t.Context(), created.ID); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("相册未删除: %v", err)
	}
	var count int
	if err := testutil.Database(t, db).QueryRowContext(t.Context(), "SELECT COUNT(*) FROM images WHERE album_id = ?", created.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("图片记录未删除: %d, %v", count, err)
	}
	if err := s.CleanupFiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} {
		assertTestImageFile(t, db, id, false)
	}
	assertTestImageFile(t, db, "keep", true)
	assertNoPendingDeletions(t, db)
}

func TestDeleteAlbumRollsBack(t *testing.T) {
	for _, deleteImages := range []bool{false, true} {
		t.Run(map[bool]string{false: "移动图片", true: "删除图片"}[deleteImages], func(t *testing.T) {
			db := openTestStore(t, t.TempDir())
			s := NewService(db, nil, nil)
			created := createTestAlbum(t, s)
			seedTestImage(t, db, created.ID, "photo")
			if _, err := testutil.Database(t, db).ExecContext(t.Context(), `CREATE TRIGGER fail_delete BEFORE DELETE ON albums BEGIN SELECT RAISE(ABORT, 'forced failure'); END`); err != nil {
				t.Fatal(err)
			}
			if err := s.DeleteAlbum(t.Context(), created.ID, deleteImages); err == nil {
				t.Fatal("应返回数据库错误")
			}
			page, err := s.PageByAlbum(t.Context(), created.ID, PageInput{})
			images := page.Images
			if err != nil || len(images) != 1 || images[0].ID != "photo" {
				t.Fatalf("事务失败后图片归属未回滚: %+v, %v", images, err)
			}
			assertTestImageFile(t, db, "photo", true)
			assertNoPendingDeletions(t, db)
		})
	}
}

func createTestAlbum(t *testing.T, s *Service) Album {
	t.Helper()
	created, err := s.CreateAlbum(t.Context(), "旅行", "描述")
	if err != nil {
		t.Fatal(err)
	}
	return created
}

func seedTestImage(t *testing.T, db *store.Store, albumID, id string) {
	t.Helper()
	dir := filepath.Join(db.ImagesRoot(), id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"source.jxr", "output.heic", "thumbnail.jpg", "intermediate.exr"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("image"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	checksum := sha256.Sum256([]byte(id))
	_, err := testutil.Database(t, db).ExecContext(t.Context(), `INSERT INTO images (id, album_id, original_name, source_sha256) VALUES (?, ?, ?, ?)`,
		id, albumID, id+".jxr", checksum[:])
	if err != nil {
		t.Fatal(err)
	}
}

func assertTestImageFile(t *testing.T, db *store.Store, id string, exists bool) {
	t.Helper()
	if !exists {
		if _, err := os.Lstat(filepath.Join(db.ImagesRoot(), id)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("图片目录 %q 应已删除: %v", id, err)
		}
		return
	}
	data, err := os.ReadFile(filepath.Join(db.ImagesRoot(), id, "source.jxr"))
	if err != nil || string(data) != "image" {
		t.Fatalf("图片文件 %q 发生变化: %q, %v", id, data, err)
	}
}

func assertNoPendingDeletions(t *testing.T, db *store.Store) {
	t.Helper()
	var count int
	err := testutil.Database(t, db).QueryRowContext(t.Context(), "SELECT COUNT(*) FROM pending_file_deletions").Scan(&count)
	if err != nil || count != 0 {
		t.Fatalf("存在意外的待清理记录: %v, %v", count, err)
	}
}
