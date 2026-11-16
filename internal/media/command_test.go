package media

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCommandTimeoutAndOutputLimit(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if _, err := runTool(ctx, "/bin/sh", "-c", "sleep 30 & wait"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("超时错误不正确: %v", err)
	}
	_, err := runTool(t.Context(), "/bin/sh", "-c", "i=0; while [ $i -lt 5000 ]; do echo 0123456789; i=$((i + 1)); done; echo failure; exit 1")
	if err == nil || len(err.Error()) > 17000 || !strings.Contains(err.Error(), "failure") {
		t.Fatalf("工具输出未正确截断: %v", err)
	}
}

func TestCommandFailureKillsOrphanedChildren(t *testing.T) {
	for _, exitCode := range []string{"0", "1"} {
		t.Run("parent-exit-"+exitCode, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "child.pid")
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err := runTool(ctx, "/bin/sh", "-c", `sleep 30 & echo $! > "$1"; exit "$2"`, "test", pidFile, exitCode)
			if err == nil {
				t.Fatal("持有输出管道的遗留子进程应导致失败")
			}
			if exitCode == "0" && !errors.Is(err, exec.ErrWaitDelay) {
				t.Fatalf("应报告 WaitDelay: %v", err)
			}
			data, err := os.ReadFile(pidFile)
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				t.Fatal(err)
			}
			exited := false
			t.Cleanup(func() {
				if !exited {
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			})
			deadline := time.NewTimer(time.Second)
			defer deadline.Stop()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				if errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
					exited = true
					return
				}
				select {
				case <-deadline.C:
					t.Fatal("命令返回后子进程仍在运行")
				case <-ticker.C:
				}
			}
		})
	}
}
