package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"HDRBridge/internal/httpapi"
)

func TestRequestTrackerRejectsAfterStop(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	tracker := httpapi.NewRequestTracker(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); <-release }))
	go tracker.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	<-entered
	tracker.Stop()
	response := httptest.NewRecorder()
	tracker.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("停机后状态 = %d", response.Code)
	}
	done := make(chan struct{})
	go func() { tracker.Wait(); close(done) }()
	select {
	case <-done:
		t.Fatal("活动请求退出前不能结束等待")
	default:
	}
	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("请求退出后仍在等待")
	}
}

func TestRequestTrackerConcurrentStop(t *testing.T) {
	var handlers sync.WaitGroup
	tracker := httpapi.NewRequestTracker(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	start := make(chan struct{})
	for range 200 {
		handlers.Go(func() {
			<-start
			tracker.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
		})
	}
	close(start)
	tracker.Stop()
	tracker.Wait()
	handlers.Wait()
}
