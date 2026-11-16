package library

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"HDRBridge/internal/store/sqlc"
	"HDRBridge/internal/testutil"
)

func TestCleanupAdvancesPastFailedBatch(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	for i := range cleanupBatchSize + 1 {
		if err := db.Queries().QueueImageFileDeletion(t.Context(), fmt.Sprintf("../invalid-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	for i := range cleanupBatchSize + 1 {
		name := fmt.Sprintf("valid-%d", i)
		if err := os.Mkdir(filepath.Join(db.StagingRoot(), name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := db.Queries().QueueFileDeletion(t.Context(), sqlc.QueueFileDeletionParams{Area: "staging", EntryID: name}); err != nil {
			t.Fatal(err)
		}
	}
	if err := NewService(db, nil, nil).CleanupFiles(t.Context()); err == nil {
		t.Fatal("无效目录应报告错误")
	}
	entries, err := os.ReadDir(db.StagingRoot())
	if err != nil || len(entries) != 0 {
		t.Fatalf("首批失败阻塞了后续清理: %v, %v", entries, err)
	}
	pending, err := pendingFiles(t, db)
	if err != nil || len(pending) != cleanupBatchSize+1 {
		t.Fatalf("失败记录应完整保留: %v, %v", pending, err)
	}
}

func TestCleanupPassHasFiniteUpperBound(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	if err := db.Queries().QueueImageFileDeletion(t.Context(), "first"); err != nil {
		t.Fatal(err)
	}
	if _, err := testutil.Database(t, db).ExecContext(t.Context(), `CREATE TRIGGER add_more_cleanup AFTER DELETE ON pending_file_deletions BEGIN INSERT INTO pending_file_deletions(area, entry_id) VALUES ('images', 'next-' || OLD.id); END`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := NewService(db, nil, nil).CleanupFiles(ctx); err != nil {
		t.Fatalf("本轮不应追赶新增任务: %v", err)
	}
	pending, err := pendingFiles(t, db)
	if err != nil || len(pending) != 1 || pending[0].EntryID != "next-1" {
		t.Fatalf("新增记录应留给下轮: %+v, %v", pending, err)
	}
}

func TestDeletionDoesNotWaitForCleaner(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	c := NewService(db, nil, nil)
	for _, id := range []string{"one", "two"} {
		checksum := sha256.Sum256([]byte(id))
		if _, err := db.Queries().CreateImage(t.Context(), sqlc.CreateImageParams{
			ID: id, AlbumID: uncategorizedID, OriginalName: id + ".jxr", SourceMediaType: "image/jxr",
			SourceSha256: checksum[:],
		}); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(db.ImagesRoot(), id), 0700); err != nil {
			t.Fatal(err)
		}
	}
	<-c.cleanupGate
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err := c.DeleteImage(ctx, "one")
	c.cleanupGate <- struct{}{}
	if err != nil {
		t.Fatalf("删除请求等待了文件清理: %v", err)
	}
	if _, err := os.Stat(filepath.Join(db.ImagesRoot(), "one")); err != nil {
		t.Fatalf("未启动清理器时文件应保留: %v", err)
	}
	runCtx, stop := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() { defer close(done); c.maintain(runCtx, time.Hour) }()
	t.Cleanup(func() { stop(); <-done })
	waitRemoved(t, filepath.Join(db.ImagesRoot(), "one"))
	if err := c.DeleteImage(t.Context(), "two"); err != nil {
		t.Fatal(err)
	}
	waitRemoved(t, filepath.Join(db.ImagesRoot(), "two"))
}

func TestCleanupWaitCanBeCanceled(t *testing.T) {
	c := NewService(openTestStore(t, t.TempDir()), nil, nil)
	<-c.cleanupGate
	defer func() { c.cleanupGate <- struct{}{} }()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := c.CleanupFiles(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("等待清理锁未响应取消: %v", err)
	}
}

func waitRemoved(t *testing.T, path string) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return
		}
		select {
		case <-timer.C:
			t.Fatalf("后台未清理 %s", path)
		case <-ticker.C:
		}
	}
}

func TestCleanupBatchRollsBackAcknowledgements(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	c := NewService(db, nil, nil)
	for _, name := range []string{"first", "second", "third"} {
		if err := os.Mkdir(filepath.Join(db.ImagesRoot(), name), 0700); err != nil {
			t.Fatal(err)
		}
		if err := db.Queries().QueueImageFileDeletion(t.Context(), name); err != nil {
			t.Fatal(err)
		}
	}
	raw := testutil.Database(t, db)
	if _, err := raw.ExecContext(t.Context(), `CREATE TRIGGER fail_ack BEFORE DELETE ON pending_file_deletions WHEN OLD.entry_id = 'second' BEGIN SELECT RAISE(ABORT, 'forced failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := c.CleanupFiles(t.Context()); err == nil {
		t.Fatal("应返回确认批次失败")
	}
	pending, err := pendingFiles(t, db)
	if err != nil || len(pending) != 3 {
		t.Fatalf("失败后应回滚整批记录: %+v, %v", pending, err)
	}
	files, err := os.ReadDir(db.ImagesRoot())
	if err != nil || len(files) != 0 {
		t.Fatalf("文件删除应先于数据库确认: %v, %v", files, err)
	}
	if _, err := raw.ExecContext(t.Context(), "DROP TRIGGER fail_ack"); err != nil {
		t.Fatal(err)
	}
	if err := c.CleanupFiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	pending, err = pendingFiles(t, db)
	if err != nil || len(pending) != 0 {
		t.Fatalf("重试应幂等确认已删除的文件: %+v, %v", pending, err)
	}
}
