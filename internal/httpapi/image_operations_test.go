package httpapi_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"HDRBridge/internal/config"
	httpapi "HDRBridge/internal/httpapi"
	"HDRBridge/internal/library"
	"HDRBridge/internal/media"
	"HDRBridge/internal/testutil"
)

type uploadFile struct {
	name string
	data []byte
}

func TestHTTPRealConversionWorkflow(t *testing.T) {
	if os.Getenv("HDRBRIDGE_REAL_TOOLS") != "1" {
		t.Skip("设置 HDRBRIDGE_REAL_TOOLS=1 验证上传到下载的真实工具链")
	}
	db := openTestStore(t)
	cfg, err := config.Load("../../config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	pipeline, err := media.NewJXR(cfg.Tools, cfg.Convert)
	if err != nil {
		t.Fatal(err)
	}

	catalog := library.NewService(db, pipeline, media.NewRaster(cfg.Convert.Timeout))
	router := httpapi.New(catalog, cfg.Upload)
	body, contentType := multipartBody(t,
		uploadFile{"broken.jxr", []byte{'I', 'I', 0xbc, 1}},
		uploadFile{"hdr.jxr", testutil.ReadImage(t, "hdr.jxr")},
		uploadFile{"sdr.jxr", testutil.ReadImage(t, "sdr.jxr")},
	)
	response := postMultipart(router, "/api/albums/uncategorized/images", body, contentType, true)
	var uploaded struct {
		Images []library.Image `json:"images"`
		Errors []struct {
			Name  string `json:"name"`
			Error string `json:"error"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &uploaded); err != nil || response.Code != 201 || len(uploaded.Images) != 2 {
		t.Fatalf("上传失败: %s %v", response.Body.String(), err)
	}
	if len(uploaded.Errors) != 1 || uploaded.Errors[0].Name != "broken.jxr" || !strings.Contains(uploaded.Errors[0].Error, "JXR 转换失败") {
		t.Fatalf("批次应只报告损坏 JXR 转换失败: %+v", uploaded.Errors)
	}
	for _, photo := range uploaded.Images {
		wantGainMap := photo.OriginalName == "hdr.jxr"
		if photo.HEICHasGainMap == nil || *photo.HEICHasGainMap != wantGainMap {
			t.Fatalf("上传响应的增益图标记错误: %s %v", photo.OriginalName, response.Body)
		}
		path := "/api/images/" + photo.ID
		detail := requestPath(t, router, http.MethodGet, path, "")
		var saved library.Image
		if err := json.Unmarshal(detail.Body.Bytes(), &saved); err != nil || detail.Code != 200 ||
			saved.HEICHasGainMap == nil || *saved.HEICHasGainMap != wantGainMap {
			t.Fatalf("详情未返回保存的增益图标记: %s %s (%v)", photo.OriginalName, detail.Body, err)
		}
		download := httptest.NewRecorder()
		router.ServeHTTP(download, httptest.NewRequest(http.MethodGet, path+"/download", nil))
		data := download.Body.Bytes()
		if download.Code != 200 || download.Header().Get("Content-Type") != "image/heic" ||
			len(data) < 16 || string(data[4:8]) != "ftyp" {
			t.Fatalf("下载 HEIC 失败: %s, 状态 %d", photo.OriginalName, download.Code)
		}
		thumb := httptest.NewRecorder()
		router.ServeHTTP(thumb, httptest.NewRequest(http.MethodGet, path+"/thumbnail", nil))
		thumbnail, err := jpeg.Decode(thumb.Body)
		if thumb.Code != 200 || err != nil {
			t.Fatalf("下载缩略图失败: %s, 状态 %d, %v", photo.OriginalName, thumb.Code, err)
		}
		if wantGainMap {
			mid := color.NRGBAModel.Convert(thumbnail.At(32, 32)).(color.NRGBA)
			if mid.R < 220 || mid.R > 235 || mid.G > 5 || mid.B < 225 || mid.B > 245 {
				t.Fatalf("渐变中点的缩略图亮度或颜色不正确: %+v", mid)
			}
		}
		if _, err := os.Stat(filepath.Join(db.ImagesRoot(), photo.ID, ".work")); !os.IsNotExist(err) {
			t.Fatalf("中间文件未清理: %v", err)
		}
	}
}

func multipartBody(t *testing.T, files ...uploadFile) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for _, file := range files {
		part, err := w.CreateFormFile("files", file.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return body.Bytes(), w.FormDataContentType()
}

func postMultipart(router http.Handler, path string, body []byte, contentType string, chunked bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	if chunked {
		req.ContentLength = -1
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func TestHTTPBatchUploadLimits(t *testing.T) {
	valid := []byte{'I', 'I', 0xbc, 1, 1, 2, 3, 4}
	for _, tc := range []struct {
		name                     string
		files                    []uploadFile
		chunked                  bool
		status, accepted, failed int
	}{
		{"部分文件失败", []uploadFile{{"good.JXR", valid}, {"fake.jxr", []byte("invalid")}, {"next.jxr", []byte{'I', 'I', 0xbc, 1, 0}}}, false, 201, 2, 1},
		{"单文件超限", []uploadFile{{"big.jxr", append(append([]byte{}, valid...), 1)}, {"next.jxr", valid}}, true, 201, 1, 1},
		{"全部超限", []uploadFile{{"big.jxr", append(append([]byte{}, valid...), 1)}}, false, 413, 0, 1},
		{"请求超限", []uploadFile{{"good.jxr", valid}, {"big.jxr", bytes.Repeat([]byte{1}, 2048)}}, false, 413, 0, 0},
		{"流式请求超限", []uploadFile{{"good.jxr", valid}, {"big.jxr", bytes.Repeat([]byte{1}, 2048)}}, true, 413, 1, -1},
		{"空批次", nil, false, 400, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestStore(t)

			catalog := library.NewService(db, testConverter{}, media.NewRaster(30*time.Second))
			router := httpapi.New(catalog, config.Upload{MaxFileBytes: 8, MaxRequestBytes: 1024})
			body, contentType := multipartBody(t, tc.files...)
			response := postMultipart(router, "/api/albums/uncategorized/images", body, contentType, tc.chunked)
			var result struct {
				Images []library.Image   `json:"images"`
				Errors []json.RawMessage `json:"errors"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if response.Code != tc.status || len(result.Images) != tc.accepted || (tc.failed >= 0 && len(result.Errors) != tc.failed) {
				t.Fatalf("响应错误: %d %s", response.Code, response.Body.String())
			}
			page, err := catalog.PageByAlbum(t.Context(), "uncategorized", library.PageInput{})
			listed := page.Images
			if err != nil || len(listed) != tc.accepted {
				t.Fatalf("持久化结果错误: %+v, %v", listed, err)
			}
			entries, err := os.ReadDir(db.StagingRoot())
			if err != nil || len(entries) != 0 {
				t.Fatalf("staging 残留: %v, %v", entries, err)
			}
		})
	}
}

func TestHTTPUploadRejectsDuplicateContent(t *testing.T) {
	router, db, catalog := newTestRouter(t)
	album, err := catalog.CreateAlbum(t.Context(), "另一相册", "")
	if err != nil {
		t.Fatal(err)
	}
	original := []byte{'I', 'I', 0xbc, 1, 1}
	different := []byte{'I', 'I', 0xbc, 1, 2}
	for _, tc := range []struct {
		name, albumID string
		files         []uploadFile
		status        int
		saved         int
	}{
		{"同批重复", "uncategorized", []uploadFile{{"a.jxr", original}, {"copy.jxr", original}}, http.StatusCreated, 1},
		{"换相册重传", album.ID, []uploadFile{{"copy.jxr", original}}, http.StatusConflict, 0},
		{"同名不同内容", album.ID, []uploadFile{{"a.jxr", different}}, http.StatusCreated, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, contentType := multipartBody(t, tc.files...)
			res := postMultipart(router, "/api/albums/"+tc.albumID+"/images", body, contentType, false)
			var result struct {
				Images []library.Image `json:"images"`
				Errors []struct {
					Name   string `json:"name"`
					Error  string `json:"error"`
					Status int    `json:"status"`
				} `json:"errors"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &result); err != nil || res.Code != tc.status || len(result.Images) != tc.saved || len(result.Errors) != len(tc.files)-tc.saved {
				t.Fatalf("上传响应错误: %d %s (%v)", res.Code, res.Body, err)
			}
			for _, failure := range result.Errors {
				if failure.Name != "copy.jxr" || failure.Status != http.StatusConflict || failure.Error != "操作冲突: 图片已存在" {
					t.Fatalf("重复上传提示错误: %+v", failure)
				}
			}
		})
	}
	if saved, err := os.ReadDir(db.ImagesRoot()); err != nil || len(saved) != 2 {
		t.Fatalf("重复上传不应留下文件: %v, %v", saved, err)
	}
	if staging, err := os.ReadDir(db.StagingRoot()); err != nil || len(staging) != 0 {
		t.Fatalf("暂存目录未清理: %v, %v", staging, err)
	}
}

func TestHTTPImageLifecycleAndArtifacts(t *testing.T) {
	router, db, catalog := newTestRouter(t)
	body, contentType := multipartBody(t, uploadFile{"日落.jxr", []byte{'I', 'I', 0xbc, 1, 0}})
	response := postMultipart(router, "/api/albums/uncategorized/images", body, contentType, false)
	var uploaded struct {
		Images []library.Image `json:"images"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &uploaded); err != nil || response.Code != 201 || len(uploaded.Images) != 1 {
		t.Fatalf("上传失败: %s %v", response.Body.String(), err)
	}
	id := uploaded.Images[0].ID
	path := "/api/images/" + id
	if _, err := testutil.Database(t, db).ExecContext(t.Context(), "INSERT INTO albums(id,name) VALUES ('target','目标相册')"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, suffix, body string
		status               int
	}{
		{http.MethodGet, "", "", 200},
		{http.MethodGet, "/thumbnail", "", 404},
		{http.MethodPatch, "", `{}`, 400},
		{http.MethodPatch, "", `{"album_id":null}`, 400},
		{http.MethodPatch, "", `{"album_id":"missing"}`, 404},
		{http.MethodPatch, "", `{"album_id":"target"}`, 200},
	} {
		response := requestPath(t, router, tc.method, path+tc.suffix, tc.body)
		if response.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.suffix, response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "source_path") {
			t.Fatal("响应泄露存储路径")
		}
	}
	dir := filepath.Join(db.ImagesRoot(), id)
	if _, err := os.Stat(filepath.Join(dir, "source.jxr")); err != nil {
		t.Fatalf("移动图片改变了源文件位置: %v", err)
	}
	row, err := db.Queries().GetImage(t.Context(), id)
	if err != nil || row.AlbumID != "target" {
		t.Fatalf("移动后的归属错误: %+v %v", row, err)
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "thumbnail.jpg"), jpg.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	for _, thumbnail := range []bool{false, true} {
		suffix, contentType := "/download", "image/heic"
		if thumbnail {
			suffix, contentType = "/thumbnail", "image/jpeg"
		}
		req := httptest.NewRequest(http.MethodGet, path+suffix, nil)
		if !thumbnail {
			req.Header.Set("Range", "bytes=0-3")
		}
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		if response.Header().Get("Content-Type") != contentType {
			t.Fatalf("文件类型错误: %s", response.Header())
		}
		if thumbnail {
			if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), jpg.Bytes()) {
				t.Fatal("缩略图内容错误")
			}
		} else {
			if response.Code != 206 || response.Body.String() != "heic" || !strings.Contains(response.Header().Get("Content-Disposition"), "attachment") {
				t.Fatalf("下载响应错误: %d %s", response.Code, response.Body.String())
			}
		}
	}
	if err := os.Remove(filepath.Join(dir, "output.heic")); err != nil {
		t.Fatal(err)
	}
	if response := requestPath(t, router, http.MethodGet, path+"/download", ""); response.Code != 404 {
		t.Fatalf("缺失的结果应返回 404: %d", response.Code)
	}
	if response := requestPath(t, router, http.MethodDelete, path, ""); response.Code != 204 {
		t.Fatalf("删除失败: %s", response.Body.String())
	}
	if response := requestPath(t, router, http.MethodGet, path, ""); response.Code != 404 {
		t.Fatalf("删除后应为 404: %d", response.Code)
	}
	if err := catalog.CleanupFiles(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("文件未清理: %v", err)
	}
}
