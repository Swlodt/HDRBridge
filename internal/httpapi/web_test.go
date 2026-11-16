package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestEmbeddedWebRoutes(t *testing.T) {
	router, _, _ := newTestRouter(t)
	for _, tc := range []struct {
		method, path, accept string
		status               int
		html                 bool
	}{
		{"GET", "/", "text/html", 200, true},
		{"GET", "/albums/uncategorized", "text/html", 200, true},
		{"HEAD", "/albums/example", "text/html", 200, true},
		{"GET", "/index.html", "text/html", 200, true},
		{"GET", "/api/missing", "text/html", 404, false},
		{"GET", "/api", "text/html", 404, false},
		{"GET", "/assets/missing.js", "text/html", 404, false},
		{"GET", "/assets/", "text/html", 404, false},
		{"GET", "/config.yaml", "text/html", 404, false},
		{"GET", "/src/App.tsx", "text/html", 404, false},
		{"GET", "/albums/example", "application/json", 404, false},
		{"GET", "/albums/example", "text/html;q=0", 404, false},
		{"GET", "/albums/example", "text/html;q=0.0", 404, false},
		{"POST", "/albums/example", "text/html", 404, false},
	} {
		t.Run(tc.method+tc.path+tc.accept, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("Accept", tc.accept)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			if response.Code != tc.status {
				t.Fatalf("状态 = %d, 期望 %d: %s", response.Code, tc.status, response.Body.String())
			}
			if strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") != tc.html {
				t.Fatalf("Content-Type 错误: %s", response.Header().Get("Content-Type"))
			}
			if tc.html && response.Header().Get("Cache-Control") != "no-cache" {
				t.Fatal("HTML 不应长期缓存")
			}
			if tc.method == "HEAD" && response.Body.Len() != 0 {
				t.Fatal("HEAD 不应返回响应体")
			}
		})
	}
}

func TestEmbeddedWebAssetsAndCache(t *testing.T) {
	router, _, _ := newTestRouter(t)
	index := httptest.NewRecorder()
	router.ServeHTTP(index, httptest.NewRequest("GET", "/", nil))
	paths := regexp.MustCompile(`(?:src|href)="(/assets/[^" ]+)"`).FindAllStringSubmatch(index.Body.String(), -1)
	if len(paths) < 2 {
		t.Fatal("构建页面缺少 JS/CSS 资源, 请先运行 npm run build")
	}
	for _, match := range paths {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", match[1], nil))
		if response.Code != 200 || response.Body.Len() == 0 || response.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Fatalf("静态资源响应错误: %s %d %+v", match[1], response.Code, response.Header())
		}
		if response.Header().Get("X-Content-Type-Options") != "nosniff" || strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("静态资源类型错误: %s", match[1])
		}
		req := httptest.NewRequest("GET", match[1], nil)
		req.Header.Set("If-None-Match", response.Header().Get("ETag"))
		cached := httptest.NewRecorder()
		router.ServeHTTP(cached, req)
		if cached.Code != http.StatusNotModified || cached.Body.Len() != 0 {
			t.Fatalf("缓存验证响应错误: %d", cached.Code)
		}
	}
}
