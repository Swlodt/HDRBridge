package testutil

import (
	"embed"
	"testing"
)

//go:embed testdata/images/*
var images embed.FS

// ReadImage 读取共享的图片夹具, 返回可由当前测试独立修改的字节
func ReadImage(t testing.TB, name string) []byte {
	t.Helper()
	data, err := images.ReadFile("testdata/images/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
