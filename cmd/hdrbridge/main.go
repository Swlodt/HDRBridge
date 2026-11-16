package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"HDRBridge/internal/config"
	"HDRBridge/internal/httpapi"
	"HDRBridge/internal/library"
	"HDRBridge/internal/logging"
	"HDRBridge/internal/media"
	"HDRBridge/internal/store"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
)

func main() {
	logging.ConfigureStreams(os.Stdout, os.Stderr, slog.LevelInfo)
	gin.DefaultWriter = logging.Writer(slog.LevelInfo)
	gin.DefaultErrorWriter = logging.Writer(slog.LevelError)
	if err := runMain(); err != nil {
		logging.Logger().Error("HDRBridge 退出", "error", err)
		os.Exit(1)
	}
}

func runMain() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// 首次信号启动优雅停机, 再次发送信号时恢复系统默认处理
	unregister := context.AfterFunc(ctx, stop)
	defer unregister()
	return run(ctx, "config.yaml")
}

func run(ctx context.Context, configPath string) (err error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("加载配置: %w", err)
	}

	db, err := store.Open(ctx, cfg.Storage.DataDir)
	if err != nil {
		return fmt.Errorf("打开存储: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("关闭存储: %w", closeErr))
		}
	}()

	pipeline, err := media.NewJXR(cfg.Tools, cfg.Convert)
	if err != nil {
		return fmt.Errorf("初始化转换工具: %w", err)
	}
	catalog := library.NewService(db, pipeline, media.NewRaster(cfg.Convert.Timeout))
	if err := catalog.RecoverFiles(ctx); err != nil {
		return fmt.Errorf("恢复图片文件: %w", err)
	}

	router := httpapi.New(catalog, cfg.Upload)
	server := &http.Server{
		Addr:              cfg.Server.Addr,
		ErrorLog:          logging.StdLogger(slog.LevelError),
		Handler:           http.NewCrossOriginProtection().Handler(router),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}
	listener, err := net.Listen("tcp", cfg.Server.Addr)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", cfg.Server.Addr, err)
	}
	logging.Logger().Info("HDRBridge 已启动", "address", listener.Addr(), "data", db.ImagesRoot())
	return serve(ctx, server, listener, catalog.Maintain)
}

// serve 接管 listener, 在所有服务 goroutine 退出后返回, 调用方随后才能关闭存储
func serve(ctx context.Context, server *http.Server, listener net.Listener, maintain func(context.Context)) error {
	return serveWithShutdownTimeout(ctx, server, listener, maintain, 10*time.Second)
}

func serveWithShutdownTimeout(ctx context.Context, server *http.Server, listener net.Listener, maintain func(context.Context), timeout time.Duration) error {
	requests := httpapi.NewRequestTracker(server.Handler)
	server.Handler = requests
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	group, ctx := errgroup.WithContext(ctx)
	group.Go(func() error {
		defer stop()
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP 服务: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		maintain(ctx)
		return nil
	})
	group.Go(func() error {
		<-ctx.Done()
		requests.Stop()
		// 停机必须使用独立截止时间, 已取消的服务 context 不能完成优雅退出
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		err := server.Shutdown(shutdownCtx)
		if err != nil {
			err = errors.Join(fmt.Errorf("关闭 HTTP 服务: %w", err), server.Close())
		}
		requests.Wait()
		return err
	})
	return group.Wait()
}
