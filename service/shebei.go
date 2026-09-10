package service

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type AaAllShebeiHb struct {
	ID       string `gorm:"column:id;type:varchar" json:"id"`
	NameOld  string `gorm:"column:nameold;type:varchar" json:"nameold"`
	Geom     string `gorm:"column:geom;type:geometry" json:"geom"`
	Type     string `gorm:"column:type;type:varchar" json:"type"`
	NewBmsx  string `gorm:"column:newbmsx;type:varchar" json:"newbmsx"`
	Hl       string `gorm:"column:hl;type:varchar" json:"hl"`
	Bz       string `gorm:"column:bz;type:text" json:"bz"`
	NameType string `gorm:"column:nametype;type:text" json:"nametype"`
	Sid      int    `gorm:"column:sid;primaryKey;autoIncrement" json:"sid"`
}

type AaAllShebeiHbResult struct {
	AaAllShebeiHb
	Lng         float64 `geom:"column:lng" json:"lng"`
	Lat         float64 `geom:"column:lat" json:"lat"`
	Alt         float64 `geom:"column:alt" json:"alt"`
	RoadHeading float64 `geom:"column:road_heading;type:float" json:"roadHeading"`
}

func (AaAllShebeiHb) TableName() string { return "aa_all_shebei_hb" }

// GET /api/searchShebei?keyword=xxx&pageNumber=1&pageSize=20
func (s *DataAccess) searchShebei(c *gin.Context) {
	db, done := s.GetDB(c)
	if done {
		return
	}

	keyword := strings.TrimSpace(c.Query("keyword"))
	pageNumber, _ := strconv.Atoi(c.DefaultQuery("pageNumber", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if pageNumber < 1 {
		pageNumber = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	pattern := "%" + keyword + "%"
	query := db.Table(AaAllShebeiHb{}.TableName()).Where(`
		newbmsx ILIKE ? OR bz ILIKE ? OR nametype ILIKE ? OR id ILIKE ?
	`, pattern, pattern, pattern, pattern)

	var total int64
	query.Count(&total)

	var items []AaAllShebeiHbResult
	result := db.Raw(`
		WITH nearest_heading AS (
			SELECT
				p.id,p.nametype,p.type,p.newbmsx,p.bz,p.hl,p.nameold,p.sid, ST_Transform(p.geom, 4326) as geom,
				degrees(
						ST_Azimuth(
								ST_LineInterpolatePoint(
										r.line_geom,
										GREATEST(
												0.0,
												ST_LineLocatePoint(
														r.line_geom,
														ST_ClosestPoint(r.line_geom, ST_Transform(p.geom, 4326))
												) - 0.00001
										)
								),
								ST_LineInterpolatePoint(
										r.line_geom,
										LEAST(
												1.0,
												ST_LineLocatePoint(
														r.line_geom,
														ST_ClosestPoint(r.line_geom, ST_Transform(p.geom, 4326))
												) + 0.00001
										)
								)
						)
				) AS road_heading
			FROM aa_all_shebei_hb p
					 CROSS JOIN LATERAL (
				SELECT
					ST_LineMerge(
							ST_Transform(h.geom, 4326)
					) AS line_geom
				FROM hdroad h
				ORDER BY
					ST_Transform(h.geom, 4326) <->  ST_Transform(p.geom, 4326)
				LIMIT 1
				) r
		)
		select nh.id,nh.nametype,nh.type,nh.newbmsx,nh.bz,nh.hl,nh.nameold,nh.sid,nh.road_heading, 
               ST_X(nh.geom) AS lng,ST_Y(nh.geom) AS lat,ST_Z(nh.geom) AS alt
		from nearest_heading nh
		where nh.newbmsx ILIKE ? OR nh.bz ILIKE ? OR nh.nametype ILIKE ? OR nh.id ILIKE ?
		offset ? limit ?
		;
		`, pattern, pattern, pattern, pattern, (pageNumber-1)*pageSize, pageSize).Find(&items)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"msg": "Failed to retrieve data : " + result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"total":      total,
		"pageNumber": pageNumber,
		"pageSize":   pageSize,
		"data":       items,
	})
}
