package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"HDRBridge/db/migrations"
	"HDRBridge/internal/logging"
	"HDRBridge/internal/store/sqlc"

	"github.com/pressly/goose/v3"
	"golang.org/x/sys/unix"
	"modernc.org/sqlite"
)

func init() {
	// SQLite 内置 lower 仅处理 ASCII, 文件名搜索需要统一处理 Unicode
	sqlite.MustRegisterDeterministicScalarFunction("hdrbridge_fold", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		value, ok := args[0].(string)
		if !ok {
			return nil, errors.New("文件名搜索参数必须为字符串")
		}
		return strings.ToLower(value), nil
	})
}

// Store 持有数据库连接池和进程文件锁, 创建后不得复制
type Store struct {
	db        *sql.DB
	queries   *sqlc.Queries
	root      string
	lock      *os.File
	closeOnce sync.Once
	closeErr  error
}

// Queries 提供共享的 sqlc 查询入口, 事务操作使用 WithTx
func (s *Store) Queries() *sqlc.Queries { return s.queries }

// DataDir 返回持久化数据根目录
func (s *Store) DataDir() string { return s.root }

// ImagesRoot 返回固定的图片目录
func (s *Store) ImagesRoot() string { return filepath.Join(s.root, "images") }

// StagingRoot 返回尚未发布的上传目录
func (s *Store) StagingRoot() string { return filepath.Join(s.root, "staging") }

// WithTx 在同一事务内执行查询, 回调不得重新获取连接或等待后台任务退出
func (s *Store) WithTx(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		// Commit 或 context 取消可能已结束事务, ErrTxDone 不代表清理失败
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			logging.Logger().Warn("回滚事务失败", "error", err)
		}
	}()
	if err := fn(s.queries.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

// Open 锁定数据目录并完成迁移
func Open(ctx context.Context, root string) (_ *Store, err error) {
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	imagesRoot := filepath.Join(root, "images")
	if err = os.MkdirAll(imagesRoot, 0700); err != nil {
		return nil, err
	}
	stagingRoot := filepath.Join(root, "staging")
	if err = os.MkdirAll(stagingRoot, 0700); err != nil {
		return nil, err
	}

	lock, err := os.OpenFile(filepath.Join(root, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			logging.CloseResource(lock, lock.Name())
		}
	}()

	if err = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return nil, fmt.Errorf("lock data directory %q: %w", root, err)
	}

	uri := url.URL{
		Scheme: "file",
		Path:   filepath.Join(root, "hdrbridge.db"),
	}
	args := url.Values{"_pragma": {"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(WAL)"}}
	uri.RawQuery = args.Encode()

	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			logging.CloseResource(db, uri.Path)
		}
	}()

	db.SetMaxOpenConns(1)
	provider, err := goose.NewProvider(goose.DialectSQLite3, db, migrations.Files)
	if err != nil {
		return nil, err
	}
	if _, err = provider.Up(ctx); err != nil {
		return nil, err
	}

	return &Store{
		db:      db,
		queries: sqlc.New(db),
		root:    root,
		lock:    lock,
	}, nil
}

// Close 在所有业务操作退出后关闭连接池并释放数据目录锁, 重复调用返回首次结果
func (s *Store) Close() error {
	s.closeOnce.Do(func() { s.closeErr = errors.Join(s.db.Close(), s.lock.Close()) })
	return s.closeErr
}
