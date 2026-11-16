package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"HDRBridge/internal/store"
)

func TestMainFailureExitCode(t *testing.T) {
	if os.Getenv("HDRBRIDGE_TEST_MAIN_FAILURE") == "1" {
		main()
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMainFailureExitCode$")
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), "HDRBRIDGE_TEST_MAIN_FAILURE=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("启动失败应以 1 退出, 得到 %v: %s", err, &stderr)
	}
	if text := stderr.String(); !strings.Contains(text, "level=ERROR") || !strings.Contains(text, "加载配置") || strings.Contains(text, "panic:") {
		t.Fatalf("启动失败应向 stderr 记录配置错误: %s", text)
	}
	if stdout.Len() != 0 {
		t.Fatalf("启动失败日志应只写入 stderr: %s", &stdout)
	}
}

func TestRunFailureReleasesStore(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	dataDir := filepath.Join(root, "data")
	body := fmt.Sprintf("storage:\n  data_dir: %q\ntools:\n  jxr_dec: %q\n", dataDir, filepath.Join(root, "missing-tool"))
	if err := os.WriteFile(configPath, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(t.Context(), configPath); err == nil || !strings.Contains(err.Error(), "初始化转换工具") {
		t.Fatalf("应返回工具初始化错误: %v", err)
	}
	db, err := store.Open(t.Context(), dataDir)
	if err != nil {
		t.Fatalf("启动失败后未释放数据目录锁: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestServeJoinsBackgroundServices(t *testing.T) {
	listenerFailure := errors.New("listener failed")
	for _, tc := range []struct {
		name        string
		listenerErr error
		cancel      bool
	}{
		{name: "取消服务", cancel: true},
		{name: "监听失败", listenerErr: listenerFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			listener := &testListener{closed: make(chan struct{}), err: tc.listenerErr}
			maintenanceDone := make(chan struct{})
			maintenanceStarted := make(chan struct{})
			done := make(chan error, 1)
			go func() {
				done <- serve(ctx, &http.Server{}, listener, func(ctx context.Context) {
					defer close(maintenanceDone)
					close(maintenanceStarted)
					<-ctx.Done()
				})
			}()
			select {
			case <-maintenanceStarted:
			case <-time.After(3 * time.Second):
				t.Fatal("文件清理服务未启动")
			}
			if tc.cancel {
				cancel()
			}
			select {
			case err := <-done:
				if !errors.Is(err, tc.listenerErr) {
					t.Fatalf("serve 错误 = %v, 期望 %v", err, tc.listenerErr)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("服务未停止")
			}
			for name, stopped := range map[string]<-chan struct{}{"maintenance": maintenanceDone, "listener": listener.closed} {
				select {
				case <-stopped:
				default:
					t.Errorf("serve 返回后 %s 仍在运行", name)
				}
			}
		})
	}
}

type testListener struct {
	closed chan struct{}
	once   sync.Once
	err    error
}

func (l *testListener) Accept() (net.Conn, error) {
	if l.err != nil {
		return nil, l.err
	}
	<-l.closed
	return nil, net.ErrClosed
}

func (l *testListener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *testListener) Addr() net.Addr { return &net.TCPAddr{} }

func TestServeWaitsForHandlersAfterForcedClose(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	listener := &pipeListener{connections: make(chan net.Conn, 1), closed: make(chan struct{})}
	listener.connections <- serverConn
	client := &http.Client{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) { return clientConn, nil }}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(canceled)
		<-release
	})}
	done := make(chan error, 1)
	go func() {
		done <- serveWithShutdownTimeout(ctx, server, listener, func(ctx context.Context) { <-ctx.Done() }, 20*time.Millisecond)
	}()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		response, _ := client.Get("http://example.test/")
		if response != nil {
			response.Body.Close()
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("请求未进入 handler")
	}
	cancel()
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("连接未关闭")
	}
	select {
	case err := <-done:
		t.Fatalf("handler 退出前服务已返回: %v", err)
	default:
	}
	close(release)
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("应报告停机超时: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler 退出后服务仍未返回")
	}
	<-clientDone
}

type pipeListener struct {
	connections chan net.Conn
	closed      chan struct{}
	once        sync.Once
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.connections:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}
func (l *pipeListener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *pipeListener) Addr() net.Addr { return &net.TCPAddr{} }
