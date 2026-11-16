-- name: QueueAlbumImageFileDeletions :exec
INSERT INTO pending_file_deletions (area, entry_id)
SELECT 'images', id FROM images WHERE album_id = ?
ON CONFLICT(area, entry_id) DO NOTHING;

-- name: FileDeletionHighWaterMark :one
SELECT CAST(COALESCE(MAX(id), 0) AS INTEGER) FROM pending_file_deletions;

-- name: PendingFileDeletionBatch :many
SELECT * FROM pending_file_deletions
WHERE id > sqlc.arg(after_id) AND id <= sqlc.arg(through_id)
ORDER BY id LIMIT sqlc.arg(batch_limit);

-- name: CompleteFileDeletion :exec
DELETE FROM pending_file_deletions WHERE id = ?;

-- name: QueueImageFileDeletion :exec
INSERT INTO pending_file_deletions (area, entry_id) VALUES ('images', ?)
ON CONFLICT(area, entry_id) DO NOTHING;

-- name: QueueFileDeletion :exec
INSERT INTO pending_file_deletions (area, entry_id) VALUES (?, ?)
ON CONFLICT(area, entry_id) DO NOTHING;
