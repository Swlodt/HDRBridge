// Package testutil 提供跨包测试共用的图片夹具和数据库故障注入入口
package testutil

import (
	"database/sql"
	"net/url"
	"path/filepath"
	"testing"

	"HDRBridge/internal/store"
)

// Database 为测试夹具打开独立连接, 不暴露生产 Store 的连接池
func Database(t *testing.T, storage *store.Store) *sql.DB {
	t.Helper()
	uri := url.URL{Scheme: "file", Path: filepath.Join(storage.DataDir(), "hdrbridge.db")}
	uri.RawQuery = url.Values{"_pragma": {"foreign_keys(1)", "busy_timeout(5000)"}}.Encode()
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}
