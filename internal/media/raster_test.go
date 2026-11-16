package media

import (
	"context"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"HDRBridge/internal/apperr"
)

func writePNG(t *testing.T, dir string) string {
	t.Helper()
	source := filepath.Join(dir, "source.png")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return source
}

func TestRasterClassifiesFailures(t *testing.T) {
	for _, tc := range []struct {
		name, convert, metadata string
		want                    error
	}{
		{"有效产物", `cp "$6" "$8"`, "exit 1", nil},
		{"输入损坏", "echo decode-failed >&2; exit 11", "echo '  pixelWidth: <nil>'; echo '  pixelHeight: <nil>'", apperr.ErrInvalid},
		{"输出失败", "echo disk-write-failed >&2; exit 13", "echo '  pixelWidth: 4'; echo '  pixelHeight: 4'", apperr.ErrInternal},
		{"产物缺失", "echo missing-output; exit 0", "echo '  pixelWidth: 4'; echo '  pixelHeight: 4'", apperr.ErrInternal},
		{"诊断失败", "echo tool-crashed >&2; exit 13", "echo query-failed >&2; exit 1", apperr.ErrInternal},
		{"产物截断", `head -c 33 "$6" > "$8"`, "exit 1", apperr.ErrInternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			source := writePNG(t, dir)
			program := filepath.Join(dir, "sips")
			script := "#!/bin/sh\nif [ \"$1\" = '-g' ]; then\n" + tc.metadata + "\nelse\n" + tc.convert + "\nfi\n"
			if err := os.WriteFile(program, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			r := &Raster{program: program, timeout: 5 * time.Second}
			err := r.MakePreview(t.Context(), source, filepath.Join(dir, "preview.png"))
			if !errors.Is(err, tc.want) {
				t.Fatalf("预览错误 = %v, 期望 %v", err, tc.want)
			}
			if tc.name == "输出失败" && (!strings.Contains(err.Error(), "disk-write-failed") || strings.Contains(apperr.Message(err), "disk-write-failed")) {
				t.Fatalf("工具诊断应只保留在内部错误中: %v", err)
			}
		})
	}
}

func TestRasterTimeoutAndCancellation(t *testing.T) {
	dir := t.TempDir()
	program := filepath.Join(dir, "sips")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	r := &Raster{program: program, timeout: 100 * time.Millisecond}
	err := r.MakePreview(t.Context(), "source", "output")
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, apperr.ErrInternal) || errors.Is(err, apperr.ErrInvalid) {
		t.Fatalf("预览处理超时应为服务端错误: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := r.MakePreview(ctx, "source", "output"); !errors.Is(err, context.Canceled) {
		t.Fatalf("请求取消应保留取消语义: %v", err)
	}
}

func TestRasterRealOutputPermissionFailure(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("位图预览使用 macOS sips")
	}
	dir := t.TempDir()
	source := writePNG(t, dir)
	outputDir := filepath.Join(dir, "readonly")
	if err := os.Mkdir(outputDir, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(outputDir, 0700) })
	err := NewRaster(5*time.Second).MakePreview(t.Context(), source, filepath.Join(outputDir, "preview.png"))
	if !errors.Is(err, apperr.ErrInternal) || errors.Is(err, apperr.ErrInvalid) {
		t.Fatalf("有效 PNG 的输出目录故障不能误报为输入错误: %v", err)
	}
}
