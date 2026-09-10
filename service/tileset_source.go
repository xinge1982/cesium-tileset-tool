package service

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type tilesetSourceUpdateBody struct {
	Key    string                 `json:"key"`
	Values map[string]interface{} `json:"values"`
}

type TilesetSourceController struct {
	searchService *TilesetSourceSearchService
}

func NewTilesetSourceController(
	searchService *TilesetSourceSearchService,
) *TilesetSourceController {
	return &TilesetSourceController{
		searchService: searchService,
	}
}

func (controller *TilesetSourceController) Search(
	ctx *gin.Context,
) {
	if controller.searchService.conn == nil ||
		controller.searchService.conn.DB == nil ||
		controller.searchService.conn.Ready == false {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "database not ready",
		})
		return
	}

	page, err := parsePositiveInt(
		ctx.Query("page"),
		1,
	)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid page",
		})
		return
	}

	pageSize, err := parsePositiveInt(
		ctx.Query("pageSize"),
		50,
	)
	if err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid pageSize",
		})
		return
	}

	result, err := controller.searchService.Search(
		ctx.Request.Context(),
		TilesetSourceSearchRequest{
			TilesetKey: ctx.Param("tilesetKey"),
			SourceID:   ctx.Param("sourceId"),
			Keyword:    strings.TrimSpace(ctx.Query("keyword")),
			Page:       page,
			PageSize:   pageSize,
		},
	)
	if err != nil {
		writeSearchError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"data": result,
	})
}

func (controller *TilesetSourceController) Detail(
	ctx *gin.Context,
) {
	if controller.searchService.conn == nil ||
		controller.searchService.conn.DB == nil ||
		controller.searchService.conn.Ready == false {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "database not ready",
		})
		return
	}

	result, err := controller.searchService.Detail(
		ctx.Request.Context(),
		TilesetSourceDetailRequest{
			TilesetKey: ctx.Param("tilesetKey"),
			SourceID:   ctx.Param("sourceId"),
			Key:        strings.TrimSpace(ctx.Query("key")),
		},
	)
	if err != nil {
		writeSearchError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"data": result,
	})
}

func (controller *TilesetSourceController) Update(
	ctx *gin.Context,
) {
	if controller.searchService.conn == nil ||
		controller.searchService.conn.DB == nil ||
		controller.searchService.conn.Ready == false {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "database not ready",
		})
		return
	}

	var body tilesetSourceUpdateBody
	if err := ctx.ShouldBindJSON(&body); err != nil {
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid request body",
		})
		return
	}

	err := controller.searchService.Update(
		ctx.Request.Context(),
		TilesetSourceUpdateRequest{
			TilesetKey: ctx.Param("tilesetKey"),
			SourceID:   ctx.Param("sourceId"),
			Key:        strings.TrimSpace(body.Key),
			Values:     body.Values,
			ClientIP:   strings.TrimSpace(ctx.ClientIP()),
			UserAgent:  strings.TrimSpace(ctx.Request.UserAgent()),
		},
	)
	if err != nil {
		writeSearchError(ctx, err)
		return
	}

	ctx.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"data": gin.H{"updated": true},
	})
}

func parsePositiveInt(
	value string,
	defaultValue int,
) (int, error) {
	if strings.TrimSpace(value) == "" {
		return defaultValue, nil
	}

	number, err := strconv.Atoi(value)
	if err != nil || number < 1 {
		return 0, strconv.ErrSyntax
	}

	return number, nil
}

func writeSearchError(
	ctx *gin.Context,
	err error,
) {
	message := err.Error()

	switch {
	case strings.Contains(message, "does not exist"),
		strings.Contains(message, "data not found"):
		ctx.JSON(http.StatusNotFound, gin.H{
			"error": message,
		})

	case strings.Contains(message, "disabled"):
		ctx.JSON(http.StatusForbidden, gin.H{
			"error": message,
		})

	case strings.Contains(message, "invalid"),
		strings.Contains(message, "has no fields"),
		strings.Contains(message, "primary key"),
		strings.Contains(message, "no fields"),
		strings.Contains(message, "not allowed"):
		ctx.JSON(http.StatusBadRequest, gin.H{
			"error": message,
		})

	default:
		ctx.JSON(http.StatusInternalServerError, gin.H{
			"error": "tileset source operation failed:" + message,
		})
	}
}
