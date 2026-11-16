package library

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/logging"
	"HDRBridge/internal/media"
	"HDRBridge/internal/store/sqlc"
	"HDRBridge/internal/testutil"
)

var testJXR = []byte{'I', 'I', 0xbc, 1, 1, 2, 3, 4}

type converterFunc func(context.Context, string) error

func (f converterFunc) Convert(ctx context.Context, dir string) error { return f(ctx, dir) }

func writeTestHEIC(_ context.Context, dir string) error {
	return os.WriteFile(filepath.Join(dir, media.HDRName), []byte("\x00\x00\x00\x14ftypheix\x00\x00\x00\x00mif1"), 0600)
}

func TestUploadPublishesOnlyAfterConversion(t *testing.T) {
	for _, mode := range []string{"成功", "编码失败", "请求取消", "相册已删除"} {
		t.Run(mode, func(t *testing.T) {
			db := openTestStore(t, t.TempDir())
			ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
			defer cancel()
			if _, err := db.Queries().CreateAlbum(ctx, sqlc.CreateAlbumParams{ID: "target", Name: "目标"}); err != nil {
				t.Fatal(err)
			}
			started, release := make(chan string, 1), make(chan struct{})
			s := NewService(db, converterFunc(func(ctx context.Context, dir string) error {
				if err := writeTestHEIC(ctx, dir); err != nil {
					return err
				}
				started <- dir
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-release:
				}
				if mode == "编码失败" {
					return errors.New("encoder failed")
				}
				return nil
			}), nil)
			type outcome struct {
				photo Image
				err   error
			}
			done := make(chan outcome, 1)
			go func() {
				photo, err := s.Upload(ctx, "target", "test.jxr", bytes.NewReader(testJXR), 8)
				done <- outcome{photo, err}
			}()
			var dir string
			select {
			case dir = <-started:
			case result := <-done:
				t.Fatalf("转换尚未开始: %v", result.err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if filepath.Dir(dir) != db.StagingRoot() {
				t.Fatalf("转换应在暂存目录执行: %s", dir)
			}
			if _, err := db.Queries().GetImage(ctx, filepath.Base(dir)); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("转换结束前图片不应可见: %v", err)
			}
			if mode == "相册已删除" {
				if err := s.DeleteAlbum(ctx, "target", true); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "请求取消" {
				cancel()
			} else {
				close(release)
			}
			var result outcome
			select {
			case result = <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("上传未结束")
			}
			if mode == "成功" {
				if result.err != nil || result.photo.ID != filepath.Base(dir) {
					t.Fatalf("上传失败: %+v", result)
				}
				file, err := s.OpenDownload(t.Context(), result.photo.ID)
				if err != nil {
					t.Fatal(err)
				}
				_ = file.File.Close()
			} else {
				want := map[string]error{
					"编码失败": apperr.ErrInternal,
					"请求取消": context.Canceled, "相册已删除": apperr.ErrNotFound,
				}
				if !errors.Is(result.err, want[mode]) {
					t.Fatalf("上传错误 = %v, 期望 %v", result.err, want[mode])
				}
				if _, err := db.Queries().GetImage(t.Context(), filepath.Base(dir)); !errors.Is(err, sql.ErrNoRows) {
					t.Fatalf("失败上传留下了图片记录: %v", err)
				}
				if _, err := os.Stat(filepath.Join(db.ImagesRoot(), filepath.Base(dir))); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("失败上传发布了文件: %v", err)
				}
			}
			if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("暂存文件未清理: %v", err)
			}
		})
	}
}

func TestConcurrentUploadPublishesOneCopy(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	s := NewService(db, converterFunc(func(ctx context.Context, dir string) error {
		started <- struct{}{}
		select {
		case <-release:
			return writeTestHEIC(ctx, dir)
		case <-ctx.Done():
			return ctx.Err()
		}
	}), nil)
	results := make(chan error, 2)
	for _, name := range []string{"first.jxr", "second.jxr"} {
		go func() {
			_, err := s.Upload(ctx, uncategorizedID, name, bytes.NewReader(testJXR), int64(len(testJXR)))
			results <- err
		}()
	}
	// 两次上传都通过了转换前的查重, 由发布事务决定最终保留哪一张
	for range 2 {
		select {
		case <-started:
		case err := <-results:
			t.Fatalf("转换尚未开始: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(release)
	var saved, duplicates int
	for range 2 {
		select {
		case err := <-results:
			switch {
			case err == nil:
				saved++
			case errors.Is(err, errImageExists):
				duplicates++
			default:
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if saved != 1 || duplicates != 1 {
		t.Fatalf("应只有一次上传成功: 保存 %d, 重复 %d", saved, duplicates)
	}
	ids, err := db.Queries().AllImageIDs(t.Context())
	if err != nil || len(ids) != 1 {
		t.Fatalf("数据库应只保存一张图片: %v, %v", ids, err)
	}
	if entries, err := os.ReadDir(db.ImagesRoot()); err != nil || len(entries) != 1 || entries[0].Name() != ids[0] {
		t.Fatalf("重复上传留下了额外文件: %v, %v", entries, err)
	}
	if entries, err := os.ReadDir(db.StagingRoot()); err != nil || len(entries) != 0 {
		t.Fatalf("暂存目录未清理: %v, %v", entries, err)
	}
	if _, err := s.Upload(ctx, uncategorizedID, "again.jxr", bytes.NewReader(testJXR), int64(len(testJXR))); !errors.Is(err, errImageExists) || len(started) != 0 {
		t.Fatalf("顺序重复上传应在转换前拒绝: %v", err)
	}
}

func TestUploadValidatesAndPublishes(t *testing.T) {
	previous := logging.Logger()
	t.Cleanup(func() { slog.SetDefault(previous) })
	var logs bytes.Buffer
	logging.Configure(&logs, slog.LevelWarn)
	db := openTestStore(t, t.TempDir())
	s := NewService(db, converterFunc(writeTestHEIC), nil)
	row, err := s.Upload(t.Context(), "uncategorized", `C:\photos\日落.JXR`, bytes.NewReader(testJXR), 8)
	if err != nil || row.SourceMediaType != media.JPEGXR || row.OriginalName != "日落.JXR" {
		t.Fatalf("上传结果错误: %+v, %v", row, err)
	}
	data, err := os.ReadFile(filepath.Join(db.ImagesRoot(), row.ID, "source.jxr"))
	if err != nil || !bytes.Equal(data, testJXR) {
		t.Fatalf("源文件内容错误: %v, %v", data, err)
	}
	for _, tc := range []struct {
		name string
		data []byte
		want error
	}{
		{"photo.png", testJXR, apperr.ErrInvalid},
		{"fake.jxr", []byte("not jxr"), apperr.ErrInvalid},
		{"empty.jxr", nil, apperr.ErrInvalid},
		{"large.jxr", append(append([]byte{}, testJXR...), 0), apperr.ErrTooLarge},
	} {
		if _, err := s.Upload(t.Context(), "uncategorized", tc.name, bytes.NewReader(tc.data), 8); !errors.Is(err, tc.want) {
			t.Fatalf("上传 %q 错误为 %v, 期望 %v", tc.name, err, tc.want)
		}
	}
	if _, err := s.Upload(t.Context(), "missing", "photo.jxr", bytes.NewReader(testJXR), 8); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("不存在的相册应拒绝上传: %v", err)
	}
	if _, err := s.Upload(t.Context(), "uncategorized", "broken.jxr", io.MultiReader(bytes.NewReader(testJXR[:4]), brokenReader{}), 8); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("中断上传应返回读错误: %v", err)
	}
	if _, err := testutil.Database(t, db).ExecContext(t.Context(), `CREATE TRIGGER fail_upload BEFORE INSERT ON images BEGIN SELECT RAISE(ABORT, 'forced failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Upload(t.Context(), "uncategorized", "failed.jxr", bytes.NewReader(testJXR), 8); err == nil {
		t.Fatal("数据库故障应拒绝上传")
	}
	staging, err := os.ReadDir(db.StagingRoot())
	if err != nil || len(staging) != 0 {
		t.Fatalf("上传临时文件未清理: %v, %v", staging, err)
	}
	images, err := os.ReadDir(db.ImagesRoot())
	if err != nil || len(images) != 1 || images[0].Name() != row.ID {
		t.Fatalf("失败上传残留文件: %v, %v", images, err)
	}
	page, err := s.PageByAlbum(t.Context(), "uncategorized", PageInput{})
	listed := page.Images
	if err != nil || len(listed) != 1 || listed[0].ID != row.ID {
		t.Fatalf("失败上传残留记录: %+v, %v", listed, err)
	}
	if logs.Len() != 0 {
		t.Fatalf("上传成功或已处理的失败不应产生重复关闭、回滚告警: %s", &logs)
	}
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
