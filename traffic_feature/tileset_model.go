package traffic_feature

import (
	"bytes"
	"cesium-tileset-tool/cesium"
	"cesium-tileset-tool/common"
	"cesium-tileset-tool/config"
	"cesium-tileset-tool/mapmodel"
	"cesium-tileset-tool/minioconn"
	"cesium-tileset-tool/pg"
	"cesium-tileset-tool/utils"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"github.com/qmuntal/gltf"
	log "github.com/sirupsen/logrus"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type GeoHashTile struct {
	Geohash    string    `gorm:"column:geohash;" json:"geohash"`
	Level      int       `gorm:"column:level"`
	TotalCount int       `gorm:"column:total_count"`
	MinX       float64   `gorm:"column:minx"`
	MinY       float64   `gorm:"column:miny"`
	MaxX       float64   `gorm:"column:maxx"`
	MaxY       float64   `gorm:"column:maxy"`
	MinZ       float64   `gorm:"column:minz"`
	MaxZ       float64   `gorm:"column:maxz"`
	UpdateTime time.Time `gorm:"-"`
}

type GeoHashModel struct {
	TableName  string  `gorm:"column:table_name" json:"tableName"`
	Model      string  `gorm:"column:model" json:"model"`
	Id         string  `gorm:"column:id" json:"id"`
	Name       string  `gorm:"column:name" json:"name"`
	Type       string  `gorm:"column:type" json:"type"`
	Lng        float64 `gorm:"column:lng" json:"lng"`
	Lat        float64 `gorm:"column:lat" json:"lat"`
	Alt        float64 `gorm:"column:alt" json:"alt"`
	TypeCode   string  `gorm:"column:type_code" json:"typeCode"`
	STypeCode  string  `gorm:"column:stype_code" json:"sTypeCode"`
	GBTypeCode string  `gorm:"column:gbtype_code" json:"gbTypeCode"`
	Width      float64 `gorm:"column:width" json:"width"`
	Height     float64 `gorm:"column:height" json:"height"`
	KmValue    float64 `gorm:"column:km_value" json:"kmValue"`
	RoadName   string  `gorm:"column:road_name" json:"roadName"`
	TravelType int     `gorm:"column:travel_type" json:"travelType"`

	ObjAngle    float64         `gorm:"column:obj_angle" json:"objAngle"`
	Transform   pq.Float64Array `gorm:"column:transform;type:double precision[]" json:"transform"` // PostgreSQL double precision[]
	Metadata    datatypes.JSON  `gorm:"column:metadata;type:jsonb;comment:附加数据" json:"metadata"`   // PostgreSQL double precision[]
	ServiceData datatypes.JSON  `gorm:"column:service_data;type:jsonb;comment:服务数据" json:"serviceData"`
	Gltf        *GltfModel      `gorm:"-" json:"gltf"`
}

func geoHashModelGroupKey(model *GeoHashModel) string {
	if model == nil {
		return ""
	}
	return model.TableName + "|" + model.Model
}

func appendGeoHashModelsByGroup(target map[string][]*GeoHashModel, models ...*GeoHashModel) {
	for _, model := range models {
		if model == nil {
			continue
		}
		groupKey := geoHashModelGroupKey(model)
		target[groupKey] = append(target[groupKey], model)
	}
}

type GeoHashData struct {
	Lng     float64 `gorm:"column:lng" json:"lng"`
	Lat     float64 `gorm:"column:lat" json:"lat"`
	Alt     float64 `gorm:"column:alt" json:"alt"`
	GeoHash string  `gorm:"column:geohash" json:"geohash"`
}

type GltfModel struct {
	Name        string
	Content     []byte
	ContentType string
}

type BuildModel struct {
	Region    [6]float64
	Transform [16]float64
	Content   []byte
}

type TileJob struct {
	t *GeoHashTile
}

type TileJobResult struct {
	geohash string
	node    *TileNode
	err     error
}

type tileLODLevel struct {
	Level          int
	ModelFolder    string
	GeometricError float64
}

type builtTileLOD struct {
	level tileLODLevel
	uri   string
	built *BuildModel
}

type localLODModelCache struct {
	sync.RWMutex
	models map[string]*GltfModel
}

const tileModelRootDir = "tiles"

// Each Geohash parent covers a larger area than its children. Keeping its
// error strictly larger prevents several spatial levels from being refined at
// the same SSE threshold. The first configured positive LOD error is used as
// the leaf spatial error floor, so the spatial tree follows the project's LOD
// configuration instead of a separate hard-coded error table.
const geohashParentGeometricErrorScale = 2.0

var buildingDemMap = make(map[string]cesium.Vec3)
var buildingDemLock = sync.RWMutex{}

// tileModelRelativePath returns a URL-style relative path for a geohash tile.
// A directory is added for every two geohash characters. Each directory keeps
// the complete prefix accumulated so far, making its spatial prefix directly
// identifiable while preventing a single directory from holding too many GLBs.
//
// Example: wtw3sjq9 LOD2 -> tiles/wt/wtw3/wtw3sj/wtw3sjq9/lod2.glb
func tileModelRelativePath(geohash string, lod int) string {
	parts := []string{tileModelRootDir}
	prefixLength := len(geohash) - 2
	for start := 0; start < prefixLength; start += 2 {
		end := start + 2
		if end > prefixLength {
			end = prefixLength
		}
		parts = append(parts, geohash[:end])
	}
	parts = append(parts, geohash, fmt.Sprintf("lod%d.glb", lod))
	return path.Join(parts...)
}

func configuredTileLODLevels(lod config.TilesetLODConfig) []tileLODLevel {
	if !lod.Enabled {
		return []tileLODLevel{{
			Level:          3,
			ModelFolder:    lod.LOD3.ModelFolder,
			GeometricError: lod.LOD3.GeometricError,
		}}
	}

	return []tileLODLevel{
		{Level: 0, ModelFolder: lod.LOD0.ModelFolder, GeometricError: lod.LOD0.GeometricError},
		{Level: 1, ModelFolder: lod.LOD1.ModelFolder, GeometricError: lod.LOD1.GeometricError},
		{Level: 2, ModelFolder: lod.LOD2.ModelFolder, GeometricError: lod.LOD2.GeometricError},
		{Level: 3, ModelFolder: lod.LOD3.ModelFolder, GeometricError: lod.LOD3.GeometricError},
	}
}

func geohashGeometricErrorBase(levels []tileLODLevel) float64 {
	for _, level := range levels {
		if level.GeometricError > 0 {
			return level.GeometricError
		}
	}

	// A zero-error spatial tree would never refine to its content children.
	// This fallback applies when LOD is disabled or all configured errors are 0.
	return 1
}

func localLODModelRoot(cfg *config.Config, level tileLODLevel) (string, error) {
	if cfg == nil {
		return "", errors.New("tileset config is nil")
	}
	folder := filepath.Clean(strings.TrimSpace(level.ModelFolder))
	if folder == "." || folder == "" {
		return "", fmt.Errorf("LOD%d model folder is empty", level.Level)
	}
	if filepath.IsAbs(folder) {
		return "", fmt.Errorf("LOD%d model folder must be relative to NetworkFolder", level.Level)
	}

	root, err := filepath.Abs(cfg.NetworkFolder)
	if err != nil {
		return "", fmt.Errorf("resolve NetworkFolder: %w", err)
	}
	modelRoot, err := filepath.Abs(filepath.Join(root, folder))
	if err != nil {
		return "", fmt.Errorf("resolve LOD%d model folder: %w", level.Level, err)
	}
	rel, err := filepath.Rel(root, modelRoot)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("LOD%d model folder escapes NetworkFolder", level.Level)
	}
	return modelRoot, nil
}

func ensureLocalLODModelDirectories(cfg *config.Config, lod config.TilesetLODConfig, levels []tileLODLevel) error {
	previousError := math.Inf(1)
	for _, level := range levels {
		if level.GeometricError < 0 {
			return fmt.Errorf("LOD%d geometricError must not be negative", level.Level)
		}
		if level.GeometricError >= previousError {
			return fmt.Errorf("LOD%d geometricError %.3f must be lower than the preceding LOD error %.3f", level.Level, level.GeometricError, previousError)
		}
		previousError = level.GeometricError
		if level.Level == 3 {
			if level.GeometricError != 0 {
				return fmt.Errorf("LOD3 geometricError must be 0")
			}
			if !lod.LOD3.LocalFirst {
				continue
			}
		}
		root, err := localLODModelRoot(cfg, level)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(root, 0755); err != nil {
			return fmt.Errorf("create LOD%d model folder %s: %w", level.Level, root, err)
		}
	}
	return nil
}

func getNetworkPath(code string) (string, error) {
	networkFolder := ""
	if len(code) > 0 {
		configName, _, errD := config.DecodeContext(code)
		if errD != nil {
			return "", errors.New("decode context error:" + errD.Error())
		}
		cfg, errC := config.GetNetworkConfigByName(configName)
		if errC != nil {
			return "", errors.New("get config error:" + errC.Error())
		}
		networkFolder = cfg.NetworkFolder
	}
	return networkFolder, nil

}

// @Summary tileset 数据
// @Description tileset 数据
// @Tags tileset
// @Accept json
// @Produce json
// @Param Authorization header string true "Authorization token"  // Header parameter
// @Router /tilesets/{filepath} [post]
func (s *FeatureMan) getMergedTileSets(c *gin.Context) {
	code := c.GetHeader(common.ConfigContextCode)
	if len(code) == 0 {
		cid := c.Query("pgcontextid")
		code = cid
	}
	if len(code) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "pgcontextid is empty"})
		return
	}

	networkFolder, errC := getNetworkPath(code)
	if errC != nil {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "get config error:" + errC.Error()})
		return
	}

	tilesetRoot := filepath.Join(networkFolder, "tilesets")

	// 获取访问路径
	reqPath := c.Param("filepath") // 例如 /0/0.b3dm
	if reqPath == "" || reqPath == "/" {
		reqPath = "/tileset.json" // 默认访问
	}

	// 拼接真实文件路径
	cleaned := filepath.Clean(reqPath)
	fullPath := filepath.Join(tilesetRoot, cleaned)

	// 防止越权路径，比如 ../../../etc/passwd
	if !strings.HasPrefix(fullPath, filepath.Clean(tilesetRoot)) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Invalid path"})
		return
	}

	// 检查文件是否存在
	info, err := os.Stat(fullPath)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	// 如果访问的是目录，自动寻找 tileset.json 或阻止
	if info.IsDir() {
		index := filepath.Join(fullPath, "tileset.json")
		indexInfo, err := os.Stat(index)
		if err != nil || indexInfo.IsDir() {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Directory listing not allowed"})
			return
		}
		fullPath = index
		info = indexInfo
	}

	f, err := os.Open(fullPath)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "open file failed"})
		return
	}
	defer f.Close()

	ext := strings.ToLower(filepath.Ext(fullPath))
	contentType := mime.TypeByExtension(ext)
	if ext == ".glb" {
		contentType = "model/gltf-binary"
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	c.Header("Content-Type", contentType)
	c.DataFromReader(http.StatusOK, -1, contentType, f, nil)
}

func UpdateTileByGeoHash(configName string, partitionTable string, tilesetsFolder string, geohashes []string) error {
	now := time.Now()
	if configName == "" {
		return fmt.Errorf("configName is empty")
	}

	if _, err := os.Stat(tilesetsFolder); os.IsNotExist(err) {
		// 目录不存在，创建
		if errC := os.MkdirAll(tilesetsFolder, os.ModePerm); errC != nil {
			return errC
		}
	}

	dbc := pg.GetConn(configName)
	if dbc == nil {
		return fmt.Errorf("database connection is empty")
	}

	log.Infof("generate hash tile for %s", configName)

	// 查找数据配置
	var geoTable GeoTable
	for _, tile := range AllTiles {
		if tile.PartitionTableName == partitionTable {
			geoTable = tile
		}
	}
	if len(geoTable.PartitionTableName) == 0 {
		return fmt.Errorf("no partition table for %s:%s", configName, partitionTable)
	}

	db := dbc.DB

	var leafTiles []*GeoHashTile
	for _, geohash := range geohashes {
		// 取所有叶子节点
		var leafTile GeoHashTile
		err := db.Raw(fmt.Sprintf(`
        WITH b AS (
		  SELECT ST_Box2dFromGeoHash('%s') AS box
		)
		SELECT
          LENGTH('%s') AS level,
          '%s' AS geohash,
		  ST_XMin(box) AS minX,
		  ST_YMin(box) AS minY,
		  ST_XMax(box) AS maxX,
		  ST_YMax(box) AS maxY,
          -100 AS minZ,
          300 AS maxZ
		FROM b;
    `, geohash, geohash, geohash)).First(&leafTile).Error
		if err != nil {
			return err
		}
		leafTiles = append(leafTiles, &leafTile)
	}

	bound, errB := parseProjectBound(config.Instance().Bound)
	if errB != nil {
		return errB
	}
	changes, errG := doTileJob(db, now, configName, leafTiles, geoTable, tilesetsFolder, partitionTable, bound)
	if errG != nil {
		return errG
	}

	absPath := filepath.Join(tilesetsFolder, "tileset.json")
	buf, err := os.ReadFile(absPath)
	if err != nil {
		return err
	}

	var tileset Tileset
	err = json.Unmarshal(buf, &tileset)
	if err != nil {
		return err
	}

	for _, change := range changes {
		node := FindTileNodeByGeohash(tileset.Root, change.Geohash)
		if node != nil {
			bound := change.BoundingVolume
			if change.Transform != nil {
				bound.Box = cesium.WorldBoxToLocalBox(bound.Box, *change.Transform)
			}
			node.BoundingVolume = bound
			node.Children = change.Children
			node.Transform = change.Transform
			node.Content = change.Content
			node.GeometricError = change.GeometricError
			node.Refine = change.Refine
		} else {
			return fmt.Errorf("no tile with geohash %s in %s", change.Geohash, configName)
		}
	}

	if tileset.Extensions.ThreeDTILESMetadata.Schema.Classes.TilesetInfo.Properties.UpdateTime != nil {
		tileset.Extensions.ThreeDTILESMetadata.Tilesets.Main.Properties.UpdateTime = now.Format("20060102150405")
	}
	data, _ := json.MarshalIndent(tileset, "", "  ")
	_ = os.WriteFile(absPath, data, 0644)

	log.Infof("✅ tileset.json 生成完成 %s", absPath)
	return nil
}

// 生成分片数据
func GenerateAllGeoHashTile(configName string, partitionTable string, tilesetsFolder string, boundStr string) (*Tileset, error) {
	if configName == "" {
		return nil, fmt.Errorf("configName is empty")
	}
	bound, errB := parseProjectBound(boundStr)
	if errB != nil {
		return nil, errB
	}

	if _, err := os.Stat(tilesetsFolder); os.IsNotExist(err) {
		// 目录不存在，创建
		if errC := os.MkdirAll(tilesetsFolder, os.ModePerm); errC != nil {
			return nil, errC
		}
	}

	dbc := pg.GetConn(configName)
	if dbc == nil {
		return nil, fmt.Errorf("database connection is empty")
	}

	log.Infof("generate hash tile for %s", configName)

	db := dbc.DB
	// 取所有叶子节点
	var leafTiles []*GeoHashTile
	err := db.Raw(fmt.Sprintf(`
        SELECT parent.geohash, parent.level, parent.total_count,
			   ST_XMin(bbox) AS minx,
			   ST_YMin(bbox) AS miny,
			   ST_XMax(bbox) AS maxx,
			   ST_YMax(bbox) AS maxy,
			   0 AS minz,
			   400 AS maxz
        FROM %s parent
        WHERE NOT EXISTS (
            SELECT 1 FROM %s child
            WHERE child.geohash LIKE parent.geohash || '%%'
              AND child.level = parent.level + 1
        )
        AND parent.total_count > 0
		AND ST_Intersects(parent.bbox, ST_MakeEnvelope(?, ?, ?, ?, 4326));
    `, partitionTable, partitionTable), bound.args()...).Scan(&leafTiles).Error
	if err != nil {
		return nil, err
	}

	// 查找数据配置
	var geoTable GeoTable
	for _, tile := range AllTiles {
		if tile.PartitionTableName == partitionTable {
			geoTable = tile
		}
	}
	if len(geoTable.PartitionTableName) == 0 {
		return nil, fmt.Errorf("no partition table for %s:%s", configName, partitionTable)
	}

	// 收集geohash
	now := time.Now()
	lodLevels := configuredTileLODLevels(geoTable.LOD)
	geohashErrorBase := geohashGeometricErrorBase(lodLevels)
	// 构建叶子节点
	tilesByGeohash, errG := doTileJob(db, now, configName, leafTiles, geoTable, tilesetsFolder, partitionTable, bound)
	if errG != nil {
		return nil, errG
	}

	parentMap := map[string]*TileNode{}
	for gh, node := range tilesByGeohash {
		var hash = gh
		parent := hash[:len(hash)-1]
		if parentMap[parent] == nil {
			parentMap[parent] = &TileNode{
				BoundingVolume: BoundingVolume{},
				Content:        nil,
				GeometricError: 0,
				Transform:      nil,
				Refine:         "ADD",
				Children:       nil,
				Level:          len(parent),
				Geohash:        parent,
			}
		}

		parentMap[parent].Children = append(parentMap[parent].Children, node)
	}

	// 自下而上组织层级
	var copyMap map[string]*TileNode
	for {
		added := 0
		copyMap = make(map[string]*TileNode)
		var keys []string

		for k, v := range parentMap {
			n := v // 复制结构体内容
			copyMap[k] = n
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parentMap = make(map[string]*TileNode)
		for _, s := range keys {
			node := copyMap[s]
			gh := node.Geohash
			parent := gh[:len(gh)-1]
			if len(parent) < int(MIN_GEOHASH_LEVEL) {
				parentMap[gh] = node
				continue
			}

			parentNode, isChild := findExistParent(parentMap, parent)
			if parentNode == nil {
				parentNode = &TileNode{
					BoundingVolume: BoundingVolume{},
					Content:        nil,
					GeometricError: 0,
					Transform:      nil,
					Refine:         "ADD",
					Children:       nil,
					Level:          len(parent),
					Geohash:        parent,
				}
			}
			exist := false
			for _, child := range parentNode.Children {
				if child == node {
					exist = true
				}
			}
			if !exist {
				parentNode.Children = append(parentNode.Children, node)
				added += 1
			}

			if !isChild {
				parentMap[parent] = parentNode
			}
		}

		if added == 0 {
			break
		}
	}

	// 合并 boundingVolume
	root := &TileNode{
		BoundingVolume: BoundingVolume{},
		GeometricError: 0,
		Children:       nil,
		Level:          2,
		Geohash:        "root",
		Refine:         "ADD",
	}
	for _, n := range parentMap {
		root.Children = append(root.Children, n)
	}
	refreshGeoHashTileBound(root, geohashErrorBase)

	tileset := &Tileset{
		GeometricError: root.GeometricError,
		Root:           root,
		ExtensionsUsed: []string{"3DTILES_metadata"},
		Extensions: Extensions{ThreeDTILESMetadata{
			Schema: Schema{
				Classes: Classes{
					TilesetInfo: TilesetInfo{
						Properties: Properties{
							Project: gin.H{
								"type": "STRING",
							},
							LayerType: gin.H{
								"type": "STRING",
							},
							UpdateTime: gin.H{
								"type": "STRING",
							},
						},
					},
				},
			},
			Tilesets: Tilesets{
				Main: Main{
					Class: "TilesetInfo",
					Properties: Properties{
						Project:    configName,
						LayerType:  "pipe",
						UpdateTime: now.Format("20060102150405"),
					},
				},
			},
		}},
	}
	tileset.Asset.Version = "1.1"

	data, _ := json.MarshalIndent(tileset, "", "  ")
	absPath := filepath.Join(tilesetsFolder, "tileset.json")
	_ = os.WriteFile(absPath, data, 0644)

	// 删除过期的切片文件，保留一段时间以备没有刷新的页面使用的tileset.json还包含老的切片
	if errR := RemoveFilesOlderThan(tilesetsFolder, 3*24*time.Hour); errR != nil {
		log.Errorf("删除过期文件错误：%s", errR.Error())
	} else {
		log.Infof("✅ 删除过期文件完成 %s", tilesetsFolder)
	}

	log.Infof("✅ tileset.json 生成完成 %s", absPath)

	return tileset, nil
}

func refreshTileBound(node *TileNode) {
	refreshTileBoundWithGeometricError(node, 0)
}

func refreshGeoHashTileBound(node *TileNode, geohashErrorBase float64) {
	refreshTileBoundWithGeometricError(node, geohashErrorBase)
}

func refreshTileBoundWithGeometricError(node *TileNode, geohashErrorBase float64) {
	if node.Children == nil || len(node.Children) == 0 {
		return
	}

	// LOD nodes already have a bounding volume in the coordinate system inherited
	// from their outer LOD node. Do not merge it again from the next LOD level.
	if node.Content != nil {
		for _, child := range node.Children {
			refreshTileBoundWithGeometricError(child, geohashErrorBase)
		}
		return
	}
	// 合并 boundingVolume

	var boxes []cesium.Box12
	maxChildError := 0.0
	for _, n := range node.Children {
		refreshTileBoundWithGeometricError(n, geohashErrorBase)
		boxes = append(boxes, n.BoundingVolume.Box)
		if n.GeometricError > maxChildError {
			maxChildError = n.GeometricError
		}
		if n.Transform != nil {
			box := cesium.WorldBoxToLocalBox(n.BoundingVolume.Box, *n.Transform)
			n.BoundingVolume.Box = box
		}
	}
	box := cesium.MergeBoxes(boxes, 1.05)
	node.BoundingVolume = BoundingVolume{
		Box: box,
	}
	parentError := maxChildError
	if geohashErrorBase > 0 {
		if maxChildError > 0 {
			parentError = maxChildError * geohashParentGeometricErrorScale
		}
		if parentError < geohashErrorBase {
			parentError = geohashErrorBase
		}
	}
	if node.GeometricError < parentError {
		node.GeometricError = parentError
	}
}

func doTileJob(db *gorm.DB, now time.Time, configName string, leafTiles []*GeoHashTile, geoTable GeoTable, tilesetsFolder string, partitionTable string, bound projectBound) (map[string]*TileNode, error) {
	tilesByGeohash := make(map[string]*TileNode)
	workerCount := 8 // tune this based on CPU / DB capacity
	cfg := config.Instance()
	lodLevels := configuredTileLODLevels(geoTable.LOD)
	if err := ensureLocalLODModelDirectories(cfg, geoTable.LOD, lodLevels); err != nil {
		return nil, err
	}
	localModelCache := &localLODModelCache{models: make(map[string]*GltfModel)}

	jobs := make(chan TileJob)
	results := make(chan TileJobResult)

	var wg sync.WaitGroup

	// workers
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := range jobs {
				t := j.t

				models, err2 := queryGeoHashModelData(configName, geoTable, db, t.Geohash, "", bound)
				if err2 != nil {
					results <- TileJobResult{err: err2}
					continue
				}
				if len(models) == 0 {
					continue
				}

				builtLODs := make([]builtTileLOD, 0, len(lodLevels))
				for _, lodLevel := range lodLevels {
					lodModels := models
					if lodLevel.Level < 3 {
						lodModels, err2 = loadLODModelsBySource(
							cfg, geoTable, lodLevel, models, localModelCache)
						if err2 != nil {
							results <- TileJobResult{err: err2}
							builtLODs = nil
							break
						}
					}
					if len(lodModels) == 0 {
						continue
					}

					fn, built, err3 := buildTileByHashModels(lodModels, t, tilesetsFolder, lodLevel.Level)
					if err3 != nil {
						results <- TileJobResult{err: fmt.Errorf("build LOD%d tile %s: %w", lodLevel.Level, t.Geohash, err3)}
						builtLODs = nil
						break
					}
					builtLODs = append(builtLODs, builtTileLOD{level: lodLevel, uri: fn, built: built})
				}
				if len(builtLODs) == 0 {
					continue
				}

				node := buildLODNodeChain(t, builtLODs, now.Format("20060102150405"))

				results <- TileJobResult{
					geohash: t.Geohash,
					node:    node,
				}
			}
		}()
	}

	// send jobs
	go func() {
		for _, t := range leafTiles {
			jobs <- TileJob{t: t}
		}
		close(jobs)
	}()

	// close results after all workers done
	go func() {
		wg.Wait()
		close(results)
	}()

	var firstErr error
	var mu sync.Mutex

	for r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		if r.node == nil {
			continue
		}

		mu.Lock()
		tilesByGeohash[r.geohash] = r.node
		mu.Unlock()
	}

	if firstErr != nil {
		return nil, firstErr
	}

	return tilesByGeohash, nil
}

// RemoveFilesOlderThan removes files in dir whose ModTime is older than maxAge.
// Example: maxAge = time.Hour
func RemoveFilesOlderThan(dir string, maxAge time.Duration) error {
	now := time.Now()
	cutoff := now.Add(-maxAge)

	return filepath.WalkDir(dir, func(fullPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk tileset path failed (%s): %w", fullPath, walkErr)
		}
		if entry.IsDir() {
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("get file info failed (%s): %w", fullPath, err)
		}
		if !info.ModTime().Before(cutoff) {
			return nil
		}
		if err := os.Remove(fullPath); err != nil {
			return fmt.Errorf("remove file failed (%s): %w", fullPath, err)
		}
		log.Infof("remove file expired (%s)", fullPath)
		return nil
	})
}

func loadLocalLODModels(cfg *config.Config, level tileLODLevel, source map[string][]*GeoHashModel, cache *localLODModelCache) (map[string][]*GeoHashModel, error) {
	root, err := localLODModelRoot(cfg, level)
	if err != nil {
		return nil, err
	}

	result := make(map[string][]*GeoHashModel)
	for _, instances := range source {
		if len(instances) == 0 || instances[0] == nil {
			continue
		}
		modelName := instances[0].Model
		tableName := instances[0].TableName
		modelPath, err := localLODModelPath(root, tableName, modelName, level.Level)
		if err != nil {
			return nil, err
		}
		cache.RLock()
		gltfModel, cached := cache.models[modelPath]
		cache.RUnlock()
		if !cached {
			content, readErr := os.ReadFile(modelPath)
			if readErr != nil {
				if !os.IsNotExist(readErr) {
					return nil, fmt.Errorf("read LOD%d model %s: %w", level.Level, modelPath, readErr)
				}
				log.Warnf("LOD%d model not found, skip: %s", level.Level, modelPath)
			} else {
				gltfModel = &GltfModel{
					Name:        modelName,
					Content:     content,
					ContentType: detectGltfFormat(content),
				}
			}
			cache.Lock()
			cache.models[modelPath] = gltfModel
			cache.Unlock()
		}
		if gltfModel == nil {
			continue
		}
		for _, instance := range instances {
			cloned := *instance
			cloned.Gltf = gltfModel
			appendGeoHashModelsByGroup(result, &cloned)
		}
	}
	return result, nil
}

func localLODModelPath(root, tableName, modelName string, level int) (string, error) {
	cleanTable := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(tableName, "\\", "/")))
	cleanName := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(modelName, "\\", "/")))
	for _, value := range []string{cleanTable, cleanName} {
		if filepath.IsAbs(value) || value == "." || value == ".." ||
			strings.HasPrefix(value, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("LOD%d model path escapes model folder: %s/%s", level, tableName, modelName)
		}
	}
	return filepath.Join(root, cleanTable, cleanName), nil
}

func loadLODModelsBySource(cfg *config.Config, geoTable GeoTable, level tileLODLevel,
	source map[string][]*GeoHashModel, cache *localLODModelCache) (map[string][]*GeoHashModel, error) {
	if level.Level < 0 || level.Level > 2 {
		return nil, fmt.Errorf("source LOD model configuration does not support LOD%d", level.Level)
	}
	result := make(map[string][]*GeoHashModel)
	for _, instances := range source {
		if len(instances) == 0 || instances[0] == nil {
			continue
		}
		tableName := instances[0].TableName
		sourceLOD := config.TilesetSourceLODConfig{}
		if sourceCfg, ok := geoTable.TilesetSources[tableName]; ok {
			sourceLOD = sourceCfg.LOD
		}
		mode, err := resolveLODModelMode(sourceLOD, level.Level, instances[0].Model)
		if err != nil {
			return nil, fmt.Errorf("resolve LOD%d model mode for table %s model %s: %w",
				level.Level, tableName, instances[0].Model, err)
		}

		subset := make(map[string][]*GeoHashModel)
		appendGeoHashModelsByGroup(subset, instances...)
		switch mode {
		case "local":
			loaded, err := loadLocalLODModels(cfg, level, subset, cache)
			if err != nil {
				return nil, err
			}
			for _, models := range loaded {
				appendGeoHashModelsByGroup(result, models...)
			}
		case "copy-lod3":
			if err := copyLOD3ModelsToLocalLOD(cfg, level, instances, cache); err != nil {
				return nil, err
			}
			appendGeoHashModelsByGroup(result, instances...)
		case "reuse-lod3":
			appendGeoHashModelsByGroup(result, instances...)
		case "skip":
			continue
		default:
			return nil, fmt.Errorf("unsupported lod%dModelMode %q for table %s",
				level.Level, mode, tableName)
		}
	}
	return result, nil
}

func resolveLODModelMode(lod config.TilesetSourceLODConfig, level int, modelName string) (string, error) {
	var mode string
	var prefixes []string
	var unmatchedMode string
	switch level {
	case 0:
		mode = lod.LOD0ModelMode
		prefixes = lod.LOD0ModelPrefixes
		unmatchedMode = lod.LOD0UnmatchedMode
	case 1:
		mode = lod.LOD1ModelMode
		prefixes = lod.LOD1ModelPrefixes
		unmatchedMode = lod.LOD1UnmatchedMode
	case 2:
		mode = lod.LOD2ModelMode
		prefixes = lod.LOD2ModelPrefixes
		unmatchedMode = lod.LOD2UnmatchedMode
	default:
		return "", fmt.Errorf("unsupported source LOD level %d", level)
	}

	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		return "local", nil
	}
	if mode != "copy-lod3-by-prefix" {
		return mode, nil
	}

	normalizedModel := path.Clean(strings.ReplaceAll(strings.TrimSpace(modelName), "\\", "/"))
	for _, configuredPrefix := range prefixes {
		prefix := strings.ReplaceAll(strings.TrimSpace(configuredPrefix), "\\", "/")
		prefix = strings.TrimPrefix(prefix, "./")
		if prefix != "" && strings.HasPrefix(normalizedModel, prefix) {
			return "copy-lod3", nil
		}
	}

	unmatchedMode = strings.ToLower(strings.TrimSpace(unmatchedMode))
	if unmatchedMode == "" {
		unmatchedMode = "local"
	}
	switch unmatchedMode {
	case "local", "reuse-lod3", "skip":
		return unmatchedMode, nil
	default:
		return "", fmt.Errorf("unsupported lod%dUnmatchedMode %q", level, unmatchedMode)
	}
}

func copyLOD3ModelsToLocalLOD(cfg *config.Config, level tileLODLevel,
	instances []*GeoHashModel, cache *localLODModelCache) error {
	if len(instances) == 0 || instances[0] == nil || instances[0].Gltf == nil {
		return nil
	}
	root, err := localLODModelRoot(cfg, level)
	if err != nil {
		return err
	}
	model := instances[0]
	modelPath, err := localLODModelPath(root, model.TableName, model.Model, level.Level)
	if err != nil {
		return err
	}

	cache.Lock()
	defer cache.Unlock()
	if _, cached := cache.models[modelPath]; cached {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(modelPath), 0755); err != nil {
		return fmt.Errorf("create LOD%d model directory %s: %w", level.Level, filepath.Dir(modelPath), err)
	}
	if err := os.WriteFile(modelPath, model.Gltf.Content, 0644); err != nil {
		return fmt.Errorf("write LOD%d model %s: %w", level.Level, modelPath, err)
	}
	cache.models[modelPath] = model.Gltf
	return nil
}

func buildLODNodeChain(tile *GeoHashTile, lods []builtTileLOD, timestamp string) *TileNode {
	outerTransform := lods[0].built.Transform
	var root *TileNode
	var parent *TileNode

	for index, lod := range lods {
		box := cesium.RegionToBox(lod.built.Region, 1.2)
		if index > 0 {
			box = cesium.WorldBoxToLocalBox(box, outerTransform)
		}

		geohash := tile.Geohash
		if index > 0 {
			geohash = fmt.Sprintf("%s#lod%d", tile.Geohash, lod.level.Level)
		}
		geometricError := lod.level.GeometricError
		if index == len(lods)-1 {
			// The deepest available node is a leaf even when LOD3 is missing.
			geometricError = 0
		}
		node := &TileNode{
			BoundingVolume: BoundingVolume{Box: box},
			GeometricError: geometricError,
			Refine:         "REPLACE",
			Content: &TileContent{
				Uri: fmt.Sprintf("%s?t=%s", lod.uri, timestamp),
			},
			Level:   tile.Level,
			Geohash: geohash,
		}
		if root == nil {
			root = node
			node.Transform = &outerTransform
		} else {
			parent.Children = []*TileNode{node}
		}
		parent = node
	}

	return root
}

func buildTileByHashModels(models map[string][]*GeoHashModel, tile *GeoHashTile, tilesetsFolder string, lod int) (string, *BuildModel, error) {
	minZ, maxZ := 1e9, -1e9
	for _, hashModels := range models {
		for _, model := range hashModels {
			if model.Alt < minZ {
				minZ = model.Alt
			}
			if model.Alt > maxZ {
				maxZ = model.Alt
			}
		}
	}
	minZ = math.Floor(minZ)
	maxZ = math.Ceil(maxZ)
	fn := tileModelRelativePath(tile.Geohash, lod)
	region := [6]float64{tile.MinX, tile.MinY, tile.MaxX, tile.MaxY, minZ, maxZ}
	center := []float64{(tile.MaxX + tile.MinX) / 2.0, (tile.MaxY + tile.MinY) / 2.0, 0}

	log.Infof("generate hash tile for %s c:%+v r:%+v", tile.Geohash, center, region)

	built, errB := GeneratePartitionModel(models, center, [4]float64{tile.MinX, tile.MinY, tile.MaxX, tile.MaxY})
	if errB != nil {
		return "", nil, errB
	}
	log.Infof("merged hash tile for %s r:%+v t:%+v", tile.Geohash, built.Region, built.Transform)

	absPath := filepath.Join(tilesetsFolder, filepath.FromSlash(fn))
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return "", nil, fmt.Errorf("create tile model directory: %w", err)
	}
	if errW := os.WriteFile(absPath, built.Content, 0644); errW != nil {
		return "", nil, errW
	}

	log.Infof("wrote model file to %s", absPath)
	return fn, built, nil
}

func queryGeoHashModelData(configName string, tile GeoTable, db *gorm.DB, geoHash string, dataTable string, bound projectBound) (map[string][]*GeoHashModel, error) {
	var models = make(map[string][]*GeoHashModel)
	for _, name := range tile.GeoTableNames {
		if len(dataTable) > 0 && dataTable != name {
			continue
		}
		switch name {
		case SignTileTableName:
			vs, errQ := QuerySignsByGeohashBBox(configName, db, geoHash, bound, tile.LOD)
			if errQ != nil {
				return nil, errQ
			}
			for _, hashModels := range vs {
				appendGeoHashModelsByGroup(models, hashModels...)
			}
		case QbbTileTableName:
			vs, errQ := QueryQbbsByGeohashBBox(configName, db, geoHash, bound, tile.LOD)
			if errQ != nil {
				return nil, errQ
			}
			for _, hashModels := range vs {
				appendGeoHashModelsByGroup(models, hashModels...)
			}
		case DeviceTileTableName:
			vs, errQ := QueryDevicesByGeohashBBox(configName, db, geoHash, bound, tile.LOD)
			if errQ != nil {
				return nil, errQ
			}
			for _, hashModels := range vs {
				appendGeoHashModelsByGroup(models, hashModels...)
			}
		case DeviceSfzTileTableName:
			vs, errQ := QueryDevicesSfzByGeohashBBox(configName, db, geoHash, bound, tile.LOD)
			if errQ != nil {
				return nil, errQ
			}
			for _, hashModels := range vs {
				appendGeoHashModelsByGroup(models, hashModels...)
			}
		case PoleTileTableName:
			vs, errQ := QueryPolesByGeohashBBox(configName, db, geoHash, bound, tile.LOD)
			if errQ != nil {
				return nil, errQ
			}
			for _, hashModels := range vs {
				appendGeoHashModelsByGroup(models, hashModels...)
			}
		case GantryTileTableName:
			vs, errQ := QueryGantrysByGeohashBBox(configName, db, geoHash, bound, tile.LOD)
			if errQ != nil {
				return nil, errQ
			}
			for _, hashModels := range vs {
				appendGeoHashModelsByGroup(models, hashModels...)
			}
		case BridgeTileTableName:
			vs, errQ := QueryBridgesByGeohashBBox(configName, db, geoHash, bound, tile.LOD)
			if errQ != nil {
				return nil, errQ
			}
			for _, hashModels := range vs {
				appendGeoHashModelsByGroup(models, hashModels...)
			}
		case RoadSideFacilityTileTableName:
			vs, errQ := QueryRoadSideFacilityByGeohashBBox(configName, db, geoHash, bound, tile.LOD)
			if errQ != nil {
				return nil, errQ
			}
			for _, hashModels := range vs {
				appendGeoHashModelsByGroup(models, hashModels...)
			}
		case ServiceEquAreaTileTableName:
			vs, errQ := QueryServiceEquAreaByGeohashBBox(configName, db, geoHash, bound, tile.LOD)
			if errQ != nil {
				return nil, errQ
			}
			for _, hashModels := range vs {
				appendGeoHashModelsByGroup(models, hashModels...)
			}
		case RenderTollBuildingTileTableName:
			vs, errQ := QueryRenderTollBuildingsByGeohashBBox(configName, db, geoHash, bound, tile.LOD)
			if errQ != nil {
				return nil, errQ
			}
			for _, hashModels := range vs {
				appendGeoHashModelsByGroup(models, hashModels...)
			}
		default:
			return nil, fmt.Errorf("Unsupport table %s", name)
		}
	}
	return models, nil
}

// 生成分片数据
func UpdateGeoHashTileByDataLngLats(configName string, partitionTable string, tilesetsFolder string, lngLats [][]float64) (
	partitionNeedRefresh bool, errR error) {
	if configName == "" {
		return partitionNeedRefresh, fmt.Errorf("configName is empty")
	}

	if _, err := os.Stat(tilesetsFolder); os.IsNotExist(err) {
		// 目录不存在，创建
		if errC := os.MkdirAll(tilesetsFolder, os.ModePerm); errC != nil {
			return partitionNeedRefresh, errC
		}
	}

	dbc := pg.GetConn(configName)
	if dbc == nil {
		return partitionNeedRefresh, fmt.Errorf("database connection is empty")
	}

	log.Infof("generate hash tile for %s", configName)

	// 查找数据配置
	var geoTable GeoTable
	for _, tile := range AllTiles {
		if tile.PartitionTableName == partitionTable {
			geoTable = tile
		}
	}
	if len(geoTable.PartitionTableName) == 0 {
		return partitionNeedRefresh, fmt.Errorf("no partition table for %s:%s", configName, partitionTable)
	}

	db := dbc.DB
	// 取所有叶子节点
	var leafTiles []*GeoHashTile
	err := db.Raw(fmt.Sprintf(`
        SELECT parent.geohash, parent.level, parent.total_count,
			   ST_XMin(bbox) AS minx,
			   ST_YMin(bbox) AS miny,
			   ST_XMax(bbox) AS maxx,
			   ST_YMax(bbox) AS maxy,
			   0 AS minz,
			   400 AS maxz
        FROM %s parent
        WHERE NOT EXISTS (
            SELECT 1 FROM %s child
            WHERE child.geohash LIKE parent.geohash || '%%'
              AND child.level = parent.level + 1
        )
        AND parent.total_count > 0;
    `, partitionTable, partitionTable)).Scan(&leafTiles).Error
	if err != nil {
		return partitionNeedRefresh, err
	}

	// 查询需要更新的geohash
	updateHashes, err := QueryGeohashByPoints(db, lngLats)
	if err != nil {
		return partitionNeedRefresh, err
	}

	// 检测是否有新增的geohash
	now := time.Now()
	for _, hs := range updateHashes {
		match := false
		for _, tile := range leafTiles {
			if strings.HasPrefix(hs, tile.Geohash) {
				match = true
				tile.UpdateTime = now
			}
		}
		if !match {
			//现有的tiles里没有找到对应的geohash
			partitionNeedRefresh = true
		}
	}
	if partitionNeedRefresh {
		return true, nil
	}

	// 重新生成瓦片数据
	var changedTiles []*GeoHashTile
	for _, t := range leafTiles {
		if !t.UpdateTime.IsZero() {
			changedTiles = append(changedTiles, t)
		}
	}
	bound, errB := parseProjectBound(config.Instance().Bound)
	if errB != nil {
		return partitionNeedRefresh, errB
	}
	changes, err := doTileJob(db, now, configName, changedTiles, geoTable, tilesetsFolder, partitionTable, bound)
	if err != nil {
		return partitionNeedRefresh, err
	}

	absPath := filepath.Join(tilesetsFolder, "tileset.json")
	buf, err := os.ReadFile(absPath)
	if err != nil {
		return partitionNeedRefresh, err
	}

	var tileset Tileset
	err = json.Unmarshal(buf, &tileset)
	if err != nil {
		return partitionNeedRefresh, err
	}
	for geohash, change := range changes {
		node := FindTileNodeByGeohash(tileset.Root, geohash)
		if node != nil {
			bound := change.BoundingVolume
			if change.Transform != nil {
				bound.Box = cesium.WorldBoxToLocalBox(bound.Box, *change.Transform)
			}
			node.BoundingVolume = bound
			node.Content = change.Content
			node.GeometricError = change.GeometricError
			node.Transform = change.Transform
			node.Refine = change.Refine
			node.Children = change.Children
		}
	}
	if tileset.Extensions.ThreeDTILESMetadata.Schema.Classes.TilesetInfo.Properties.UpdateTime != nil {
		tileset.Extensions.ThreeDTILESMetadata.Tilesets.Main.Properties.UpdateTime = now.Format("20060102150405")
	}
	data, _ := json.MarshalIndent(tileset, "", "  ")
	_ = os.WriteFile(absPath, data, 0644)

	log.Infof("✅ tileset.json 生成完成 %s", absPath)

	return partitionNeedRefresh, nil
}

func FindTileNodeByGeohash(root *TileNode, geohash string) *TileNode {
	if root == nil {
		return nil
	}

	// 当前节点匹配
	if root.Geohash == geohash {
		return root
	}

	// 递归遍历子节点
	for _, child := range root.Children {
		if result := FindTileNodeByGeohash(child, geohash); result != nil {
			return result
		}
	}

	return nil
}

func findExistParent(parentMap map[string]*TileNode, hash string) (*TileNode, bool) {
	for s, node := range parentMap {
		if !strings.HasPrefix(hash, s) {
			continue
		}
		found := FindNodeByGeohash(node, hash)
		if found != nil {
			return found, node.Level < found.Level
		}
	}
	return nil, false
}

// FindNodeByGeohash 在 TileNode 树中递归查找 geohash 对应的节点
func FindNodeByGeohash(root *TileNode, target string) *TileNode {
	if root == nil {
		return nil
	}

	// 1. 当前节点是否匹配
	if root.Geohash == target {
		return root
	}

	// 2. 遍历所有子节点
	for _, child := range root.Children {
		if child == nil {
			continue
		}
		res := FindNodeByGeohash(child, target)
		if res != nil {
			return res // 找到立即返回
		}
	}

	// 3. 递归都没有找到
	return nil
}

// 查询分片的所有模型数据
func QueryQbbsByGeohashBBox(configName string, db *gorm.DB, geohash string, bound projectBound, lod config.TilesetLODConfig) (map[string][]*GeoHashModel, error) {
	var qbbs []*GeoHashModel
	err := db.Raw(fmt.Sprintf(`
		SELECT dev.id, dev.chn_name as name, 'hdQbb' as type, dev.model, '%s' as table_name, 
		       ST_X(ST_TRANSFORM(dev.geom, 4326)) AS lng,
		       ST_Y(ST_TRANSFORM(dev.geom, 4326)) AS lat,
		       ST_Z(ST_TRANSFORM(dev.geom, 4326)) AS alt, 
		       dev.transform, dev.obj_angle
		FROM %s dev
		WHERE ST_GeoHash(dev.geom, ?) LIKE ? and (dev.model like '%%glb' or dev.model like '%%gltf')
		  AND ST_Intersects(ST_Transform(dev.geom, 4326), ST_MakeEnvelope(?, ?, ?, ?, 4326))
	`, QbbTileTableName, QbbTileTableName),
		append([]interface{}{len(geohash), geohash}, bound.args()...)...).Scan(&qbbs).Error
	if err != nil {
		return nil, err
	}

	models, err2 := getModelContentFromMinio(db, configName, qbbs, lod)
	if err2 != nil {
		return nil, err2
	}

	return models, err
}

// 查询分片的所有模型数据
func QueryDevicesSfzByGeohashBBox(configName string, db *gorm.DB, geohash string, bound projectBound, lod config.TilesetLODConfig) (map[string][]*GeoHashModel, error) {
	var devices []*GeoHashModel
	err := db.Raw(fmt.Sprintf(`
		SELECT dev.id, dev.chn_name as name, 'hdDevice' as type, dev.model, '%s' as table_name, 
		       ST_X(ST_TRANSFORM(dev.geom, 4326)) AS lng,
		       ST_Y(ST_TRANSFORM(dev.geom, 4326)) AS lat,
		       ST_Z(ST_TRANSFORM(dev.geom, 4326)) AS alt, 
		       dev.transform, dev.obj_angle
		FROM %s dev
		WHERE ST_GeoHash(dev.geom, ?) LIKE ? and (dev.model like '%%glb' or dev.model like '%%gltf')
		  AND ST_Intersects(ST_Transform(dev.geom, 4326), ST_MakeEnvelope(?, ?, ?, ?, 4326))
	`, DeviceSfzTileTableName, DeviceSfzTileTableName),
		append([]interface{}{len(geohash), geohash}, bound.args()...)...).Scan(&devices).Error
	if err != nil {
		return nil, err
	}

	models, err2 := getModelContentFromMinio(db, configName, devices, lod)
	if err2 != nil {
		return nil, err2
	}

	return models, err
}

// 查询分片的所有模型数据
func QueryDevicesByGeohashBBox(configName string, db *gorm.DB, geohash string, bound projectBound, lod config.TilesetLODConfig) (map[string][]*GeoHashModel, error) {
	var devices []*GeoHashModel
	err := db.Raw(fmt.Sprintf(`
		SELECT dev.id, dev.chn_name as name, 'hdDevice' as type, dev.model, '%s' as table_name, 
		       ST_X(ST_TRANSFORM(dev.geom, 4326)) AS lng,
		       ST_Y(ST_TRANSFORM(dev.geom, 4326)) AS lat,
		       ST_Z(ST_TRANSFORM(dev.geom, 4326)) AS alt, 
		       dev.transform, dev.obj_angle, edit.metadata, edit.service_data
		FROM %s dev
		LEFT JOIN %s edit on edit.id::text = dev.id::text  and edit.device_table = '%s'
		WHERE ST_GeoHash(dev.geom, ?) LIKE ? and (dev.model like '%%glb' or dev.model like '%%gltf')
		  AND ST_Intersects(ST_Transform(dev.geom, 4326), ST_MakeEnvelope(?, ?, ?, ?, 4326))
	`, DeviceTileTableName, DeviceTileTableName, DeviceEdit{}.TableName(), DeviceTileTableName),
		append([]interface{}{len(geohash), geohash}, bound.args()...)...).Scan(&devices).Error
	if err != nil {
		return nil, err
	}

	models, err2 := getModelContentFromMinio(db, configName, devices, lod)
	if err2 != nil {
		return nil, err2
	}

	return models, err
}

// 查询数据带有id的所有geohash
func QueryGeohashByPoints(db *gorm.DB, lngLats [][]float64) ([]string, error) {
	var pts []string
	for _, ll := range lngLats {
		if len(ll) >= 2 {
			pts = append(pts, fmt.Sprintf("(%f, %f)", ll[0], ll[1]))
		}
	}

	var list []string
	var datas []*GeoHashData
	err := db.Raw(fmt.Sprintf(`
		WITH pts(lng, lat) AS (
			VALUES
			%s
		)
		SELECT
			lng,
			lat,
			ST_GeoHash(
				ST_SetSRID(ST_MakePoint(lng, lat), 4326),
				%d
			) AS geohash
		FROM pts;
	`, strings.Join(pts, ","), MAX_GEOHASH_LEVEL)).Scan(&datas).Error
	if err != nil {
		return nil, err
	}

	for _, data := range datas {
		list = append(list, data.GeoHash)
	}

	return list, nil
}

// 查询分片的所有模型数据
func QuerySignsByGeohashBBox(configName string, db *gorm.DB, geohash string, bound projectBound, lod config.TilesetLODConfig) (map[string][]*GeoHashModel, error) {
	var devices []*GeoHashModel
	err := db.Raw(fmt.Sprintf(`
		SELECT dev.id, dev.id::text as name, 'hdSign' as type, dev.model, '%s' as table_name,
			ST_X(ST_TRANSFORM(dev.geom, 4326)) AS lng,
			ST_Y(ST_TRANSFORM(dev.geom, 4326)) AS lat,
			ST_Z(ST_TRANSFORM(dev.geom, 4326)) AS alt, 
			dev.width, dev.height, edit.service_data,
			NULLIF(dev.type, '0')::text as type_code, 
			NULLIF(dev.s_type, '0')::text as stype_code, 
			NULLIF(dev.gb_type, '0')::text as gbtype_code,
			dev.transform, dev.obj_angle,
			r.name AS road_name,
			r.traveltype as travel_type,
			CASE
				WHEN r.line_geom IS NULL OR cp.proj_pt IS NULL THEN NULL
				ELSE ROUND(
					ST_M(
						ST_LineInterpolatePoint(
							ST_AddMeasure(r.line_geom, r.fromz, r.toz),
							ST_LineLocatePoint(r.line_geom, cp.proj_pt)
						)
					)::numeric,
					6
				)
			END AS km_value
		FROM %s dev
		LEFT JOIN %s edit on edit.id::text = dev.id::text and edit.sign_table = '%s'
		LEFT JOIN LATERAL (
			SELECT r.*,
			d.geom AS line_geom
			FROM km_stake_road r
			CROSS JOIN LATERAL ST_Dump(r.geom) d
			WHERE d.geom IS NOT NULL
			ORDER BY d.geom <-> dev.geom
			LIMIT 1
			) r ON TRUE
		LEFT JOIN LATERAL (
			SELECT ST_ClosestPoint(r.line_geom, dev.geom) AS proj_pt
			WHERE r.line_geom IS NOT NULL
		) cp ON TRUE
		WHERE ST_GeoHash(dev.geom, ?) LIKE ? and (dev.model like '%%glb' or dev.model like '%%gltf')
		  AND ST_Intersects(ST_Transform(dev.geom, 4326), ST_MakeEnvelope(?, ?, ?, ?, 4326))
	`, SignTileTableName, SignTileTableName, SignEdit{}.TableName(), SignTileTableName),
		append([]interface{}{len(geohash), geohash}, bound.args()...)...).Scan(&devices).Error
	if err != nil {
		return nil, err
	}

	models, err2 := getModelContentFromMinio(db, configName, devices, lod)
	if err2 != nil {
		return nil, err2
	}

	return models, err
}

// 查询分片的所有模型数据
func QueryPolesByGeohashBBox(configName string, db *gorm.DB, geohash string, bound projectBound, lod config.TilesetLODConfig) (map[string][]*GeoHashModel, error) {
	var devices []*GeoHashModel
	err := db.Raw(fmt.Sprintf(`
		SELECT dev.pole_id as id, dev.id::text as name, 'hdPole' as type, dev.model, '%s' as table_name,
		       ST_X(ST_TRANSFORM(dev.geom, 4326)) AS lng,
		       ST_Y(ST_TRANSFORM(dev.geom, 4326)) AS lat,
		       ST_Z(ST_TRANSFORM(dev.geom, 4326)) AS alt, 
		       dev.transform, dev.obj_angle, edit.service_data
		FROM %s dev
		LEFT JOIN %s edit on edit.id::text = dev.id::text and edit.pole_table = '%s'
		WHERE ST_GeoHash(dev.geom, ?) LIKE ? and (dev.model like '%%glb' or dev.model like '%%gltf')
		  AND ST_Intersects(ST_Transform(dev.geom, 4326), ST_MakeEnvelope(?, ?, ?, ?, 4326))
	`, PoleTileTableName, PoleTileTableName, PoleEdit{}.TableName(), PoleTileTableName),
		append([]interface{}{len(geohash), geohash}, bound.args()...)...).Scan(&devices).Error
	if err != nil {
		return nil, err
	}

	models, err2 := getModelContentFromMinio(db, configName, devices, lod)
	if err2 != nil {
		return nil, err2
	}

	return models, err
}

// 查询分片的所有模型数据
func QueryGantrysByGeohashBBox(configName string, db *gorm.DB, geohash string, bound projectBound, lod config.TilesetLODConfig) (map[string][]*GeoHashModel, error) {
	var devices []*GeoHashModel
	err := db.Raw(fmt.Sprintf(`
		SELECT dev.id, dev.id::text as name, 'hdGantry' as type, dev.model, '%s' as table_name,
		       ST_X(ST_TRANSFORM(dev.geom, 4326)) AS lng,
		       ST_Y(ST_TRANSFORM(dev.geom, 4326)) AS lat,
		       ST_Z(ST_TRANSFORM(dev.geom, 4326)) AS alt, 
		       dev.transform, dev.obj_angle, edit.service_data
		FROM %s dev
		LEFT JOIN %s edit on edit.id::text = dev.id::text and edit.gantry_table = '%s'
		WHERE ST_GeoHash(dev.geom, ?) LIKE ? and (dev.model like '%%glb' or dev.model like '%%gltf')
		  AND ST_Intersects(ST_Transform(dev.geom, 4326), ST_MakeEnvelope(?, ?, ?, ?, 4326))
	`, GantryTileTableName, GantryTileTableName, GantryEdit{}.TableName(), GantryTileTableName),
		append([]interface{}{len(geohash), geohash}, bound.args()...)...).Scan(&devices).Error
	if err != nil {
		return nil, err
	}

	models, err2 := getModelContentFromMinio(db, configName, devices, lod)
	if err2 != nil {
		return nil, err2
	}

	return models, err
}

// 查询分片的所有模型数据
func QueryBridgesByGeohashBBox(configName string, db *gorm.DB, geohash string, bound projectBound, lod config.TilesetLODConfig) (map[string][]*GeoHashModel, error) {
	var devices []*GeoHashModel
	err := db.Raw(fmt.Sprintf(`
		SELECT dev.id, dev.id::text as name, 'hdBridge' as type, dev.model, '%s' as table_name,
		       ST_X(ST_TRANSFORM(dev.geom, 4326)) AS lng,
		       ST_Y(ST_TRANSFORM(dev.geom, 4326)) AS lat,
		       ST_Z(ST_TRANSFORM(dev.geom, 4326)) AS alt, 
		       dev.transform, dev.obj_angle
		FROM %s dev
		WHERE ST_GeoHash(dev.geom, ?) LIKE ? and (dev.model like '%%glb' or dev.model like '%%gltf')
		  AND ST_Intersects(ST_Transform(dev.geom, 4326), ST_MakeEnvelope(?, ?, ?, ?, 4326))
	`, BridgeTileTableName, BridgeTileTableName),
		append([]interface{}{len(geohash), geohash}, bound.args()...)...).Scan(&devices).Error
	if err != nil {
		return nil, err
	}

	models, err2 := getModelContentFromMinio(db, configName, devices, lod)
	if err2 != nil {
		return nil, err2
	}

	return models, err
}

// 查询分片的所有模型数据
func QueryRoadSideFacilityByGeohashBBox(configName string, db *gorm.DB, geohash string, bound projectBound, lod config.TilesetLODConfig) (map[string][]*GeoHashModel, error) {
	var devices []*GeoHashModel
	err := db.Raw(fmt.Sprintf(`
		SELECT dev.id, dev.id::text as name, 'hdBuilding' as type, '%s' as table_name,
			   (
				   ST_XMin(Box3D(ST_Transform(dev.geom, 4326)))
					   + ST_XMax(Box3D(ST_Transform(dev.geom, 4326)))
				   ) / 2.0 as lng,
			   (
				   ST_YMin(Box3D(ST_Transform(dev.geom, 4326)))
					   + ST_YMax(Box3D(ST_Transform(dev.geom, 4326)))
				   ) / 2.0 as lat,
			   (
				   ST_ZMin(Box3D(ST_Transform(dev.geom, 4326)))
					   + ST_ZMax(Box3D(ST_Transform(dev.geom, 4326)))
				   ) / 2.0 as alt,
			   id::text || '.glb' as model
		FROM %s dev
		WHERE ST_GeoHash(dev.geom, ?) LIKE ? 
		  AND ST_Intersects(ST_Transform(dev.geom, 4326), ST_MakeEnvelope(?, ?, ?, ?, 4326))
	`, RoadSideFacilityTileTableName, RoadSideFacilityTileTableName),
		append([]interface{}{len(geohash), geohash}, bound.args()...)...).Scan(&devices).Error
	if err != nil {
		return nil, err
	}

	models, err2 := getModelContentFromMinio(db, configName, devices, lod)
	if err2 != nil {
		return nil, err2
	}

	return models, err
}

// 查询分片的所有模型数据
func QueryServiceEquAreaByGeohashBBox(configName string, db *gorm.DB, geohash string, bound projectBound, lod config.TilesetLODConfig) (map[string][]*GeoHashModel, error) {
	var devices []*GeoHashModel
	err := db.Raw(fmt.Sprintf(`
		SELECT dev.id, dev.id::text as name, 'hdBuilding' as type, '%s' as table_name,
			   (
				   ST_XMin(Box3D(ST_Transform(dev.geom, 4326)))
					   + ST_XMax(Box3D(ST_Transform(dev.geom, 4326)))
				   ) / 2.0 as lng,
			   (
				   ST_YMin(Box3D(ST_Transform(dev.geom, 4326)))
					   + ST_YMax(Box3D(ST_Transform(dev.geom, 4326)))
				   ) / 2.0 as lat,
			   (
				   ST_ZMin(Box3D(ST_Transform(dev.geom, 4326)))
					   + ST_ZMax(Box3D(ST_Transform(dev.geom, 4326)))
				   ) / 2.0 as alt,
			   id::text || '.glb' as model
		FROM %s dev
		WHERE ST_GeoHash(dev.geom, ?) LIKE ?
		  AND ST_Intersects(ST_Transform(dev.geom, 4326), ST_MakeEnvelope(?, ?, ?, ?, 4326))
	`, ServiceEquAreaTileTableName, ServiceEquAreaTileTableName),
		append([]interface{}{len(geohash), geohash}, bound.args()...)...).Scan(&devices).Error
	if err != nil {
		return nil, err
	}

	models, err2 := getModelContentFromMinio(db, configName, devices, lod)
	if err2 != nil {
		return nil, err2
	}

	return models, err
}

// 查询分片的所有模型数据
func QueryRenderTollBuildingsByGeohashBBox(configName string, db *gorm.DB, geohash string, bound projectBound, lod config.TilesetLODConfig) (map[string][]*GeoHashModel, error) {
	var devices []*GeoHashModel

	err := db.Raw(fmt.Sprintf(`
		SELECT dev.id, dev.id::text as name, 'hdBuilding' as type, dev.model, '%s' as table_name,
		       ST_X(ST_TRANSFORM(dev.geom, 4326)) AS lng,
		       ST_Y(ST_TRANSFORM(dev.geom, 4326)) AS lat,
		       ST_Z(ST_TRANSFORM(dev.geom, 4326)) AS alt, height
		FROM %s dev
		WHERE ST_GeoHash(dev.geom, ?) LIKE ? and (dev.model like '%%glb' or dev.model like '%%gltf')
		  AND ST_Intersects(ST_Transform(dev.geom, 4326), ST_MakeEnvelope(?, ?, ?, ?, 4326))
	`, RenderTollBuildingTileTableName, RenderTollBuildingTileTableName),
		append([]interface{}{len(geohash), geohash}, bound.args()...)...).Scan(&devices).Error
	if err != nil {
		return nil, err
	}

	models, err2 := getModelContentFromMinio(db, configName, devices, lod)
	if err2 != nil {
		return nil, err2
	}

	return models, err
}

type MinioBucket struct {
	client     *minioconn.MinioConn
	bucketName string
	prefix     string
}

func getModelContentFromMinio(db *gorm.DB, cfgName string, devices []*GeoHashModel, lod config.TilesetLODConfig) (
	map[string][]*GeoHashModel, error) {
	models := make(map[string][]*GeoHashModel)
	code := config.EncodeContext(cfgName, "")
	cfg := config.Instance()
	localFirst := cfg != nil && lod.LOD3.LocalFirst
	minioFallback := cfg == nil || lod.LOD3.MinioFallback
	var localRoot string
	var err error
	if localFirst {
		localRoot, err = localLODModelRoot(cfg, tileLODLevel{
			Level:       3,
			ModelFolder: lod.LOD3.ModelFolder,
		})
		if err != nil {
			return nil, err
		}
	}
	localModels := make(map[string]*GltfModel)

	clientMap := make(map[string]MinioBucket)
	for _, device := range devices {
		if localFirst {
			modelPath, pathErr := localLODModelPath(localRoot, device.TableName, device.Model, 3)
			if pathErr != nil {
				return nil, pathErr
			}
			model, cached := localModels[modelPath]
			if !cached {
				content, readErr := os.ReadFile(modelPath)
				if readErr == nil {
					model = &GltfModel{
						Name:        device.Model,
						Content:     content,
						ContentType: detectGltfFormat(content),
					}
				} else if !os.IsNotExist(readErr) {
					return nil, fmt.Errorf("read LOD3 model %s: %w", modelPath, readErr)
				}
				localModels[modelPath] = model
			}
			if model != nil {
				device.Gltf = model
				appendGeoHashModelsByGroup(models, device)
				continue
			}
			if !minioFallback {
				log.Warnf("LOD3 local model not found, skip: %s", modelPath)
				continue
			}
		}
		if !minioFallback {
			log.Warnf("LOD3 MinIO fallback disabled, skip model: %s/%s", device.TableName, device.Model)
			continue
		}

		var client *minioconn.MinioConn
		var bucketName string
		var prefix string
		var errG error
		if cm, ok := clientMap[device.Type]; ok {
			client = cm.client
			bucketName = cm.bucketName
			prefix = cm.prefix
		} else {
			switch device.Type {
			case "hdSign":
				client, bucketName, prefix, errG = getMinioOutputSignGltfClient(code)
			case "hdPole":
				client, bucketName, prefix, errG = getMinioOutputPoleGltfClient(code)
			case "hdGantry":
				client, bucketName, prefix, errG = minioconn.GetMinioBaseGltfClient(code)
			case "hdDevice":
				client, bucketName, prefix, errG = minioconn.GetMinioBaseGltfClient(code)
			case "hdQbb":
				client, bucketName, prefix, errG = minioconn.GetMinioBaseGltfClient(code)
			case "hdBuilding":
				client, bucketName, prefix, errG = minioconn.GetMinioBaseGltfClient(code)
			default:
				return nil, fmt.Errorf("生成模型设备类型错误: %s", device.Type)
			}

			if errG != nil {
				return nil, errG
			}

			clientMap[device.Type] = MinioBucket{
				client:     client,
				bucketName: bucketName,
				prefix:     prefix,
			}
		}

		objectName, errJ := url.JoinPath(prefix, device.Model)
		if errJ != nil {
			return nil, fmt.Errorf("地址错误: %s", errJ.Error())
		}

		// 从 MinIO 获取对象
		object, errG := client.GetObject(context.Background(), bucketName, objectName)

		var content []byte
		var errO error
		// 读取对象内容
		if errG != nil {
			log.Errorf("Failed to read object content: %s/%s %v", bucketName, objectName, errO)
		} else {
			content, errO = io.ReadAll(object)
			if errO != nil {
				log.Errorf("Failed to read object content: %s/%s %v", bucketName, objectName, errO)
			}
			_ = object.Close()
		}

		contentType := detectGltfFormat(content)
		model := &GltfModel{
			Name:        device.Model,
			Content:     content,
			ContentType: contentType,
		}

		device.Gltf = model
		appendGeoHashModelsByGroup(models, device)
	}
	return models, nil
}

// 根据分片模型数据生成分片模型
func GeneratePartitionModel(models map[string][]*GeoHashModel, center []float64, region [4]float64) (*BuildModel, error) {
	bm, err := mapmodel.NewBuildModels(center, region)
	if err != nil {
		return nil, err
	}

	var modelCount = 0
	for _, hashModels := range models {

		if len(hashModels) == 0 || hashModels[0].Gltf == nil {
			continue
		}
		model := hashModels[0]
		log.Infof("generate model %s %s", model.Name, model.Gltf.Name)

		var mdoc *gltf.Document
		if strings.Contains(detectGltfFormat(model.Gltf.Content), "json") {
			mdoc, err = mapmodel.RepairGLTF(model.Gltf.Content)
			if err != nil {
				log.Errorf("Failed to repair gltf: %s, try decode as glb", err.Error())

				decoder := gltf.NewDecoder(bytes.NewReader(model.Gltf.Content))
				mdoc = new(gltf.Document)
				if errD := decoder.Decode(mdoc); errD != nil {
					log.Errorf("Failed to decode glb: %s", errD.Error())
					continue
				}
				log.Infof("glb model decoded %s %s", model.Name, model.Gltf.Name)
			} else {
				log.Infof("gltf model decoded %s %s", model.Name, model.Gltf.Name)
			}
		} else {
			decoder := gltf.NewDecoder(bytes.NewReader(model.Gltf.Content))
			mdoc = new(gltf.Document)
			if errD := decoder.Decode(mdoc); errD != nil {
				log.Errorf("Failed to decode glb: %s", errD.Error())
				continue
			}
			log.Infof("glb model decoded %s %s", model.Name, model.Gltf.Name)
		}

		m := &mapmodel.Model{
			Doc:    mdoc,
			Coords: make([][3]float64, 0),
			R:      make([][3]float64, 0),
			S:      make([][3]float64, 0),
			Fields: make(map[string][]string),
		}

		for _, hm := range hashModels {
			var sData = make(map[string]any)
			if len(hm.ServiceData) > 0 {
				errM := json.Unmarshal([]byte(hm.ServiceData), &sData)
				if errM != nil {
					log.Errorf("Failed to unmarshal service data: %s", errM.Error())
				}
			}

			var scaleX, scaleY, scaleZ float64
			scaleX = 1.0
			scaleY = 1.0
			scaleZ = 1.0
			if len(hm.Transform) == 10 && hm.Transform[7]*hm.Transform[8]*hm.Transform[9] != 0 {
				scaleX = hm.Transform[7]
				scaleY = hm.Transform[8]
				scaleZ = hm.Transform[9]
			}
			m.Coords = append(m.Coords, [3]float64{hm.Lng, hm.Lat, hm.Alt})
			m.R = append(m.R, [3]float64{hm.ObjAngle, 0, 0})
			m.Fields["id"] = append(m.Fields["id"], fmt.Sprintf("%s.%s", hm.TableName, hm.Id))
			m.Fields["fid"] = append(m.Fields["fid"], hm.Id)
			m.Fields["ftype"] = append(m.Fields["ftype"], hm.Type)
			m.Fields["name"] = append(m.Fields["name"], utils.ToString(sData["bindName"]))
			m.Fields["type"] = append(m.Fields["type"], utils.ToString(sData["bindType"]))
			m.Fields["bid"] = append(m.Fields["bid"], utils.ToString(sData["bindId"]))
			m.Fields["width"] = append(m.Fields["width"], utils.ToString(utils.ToFixed(hm.Width, 3)))
			m.Fields["height"] = append(m.Fields["height"], utils.ToString(utils.ToFixed(hm.Height, 3)))
			m.Fields["lng"] = append(m.Fields["lng"], utils.ToString(utils.ToFixed(hm.Lng, 8)))
			m.Fields["lat"] = append(m.Fields["lat"], utils.ToString(utils.ToFixed(hm.Lat, 8)))
			m.Fields["alt"] = append(m.Fields["alt"], utils.ToString(utils.ToFixed(hm.Alt, 3)))
			m.Fields["km_value"] = append(m.Fields["km_value"], fmt.Sprintf("%.3f", utils.ToFixed(hm.KmValue, 3)))
			m.Fields["travel_type"] = append(m.Fields["travel_type"], utils.ToString(hm.TravelType))
			m.Fields["road_name"] = append(m.Fields["road_name"], hm.RoadName)
			m.Fields["type_code"] = append(m.Fields["type_code"], hm.TypeCode)
			m.Fields["stype_code"] = append(m.Fields["stype_code"], hm.STypeCode)
			m.Fields["gbtype_code"] = append(m.Fields["gbtype_code"], hm.GBTypeCode)

			m.S = append(m.S, [3]float64{scaleX, scaleY, scaleZ})

			// 直接使用cesium的transform矩阵
			var useTransform bool
			var trans = [16]float64{}
			if hm.Metadata != nil {
				var meta = TIleMetadata{}
				if errM := json.Unmarshal(hm.Metadata, &meta); errM != nil {
					log.Error(errM)
				}
				if meta.Transform != nil && len(meta.Transform) == 16 {
					useTransform = true
					trans = [16]float64{
						meta.Transform["0"],
						meta.Transform["1"],
						meta.Transform["2"],
						meta.Transform["3"],
						meta.Transform["4"],
						meta.Transform["5"],
						meta.Transform["6"],
						meta.Transform["7"],
						meta.Transform["8"],
						meta.Transform["9"],
						meta.Transform["10"],
						meta.Transform["11"],
						meta.Transform["12"],
						meta.Transform["13"],
						meta.Transform["14"],
						meta.Transform["15"],
					}
				}
			}
			m.WT = append(m.WT, trans)
			m.UseWT = append(m.UseWT, useTransform)
			log.Infof("added %s %s model %s lng:%f lat:%f alt:%f ", hm.Type, hm.Id, hm.Model, hm.Lng, hm.Lat, hm.Alt)
		}

		errM := bm.AddModel(m)
		if errM != nil {
			log.Errorf("Failed to add model %s: %s", model.Id, errM.Error())
			continue
		}

		modelCount += 1
	}

	glbbuf, err := bm.BuildBinary()
	if err != nil {
		return nil, err
	}

	builed := &BuildModel{
		Region:    bm.Region(),
		Transform: bm.Transform(),
		Content:   glbbuf,
	}
	return builed, nil
}

func findRootGeohash(m map[string]*TileNode) string {
	minLen := 999
	root := ""
	for g := range m {
		if len(g) < minLen {
			minLen = len(g)
			root = g
		}
	}
	return root
}

// 确保模型数据输出
func EnsureOutputModelBytesFromGltf(doc *gltf.Document, asBinary bool) ([]byte, error) {
	buff := new(bytes.Buffer)
	e := gltf.NewEncoder(buff)
	e.AsBinary = false
	if err := e.Encode(doc); err != nil {
		return nil, errors.New("模型组装错误：" + err.Error())
	}
	gltfBuff := buff.Bytes()

	if asBinary {
		//按glb规范将所有数据写入buff
		mdoc, err := mapmodel.RepairGLTF(gltfBuff)
		if err != nil {
			return nil, err
		}
		buff = new(bytes.Buffer)
		e = gltf.NewEncoder(buff)
		e.AsBinary = true
		if err = e.Encode(mdoc); err != nil {
			return nil, errors.New("模型组装错误：" + err.Error())
		}
		return buff.Bytes(), nil
	} else {
		return gltfBuff, nil
	}
}

func getMinioOutputPoleGltfClient(code string) (*minioconn.MinioConn, string, string, error) {
	if len(code) > 0 {
		configName, _, errD := config.DecodeContext(code)
		if errD != nil {
			return nil, "", "", errors.New("decode context error:" + errD.Error())
		}
		cfg, errC := config.GetNetworkConfigByName(configName)
		if errC != nil {
			return nil, "", "", errors.New("get config error:" + errC.Error())
		}

		client := minioconn.GetMinioConn(configName)
		if client == nil {
			return nil, "", "", errors.New("minio client not exists")
		}

		prefix := ""
		bucket := cfg.MinioOutput.Bucket
		if cfg.MinioOutput.PoleGltfBucket != "" {
			var keys = strings.Split(cfg.MinioOutput.PoleGltfBucket, "/")
			if len(keys) > 0 {
				bucket = keys[0]
				if len(keys) > 1 {
					prefix = strings.Join(keys[1:], "/")
				}
			}
		}
		return client, bucket, prefix, nil
	}

	return nil, "", "", errors.New("context is null")
}

func getMinioOutputDeviceGltfPath(code string) (*minioconn.MinioConn, string, string, error) {
	if len(code) > 0 {
		configName, _, errD := config.DecodeContext(code)
		if errD != nil {
			return nil, "", "", errors.New("decode context error:" + errD.Error())
		}
		cfg, errC := config.GetNetworkConfigByName(configName)
		if errC != nil {
			return nil, "", "", errors.New("get config error:" + errC.Error())
		}

		client := minioconn.GetMinioConn(configName)
		if client == nil {
			return nil, "", "", errors.New("minio client not exists")
		}

		prefix := ""
		bucket := cfg.MinioOutput.Bucket
		if cfg.MinioOutput.DeviceGltfBucket != "" {
			var keys = strings.Split(cfg.MinioOutput.DeviceGltfBucket, "/")
			if len(keys) > 0 {
				bucket = keys[0]
				if len(keys) > 1 {
					prefix = strings.Join(keys[1:], "/")
				}
			}
		}
		return client, bucket, prefix, nil
	}

	return nil, "", "", errors.New("context is null")
}

func getMinioOutputSignGltfClient(code string) (*minioconn.MinioConn, string, string, error) {
	if len(code) > 0 {
		configName, _, errD := config.DecodeContext(code)
		if errD != nil {
			return nil, "", "", errors.New("decode context error:" + errD.Error())
		}
		cfg, errC := config.GetNetworkConfigByName(configName)
		if errC != nil {
			return nil, "", "", errors.New("get config error:" + errC.Error())
		}

		client := minioconn.GetMinioConn(configName)
		if client == nil {
			return nil, "", "", errors.New("minio client not exists")
		}

		prefix := ""
		bucket := cfg.MinioOutput.Bucket
		if cfg.MinioOutput.SignGltfBucket != "" {
			var keys = strings.Split(cfg.MinioOutput.SignGltfBucket, "/")
			if len(keys) > 0 {
				bucket = keys[0]
				if len(keys) > 1 {
					prefix = strings.Join(keys[1:], "/")
				}
			}
		}
		return client, bucket, prefix, nil
	}

	return nil, "", "", errors.New("context is null")
}
