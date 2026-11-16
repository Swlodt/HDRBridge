package migrations

import "embed"

// Files 随服务发布版本化迁移
//
//go:embed *.sql
var Files embed.FS
