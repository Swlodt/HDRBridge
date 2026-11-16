package library

import (
	"context"
	"fmt"

	"HDRBridge/internal/store/sqlc"
)

// queueOrphan 登记残留目录并唤醒清理器, 调用前必须确认目录已不再被上传或转换使用
func (s *Service) queueOrphan(ctx context.Context, area, id string) error {
	if (area != "images" && area != "staging") || !validImageID(id) {
		return fmt.Errorf("无效的清理目录: %q/%q", area, id)
	}
	if err := s.db.Queries().QueueFileDeletion(ctx, sqlc.QueueFileDeletionParams{Area: area, EntryID: id}); err != nil {
		return err
	}
	s.notifyCleanup()
	return nil
}

// DeleteImage 提交图片删除并通知后台清理文件
func (s *Service) DeleteImage(ctx context.Context, id string) error {
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.GetImage(ctx, id); err != nil {
			return notFound(err)
		}
		if err := q.QueueImageFileDeletion(ctx, id); err != nil {
			return err
		}
		return q.DeleteImage(ctx, id)
	})
	if err != nil {
		return err
	}
	s.notifyCleanup()
	return nil
}

// DeleteAlbum 将图片归属变更或删除与相册删除一起提交
func (s *Service) DeleteAlbum(ctx context.Context, id string, deleteImages bool) error {
	if err := checkAlbumMutable(id); err != nil {
		return err
	}
	err := s.db.WithTx(ctx, func(q *sqlc.Queries) error {
		if _, err := q.GetAlbum(ctx, id); err != nil {
			return notFound(err)
		}
		if deleteImages {
			if err := q.QueueAlbumImageFileDeletions(ctx, id); err != nil {
				return err
			}
			if err := q.DeleteAlbumImages(ctx, id); err != nil {
				return err
			}
		} else if err := q.MoveImagesByAlbum(ctx, sqlc.MoveImagesByAlbumParams{FromAlbumID: id, ToAlbumID: uncategorizedID}); err != nil {
			return err
		}
		_, err := q.DeleteAlbum(ctx, id)
		return err
	})
	if err != nil {
		return err
	}
	if deleteImages {
		s.notifyCleanup()
	}
	return nil
}

func (s *Service) notifyCleanup() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
