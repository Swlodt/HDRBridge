package media

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestHEICHasGainMap(t *testing.T) {
	ftyp := func(payload string) []byte {
		header := binary.BigEndian.AppendUint32(nil, uint32(8+len(payload)))
		return append(header, ("ftyp" + payload)...)
	}
	path := filepath.Join(t.TempDir(), "output.heic")
	for _, tc := range []struct {
		name string
		data []byte
		hdr  bool
		bad  bool
	}{
		{name: "兼容品牌含增益图", data: ftyp("heix\x00\x00\x00\x00mif1tmap"), hdr: true},
		{name: "其他位置的同名字节", data: append(ftyp("heixtmapmif1"), "\x00\x00\x00\x0cmdattmap"...)},
		{name: "截断品牌列表", data: ftyp("heix\x00\x00\x00\x00tmap")[:18], bad: true},
		{name: "其他 ISO 图像格式", data: ftyp("avif\x00\x00\x00\x00tmap"), bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			hdr, err := HEICHasGainMap(path)
			if hdr != tc.hdr || (err != nil) != tc.bad {
				t.Fatalf("检测到增益图: %v, 错误: %v", hdr, err)
			}
		})
	}
}

func TestIsJXRRejectsTruncatedAndOtherFormats(t *testing.T) {
	signature := []byte{'I', 'I', 0xbc, 1}
	for n := 0; n < len(signature); n++ {
		if isJXR(bytes.NewReader(signature[:n])) {
			t.Errorf("不应接受 %d 字节的截断签名", n)
		}
	}
	if !isJXR(bytes.NewReader(signature)) {
		t.Fatal("拒绝了 JXR 签名")
	}
	if isJXR(bytes.NewReader([]byte{'I', 'I', 42, 0})) {
		t.Fatal("不应将 TIFF 当作 JXR")
	}
}
