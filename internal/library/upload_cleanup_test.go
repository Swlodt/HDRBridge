package library

import (
	"bytes"
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"HDRBridge/internal/store/sqlc"
	"HDRBridge/internal/testutil"
	"modernc.org/sqlite"
)

var uploadCleanupFunctionID atomic.Uint64

type uploadReaderFunc func([]byte) (int, error)

func (f uploadReaderFunc) Read(p []byte) (int, error) { return f(p) }

func TestUploadQueuesFailedCleanup(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("权限故障测试需要非 root 用户")
	}
	for _, area := range []string{"staging", "images"} {
		t.Run(area, func(t *testing.T) {
			root := t.TempDir()
			var dir, function string
			if area == "images" {
				// SQLite 自定义函数必须在连接创建前注册, 在入库时制造文件清理和事务故障
				function = fmt.Sprintf("upload_cleanup_failure_%d", uploadCleanupFunctionID.Add(1))
				if err := sqlite.RegisterScalarFunction(function, 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
					dir = filepath.Join(root, "images", args[0].(string))
					if err := os.Chmod(dir, 0500); err != nil {
						return nil, err
					}
					return nil, errors.New("forced upload failure")
				}); err != nil {
					t.Fatal(err)
				}
			}
			db := openTestStore(t, root)
			s := NewService(db, converterFunc(writeTestHEIC), nil)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			t.Cleanup(func() {
				if dir != "" {
					_ = os.Chmod(dir, 0700)
				}
			})
			var source io.Reader = bytes.NewReader(testJXR)
			if area == "staging" {
				source = uploadReaderFunc(func(p []byte) (int, error) {
					entries, err := os.ReadDir(db.StagingRoot())
					if err != nil || len(entries) != 1 {
						t.Fatalf("上传目录错误: %v, %v", entries, err)
					}
					dir = filepath.Join(db.StagingRoot(), entries[0].Name())
					if err := os.Chmod(dir, 0500); err != nil {
						t.Fatal(err)
					}
					cancel()
					return copy(p, testJXR), nil
				})
			} else {
				if _, err := testutil.Database(t, db).ExecContext(t.Context(), "CREATE TRIGGER fail_upload BEFORE INSERT ON images BEGIN SELECT "+function+"(NEW.id); END"); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Upload(ctx, "uncategorized", "photo.jxr", source, 100); err == nil {
				t.Fatal("应返回上传失败")
			} else if area == "staging" && !errors.Is(err, context.Canceled) {
				t.Fatalf("应保留请求取消错误: %v", err)
			}
			pending, err := db.Queries().PendingFileDeletionBatch(t.Context(), sqlc.PendingFileDeletionBatchParams{ThroughID: 100, BatchLimit: 100})
			if err != nil || len(pending) != 1 || pending[0].Area != area || pending[0].EntryID != filepath.Base(dir) {
				t.Fatalf("未记录上传残留: %+v, %v", pending, err)
			}
			if _, err := db.Queries().GetImage(t.Context(), filepath.Base(dir)); err == nil {
				t.Fatal("失败上传不应留下图片记录")
			}
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := s.CleanupFiles(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("后台未清理上传残留: %v", err)
			}
			pending, err = db.Queries().PendingFileDeletionBatch(t.Context(), sqlc.PendingFileDeletionBatchParams{ThroughID: 100, BatchLimit: 100})
			if err != nil || len(pending) != 0 {
				t.Fatalf("清理完成记录未移除: %+v, %v", pending, err)
			}
		})
	}
}
