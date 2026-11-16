package library

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"HDRBridge/internal/config"
	"HDRBridge/internal/media"
)

func TestUploadConversionTimeoutBoundaries(t *testing.T) {
	for _, mode := range []string{"主转换超时", "缩略图超时", "缩略图期间取消请求"} {
		t.Run(mode, func(t *testing.T) {
			tools := t.TempDir()
			decode := "exit 0"
			if mode == "主转换超时" {
				decode = "exec sleep 30"
			}
			for name, script := range map[string]string{
				"decode":  decode,
				"encoder": `printf 'finished HEIC' > "$2/intermediate.heic"`,
				"magick": `for arg do last=$arg; done
case "$last" in
  *.jpg) touch "${last}.started"; exec sleep 30;;
esac`,
			} {
				if err := os.WriteFile(filepath.Join(tools, name), []byte("#!/bin/sh\n"+script+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			pipeline, err := media.NewJXR(config.Tools{
				JXRDec: filepath.Join(tools, "decode"), Magick: filepath.Join(tools, "magick"), Encoder: filepath.Join(tools, "encoder"),
			}, config.Convert{Timeout: 2 * time.Second})
			if err != nil {
				t.Fatal(err)
			}
			db := openTestStore(t, t.TempDir())
			started := make(chan string, 1)
			s := NewService(db, converterFunc(func(ctx context.Context, dir string) error {
				started <- dir
				return pipeline.Convert(ctx, dir)
			}), nil)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			type outcome struct {
				photo Image
				err   error
			}
			done := make(chan outcome, 1)
			go func() {
				photo, err := s.Upload(ctx, "uncategorized", "photo.jxr", bytes.NewReader(testJXR), 8)
				done <- outcome{photo, err}
			}()
			var dir string
			select {
			case dir = <-started:
			case result := <-done:
				t.Fatalf("转换未启动: %v", result.err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if mode == "缩略图期间取消请求" {
				marker := filepath.Join(dir, ".work", media.ThumbnailName+".started")
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				for {
					if _, err := os.Stat(marker); err == nil {
						cancel()
						break
					}
					select {
					case result := <-done:
						t.Fatalf("缩略图启动前上传已结束: %v", result.err)
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-ticker.C:
					}
				}
			}
			var result outcome
			select {
			case result = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("转换没有退出")
			}
			ids, err := db.Queries().AllImageIDs(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if mode == "缩略图超时" {
				if result.err != nil || len(ids) != 1 || ids[0] != result.photo.ID {
					t.Fatalf("缩略图超时应保留已完成的上传: %+v, %v", result, ids)
				}
				artifact, err := s.OpenDownload(t.Context(), result.photo.ID)
				if err != nil {
					t.Fatal(err)
				}
				data, readErr := io.ReadAll(artifact.File)
				closeErr := artifact.File.Close()
				if readErr != nil || closeErr != nil || string(data) != "finished HEIC" {
					t.Fatalf("已完成的 HEIC 应可下载: %q, %v, %v", data, readErr, closeErr)
				}
			} else {
				want := context.DeadlineExceeded
				if mode == "缩略图期间取消请求" {
					want = context.Canceled
				}
				if !errors.Is(result.err, want) || len(ids) != 0 {
					t.Fatalf("失败上传不应发布图片: %+v, %v", result, ids)
				}
				if _, err := os.Stat(filepath.Join(db.ImagesRoot(), filepath.Base(dir))); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("失败上传发布了文件: %v", err)
				}
			}
			if entries, err := os.ReadDir(db.StagingRoot()); err != nil || len(entries) != 0 {
				t.Fatalf("暂存文件未清理: %v, %v", entries, err)
			}
		})
	}
}
