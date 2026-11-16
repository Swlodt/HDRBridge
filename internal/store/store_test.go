package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"HDRBridge/internal/store/sqlc"
)

func TestOpenInitializesStore(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	s := openTestStore(t, root)

	for _, name := range []string{"images", "staging"} {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if !info.IsDir() {
			t.Errorf("%s 应为目录, 得到 %v", name, info.Mode())
		}
	}
	info, err := os.Stat(filepath.Join(root, "hdrbridge.db"))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Errorf("hdrbridge.db 应为普通文件, 得到 %v", info.Mode())
	}
	checkAlbum(t, s, "uncategorized", "未分类")
}

func TestReopenPreservesAlbums(t *testing.T) {
	root := filepath.Join(t.TempDir(), "store")
	if !t.Run("首次打开并写入", func(t *testing.T) {
		s := openTestStore(t, root)
		_, err := s.db.ExecContext(t.Context(), "INSERT INTO albums (id, name) VALUES (?, ?)", "travel", "旅行")
		if err != nil {
			t.Fatal(err)
		}
	}) {
		return
	}

	s := openTestStore(t, root)
	checkAlbum(t, s, "travel", "旅行")
	checkAlbum(t, s, "uncategorized", "未分类")
}

func openTestStore(t *testing.T, root string) *Store {
	t.Helper()
	s, err := Open(t.Context(), root)
	if err != nil {
		t.Fatalf("Open(%q): %v", root, err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("关闭存储 %q: %v", root, err)
		}
	})
	return s
}

func checkAlbum(t *testing.T, s *Store, id, wantName string) {
	t.Helper()
	var count int
	var name string
	err := s.db.QueryRowContext(t.Context(),
		"SELECT COUNT(*), COALESCE(MIN(name), '') FROM albums WHERE id = ?", id,
	).Scan(&count, &name)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 || name != wantName {
		t.Errorf("相册 %q: 得到 %d 条, 名称 %q, 期望 1 条且名称为 %q", id, count, name, wantName)
	}
}

func TestOpenFailureReleasesDirectoryLock(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if db, err := Open(ctx, root); err == nil {
		_ = db.Close()
		t.Fatal("已取消的迁移不应成功")
	}
	openTestStore(t, root)
}

func TestOpenExcludesSecondProcessOwner(t *testing.T) {
	root := t.TempDir()
	openTestStore(t, root)
	if other, err := Open(t.Context(), root); err == nil {
		_ = other.Close()
		t.Fatal("同一数据目录不能被重复持有")
	}
}

func TestWithTxRollsBackPanicAndReleasesConnection(t *testing.T) {
	s := openTestStore(t, t.TempDir())
	failure := errors.New("事务回调 panic")
	func() {
		defer func() {
			if got := recover(); got != failure {
				t.Errorf("未保留 panic: %v", got)
			}
		}()
		_ = s.WithTx(t.Context(), func(q *sqlc.Queries) error {
			if _, err := q.CreateAlbum(t.Context(), sqlc.CreateAlbumParams{ID: "rollback", Name: "回滚"}); err != nil {
				t.Fatal(err)
			}
			panic(failure)
		})
	}()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := s.Queries().GetAlbum(ctx, "rollback"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("panic 后未回滚事务或释放连接: %v", err)
	}
}
