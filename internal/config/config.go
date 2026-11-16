package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"HDRBridge/internal/logging"

	"gopkg.in/yaml.v3"
)

// Config 汇集服务启动所需配置, 相对路径在加载时解析
type Config struct {
	Server  Server  `yaml:"server"`
	Storage Storage `yaml:"storage"`
	Upload  Upload  `yaml:"upload"`
	Convert Convert `yaml:"convert"`
	Tools   Tools   `yaml:"tools"`
}

// Server 定义 HTTP 监听地址
type Server struct {
	Addr string `yaml:"addr"`
}

// Storage 定义持久化数据的根目录
type Storage struct {
	DataDir string `yaml:"data_dir"`
}

// Upload 分别限制单文件和完整 multipart 请求的字节数
type Upload struct {
	MaxFileBytes    int64 `yaml:"max_file_bytes"`
	MaxRequestBytes int64 `yaml:"max_request_bytes"`
}

// Convert 定义单张图片转换或预览生成的最长时间
type Convert struct {
	Timeout time.Duration `yaml:"timeout"`
}

// Tools 定义转换流水线依赖的外部可执行文件
type Tools struct {
	JXRDec  string `yaml:"jxr_dec"`
	Magick  string `yaml:"magick"`
	Encoder string `yaml:"encoder"`
}

func defaultConfig() Config {
	return Config{
		Server: Server{
			Addr: "127.0.0.1:8080",
		},
		Storage: Storage{
			DataDir: "./runtime",
		},
		Upload: Upload{
			MaxFileBytes:    256 << 20,
			MaxRequestBytes: 1 << 30,
		},
		Convert: Convert{
			Timeout: 30 * time.Second,
		},
		Tools: Tools{
			JXRDec:  "JxrDecApp",
			Magick:  defaultMagick(),
			Encoder: "toGainMapHDR",
		},
	}
}

func defaultMagick() string {
	for _, name := range []string{"/opt/homebrew/opt/imagemagick-full/bin/magick", "/usr/local/opt/imagemagick-full/bin/magick"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return "magick"
}

// Load 严格读取单个 YAML 文档, 校验配置并解析相对路径
func Load(filename string) (Config, error) {
	f, err := os.Open(filename)
	if err != nil {
		return Config{}, fmt.Errorf("failed to open config file: %w", err)
	}
	defer logging.CloseResource(f, filename)

	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)

	c := defaultConfig()
	if err := decoder.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("failed to decode config file %s: %w", filename, err)
	}

	if err := decoder.Decode(new(yaml.Node)); err != io.EOF {
		if err == nil {
			err = errors.New("only one YAML document is allowed")
		}
		return Config{}, fmt.Errorf("failed to decode config file %s: %w", filename, err)
	}

	dir, err := filepath.Abs(filepath.Dir(filename))
	if err != nil {
		return Config{}, fmt.Errorf("failed to resolve absolute path: %w", err)
	}

	if err := c.resolve(dir); err != nil {
		return Config{}, fmt.Errorf("invalid config file %s: %w", filename, err)
	}

	return c, nil
}

func (c *Config) resolve(dir string) error {
	_, port, err := net.SplitHostPort(c.Server.Addr)
	if err != nil {
		return fmt.Errorf("invalid server address: %w", err)
	}
	if _, err = strconv.ParseUint(port, 10, 16); err != nil {
		return fmt.Errorf("port must be an integer 0-65535: %w", err)
	}

	if c.Upload.MaxFileBytes < 4 || c.Upload.MaxFileBytes >= 1<<62 {
		return errors.New("upload.max_file_bytes must be at least 4 and less than 2^62")
	}
	if c.Upload.MaxRequestBytes <= 0 {
		return errors.New("upload.max_request_bytes must be a positive integer")
	}
	if c.Convert.Timeout <= 0 {
		return errors.New("convert.timeout mandatory positive duration")
	}
	if strings.TrimSpace(c.Storage.DataDir) == "" {
		return errors.New("storage.data_dir cannot be empty")
	}
	if !filepath.IsAbs(c.Storage.DataDir) {
		c.Storage.DataDir = filepath.Join(dir, c.Storage.DataDir)
	}

	for _, tool := range []struct {
		key   string
		value *string
	}{
		{"jxr_dec", &c.Tools.JXRDec},
		{"magick", &c.Tools.Magick},
		{"encoder", &c.Tools.Encoder},
	} {
		if strings.TrimSpace(*tool.value) == "" {
			return fmt.Errorf("tools.%s cannot be empty", tool.key)
		}
		if strings.ContainsRune(*tool.value, filepath.Separator) && !filepath.IsAbs(*tool.value) {
			*tool.value = filepath.Join(dir, *tool.value)
		}
	}

	return nil
}
