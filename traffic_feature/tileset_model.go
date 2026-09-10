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

const tileModelRootDir = "tiles"

// tileModelRelativePath returns a URL-style relative path for a geohash tile.
// A directory is added for every two geohash characters. Each directory keeps
// the complete prefix accumulated so far, making its spatial prefix directly
// identifiable while preventing a single directory from holding too many GLBs.
//
// Example: wtw3sjq9 -> tiles/wt/wtw3/wtw3sj/wtw3sjq9.glb
func tileModelRelativePath(geohash string) string {
	parts := []string{tileModelRootDir}
	prefixLength := len(geohash) - 2
	for start := 0; start < prefixLength; start += 2 {
		end := start + 2
		if end > prefixLength {
			end = prefixLength
		}
		parts = append(parts, geohash[:end])
	}
	parts = append(parts, geohash+".glb")
	return path.Join(parts...)
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

	changes, errG := doTileJob(db, now, configName, leafTiles, geoTable, tilesetsFolder, partitionTable)
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
			node.Children = change.Children
			node.Transform = change.Transform
			node.Content = change.Content
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
func GenerateAllGeoHashTile(configName string, partitionTable string, tilesetsFolder string) (*Tileset, error) {
	if configName == "" {
		return nil, fmt.Errorf("configName is empty")
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
        AND parent.total_count > 0;
    `, partitionTable, partitionTable)).Scan(&leafTiles).Error
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
	// 构建叶子节点
	tilesByGeohash, errG := doTileJob(db, now, configName, leafTiles, geoTable, tilesetsFolder, partitionTable)
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
				GeometricError: getGeometricError(len(parent)),
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
					GeometricError: getGeometricError(len(parent)),
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
		GeometricError: getGeometricError(2),
		Children:       nil,
		Level:          2,
		Geohash:        "root",
		Refine:         "ADD",
	}
	for _, n := range parentMap {
		root.Children = append(root.Children, n)
	}
	refreshTileBound(root)

	tileset := &Tileset{
		GeometricError: getGeometricError(2),
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
	if node.Children == nil || len(node.Children) == 0 {
		return
	}
	// 合并 boundingVolume

	var boxes []cesium.Box12
	for _, n := range node.Children {
		refreshTileBound(n)
		boxes = append(boxes, n.BoundingVolume.Box)
		if n.Transform != nil {
			box := cesium.WorldBoxToLocalBox(n.BoundingVolume.Box, *n.Transform)
			n.BoundingVolume.Box = box
		}
		if n.Content != nil {
			n.GeometricError = 0
		}
	}
	box := cesium.MergeBoxes(boxes, 1.05)
	node.BoundingVolume = BoundingVolume{
		Box: box,
	}
}

func doTileJob(db *gorm.DB, now time.Time, configName string, leafTiles []*GeoHashTile, geoTable GeoTable, tilesetsFolder string, partitionTable string) (map[string]*TileNode, error) {
	tilesByGeohash := make(map[string]*TileNode)
	workerCount := 8 // tune this based on CPU / DB capacity

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

				models, err2 := queryGeoHashModelData(configName, geoTable, db, t.Geohash, "")
				if err2 != nil {
					results <- TileJobResult{err: err2}
					continue
				}
				if len(models) == 0 {
					continue
				}

				fn, built, err3 := buildTileByHashModels(models, t, tilesetsFolder)
				if err3 != nil {
					log.Errorf("Failed to build tile for %s:%s %s", configName, partitionTable, err3.Error())
					continue
				}

				transform := built.Transform
				box := cesium.RegionToBox(built.Region, 1.2)

				/*				box := cesium.GetBoxFromBoundHeight(fmt.Sprintf("%.12f,%.12f,%.12f,%.12f", t.MinX, t.MinY, t.MaxX, t.MaxY),
								built.Region[4], built.Region[5])*/
				node := &TileNode{
					BoundingVolume: BoundingVolume{
						Box: box,
					},
					GeometricError: getGeometricError(t.Level),
					Content: &TileContent{
						Uri: fmt.Sprintf("%s?t=%s", fn, now.Format("20060102150405")),
					},
					Transform: &transform,
					Level:     t.Level,
					Geohash:   t.Geohash,
				}

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

func buildTileByHashModels(models map[string][]*GeoHashModel, tile *GeoHashTile, tilesetsFolder string) (string, *BuildModel, error) {
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
	fn := tileModelRelativePath(tile.Geohash)
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

func queryGeoHashModelData(configName string, tile GeoTable, db *gorm.DB, geoHash string, dataTable string) (map[string][]*GeoHashModel, error) {
	var models = make(map[string][]*GeoHashModel)
	for _, name := range tile.GeoTableNames {
		if len(dataTable) > 0 && dataTable != name {
			continue
		}
		switch name {
		case SignTileTableName:
			vs, errQ := QuerySignsByGeohashBBox(configName, db, geoHash)
			if errQ != nil {
				return nil, errQ
			}
			for s, hashModels := range vs {
				models[s] = append(models[s], hashModels...)
			}
		case DeviceTileTableName:
			vs, errQ := QueryDevicesByGeohashBBox(configName, db, geoHash)
			if errQ != nil {
				return nil, errQ
			}
			for s, hashModels := range vs {
				models[s] = append(models[s], hashModels...)
			}
		case PoleTileTableName:
			vs, errQ := QueryPolesByGeohashBBox(configName, db, geoHash)
			if errQ != nil {
				return nil, errQ
			}
			for s, hashModels := range vs {
				models[s] = append(models[s], hashModels...)
			}
		case GantryTileTableName:
			vs, errQ := QueryGantrysByGeohashBBox(configName, db, geoHash)
			if errQ != nil {
				return nil, errQ
			}
			for s, hashModels := range vs {
				models[s] = append(models[s], hashModels...)
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
	var changes []*TileChange
	for _, t := range leafTiles {
		if t.UpdateTime.IsZero() {
			continue
		}

		models, err2 := queryGeoHashModelData(configName, geoTable, db, t.Geohash, "")
		if err2 != nil {
			return partitionNeedRefresh, err2
		}
		if len(models) == 0 {
			continue
		}

		fn, _, err3 := buildTileByHashModels(models, t, tilesetsFolder)
		if err3 != nil {
			return partitionNeedRefresh, err3
		}
		changes = append(changes, &TileChange{
			Geohash: t.Geohash,
			Content: &TileContent{Uri: fmt.Sprintf("%s?t=%s", fn, now.Format("20060102150405"))},
		})
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
	if tileset.Root != nil {
		refreshTileBound(tileset.Root)
	}
	for _, change := range changes {
		node := FindTileNodeByGeohash(tileset.Root, change.Geohash)
		if node != nil {
			node.Content = change.Content
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
func QueryDevicesByGeohashBBox(configName string, db *gorm.DB, geohash string) (map[string][]*GeoHashModel, error) {
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
	`, DeviceTileTableName, DeviceTileTableName, DeviceEdit{}.TableName(), DeviceTileTableName), len(geohash), geohash).Scan(&devices).Error
	if err != nil {
		return nil, err
	}

	models, err2 := getModelContentFromMinio(db, configName, devices)
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
func QuerySignsByGeohashBBox(configName string, db *gorm.DB, geohash string) (map[string][]*GeoHashModel, error) {
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
	`, SignTileTableName, SignTileTableName, SignEdit{}.TableName(), SignTileTableName), len(geohash), geohash).Scan(&devices).Error
	if err != nil {
		return nil, err
	}

	models, err2 := getModelContentFromMinio(db, configName, devices)
	if err2 != nil {
		return nil, err2
	}

	return models, err
}

// 查询分片的所有模型数据
func QueryPolesByGeohashBBox(configName string, db *gorm.DB, geohash string) (map[string][]*GeoHashModel, error) {
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
	`, PoleTileTableName, PoleTileTableName, PoleEdit{}.TableName(), PoleTileTableName), len(geohash), geohash).Scan(&devices).Error
	if err != nil {
		return nil, err
	}

	models, err2 := getModelContentFromMinio(db, configName, devices)
	if err2 != nil {
		return nil, err2
	}

	return models, err
}

// 查询分片的所有模型数据
func QueryGantrysByGeohashBBox(configName string, db *gorm.DB, geohash string) (map[string][]*GeoHashModel, error) {
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
	`, GantryTileTableName, GantryTileTableName, GantryEdit{}.TableName(), GantryTileTableName), len(geohash), geohash).Scan(&devices).Error
	if err != nil {
		return nil, err
	}

	models, err2 := getModelContentFromMinio(db, configName, devices)
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

func getModelContentFromMinio(db *gorm.DB, cfgName string, devices []*GeoHashModel) (
	map[string][]*GeoHashModel, error) {
	models := make(map[string][]*GeoHashModel)
	code := config.EncodeContext(cfgName, "")

	clientMap := make(map[string]MinioBucket)
	for _, device := range devices {
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
		models[device.Model] = append(models[device.Model], device)
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

// 根据层级估算 geometricError（你可根据瓦片大小调整）
func getGeometricError(level int) float64 {
	switch level {
	case 2:
		return 400
	case 3:
		return 200
	case 4:
		return 100
	case 5:
		return 50
	case 6:
		return 25
	default:
		return 25
	}
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
