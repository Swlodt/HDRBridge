package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"HDRBridge/internal/library"
	"HDRBridge/internal/store/sqlc"
	"HDRBridge/internal/testutil"
)

func TestAlbumHTTPLifecycle(t *testing.T) {
	router, _, _ := newTestRouter(t)
	initial := listAlbums(t, router)
	if len(initial) != 1 || initial[0].ID != "uncategorized" || initial[0].Name != "未分类" {
		t.Fatalf("新库相册错误: %+v", initial)
	}

	response := request(t, router, http.MethodPost, `{"name":"  旅行  ","description":"  原描述  "}`)
	if response.Code != http.StatusCreated {
		t.Fatalf("创建相册返回 %d: %s", response.Code, response.Body.String())
	}
	var created sqlc.Album
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.ID == "uncategorized" || created.Name != "旅行" || created.Description != "原描述" {
		t.Fatalf("创建结果错误: %+v", created)
	}
	albums := listAlbums(t, router)
	if len(albums) != 2 || albums[0] != initial[0] || albums[1] != created {
		t.Fatalf("创建后列表错误: %+v", albums)
	}

	response = request(t, router, http.MethodPost, `{"name":"周末"}`)
	var second sqlc.Album
	if err := json.Unmarshal(response.Body.Bytes(), &second); err != nil || response.Code != http.StatusCreated {
		t.Fatalf("创建第二个相册失败: %s, %v", response.Body.String(), err)
	}
	if second.ID == "" || second.ID == initial[0].ID || second.ID == created.ID {
		t.Fatalf("第二个相册的 ID 无效或重复: %q", second.ID)
	}

	path := "/api/albums/" + created.ID
	for _, tc := range []struct {
		method, path, body string
		status             int
		name, description  string
	}{
		{http.MethodGet, path, "", 200, "旅行", "原描述"},
		{http.MethodPatch, path, `{"name":"  新相册  "}`, 200, "新相册", "原描述"},
		{http.MethodPatch, path, `{"description":"新描述"}`, 200, "新相册", "新描述"},
		{http.MethodPatch, path, `{"description":""}`, 200, "新相册", ""},
		{http.MethodGet, path, "", 200, "新相册", ""},
		{http.MethodGet, path + "/images", "", 200, "", ""},
		{http.MethodDelete, path, "", 204, "", ""},
		{http.MethodGet, path, "", 404, "", ""},
		{http.MethodGet, path + "/images", "", 404, "", ""},
		{http.MethodPatch, path, `{"name":"新名称"}`, 404, "", ""},
		{http.MethodDelete, path, "", 404, "", ""},
	} {
		response := requestPath(t, router, tc.method, tc.path, tc.body)
		if response.Code != tc.status {
			t.Fatalf("%s %s 应返回 %d, 得到 %d: %s", tc.method, tc.path, tc.status, response.Code, response.Body.String())
		}
		if tc.name != "" {
			var got sqlc.Album
			if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil || got.ID != created.ID || got.Name != tc.name || got.Description != tc.description {
				t.Fatalf("相册响应错误: %s, %v", response.Body.String(), err)
			}
		} else if tc.status == 200 && !strings.Contains(response.Body.String(), `"images":[]`) {
			t.Fatalf("空相册应返回空图片列表: %s", response.Body.String())
		} else if tc.status == 204 && response.Body.Len() != 0 {
			t.Fatalf("204 不应返回响应体: %s", response.Body.String())
		}
	}
}

func TestAlbumHTTPInvalidMutations(t *testing.T) {
	router, _, catalog := newTestRouter(t)
	created, err := catalog.CreateAlbum(t.Context(), "原名称", "原描述")
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/albums/" + created.ID
	for _, body := range []string{
		`{}`, `null`, `[]`, `{"name":null}`, `{"description":null}`, `{"name":42}`,
		`{"name":" "}`, `{"name":"` + strings.Repeat("旅", 121) + `"}`,
		`{"description":"` + strings.Repeat("图", 2001) + `"}`,
		`{"unexpected":true}`, `{"name":"新名称"} {}`, `{"name":"新名称"} trailing`,
		`{"name":"新名称"}` + strings.Repeat(" ", 16<<10),
	} {
		response := requestPath(t, router, http.MethodPatch, path, body)
		if response.Code != 400 {
			t.Fatalf("无效 PATCH 应返回 400, 得到 %d: %s", response.Code, response.Body.String())
		}
	}
	for _, query := range []string{"delete_images=1", "delete_images=", "delete_images=true&delete_images=false", "delete_images=invalid", "delete_images=%ZZ", "delete_image=true"} {
		response := requestPath(t, router, http.MethodDelete, path+"?"+query, "")
		if response.Code != 400 {
			t.Fatalf("无效删除参数 %q 应返回 400, 得到 %d: %s", query, response.Code, response.Body.String())
		}
	}
	got, err := catalog.GetAlbum(t.Context(), created.ID)
	if err != nil || got != created {
		t.Fatalf("无效请求改变了相册: %+v, %v", got, err)
	}
	for _, method := range []string{http.MethodPatch, http.MethodDelete} {
		response := requestPath(t, router, method, "/api/albums/uncategorized", `{"name":"改名"}`)
		if response.Code != 409 {
			t.Fatalf("内置相册保护应返回 409, 得到 %d: %s", response.Code, response.Body.String())
		}
	}
}

func TestAlbumHTTPRejectsNULInName(t *testing.T) {
	router, _, catalog := newTestRouter(t)
	created, err := catalog.CreateAlbum(t.Context(), "原名称", "原描述")
	if err != nil {
		t.Fatal(err)
	}
	before, err := catalog.ListAlbums(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"name":"\u0000A"}`,
		`{"name":"A\u0000B"}`,
		`{"name":"A\u0000"}`,
	} {
		for _, target := range []struct{ method, path string }{
			{http.MethodPost, "/api/albums"},
			{http.MethodPatch, "/api/albums/" + created.ID},
		} {
			response := requestPath(t, router, target.method, target.path, body)
			if response.Code != http.StatusBadRequest {
				t.Errorf("%s %s 应返回 400, 得到 %d: %s", target.method, body, response.Code, response.Body.String())
			}
		}
	}
	after, err := catalog.ListAlbums(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(before, after) {
		t.Errorf("无效请求改变了相册: 原有 %+v, 现有 %+v", before, after)
	}
}

func TestAlbumHTTPDeleteModes(t *testing.T) {
	for _, mode := range []struct {
		query     string
		remaining int
	}{
		{"", 3},
		{"?delete_images=false", 3},
		{"?delete_images=true", 1},
	} {
		t.Run(mode.query, func(t *testing.T) {
			router, db, _ := newTestRouter(t)
			_, err := testutil.Database(t, db).ExecContext(t.Context(), `
				INSERT INTO albums (id, name) VALUES ('travel', '旅行');
				WITH seeds(id, album_id, original_name, created_at) AS (VALUES
				('a', 'travel', 'a.jxr', '2020-01-01T00:00:00.000Z'),
				('b', 'travel', 'b.jxr', '2020-01-01T00:00:00.000Z'),
				('other', 'uncategorized', 'c.jxr', '2021-01-01T00:00:00.000Z')
				)
				INSERT INTO images (id, album_id, original_name, created_at, source_sha256)
				SELECT id, album_id, original_name, created_at, CAST(printf('%032s', id) AS BLOB) FROM seeds;`)
			if err != nil {
				t.Fatal(err)
			}
			response := requestPath(t, router, http.MethodDelete, "/api/albums/travel"+mode.query, "")
			if response.Code != http.StatusNoContent {
				t.Fatalf("删除结果错误: %d, %s", response.Code, response.Body.String())
			}
			var count int
			if err := testutil.Database(t, db).QueryRowContext(t.Context(), "SELECT COUNT(*) FROM images").Scan(&count); err != nil || count != mode.remaining {
				t.Fatalf("删除后图片数量错误: %d, %v", count, err)
			}
		})
	}
}

func TestAlbumHTTPDatabaseFailure(t *testing.T) {
	router, db, _ := newTestRouter(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/albums"},
		{http.MethodPost, "/api/albums"},
		{http.MethodGet, "/api/albums/any"},
		{http.MethodGet, "/api/albums/any/images"},
		{http.MethodPatch, "/api/albums/any"},
		{http.MethodDelete, "/api/albums/any"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			response := requestPath(t, router, tc.method, tc.path, `{"name":"旅行"}`)
			var problem map[string]string
			err := json.Unmarshal(response.Body.Bytes(), &problem)
			if response.Code != http.StatusInternalServerError || err != nil || len(problem) != 1 || problem["error"] != "服务器内部错误" {
				t.Fatalf("数据库故障响应错误: %d, %s (%v)", response.Code, response.Body.String(), err)
			}
		})
	}
}

func TestImagePaginationAndSearch(t *testing.T) {
	router, db, _ := newTestRouter(t)
	_, err := testutil.Database(t, db).ExecContext(t.Context(), `
		WITH seeds(id, album_id, original_name, created_at) AS (VALUES
		('a', 'uncategorized', 'ÄBC.jxr', '2020-01-01T00:00:00.000Z'),
		('b', 'uncategorized', 'Äbc.jxr', '2020-01-01T00:00:00.000Z'),
		('c', 'uncategorized', 'other.jxr', '2020-01-01T00:00:00.000Z'),
		('d', 'uncategorized', 'abc.jxr', '2020-01-01T00:00:00.000Z')
		)
		INSERT INTO images (id, album_id, original_name, created_at, source_sha256)
		SELECT id, album_id, original_name, created_at, CAST(printf('%032s', id) AS BLOB) FROM seeds`)
	if err != nil {
		t.Fatal(err)
	}
	getPage := func(query string) library.ImagePage {
		t.Helper()
		response := requestPath(t, router, http.MethodGet, "/api/albums/uncategorized/images"+query, "")
		var page library.ImagePage
		if err := json.Unmarshal(response.Body.Bytes(), &page); response.Code != 200 || err != nil {
			t.Fatalf("分页失败: %d %s %v", response.Code, response.Body.String(), err)
		}
		if strings.Contains(response.Body.String(), "source_path") || strings.Contains(response.Body.String(), "images/a/source.jxr") {
			t.Fatal("图片列表泄露磁盘路径")
		}
		return page
	}
	first := getPage("?limit=2")
	if len(first.Images) != 2 || first.Images[0].ID != "d" || first.Images[1].ID != "c" || first.NextCursor == nil || first.Matched != 4 || first.Total != 4 {
		t.Fatalf("首页错误: %+v", first)
	}
	second := getPage("?limit=2&cursor=" + url.QueryEscape(*first.NextCursor))
	if len(second.Images) != 2 || second.Images[0].ID != "b" || second.Images[1].ID != "a" || second.NextCursor != nil {
		t.Fatalf("第二页错误: %+v", second)
	}
	filtered := getPage("?search=" + url.QueryEscape("äB"))
	if filtered.Matched != 2 || len(filtered.Images) != 2 || filtered.Images[0].ID != "b" || filtered.Images[1].ID != "a" || filtered.Total != 4 {
		t.Fatalf("筛选或全局计数错误: %+v", filtered)
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=x", "?cursor=invalid", "?filter=unknown", "?filter=", "?search=a&search=b", "?other=1"} {
		response := requestPath(t, router, http.MethodGet, "/api/albums/uncategorized/images"+query, "")
		if response.Code != 400 {
			t.Fatalf("无效参数 %s: %d %s", query, response.Code, response.Body.String())
		}
	}
}

func TestLibraryImagesAcrossAlbums(t *testing.T) {
	router, db, _ := newTestRouter(t)
	_, err := testutil.Database(t, db).ExecContext(t.Context(), `
		INSERT INTO albums (id, name) VALUES ('trip', '旅行');
		WITH seeds(id, album_id, original_name, created_at) AS (VALUES
		('a', 'uncategorized', 'ÄBC.jxr', '2026-01-01T00:00:00.000Z'),
		('b', 'trip', 'Äbc.jxr', '2026-01-01T00:00:00.000Z'),
		('c', 'trip', 'other.jxr', '2026-01-02T00:00:00.000Z'),
		('d', 'uncategorized', 'pending.jxr', '2026-01-03T00:00:00.000Z')
		)
		INSERT INTO images (id, album_id, original_name, created_at, source_sha256)
		SELECT id, album_id, original_name, created_at, CAST(printf('%032s', id) AS BLOB) FROM seeds`)
	if err != nil {
		t.Fatal(err)
	}
	query := "/api/images?limit=1&search=" + url.QueryEscape("äB")
	for i, want := range []struct{ id, albumID string }{{"b", "trip"}, {"a", "uncategorized"}} {
		response := requestPath(t, router, http.MethodGet, query, "")
		var page library.ImagePage
		if err := json.Unmarshal(response.Body.Bytes(), &page); response.Code != 200 || err != nil {
			t.Fatalf("资料库分页失败: %d %s %v", response.Code, response.Body.String(), err)
		}
		if page.Total != 4 || page.Matched != 2 || len(page.Images) != 1 || page.Images[0].ID != want.id || page.Images[0].AlbumID != want.albumID {
			t.Fatalf("第 %d 页图片或统计错误: %+v", i+1, page)
		}
		if i == 0 && page.NextCursor != nil {
			query += "&cursor=" + url.QueryEscape(*page.NextCursor)
		} else if i == 0 || page.NextCursor != nil {
			t.Fatalf("第 %d 页游标错误: %+v", i+1, page)
		}
	}
	if response := requestPath(t, router, http.MethodGet, "/api/images?cursor=invalid", ""); response.Code != 400 {
		t.Fatalf("无效资料库游标应返回 400: %d %s", response.Code, response.Body.String())
	}
}
