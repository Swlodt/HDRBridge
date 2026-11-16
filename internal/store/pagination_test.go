package store

import (
	"database/sql"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestCursorPageUsesIndexRange(t *testing.T) {
	s := openTestStore(t, t.TempDir())
	source, err := os.ReadFile("../../db/queries/images.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, index string }{
		{"PageAlbumImagesBefore", "images_album_created_at"},
		{"PageImagesBefore", "images_created_at"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := strings.Split(strings.Split(string(source), "-- name: "+tc.name+" :many")[1], "-- name:")[0]
			query = regexp.MustCompile(`sqlc.arg\((\w+)\)`).ReplaceAllString(query, ":$1")
			args := []any{sql.Named("search", ""),
				sql.Named("cursor_time", "2026-01-01T00:00:00.000Z"), sql.Named("cursor_id", "cursor"), sql.Named("page_limit", 51)}
			if tc.name == "PageAlbumImagesBefore" {
				args = append(args, sql.Named("album_id", "uncategorized"))
			}
			plan := explainQueryPlan(t, s, query, args...)
			// 游标之前的记录可能很多, 分页查询必须直接定位到复合键范围
			if !strings.Contains(plan, "INDEX "+tc.index) || !strings.Contains(plan, "(created_at,id)<(?,?)") || strings.Contains(plan, "TEMP B-TREE") {
				t.Fatalf("深分页没有利用索引范围和顺序: %s", plan)
			}
		})
	}
}

func TestCountAlbumImagesUsesCoveringIndex(t *testing.T) {
	s := openTestStore(t, t.TempDir())
	source, err := os.ReadFile("../../db/queries/images.sql")
	if err != nil {
		t.Fatal(err)
	}
	query := strings.Split(strings.Split(string(source), "-- name: CountAlbumImages :one")[1], "-- name:")[0]
	plan := explainQueryPlan(t, s, query, "uncategorized")
	if !strings.Contains(plan, "COVERING INDEX images_album_created_at") {
		t.Fatalf("相册计数应使用覆盖索引: %s", plan)
	}
}

func explainQueryPlan(t *testing.T, s *Store, query string, args ...any) string {
	t.Helper()
	rows, err := s.db.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(details, "\n")
}
