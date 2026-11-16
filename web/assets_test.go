package web

import (
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func BenchmarkConditionalAsset(b *testing.B) {
	entries, err := assets.ReadDir("dist/assets")
	if err != nil {
		b.Fatal(err)
	}
	var name string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".js") {
			name = "/assets/" + entry.Name()
			break
		}
	}
	if name == "" {
		b.Fatal("请先构建前端 JS")
	}
	var handler Handler
	warm := httptest.NewRecorder()
	if !handler.Serve(warm, httptest.NewRequest("GET", name, nil)) {
		b.Fatal("缺少构建资源")
	}
	req := httptest.NewRequest("GET", name, nil)
	req.Header.Set("If-None-Match", warm.Header().Get("ETag"))
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		response := httptest.NewRecorder()
		handler.Serve(response, req)
		if response.Code != 304 {
			b.Fatalf("条件请求返回 %d", response.Code)
		}
	}
}

func TestHandlerConcurrentReads(t *testing.T) {
	var handler Handler
	var group sync.WaitGroup
	for range 16 {
		group.Go(func() {
			response := httptest.NewRecorder()
			if !handler.Serve(response, httptest.NewRequest("GET", "/albums/uncategorized", nil)) || response.Code != 200 {
				t.Errorf("并发 HTML 响应错误: %d", response.Code)
			}
			if !strings.Contains(response.Body.String(), `<div id="root">`) || response.Header().Get("ETag") == "" {
				t.Error("缓存资源内容不完整")
			}
		})
	}
	group.Wait()
	for range 32 {
		if handler.Serve(httptest.NewRecorder(), httptest.NewRequest("GET", "/assets/absent.js", nil)) {
			t.Fatal("缺失资源不能回退到 HTML")
		}
	}
	if len(handler.cache) != 1 {
		t.Fatalf("只应缓存已有的 index.html, 得到 %d", len(handler.cache))
	}
}
