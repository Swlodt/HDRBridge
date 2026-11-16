// Package media 识别源图片格式并生成 HDR 转换产物和预览
package media

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"slices"
	"strings"

	"HDRBridge/internal/logging"

	"github.com/gabriel-vasile/mimetype"
)

// Format 以 MIME 类型标识源图片格式
type Format string

const (
	// JPEGXR 是唯一需要转换为 HEIC 的源格式
	JPEGXR Format = "image/jxr"
	// SVG 预览仅作为受限图片资源提供
	SVG Format = "image/svg+xml"
)

var formats = map[Format]struct {
	extensions []string
	browser    bool
}{
	JPEGXR:                {[]string{".jxr"}, false},
	"image/jpeg":          {[]string{".jpg", ".jpeg", ".jpe", ".jfif"}, true},
	"image/png":           {[]string{".png", ".apng"}, true},
	"image/gif":           {[]string{".gif"}, true},
	"image/webp":          {[]string{".webp"}, true},
	"image/avif":          {[]string{".avif"}, true},
	"image/bmp":           {[]string{".bmp"}, true},
	"image/x-icon":        {[]string{".ico"}, true},
	SVG:                   {[]string{".svg"}, true},
	"image/tiff":          {[]string{".tif", ".tiff"}, false},
	"image/heic":          {[]string{".heic", ".heif"}, false},
	"image/heif":          {[]string{".heic", ".heif"}, false},
	"image/heic-sequence": {[]string{".heic", ".heif", ".heics"}, false},
	"image/heif-sequence": {[]string{".heic", ".heif", ".heics"}, false},
	"image/jxl":           {[]string{".jxl"}, false},
}

// MatchesExtension 要求文件名的扩展名与检测到的格式一致
func (f Format) MatchesExtension(ext string) bool {
	return slices.Contains(formats[f].extensions, strings.ToLower(ext))
}

// BrowserPreview 表示原件可以由浏览器直接预览
func (f Format) BrowserPreview() bool { return formats[f].browser }

// DetectFormat 只读取有限的文件头, 不信任客户端声明的 MIME 类型
func DetectFormat(r io.Reader) (Format, error) {
	data, err := readImageHeader(r)
	if err != nil {
		return "", err
	}
	if isJXR(bytes.NewReader(data)) {
		return JPEGXR, nil
	}
	if len(data) >= 8 && string(data[4:8]) == "ftyp" {
		return isoImageFormat(fileTypeBox(data)), nil
	}
	if bytes.HasPrefix(data, []byte("II+\x00\x08\x00\x00\x00")) || bytes.HasPrefix(data, []byte("MM\x00+\x00\x08\x00\x00")) {
		return "image/tiff", nil
	}
	f := Format(mimetype.Detect(data).String())
	if f == "image/vnd.mozilla.apng" {
		f = "image/png"
	}
	if _, ok := formats[f]; ok {
		return f, nil
	}
	return "", nil
}

// HEICHasGainMap 读取 JXR 转换产物的 tmap 品牌, 不解码图像
func HEICHasGainMap(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer logging.CloseResource(file, path)
	data, err := readImageHeader(file)
	if err != nil {
		return false, err
	}
	box := fileTypeBox(data)
	if isoImageFormat(box) != "image/heic" {
		return false, errors.New("无法识别 HEIC 的 ftyp")
	}
	// tmap 必须位于兼容品牌列表, minor_version 或其他 box 中的同名字节不代表 gain map
	for i := 16; i < len(box); i += 4 {
		if string(box[i:i+4]) == "tmap" {
			return true, nil
		}
	}
	return false, nil
}

func readImageHeader(r io.Reader) ([]byte, error) {
	var header [4096]byte
	n, err := io.ReadFull(r, header[:])
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, err
	}
	return header[:n], nil
}

func fileTypeBox(data []byte) []byte {
	if len(data) < 16 || string(data[4:8]) != "ftyp" {
		return nil
	}
	size := int64(binary.BigEndian.Uint32(data[:4]))
	if size < 16 || size > int64(len(data)) || size%4 != 0 {
		return nil
	}
	return data[:int(size)]
}

func isoImageFormat(box []byte) Format {
	var format Format
	var priority int
	for i := 8; i < len(box); i += 4 {
		if i == 12 {
			continue
		}
		// 通用 HEIF brand 可与 HEVC 或 AV1 brand 同时出现, 优先使用具体格式
		switch string(box[i : i+4]) {
		case "avif", "avis":
			return "image/avif"
		case "hevc", "hevx", "hevm", "hevs":
			format, priority = "image/heic-sequence", 4
		case "heic", "heix", "heim", "heis":
			if priority < 3 {
				format, priority = "image/heic", 3
			}
		case "msf1", "avcs":
			if priority < 2 {
				format, priority = "image/heif-sequence", 2
			}
		case "mif1", "avic":
			if priority == 0 {
				format, priority = "image/heif", 1
			}
		}
	}
	return format
}

// isJXR 消耗文件头并校验 JPEG XR 签名, 不代替解码器的完整格式校验
func isJXR(r io.Reader) bool {
	var header [4]byte
	_, err := io.ReadFull(r, header[:])
	return err == nil && header == [4]byte{'I', 'I', 0xbc, 1}
}
