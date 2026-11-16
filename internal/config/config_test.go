package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func file(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "service.yaml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestConfigOverrides(t *testing.T) {
	path := file(t, `server:
  addr: "[::1]:9123"
storage:
  data_dir: ../images
upload:
  max_file_bytes: 4096
  max_request_bytes: 16384
convert:
  timeout: 90s
tools:
  encoder: ./bin/toGainMapHDR
  magick: /opt/bin/magick
`)
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path)
	if c.Server.Addr != "[::1]:9123" ||
		c.Storage.DataDir != filepath.Join(dir, "../images") ||
		c.Upload.MaxFileBytes != 4096 ||
		c.Upload.MaxRequestBytes != 16384 ||
		c.Convert.Timeout != 90*time.Second ||
		c.Tools.Encoder != filepath.Join(dir, "bin/toGainMapHDR") ||
		c.Tools.Magick != "/opt/bin/magick" ||
		c.Tools.JXRDec != "JxrDecApp" {
		t.Fatalf("配置解析或相对路径不正确: %+v", c)
	}
}

func TestShippedConfig(t *testing.T) {
	path := "../../config.example.yaml"
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	want := defaultConfig()
	want.Tools = Tools{
		JXRDec:  "/opt/homebrew/opt/jxrlib/bin/JxrDecApp",
		Magick:  "/opt/homebrew/opt/imagemagick-full/bin/magick",
		Encoder: "tools/toGainMapHDR/toGainMapHDR",
	}
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if err = want.resolve(dir); err != nil {
		t.Fatal(err)
	}
	if c != want {
		t.Fatalf("示例配置不正确: %+v != %+v", c, want)
	}
}

func TestBadConfig(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"server:\n  address: localhost:9000", "field address not found"},
		{"upload:\n  max_file_bytes: 0", "upload.max_file_bytes"},
		{"upload:\n  max_request_bytes: -1", "upload.max_request_bytes"},
		{"convert:\n  timeout: 0s", "convert.timeout"},
		{"convert:\n  concurrency: 1", "field concurrency not found"},
		{"server:\n  addr: 127.0.0.1:65536", "0-65535"},
		{"tools:\n  encoder: ''", "tools.encoder"},
		{"storage:\n  data_dir: ''", "storage.data_dir"},
		{"{}\n---\n{}", "only one YAML document"},
		{"{}\n---\nconvert: [", "yaml:"},
		{"convert: [", "yaml:"},
		{"", "EOF"},
	} {
		_, err := Load(file(t, tc.body))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Load(%q) = %v, 错误应包含 %q", tc.body, err, tc.want)
		}
	}
}

func TestConfigFileErrorIncludesPath(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.yaml")
		_, err := Load(path)
		if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), path) {
			t.Errorf("Load(%q) 应返回包含文件路径的 os.ErrNotExist, 得到 %v", path, err)
		}
	})
	t.Run("malformed", func(t *testing.T) {
		path := file(t, "convert: [")
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("Load(%q) 应返回包含文件路径的 YAML 解析错误, 得到 %v", path, err)
		}
	})
}
