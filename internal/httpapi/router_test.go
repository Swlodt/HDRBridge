package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"HDRBridge/internal/config"
	httpapi "HDRBridge/internal/httpapi"
	"HDRBridge/internal/library"
	"HDRBridge/internal/media"
	"HDRBridge/internal/store"
	"HDRBridge/internal/store/sqlc"
)

func TestAlbumHTTPInvalidInput(t *testing.T) {
	router, _, _ := newTestRouter(t)
	before := listAlbums(t, router)
	for _, tc := range []struct{ name, body, reason string }{
		{"损坏的 JSON", `{"name":`, "JSON"},
		{"空请求体", "", "JSON"},
		{"字段类型错误", `{"name":123}`, "JSON"},
		{"缺少名称", `{}`, "名称"},
		{"空白名称", `{"name":" \t\n "}`, "名称"},
		{"名称超长", `{"name":"` + strings.Repeat("旅", 121) + `"}`, "名称"},
		{"描述超长", `{"name":"旅行","description":"` + strings.Repeat("图", 2001) + `"}`, "描述"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := request(t, router, http.MethodPost, tc.body)
			if response.Code != http.StatusBadRequest {
				t.Errorf("应返回 400, 得到 %d: %s", response.Code, response.Body.String())
			}
			var problem struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &problem); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(problem.Error, tc.reason) {
				t.Errorf("错误 %q 应包含 %q", problem.Error, tc.reason)
			}
			if after := listAlbums(t, router); !slices.Equal(before, after) {
				t.Errorf("失败请求改变了相册列表: %+v", after)
			}
		})
	}
}

func newTestRouter(t *testing.T) (http.Handler, *store.Store, *library.Service) {
	t.Helper()
	db := openTestStore(t)
	catalog := library.NewService(db, testConverter{}, media.NewRaster(30*time.Second))
	return httpapi.New(catalog, config.Upload{MaxFileBytes: 256 << 20, MaxRequestBytes: 1 << 30}), db, catalog
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	db, err := store.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	return db
}

type testConverter struct{}

func (testConverter) Convert(_ context.Context, dir string) error {
	return os.WriteFile(filepath.Join(dir, media.HDRName), []byte("heic-data"), 0600)
}

func request(t *testing.T, router http.Handler, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	return requestPath(t, router, method, "/api/albums", body)
}

func requestPath(t *testing.T, router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusNoContent && !strings.HasPrefix(response.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("响应应使用 JSON Content-Type, 得到 %q", response.Header().Get("Content-Type"))
	}
	return response
}

func listAlbums(t *testing.T, router http.Handler) []sqlc.Album {
	t.Helper()
	response := request(t, router, http.MethodGet, "")
	if response.Code != http.StatusOK {
		t.Fatalf("获取相册返回 %d: %s", response.Code, response.Body.String())
	}
	var albums []sqlc.Album
	if err := json.Unmarshal(response.Body.Bytes(), &albums); err != nil {
		t.Fatal(err)
	}
	if albums == nil {
		t.Fatal("相册列表应为 JSON 数组")
	}
	return albums
}
