-- name: CreateAlbum :one
INSERT INTO albums(id,name,description) VALUES(?,?,?) RETURNING *;

-- name: Albums :many
SELECT * FROM albums ORDER BY id != 'uncategorized', created_at,id;

-- name: GetAlbum :one
SELECT * FROM albums WHERE id = ?;

-- name: UpdateAlbum :one
UPDATE albums
SET name = COALESCE(sqlc.narg(name), name),
    description = COALESCE(sqlc.narg(description), description),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteAlbum :execrows
DELETE FROM albums WHERE id = ? AND id != 'uncategorized';
