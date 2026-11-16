package httpapi_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"mime"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"HDRBridge/internal/library"
	"HDRBridge/internal/media"
	"HDRBridge/internal/testutil"
)

func TestHTTPMixedImageUpload(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("位图预览使用 macOS sips")
	}
	router, _, catalog := newTestRouter(t)
	frame := image.NewPaletted(image.Rect(0, 0, 600, 300), color.Palette{color.Transparent, color.RGBA{R: 255, A: 255}})
	frame.SetColorIndex(0, 0, 1)
	var ani bytes.Buffer
	if err := gif.EncodeAll(&ani, &gif.GIF{Image: []*image.Paletted{frame, frame}, Delay: []int{10, 10}}); err != nil {
		t.Fatal(err)
	}
	heic := testutil.ReadImage(t, "solid.heic")
	jxr := []byte{'I', 'I', 0xbc, 1, 0}
	body, contentType := multipartBody(t,
		uploadFile{"动图.GIF", ani.Bytes()}, uploadFile{"相机.heic", heic}, uploadFile{"hdr.jxr", jxr},
		uploadFile{"伪装.jxr", ani.Bytes()}, uploadFile{"伪装.png", []byte("<html><script>alert(1)</script></html>")},
		uploadFile{"损坏.png", []byte("\x89PNG\r\n\x1a\n")},
	)
	response := postMultipart(router, "/api/albums/uncategorized/images", body, contentType, true)
	var result struct {
		Images []library.Image   `json:"images"`
		Errors []json.RawMessage `json:"errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || response.Code != 201 || len(result.Images) != 3 || len(result.Errors) != 3 {
		t.Fatalf("混合上传响应错误: %d %s (%v)", response.Code, response.Body, err)
	}
	jxrPhoto := result.Images[2]
	if jxrPhoto.SourceMediaType != media.JPEGXR {
		t.Fatalf("JXR 格式错误: %+v", jxrPhoto)
	}
	if jxrPhoto.HEICHasGainMap != nil {
		t.Fatal("无法识别的 HEIC 产物应省略增益图标记")
	}
	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/images/"+jxrPhoto.ID+"/download", nil))
	if res.Code != http.StatusOK || res.Header().Get("Content-Type") != "image/heic" || res.Body.String() != "heic-data" {
		t.Fatalf("JXR 上传成功后应立即可下载: %d %v", res.Code, res.Header())
	}
	ctx := t.Context()
	if err := catalog.RecoverFiles(ctx); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		mediaType string
		data      []byte
	}{
		{"image/gif", ani.Bytes()}, {"image/heic", heic},
	} {
		photo := result.Images[i]
		if string(photo.SourceMediaType) != tc.mediaType || photo.HEICHasGainMap != nil {
			t.Fatalf("原图入库后应可直接使用: %+v", photo)
		}
		base := "/api/images/" + photo.ID
		for _, endpoint := range []string{"/thumbnail", "/preview", "/download"} {
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, base+endpoint, nil))
			if res.Code != 200 {
				t.Fatalf("%s %s: %d %s", photo.OriginalName, endpoint, res.Code, res.Body)
			}
			if endpoint == "/thumbnail" || (endpoint == "/preview" && tc.mediaType == "image/heic") {
				preview, err := png.Decode(res.Body)
				if res.Header().Get("Content-Type") != "image/png" || err != nil || preview.Bounds().Dx() != 512 || preview.Bounds().Dy() > 512 {
					t.Fatalf("%s 未返回有效 PNG 预览: %v", endpoint, err)
				}
				continue
			}
			if res.Header().Get("Content-Type") != tc.mediaType || !bytes.Equal(res.Body.Bytes(), tc.data) {
				t.Fatalf("%s 未保留原文件", endpoint)
			}
			if endpoint == "/download" {
				disposition, params, err := mime.ParseMediaType(res.Header().Get("Content-Disposition"))
				if err != nil || disposition != "attachment" || params["filename"] != photo.OriginalName {
					t.Fatalf("下载文件名错误: %s (%v)", res.Header().Get("Content-Disposition"), err)
				}
			} else if res.Header().Get("Content-Disposition") != "" {
				t.Fatal("详情预览不应触发下载")
			}
		}
		req := httptest.NewRequest(http.MethodGet, base+"/download", nil)
		req.Header.Set("Range", "bytes=0-3")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != http.StatusPartialContent || !bytes.Equal(res.Body.Bytes(), tc.data[:4]) {
			t.Fatalf("原图 Range 响应错误: %d", res.Code)
		}
	}
	page := requestPath(t, router, http.MethodGet, "/api/images", "")
	var ready library.ImagePage
	if err := json.Unmarshal(page.Body.Bytes(), &ready); err != nil || page.Code != 200 || ready.Matched != 3 || ready.Total != 3 {
		t.Fatalf("混合列表统计错误: %d %s (%v)", page.Code, page.Body, err)
	}
}

func TestHTTPSVGPreviewIsSandboxed(t *testing.T) {
	router, _, _ := newTestRouter(t)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	body, contentType := multipartBody(t, uploadFile{"icon.svg", svg})
	res := postMultipart(router, "/api/albums/uncategorized/images", body, contentType, false)
	var result struct {
		Images []library.Image `json:"images"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil || res.Code != 201 || len(result.Images) != 1 {
		t.Fatalf("SVG 上传失败: %d %s (%v)", res.Code, res.Body, err)
	}
	path := "/api/images/" + result.Images[0].ID + "/preview"
	res = httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
	csp := res.Header().Get("Content-Security-Policy")
	if res.Code != 200 || res.Header().Get("Content-Type") != "image/svg+xml" || !bytes.Equal(res.Body.Bytes(), svg) ||
		!strings.HasPrefix(csp, "sandbox;") || !strings.Contains(csp, "default-src 'none'") || res.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("SVG 预览缺少隔离: %d %v", res.Code, res.Header())
	}
}
