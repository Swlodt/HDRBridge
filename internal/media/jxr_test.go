package media

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"HDRBridge/internal/config"
)

func TestJXRRequiresNonemptyHEIC(t *testing.T) {
	for _, tc := range []struct {
		name, command string
		wantError     bool
	}{
		{"缺失产物", "exit 0", true},
		{"空产物", `: > "$2/intermediate.heic"`, true},
		{"非空产物不校验格式", `printf 'output' > "$2/intermediate.heic"`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tools := t.TempDir()
			noop, encoder := filepath.Join(tools, "noop"), filepath.Join(tools, "encoder")
			if err := os.WriteFile(noop, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(encoder, []byte("#!/bin/sh\n"+tc.command+"\n"), 0700); err != nil {
				t.Fatal(err)
			}
			pipeline, err := NewJXR(config.Tools{JXRDec: noop, Magick: noop, Encoder: encoder}, config.Convert{Timeout: time.Second})
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			err = pipeline.Convert(t.Context(), dir)
			if (err != nil) != tc.wantError {
				t.Fatalf("转换错误不符合预期: %v", err)
			}
			if tc.name == "空产物" && !strings.Contains(err.Error(), "HEIC 文件为空") {
				t.Fatalf("错误缺少空文件说明: %v", err)
			}
			data, fileErr := os.ReadFile(filepath.Join(dir, "output.heic"))
			if tc.wantError {
				if !errors.Is(fileErr, os.ErrNotExist) {
					t.Fatalf("无效产物不应发布: %v", fileErr)
				}
			} else if fileErr != nil || string(data) != "output" {
				t.Fatalf("非空 HEIC 应保留, 缩略图失败不影响成功: %q, %v", data, fileErr)
			}
			if _, err := os.Stat(filepath.Join(dir, ".work")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("工作目录未清理: %v", err)
			}
		})
	}
}
