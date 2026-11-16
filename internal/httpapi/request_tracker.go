package httpapi

import (
	"net/http"
	"sync"
)

// RequestTracker 跟踪活动请求, 停机后拒绝新请求, 创建后不得复制
type RequestTracker struct {
	handler  http.Handler
	mu       sync.Mutex
	stopping bool
	active   int
	done     chan struct{}
}

// NewRequestTracker 包装 handler, nil 时使用 http.DefaultServeMux
func NewRequestTracker(handler http.Handler) *RequestTracker {
	if handler == nil {
		handler = http.DefaultServeMux
	}
	return &RequestTracker{handler: handler, done: make(chan struct{})}
}

// ServeHTTP 登记请求直到 handler 返回, 停机后返回 503
func (t *RequestTracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t.mu.Lock()
	if t.stopping {
		t.mu.Unlock()
		http.Error(w, "服务正在关闭", http.StatusServiceUnavailable)
		return
	}
	t.active++
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.active--
		if t.stopping && t.active == 0 {
			close(t.done)
		}
	}()
	t.handler.ServeHTTP(w, r)
}

// Stop 拒绝新请求, 重复调用安全
func (t *RequestTracker) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.stopping {
		t.stopping = true
		if t.active == 0 {
			close(t.done)
		}
	}
}

// Wait 等待已登记请求退出, 调用前必须先调用 Stop
func (t *RequestTracker) Wait() { <-t.done }
