-- +goose Up
CREATE TABLE albums (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL CHECK(length(trim(name)) BETWEEN 1 AND 120),
    description TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO albums(id, name) VALUES ('uncategorized', '未分类');

CREATE TABLE images (
    id TEXT PRIMARY KEY NOT NULL,
    album_id TEXT NOT NULL REFERENCES albums(id) ON DELETE RESTRICT,
    original_name TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    source_media_type TEXT NOT NULL DEFAULT 'image/jxr'
        CHECK(source_media_type LIKE 'image/%'),
    source_sha256 BLOB NOT NULL CHECK(typeof(source_sha256) = 'blob' AND length(source_sha256) = 32),
    heic_has_gain_map BOOLEAN
        CHECK(heic_has_gain_map IS NULL OR (source_media_type = 'image/jxr' AND heic_has_gain_map IN (0, 1)))
);
CREATE INDEX images_created_at ON images(created_at, id);
CREATE INDEX images_album_created_at ON images(album_id, created_at, id);
CREATE UNIQUE INDEX images_source_sha256 ON images(source_sha256);

-- 数据库提交与文件删除无法原子完成, 保留清理记录以便重启后重试
CREATE TABLE pending_file_deletions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    area TEXT NOT NULL CHECK(area IN ('images', 'staging')),
    entry_id TEXT NOT NULL,
    UNIQUE(area, entry_id)
);

-- +goose Down
DROP TABLE pending_file_deletions;
DROP TABLE images;
DROP TABLE albums;
