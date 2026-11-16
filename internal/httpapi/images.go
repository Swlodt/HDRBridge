package httpapi

import (
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/library"
	"HDRBridge/internal/logging"
	"HDRBridge/internal/media"

	"github.com/gin-gonic/gin"
)

func (a api) listImages(c *gin.Context) {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		problem(c, apperr.New(apperr.ErrInvalid, "查询参数不正确"))
		return
	}
	in := library.PageInput{}
	for key, values := range query {
		if len(values) != 1 {
			problem(c, apperr.New(apperr.ErrInvalid, "查询参数不正确"))
			return
		}
		switch key {
		case "search":
			in.Search = values[0]
		case "cursor":
			in.Cursor = values[0]
			if in.Cursor == "" {
				problem(c, apperr.New(apperr.ErrInvalid, "游标不能为空"))
				return
			}
		case "limit":
			in.Limit, err = strconv.Atoi(values[0])
			if err != nil || in.Limit < 1 || in.Limit > 100 {
				problem(c, apperr.New(apperr.ErrInvalid, "limit 必须为 1–100"))
				return
			}
		default:
			problem(c, apperr.New(apperr.ErrInvalid, "不支持的查询参数"))
			return
		}
	}
	var res library.ImagePage
	if id := c.Param("id"); id != "" {
		res, err = a.catalog.PageByAlbum(c.Request.Context(), id, in)
	} else {
		res, err = a.catalog.PageAll(c.Request.Context(), in)
	}
	if err != nil {
		problem(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (a api) getImage(c *gin.Context) {
	res, err := a.catalog.GetImage(c.Request.Context(), c.Param("id"))
	if err != nil {
		problem(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (a api) moveImage(c *gin.Context) {
	var in struct {
		AlbumID optionalString `json:"album_id"`
	}
	if !bind(c, &in) {
		return
	}
	if in.AlbumID.value == nil {
		problem(c, apperr.New(apperr.ErrInvalid, "必须提供 album_id"))
		return
	}
	res, err := a.catalog.MoveImage(c.Request.Context(), c.Param("id"), *in.AlbumID.value)
	if err != nil {
		problem(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (a api) deleteImage(c *gin.Context) {
	if err := a.catalog.DeleteImage(c.Request.Context(), c.Param("id")); err != nil {
		problem(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (a api) thumbnail(c *gin.Context) {
	file, err := a.catalog.OpenPreview(c.Request.Context(), c.Param("id"), true)
	serveArtifact(c, file, err)
}

func (a api) preview(c *gin.Context) {
	file, err := a.catalog.OpenPreview(c.Request.Context(), c.Param("id"), false)
	serveArtifact(c, file, err)
}

func (a api) download(c *gin.Context) {
	file, err := a.catalog.OpenDownload(c.Request.Context(), c.Param("id"))
	serveArtifact(c, file, err)
}

func serveArtifact(c *gin.Context, file library.Artifact, err error) {
	if err != nil {
		problem(c, err)
		return
	}
	f := file.File
	defer logging.CloseResource(f, f.Name())
	info, err := f.Stat()
	if err != nil {
		problem(c, err)
		return
	}
	c.Header("Cache-Control", "no-cache")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Type", file.MediaType)
	if file.MediaType == string(media.SVG) {
		// SVG 被直接导航打开时也不能运行脚本或加载外部资源
		c.Header("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src data:")
	}
	if file.Name != "" {
		c.Header("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": file.Name}))
	}
	http.ServeContent(c.Writer, c.Request, info.Name(), info.ModTime(), f)
}
