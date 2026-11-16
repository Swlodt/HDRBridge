package library

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestOpenFileRejectsFIFOWithoutBlocking(t *testing.T) {
	db := openTestStore(t, t.TempDir())
	s := NewService(db, nil, nil)
	id := "non-regular"
	dir := filepath.Join(db.ImagesRoot(), id)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "thumbnail.jpg")
	if err := unix.Mkfifo(name, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		f, err := s.openFile(id, thumbnailFile)
		if f != nil {
			_ = f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("不应接受 FIFO")
		}
	case <-time.After(time.Second):
		// 打开写端解除错误实现中的阻塞, 确保失败测试也回收 goroutine
		fd, err := unix.Open(name, unix.O_WRONLY|unix.O_NONBLOCK, 0)
		if err == nil {
			_ = unix.Close(fd)
		}
		<-done
		t.Fatal("打开 FIFO 阻塞了文件检查")
	}
}
