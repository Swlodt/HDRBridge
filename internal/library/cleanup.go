package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"HDRBridge/internal/logging"
	"HDRBridge/internal/store/sqlc"
)

const cleanupBatchSize = 100

// CleanupFiles 串行分批重试已提交的文件删除, 失败记录保留至下一轮
func (s *Service) CleanupFiles(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.cleanupGate:
	}
	defer func() { s.cleanupGate <- struct{}{} }()
	// 固定本轮上界并推进游标, 失败记录和持续到来的删除都不能阻塞后续批次
	through, err := s.db.Queries().FileDeletionHighWaterMark(ctx)
	if err != nil || through == 0 {
		return err
	}
	roots := make(map[string]*os.Root)
	rootErrors := make(map[string]error)
	defer func() {
		for area, root := range roots {
			logging.CloseResource(root, area)
		}
	}()
	var firstError error
	failed := 0
	var after int64
	for {
		batch, err := s.db.Queries().PendingFileDeletionBatch(ctx, sqlc.PendingFileDeletionBatchParams{
			AfterID: after, ThroughID: through, BatchLimit: cleanupBatchSize,
		})
		if err != nil {
			return errors.Join(firstError, err)
		}
		if len(batch) == 0 {
			break
		}
		completed := make([]int64, 0, len(batch))
		for _, entry := range batch {
			if err := ctx.Err(); err != nil {
				return errors.Join(firstError, err)
			}
			after = entry.ID
			err := s.removeEntry(entry, roots, rootErrors)
			if err != nil {
				failed++
				if firstError == nil {
					firstError = err
				}
			} else {
				completed = append(completed, entry.ID)
			}
		}
		if len(completed) > 0 {
			// 文件删除已在事务外完成, 提交失败时保留整批记录供下轮幂等重试
			if err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
				for _, id := range completed {
					if err := q.CompleteFileDeletion(ctx, id); err != nil {
						return err
					}
				}
				return nil
			}); err != nil {
				return errors.Join(firstError, fmt.Errorf("提交文件清理结果: %w", err))
			}
		}
	}
	if firstError != nil {
		return fmt.Errorf("%d 个目录等待清理, 首个错误: %w", failed, firstError)
	}
	return nil
}

func (s *Service) removeEntry(entry sqlc.PendingFileDeletion, roots map[string]*os.Root, rootErrors map[string]error) error {
	// 只接受固定根目录下的单层名称, 不信任清理记录中的路径
	if !validImageID(entry.EntryID) {
		return fmt.Errorf("无效的清理目录 ID: %q", entry.EntryID)
	}
	var directory string
	switch entry.Area {
	case "images":
		directory = s.db.ImagesRoot()
	case "staging":
		directory = s.db.StagingRoot()
	default:
		return fmt.Errorf("无效的清理区域: %q", entry.Area)
	}
	root := roots[entry.Area]
	if root == nil {
		if err := rootErrors[entry.Area]; err != nil {
			return err
		}
		var err error
		root, err = os.OpenRoot(directory)
		if err != nil {
			rootErrors[entry.Area] = err
			return err
		}
		roots[entry.Area] = root
	}
	if err := root.RemoveAll(entry.EntryID); err != nil {
		return fmt.Errorf("清理 %s/%q: %w", entry.Area, entry.EntryID, err)
	}
	return nil
}

// Maintain 启动时消费积压记录, 删除通知即时唤醒, 失败记录定时重试
func (s *Service) Maintain(ctx context.Context) {
	s.maintain(ctx, time.Minute)
}

func (s *Service) maintain(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	s.notifyCleanup()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
		if err := s.CleanupFiles(ctx); err != nil && ctx.Err() == nil {
			logging.Logger().Warn("部分文件等待清理", "error", err)
		}
	}
}
