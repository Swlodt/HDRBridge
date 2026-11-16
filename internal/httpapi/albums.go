package httpapi

import (
	"net/http"
	"net/url"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/library"

	"github.com/gin-gonic/gin"
)

func (a api) listAlbums(c *gin.Context) {
	res, err := a.catalog.ListAlbums(c.Request.Context())
	if err != nil {
		problem(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (a api) createAlbum(c *gin.Context) {
	var in struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if !bind(c, &in) {
		return
	}
	created, err := a.catalog.CreateAlbum(c.Request.Context(), in.Name, in.Description)
	if err != nil {
		problem(c, err)
		return
	}
	c.JSON(http.StatusCreated, created)
}

func (a api) getAlbum(c *gin.Context) {
	res, err := a.catalog.GetAlbum(c.Request.Context(), c.Param("id"))
	if err != nil {
		problem(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (a api) updateAlbum(c *gin.Context) {
	var in struct {
		Name        optionalString `json:"name"`
		Description optionalString `json:"description"`
	}
	if !bind(c, &in) {
		return
	}
	res, err := a.catalog.UpdateAlbum(c.Request.Context(), c.Param("id"), library.AlbumUpdate{
		Name: in.Name.value, Description: in.Description.value,
	})
	if err != nil {
		problem(c, err)
		return
	}
	c.JSON(http.StatusOK, res)
}

func (a api) deleteAlbum(c *gin.Context) {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		problem(c, apperr.New(apperr.ErrInvalid, "查询参数不正确"))
		return
	}
	deleteImages := false
	for key, values := range query {
		if key != "delete_images" || len(values) != 1 || (values[0] != "true" && values[0] != "false") {
			problem(c, apperr.New(apperr.ErrInvalid, "仅支持 delete_images=true 或 false"))
			return
		}
		deleteImages = values[0] == "true"
	}
	if err := a.catalog.DeleteAlbum(c.Request.Context(), c.Param("id"), deleteImages); err != nil {
		problem(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
