package library

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"HDRBridge/internal/testutil"
)

func TestRecoveryDefersUnreadableGarbage(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("权限故障测试需要非 root 用户")
	}
	db := openTestStore(t, t.TempDir())
	c := NewService(db, nil, nil)
	for _, root := range []string{db.ImagesRoot(), db.StagingRoot()} {
		dir := filepath.Join(root, "orphan")
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "data"), []byte("keep until retry"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
	}
	for range 2 {
		if err := c.RecoverFiles(t.Context()); err != nil {
			t.Fatalf("垃圾目录不应阻止启动: %v", err)
		}
	}
	pending, err := pendingFiles(t, db)
	if err != nil || len(pending) != 2 {
		t.Fatalf("恢复应幂等地记录两个区域: %+v, %v", pending, err)
	}
	if err := c.CleanupFiles(t.Context()); err == nil {
		t.Fatal("应保留无权限清理的记录")
	}
	pending, err = pendingFiles(t, db)
	if err != nil || len(pending) != 2 {
		t.Fatalf("失败记录丢失: %+v, %v", pending, err)
	}
	for _, root := range []string{db.ImagesRoot(), db.StagingRoot()} {
		if err := os.Chmod(filepath.Join(root, "orphan"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.CleanupFiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{db.ImagesRoot(), db.StagingRoot()} {
		if _, err := os.Stat(filepath.Join(root, "orphan")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("重试未删除残留: %v", err)
		}
	}
}

func TestRecoveryRejectsUnsafeStartup(t *testing.T) {
	t.Run("存储根目录不可访问", func(t *testing.T) {
		db := openTestStore(t, t.TempDir())
		if err := os.Remove(db.ImagesRoot()); err != nil {
			t.Fatal(err)
		}
		if err := NewService(db, nil, nil).RecoverFiles(t.Context()); err == nil {
			t.Fatal("缺少根目录时不能启动")
		}
	})
	t.Run("无法持久化清理任务", func(t *testing.T) {
		db := openTestStore(t, t.TempDir())
		if err := os.Mkdir(filepath.Join(db.StagingRoot(), "unfinished"), 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := testutil.Database(t, db).ExecContext(t.Context(), `CREATE TRIGGER fail_cleanup BEFORE INSERT ON pending_file_deletions BEGIN SELECT RAISE(ABORT, 'forced failure'); END`); err != nil {
			t.Fatal(err)
		}
		if err := NewService(db, nil, nil).RecoverFiles(t.Context()); err == nil {
			t.Fatal("数据库错误不能降级为清理告警")
		}
	})
}

func TestRecoveryCommitsBoundedBatches(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	c := NewService(db, nil, nil)
	for i := range 2*cleanupBatchSize + 1 {
		if err := os.Mkdir(filepath.Join(db.StagingRoot(), fmt.Sprintf("orphan-%03d", i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	raw := testutil.Database(t, db)
	trigger := fmt.Sprintf(`CREATE TRIGGER fail_recovery BEFORE INSERT ON pending_file_deletions WHEN NEW.entry_id = 'orphan-%03d' BEGIN SELECT RAISE(ABORT, 'forced failure'); END`, cleanupBatchSize+cleanupBatchSize/2)
	if _, err := raw.ExecContext(t.Context(), trigger); err != nil {
		t.Fatal(err)
	}
	if err := c.RecoverFiles(t.Context()); err == nil {
		t.Fatal("应返回恢复批次失败")
	}
	pending, err := pendingFiles(t, db)
	if err != nil || len(pending) != cleanupBatchSize {
		t.Fatalf("首批应提交, 第二批应完整回滚: %d, %v", len(pending), err)
	}
	if _, err := raw.ExecContext(t.Context(), "DROP TRIGGER fail_recovery"); err != nil {
		t.Fatal(err)
	}
	if err := c.RecoverFiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	pending, err = pendingFiles(t, db)
	if err != nil || len(pending) != 2*cleanupBatchSize+1 {
		t.Fatalf("重试应补齐全部记录且无重复: %d, %v", len(pending), err)
	}
	if err := c.CleanupFiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(db.StagingRoot())
	if err != nil || len(files) != 0 {
		t.Fatalf("残留目录未清理: %v, %v", files, err)
	}
}

func TestRecoverFilesPreservesSourceAndArtifacts(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	s := NewService(db, nil, nil)
	seedTestImage(t, db, uncategorizedID, "known")
	dir := filepath.Join(db.ImagesRoot(), "known")
	for _, name := range []string{filepath.Join(db.ImagesRoot(), "orphan"), filepath.Join(db.StagingRoot(), "unfinished")} {
		if err := os.MkdirAll(name, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecoverFiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.CleanupFiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"source.jxr", "output.heic", "thumbnail.jpg"} {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err != nil || string(data) != "image" {
			t.Fatalf("恢复改变了已入库图片文件 %s: %q, %v", name, data, err)
		}
	}
	for _, name := range []string{filepath.Join(db.ImagesRoot(), "orphan"), filepath.Join(db.StagingRoot(), "unfinished")} {
		if _, err := os.Stat(name); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("未清理中断残留 %s: %v", name, err)
		}
	}
}
