package library

import (
	"math"
	"testing"

	"HDRBridge/internal/store"
	"HDRBridge/internal/store/sqlc"
)

func openTestStore(t *testing.T, root string) *store.Store {
	t.Helper()
	db, err := store.Open(t.Context(), root)
	if err != nil {
		t.Fatalf("Open(%q): %v", root, err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("关闭存储 %q: %v", root, err)
		}
	})
	return db
}

func pendingFiles(t *testing.T, db *store.Store) ([]sqlc.PendingFileDeletion, error) {
	t.Helper()
	return db.Queries().PendingFileDeletionBatch(t.Context(), sqlc.PendingFileDeletionBatchParams{ThroughID: math.MaxInt64, BatchLimit: 1000})
}
