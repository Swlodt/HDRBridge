-- name: MoveImagesByAlbum :exec
UPDATE images
SET album_id = sqlc.arg(to_album_id), updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE album_id = sqlc.arg(from_album_id);

-- name: PageAlbumImages :many
SELECT * FROM images
WHERE album_id = sqlc.arg(album_id)
  AND (sqlc.arg(search) = '' OR instr(hdrbridge_fold(original_name), hdrbridge_fold(sqlc.arg(search))) > 0)
ORDER BY created_at DESC, id DESC LIMIT sqlc.arg(page_limit);

-- name: PageAlbumImagesBefore :many
SELECT * FROM images
WHERE album_id = sqlc.arg(album_id)
  AND (sqlc.arg(search) = '' OR instr(hdrbridge_fold(original_name), hdrbridge_fold(sqlc.arg(search))) > 0)
  AND (created_at, id) < (sqlc.arg(cursor_time), sqlc.arg(cursor_id))
ORDER BY created_at DESC, id DESC LIMIT sqlc.arg(page_limit);

-- name: CountAlbumImages :one
SELECT COUNT(*) FROM images WHERE album_id = ?;

-- name: CountMatchingAlbumImages :one
SELECT COUNT(*) FROM images
WHERE album_id = sqlc.arg(album_id)
  AND (sqlc.arg(search) = '' OR instr(hdrbridge_fold(original_name), hdrbridge_fold(sqlc.arg(search))) > 0);

-- name: PageImages :many
SELECT * FROM images
WHERE (sqlc.arg(search) = '' OR instr(hdrbridge_fold(original_name), hdrbridge_fold(sqlc.arg(search))) > 0)
ORDER BY created_at DESC, id DESC LIMIT sqlc.arg(page_limit);

-- name: PageImagesBefore :many
SELECT * FROM images
WHERE (sqlc.arg(search) = '' OR instr(hdrbridge_fold(original_name), hdrbridge_fold(sqlc.arg(search))) > 0)
  AND (created_at, id) < (sqlc.arg(cursor_time), sqlc.arg(cursor_id))
ORDER BY created_at DESC, id DESC LIMIT sqlc.arg(page_limit);

-- name: CountImages :one
SELECT COUNT(*) FROM images;

-- name: CountMatchingImages :one
SELECT COUNT(*) FROM images
WHERE instr(hdrbridge_fold(original_name), hdrbridge_fold(CAST(sqlc.arg(search) AS TEXT))) > 0;

-- name: DeleteAlbumImages :exec
DELETE FROM images WHERE album_id = ?;

-- name: ImageChecksumExists :one
SELECT CAST(EXISTS(SELECT 1 FROM images WHERE source_sha256 = ?) AS BOOLEAN) AS image_exists;

-- name: CreateImage :one
INSERT INTO images (id, album_id, original_name, source_media_type, source_sha256, heic_has_gain_map)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(source_sha256) DO NOTHING
RETURNING *;

-- name: GetImage :one
SELECT * FROM images WHERE id = ?;

-- name: MoveImage :one
UPDATE images SET album_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = ? RETURNING *;

-- name: DeleteImage :exec
DELETE FROM images WHERE id = ?;

-- name: AllImageIDs :many
SELECT id FROM images;
