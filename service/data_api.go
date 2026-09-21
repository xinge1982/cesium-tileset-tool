package service

import (
	"cesium-tileset-tool/config"
	"cesium-tileset-tool/pg"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type DataAccess struct{ conn *pg.PGConn }

func NewDataAccess(conn *pg.PGConn) *DataAccess { return &DataAccess{conn: conn} }

func (s *DataAccess) GetDB(c *gin.Context) (*gorm.DB, bool) {
	if s.conn == nil || s.conn.DB == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Database unavailable"})
		return nil, true
	}
	return s.conn.DB, false
}

func (s *DataAccess) ApiRegister(g *gin.Engine, prefix string) {
	r := g.Group(prefix)

	searchService := NewTilesetSourceSearchService(
		s.conn,
		config.Instance().Tilesets,
	)

	tilesetSourceController :=
		NewTilesetSourceController(searchService)
	modelRegistrationController :=
		newModelRegistrationController(searchService)

	r.GET(
		"/tilesets/:tilesetKey/sources/:sourceId/search",
		tilesetSourceController.Search,
	)

	r.GET(
		"/tilesets/:tilesetKey/sources/:sourceId/detail",
		tilesetSourceController.Detail,
	)

	r.PUT(
		"/tilesets/:tilesetKey/sources/:sourceId",
		tilesetSourceController.Update,
	)

	r.POST(
		"/tilesets/:tilesetKey/sources/:sourceId/model-registration/solve",
		modelRegistrationController.Solve,
	)

	r.POST(
		"/tilesets/:tilesetKey/sources/:sourceId/model-registration/confirm",
		modelRegistrationController.Confirm,
	)

	r.GET(
		"/tilesets",
		searchService.GetTilesets,
	)

	r.GET(
		"/default-view",
		func(ctx *gin.Context) {
			ctx.JSON(http.StatusOK, gin.H{
				"code": http.StatusOK,
				"data": config.Instance().DefaultView,
			})
		},
	)

}
