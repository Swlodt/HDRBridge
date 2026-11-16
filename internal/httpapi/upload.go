package httpapi

import (
	"errors"
	"io"
	"net/http"

	"HDRBridge/internal/apperr"
	"HDRBridge/internal/library"
	"HDRBridge/internal/logging"

	"github.com/gin-gonic/gin"
)

type uploadFailure struct {
	Name   string `json:"name"`
	Error  string `json:"error"`
	Status int    `json:"status"`
}

type uploadResult struct {
	Images []library.Image `json:"images"`
	Errors []uploadFailure `json:"errors"`
	Error  string          `json:"error,omitempty"`
}

func (a api) uploadImages(c *gin.Context) {
	if _, err := a.catalog.GetAlbum(c.Request.Context(), c.Param("id")); err != nil {
		problem(c, err)
		return
	}
	if c.Request.ContentLength > a.upload.MaxRequestBytes {
		problem(c, apperr.ErrTooLarge)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, a.upload.MaxRequestBytes)
	reader, err := c.Request.MultipartReader()
	if err != nil {
		problem(c, apperr.New(apperr.ErrInvalid, "请使用 multipart/form-data 上传"))
		return
	}
	result := uploadResult{Images: []library.Image{}, Errors: []uploadFailure{}}
	failRequest := func(err error) {
		status, message := http.StatusBadRequest, "multipart 数据不完整或不正确"
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status, message = http.StatusRequestEntityTooLarge, apperr.ErrTooLarge.Error()
		}
		result.Error = message
		c.JSON(status, result)
	}
	for count := 0; ; count++ {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			failRequest(err)
			return
		}
		if count >= 100 {
			result.Error = "单次请求最多上传 100 个文件"
			c.JSON(http.StatusBadRequest, result)
			return
		}
		var item library.Image
		if part.FileName() == "" || (part.FormName() != "files" && part.FormName() != "file") {
			err = apperr.New(apperr.ErrInvalid, "文件字段必须为 files 或 file")
		} else {
			item, err = a.catalog.Upload(c.Request.Context(), c.Param("id"), part.FileName(), part, a.upload.MaxFileBytes)
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			failRequest(err)
			return
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			failRequest(err)
			return
		}
		if err != nil {
			status, message := errorResponse(err)
			if status == http.StatusInternalServerError {
				logging.Logger().Error("上传文件失败", "error", err)
			}
			result.Errors = append(result.Errors, uploadFailure{part.FileName(), message, status})
		} else {
			result.Images = append(result.Images, item)
		}
		if _, err := io.Copy(io.Discard, part); err != nil {
			failRequest(err)
			return
		}
		logging.CloseResource(part, part.FileName())
	}
	// multipart 结束后的尾随数据仍计入整个请求体限制
	if _, err := io.Copy(io.Discard, c.Request.Body); err != nil {
		failRequest(err)
		return
	}
	status := http.StatusCreated
	if len(result.Images) == 0 {
		status = http.StatusBadRequest
		if len(result.Errors) == 0 {
			result.Error = "至少上传一张图片"
		} else {
			status = result.Errors[0].Status
		}
	}
	c.JSON(status, result)
}
