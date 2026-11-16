package library

import (
	"context"
	"os"

	"HDRBridge/internal/store/sqlc"
)

// RecoverFiles 在接收请求前登记孤立图片目录和残留上传目录
func (s *Service) RecoverFiles(ctx context.Context) error {
	ids, err := s.db.Queries().AllImageIDs(ctx)
	if err != nil {
		return err
	}
	known := make(map[string]bool, len(ids))
	for _, id := range ids {
		known[id] = true
	}
	for _, area := range []string{"staging", "images"} {
		directory := s.db.StagingRoot()
		if area == "images" {
			directory = s.db.ImagesRoot()
		}
		if err := s.recoverDirectory(ctx, directory, area, known); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) recoverDirectory(ctx context.Context, directory, area string, known map[string]bool) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return err
	}
	batch := make([]string, 0, cleanupBatchSize)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		if area == "images" && known[name] {
			continue
		}
		batch = append(batch, name)
		if len(batch) == cleanupBatchSize {
			if err := s.queueRecoveryBatch(ctx, area, batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
	return s.queueRecoveryBatch(ctx, area, batch)
}

func (s *Service) queueRecoveryBatch(ctx context.Context, area string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		for _, id := range ids {
			if err := q.QueueFileDeletion(ctx, sqlc.QueueFileDeletionParams{Area: area, EntryID: id}); err != nil {
				return err
			}
		}
		return nil
	})
}
