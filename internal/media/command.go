package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// runTool 同步执行工具并保留最多 16 KiB 的诊断输出, 返回前终止失败命令的进程组
func runTool(ctx context.Context, program string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// 终止整个进程组, 避免 delegate 子进程在删除文件后继续写入
	cmd.Cancel = func() error {
		return killProcessGroup(cmd.Process.Pid)
	}
	cmd.WaitDelay = time.Second
	output := &tailBuffer{}
	cmd.Stdout, cmd.Stderr = output, output
	err := cmd.Start()
	if err == nil {
		err = cmd.Wait()
		if err != nil {
			// WaitDelay 仅关闭遗留管道, 父进程先退出时仍需终止残留的子进程
			if killErr := killProcessGroup(cmd.Process.Pid); killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
				err = errors.Join(err, fmt.Errorf("停止子进程组: %w", killErr))
			}
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			err = errors.Join(ctx.Err(), err)
		}
		return output.String(), fmt.Errorf("%s: %w: %s", filepath.Base(program), err, strings.TrimSpace(output.String()))
	}
	return output.String(), nil
}

func killProcessGroup(pid int) error {
	err := syscall.Kill(-pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

type tailBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	const limit = 16 << 10
	n := len(p)
	if len(p) >= limit {
		b.data = append(b.data[:0], p[len(p)-limit:]...)
	} else {
		b.data = append(b.data, p...)
		if len(b.data) > limit {
			b.data = b.data[len(b.data)-limit:]
		}
	}
	return n, nil
}
