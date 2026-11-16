// Package web 提供嵌入二进制的 React 页面和静态资源
package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed dist
var assets embed.FS

var hashedAsset = regexp.MustCompile(`-[A-Za-z0-9_-]{8,}\.[^.]+$`)

// Handler 缓存只读构建资源及 ETag, 零值可用, 首次使用后不得复制
type Handler struct {
	mu    sync.Mutex
	cache map[string]*asset
}

type asset struct {
	data []byte
	etag string
}

// Serve 仅处理静态文件和 HTML 导航, 未命中时交由 HTTP 层返回 404
func (h *Handler) Serve(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "api" || strings.HasPrefix(name, "api/") || (name != "" && !fs.ValidPath(name)) {
		return false
	}
	isAsset := strings.HasPrefix(name, "assets/") || name == "favicon.svg"
	if !isAsset {
		if name == "assets" || (name != "index.html" && path.Ext(name) != "") || !acceptsHTML(r.Header.Get("Accept")) {
			return false
		}
		name = "index.html"
	}
	file, err := h.load(name)
	if err != nil {
		return false
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	if isAsset && hashedAsset.MatchString(name) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	w.Header().Set("ETag", file.etag)
	if !isAsset {
		w.Header().Set("Vary", "Accept")
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(file.data))
	return true
}

func (h *Handler) load(name string) (*asset, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if file, ok := h.cache[name]; ok {
		return file, nil
	}
	data, err := assets.ReadFile("dist/" + name)
	if err != nil {
		return nil, err
	}
	file := &asset{data: data, etag: fmt.Sprintf(`"%x"`, sha256.Sum256(data))}
	if h.cache == nil {
		h.cache = make(map[string]*asset)
	}
	// 仅缓存实际存在的嵌入文件, 随机 404 路径不能导致缓存无限增长
	h.cache[name] = file
	return file, nil
}

func acceptsHTML(accept string) bool {
	if accept == "" {
		return true
	}
	for part := range strings.SplitSeq(accept, ",") {
		kind, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		quality := 1.0
		if value, ok := params["q"]; ok {
			quality, err = strconv.ParseFloat(value, 64)
		}
		if err == nil && quality > 0 && quality <= 1 && (kind == "text/html" || kind == "*/*") {
			return true
		}
	}
	return false
}
