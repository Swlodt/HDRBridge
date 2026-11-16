package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"HDRBridge/internal/testutil"
)

func TestCleanupFilesRetriesAfterReopen(t *testing.T) {
	root := t.TempDir()
	imageDir := filepath.Join(root, "images", "deleted")
	if !t.Run("清理失败保留记录", func(t *testing.T) {
		db := openTestStore(t, root)
		if err := os.MkdirAll(imageDir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(imageDir, "source.jxr"), []byte("image"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := testutil.Database(t, db).ExecContext(t.Context(), "INSERT INTO pending_file_deletions(area, entry_id) VALUES ('images', 'deleted'), ('images', 'already-missing')"); err != nil {
			t.Fatal(err)
		}
		hidden := filepath.Join(root, "unavailable")
		if err := os.Rename(db.ImagesRoot(), hidden); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := os.Rename(hidden, db.ImagesRoot()); err != nil {
				t.Error(err)
			}
		}()
		if err := NewService(db, nil, nil).CleanupFiles(t.Context()); err == nil {
			t.Fatal("目录不可用时应报告清理失败")
		}
		ids, err := pendingFiles(t, db)
		if err != nil || len(ids) != 2 {
			t.Fatalf("失败后丢失了待清理记录: %v, %v", ids, err)
		}
	}) {
		return
	}
	db := openTestStore(t, root)
	if _, err := os.Stat(imageDir); err != nil {
		t.Fatalf("打开存储不应清理图片文件: %v", err)
	}
	ids, err := pendingFiles(t, db)
	if err != nil || len(ids) != 2 {
		t.Fatalf("打开存储不应移除待清理记录: %v, %v", ids, err)
	}
	if err := NewService(db, nil, nil).CleanupFiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(imageDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("重启后图片目录应已清理: %v", err)
	}
	ids, err = pendingFiles(t, db)
	if err != nil || len(ids) != 0 {
		t.Fatalf("成功清理后记录未移除: %v, %v", ids, err)
	}
}

func TestCleanupFilesConfinesDeletionToImageDirectories(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	outside := t.TempDir()
	marker := filepath.Join(outside, "keep")
	if err := os.WriteFile(marker, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(db.ImagesRoot(), "link")); err != nil {
		t.Fatal(err)
	}
	invalid := []string{".", "..", "../outside", outside, "nested/photo", `nested\photo`}
	for _, id := range append(invalid, "link") {
		if _, err := testutil.Database(t, db).ExecContext(t.Context(), "INSERT INTO pending_file_deletions(area, entry_id) VALUES ('images', ?)", id); err != nil {
			t.Fatal(err)
		}
	}
	if err := NewService(db, nil, nil).CleanupFiles(t.Context()); err == nil {
		t.Fatal("无效目录 ID 应报告错误")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "keep" {
		t.Fatalf("清理越过了 images 目录: %q, %v", data, err)
	}
	if _, err := os.Lstat(filepath.Join(db.ImagesRoot(), "link")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("应只删除 symlink 本身: %v", err)
	}
	ids, err := pendingFiles(t, db)
	if err != nil || len(ids) != len(invalid) {
		t.Fatalf("失败记录数量错误: %v, %v", ids, err)
	}
}
