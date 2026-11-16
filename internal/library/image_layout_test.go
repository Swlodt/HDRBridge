package library

import "testing"

func TestValidIDRejectsPaths(t *testing.T) {
	for _, id := range []string{"", ".", "..", "../outside", "/absolute", "nested/image", `nested\image`, "nul\x00byte"} {
		if validImageID(id) {
			t.Errorf("不应接受目录 ID %q", id)
		}
	}
	for _, id := range []string{"legacy-image", "01937d96-1234-7000-8000-123456789abc"} {
		if !validImageID(id) {
			t.Errorf("应保留合法历史 ID %q", id)
		}
	}
}
