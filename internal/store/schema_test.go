package store

import (
	"crypto/sha256"
	"testing"

	"HDRBridge/internal/store/sqlc"
)

func TestImageSchemaDefaultsAndConstraints(t *testing.T) {
	const sourceMediaType = "image/jxr"
	db := openTestStore(t, t.TempDir())
	checksum := sha256.Sum256([]byte("image"))
	row, err := db.Queries().CreateImage(t.Context(), sqlc.CreateImageParams{
		ID: "image", AlbumID: "uncategorized", OriginalName: "photo.jxr", SourceMediaType: sourceMediaType,
		SourceSha256: checksum[:],
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.SourceMediaType != sourceMediaType || row.CreatedAt == "" || row.UpdatedAt == "" {
		t.Fatalf("新图片的初始状态不正确: %+v", row)
	}
	orphanChecksum := sha256.Sum256([]byte("orphan"))
	if _, err := db.Queries().CreateImage(t.Context(), sqlc.CreateImageParams{
		ID: "orphan", AlbumID: "missing", OriginalName: "photo.jxr", SourceMediaType: sourceMediaType,
		SourceSha256: orphanChecksum[:],
	}); err == nil {
		t.Fatal("外键应阻止图片归属不存在的相册")
	}
	if _, err := db.db.ExecContext(t.Context(), "DELETE FROM albums WHERE id = 'uncategorized'"); err == nil {
		t.Fatal("外键应阻止删除仍有图片的相册")
	}
	if _, err := db.db.ExecContext(t.Context(), "UPDATE images SET source_media_type = 'text/plain' WHERE id = 'image'"); err == nil {
		t.Fatal("格式约束应拒绝非图片 MIME 类型")
	}
}
