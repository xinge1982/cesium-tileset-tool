package traffic_feature

import (
	"cesium-tileset-tool/config"
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/mmcloughlin/geohash"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// tileset 分片记录表
type TileSetPartition struct {
	ID          uint      `gorm:"column:id;primaryKey" json:"id"`
	Geohash     string    `gorm:"column:geohash;type:text;not null" json:"geohash"`
	Level       int16     `gorm:"column:level;not null" json:"level"`
	BBox        string    `gorm:"column:bbox;type:geometry(Polygon,4326);not null" json:"bbox"`
	TotalCount  int       `gorm:"column:total_count;default:0" json:"totalCount"`
	ParentHash  *string   `gorm:"column:parent_hash;type:text" json:"parentHash"`
	NeedRefine  bool      `gorm:"column:need_refine;default:false" json:"needRefine"`
	Refined     bool      `gorm:"column:refined;default:false" json:"refined"`
	Active      bool      `gorm:"column:active;default:true" json:"active"`
	Merged      bool      `gorm:"column:merged;default:false" json:"merged"`
	UpdatedTime time.Time `gorm:"column:update_time;autoUpdateTime" json:"updatedTime"`
}

type GeoCount struct {
	Geohash string
	Count   int
}

type projectBound struct {
	MinLng float64
	MinLat float64
	MaxLng float64
	MaxLat float64
}

func parseProjectBound(boundStr string) (projectBound, error) {
	values := strings.Split(boundStr, ",")
	if len(values) != 4 {
		return projectBound{}, fmt.Errorf("项目范围Bound未正确配置，应为 west,south,east,north")
	}
	numbers := make([]float64, 4)
	for i, value := range values {
		number, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
			return projectBound{}, fmt.Errorf("项目范围Bound第%d项不是有效数字: %q", i+1, value)
		}
		numbers[i] = number
	}
	bound := projectBound{numbers[0], numbers[1], numbers[2], numbers[3]}
	if bound.MinLng >= bound.MaxLng || bound.MinLat >= bound.MaxLat {
		return projectBound{}, fmt.Errorf("项目范围Bound顺序无效，应为 west < east 且 south < north")
	}
	return bound, nil
}

func (bound projectBound) args() []interface{} {
	return []interface{}{bound.MinLng, bound.MinLat, bound.MaxLng, bound.MaxLat}
}

func geohashIntersectsBound(hash string, bound projectBound) bool {
	box := geohash.BoundingBox(hash)
	return box.MaxLng >= bound.MinLng && box.MinLng <= bound.MaxLng &&
		box.MaxLat >= bound.MinLat && box.MinLat <= bound.MaxLat
}

type GeoTable struct {
	PartitionTableName string
	GeoTableNames      []string
	Threshold          int
	TilesetSources     map[string]config.TilesetSourceConfig
	LOD                config.TilesetLODConfig
}

const (
	MinTileSetLevel int16 = 3
)

type TileSetHash struct {
	GeoHash string         `json:"geoHash"`
	Bbox    string         `json:"bbox"`
	Center  string         `json:"center"`
	Models  []TileSetModel `json:"models"`
}

type TileSetModel struct {
	Model    string  `json:"model"`
	ObjAngle float64 `json:"objAngle"`
	Location string  `json:"location"`
}

// PartitionTableName 设置表名
// 复用 TileSetPartition

var AllTiles = []GeoTable{}

const (
	SignTileTableName      string = "hdtraffic_sign"
	QbbTileTableName       string = "hdtraffic_qbb"
	DeviceTileTableName    string = "hdtraffic_ene"
	DeviceSfzTileTableName string = "hdtraffic_ene_sfz"
	PoleTileTableName      string = "hdpole"
	GantryTileTableName    string = "hdgantry"
	BridgeTileTableName    string = "hdtraffic_bridges"

	MAX_GEOHASH_LEVEL int16 = 11
	MIN_GEOHASH_LEVEL int16 = 3

	MAX_TILE_MODEL_COUNT int = 100
)

func initTileSetPartitions(db *gorm.DB, minLng, minLat, maxLng, maxLat float64, tableName string, level int16) error {
	// 示例：初始化根级分片
	rootGeohashes := generateGeohashesInBounds(minLng, minLat, maxLng, maxLat, level)
	if err := initRootPartitions(db, tableName, level, rootGeohashes); err != nil {
		log.Errorf("failed to init root partitions: %v", err)
		return err
	}

	log.Println("生成 geohash 数量:", len(rootGeohashes))
	log.Println(rootGeohashes)
	return nil
}

// 初始化根级分片
func initRootPartitions(db *gorm.DB, tableName string, level int16, rootGeohashes []string) error {
	now := time.Now()
	for _, gh := range rootGeohashes {
		// 这里假设 BBox 用 WKT 表示
		parentHash := ""
		if len(gh) > 0 {
			parentHash = gh[:len(gh)-1]
		}
		bbox := geohashToBBoxWKT(gh) // 自己实现 geohash -> bbox WKT
		p := TileSetPartition{
			Geohash:     gh,
			Level:       level,
			BBox:        bbox,
			ParentHash:  &parentHash,
			UpdatedTime: now,
		}
		errC := db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "geohash"}, {Name: "level"}}, // 冲突判断条件
			DoNothing: true,                                                // 冲突时不做任何操作
		}).
			Table(tableName).
			Create(&p).Error
		if errC != nil {
			return errC
		}
	}
	return nil
}

// geohashToBBoxWKT 将 geohash 转为 WKT POLYGON 字符串
func geohashToBBoxWKT(gh string) string {
	// 获取 geohash 的 bbox (minLat, maxLat, minLng, maxLng)
	box := geohash.BoundingBox(gh)

	// WKT 格式：POLYGON((lng lat, lng lat, lng lat, lng lat, lng lat))
	wkt := fmt.Sprintf(
		"POLYGON((%f %f, %f %f, %f %f, %f %f, %f %f))",
		box.MinLng, box.MinLat,
		box.MinLng, box.MaxLat,
		box.MaxLng, box.MaxLat,
		box.MaxLng, box.MinLat,
		box.MinLng, box.MinLat, // 闭合第一个点
	)
	return wkt
}

// geohashCellSize 返回指定 level 的经纬度 cell 大小
func geohashCellSize(level int) (latErr, lngErr float64, err error) {
	// 手动表格（只列出常用前12级）
	latErrTable := []float64{
		22.5, 11.25, 5.625, 2.813, 1.40625, 0.703125,
		0.3515625, 0.17578125, 0.087890625, 0.0439453125,
		0.02197265625, 0.010986328125,
	}
	lngErrTable := []float64{
		22.5, 22.5, 5.625, 5.625, 1.40625, 1.40625,
		0.3515625, 0.3515625, 0.087890625, 0.087890625,
		0.02197265625, 0.02197265625,
	}

	if level <= 0 || level > len(latErrTable) {
		return 0, 0, fmt.Errorf("unsupport level:%d", level)
	}
	return latErrTable[level-1], lngErrTable[level-1], nil
}

// generateGeohashesInBounds 生成指定范围的 geohash 列表
func generateGeohashesInBounds(minLng, minLat, maxLng, maxLat float64, level int16) []string {
	var result []string

	// 计算geohash尺寸（约数）
	box := geohash.BoundingBox(geohash.EncodeWithPrecision((minLat+maxLat)/2, (minLng+maxLng)/2, uint(level)))
	latStep := box.MaxLat - box.MinLat
	lonStep := box.MaxLng - box.MinLng

	for lat := minLat; lat <= maxLat; lat += latStep {
		for lon := minLng; lon <= maxLng; lon += lonStep {
			h := geohash.EncodeWithPrecision(lat, lon, uint(level))
			result = append(result, h)
		}
	}

	return unique(result)
}

// 去重函数
func unique(input []string) []string {
	seen := make(map[string]struct{})
	var result []string
	for _, s := range input {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			result = append(result, s)
		}
	}
	return result
}

func InitTileSetTables(db *gorm.DB, boundStr string) error {
	bound, err := parseProjectBound(boundStr)
	if err != nil {
		return err
	}
	for _, table := range AllTiles {
		hasTable := db.Migrator().HasTable(table.PartitionTableName)
		if !hasTable {
			if err := db.Table(table.PartitionTableName).AutoMigrate(&TileSetPartition{}); err != nil {
				return err
			}
			db.Exec(fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT unique_%s_geohash_level UNIQUE (geohash, level);", table.PartitionTableName, table.PartitionTableName))
			db.Exec(fmt.Sprintf("CREATE INDEX idx_%s_bbox ON %s USING GIST(bbox);", table.PartitionTableName, table.PartitionTableName))
		} else {
			if err := db.Table(table.PartitionTableName).AutoMigrate(&TileSetPartition{}); err != nil {
				return err
			}
		}

		if errI := initTileSetPartitions(db, bound.MinLng, bound.MinLat,
			bound.MaxLng, bound.MaxLat, table.PartitionTableName, MinTileSetLevel); errI != nil {
			return errI
		}
	}

	return nil
}

func RemoveExpired(db *gorm.DB, expire time.Time, boundStr string) error {
	bound, err := parseProjectBound(boundStr)
	if err != nil {
		return err
	}
	for _, table := range AllTiles {
		log.Infof("begin remove expired tileset table: %+v", table)

		if err := db.Table(table.PartitionTableName).
			Where("update_time < ? AND ST_Intersects(bbox, ST_MakeEnvelope(?, ?, ?, ?, 4326))",
				append([]interface{}{expire}, bound.args()...)...).
			Delete(&TileSetPartition{}).Error; err != nil {
			return err
		}

		log.Infof("finished expire remove tileset table: %+v", table)
	}

	return nil
}

func RefineUntilStable(db *gorm.DB, boundStr string) error {
	bound, err := parseProjectBound(boundStr)
	if err != nil {
		return err
	}
	for _, table := range AllTiles {
		log.Infof("begin refine tileset table: %+v", table)

		minLevel, err := GetMinLevelPartition(db, table.PartitionTableName, bound)
		if err != nil {
			return fmt.Errorf("failed to get min level: %v", err)
		}
		var level = minLevel
		if errU := UpdatePartitionCount(db, table.GeoTableNames, table.PartitionTableName, "", level, table.Threshold, bound); errU != nil {
			return fmt.Errorf("failed to update partition count: %v", errU)
		}
		for {
			if level >= MAX_GEOHASH_LEVEL {
				break
			}

			errT := RefinePartitions(db, table, level, 10, bound)
			if errT != nil {
				return errT
			}

			level = level + 1
		}

		log.Infof("finished refine tileset table: %+v", table)
	}

	return nil
}

func RefinePartitions(db *gorm.DB, table GeoTable, level int16, workerCount int, bound projectBound) error {
	// Step 1: fetch partitions in a short transaction (or even without tx if safe)
	var partitions []TileSetPartition
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		partitions, err = GetNeedRefinePartitions(tx, table.PartitionTableName, level, bound)
		if err != nil {
			return fmt.Errorf("failed to get need refine partitions: %w", err)
		}

		if len(partitions) == 0 {
			if errC := CleanupChildren(tx, table.PartitionTableName, "", level+1, bound); errC != nil {
				return fmt.Errorf("failed to cleanup empty children: %w", errC)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(partitions) == 0 {
		return nil
	}

	// Step 2: process partitions in parallel, each with its own transaction
	g, _ := errgroup.WithContext(context.Background())
	g.SetLimit(workerCount)

	for _, p := range partitions {
		g.Go(func() error {
			return db.Transaction(func(tx *gorm.DB) error {
				if errC := CreateChildPartitions(tx, table.PartitionTableName, p, bound); errC != nil {
					return fmt.Errorf("partition %s: create child partitions failed: %w", p.Geohash, errC)
				}

				if errC := UpdatePartitionCount(tx, table.GeoTableNames, table.PartitionTableName, p.Geohash, level+1, table.Threshold, bound); errC != nil {
					return fmt.Errorf("partition %s: update partition count failed: %w", p.Geohash, errC)
				}

				if errC := CleanupChildren(tx, table.PartitionTableName, p.Geohash, level+1, bound); errC != nil {
					return fmt.Errorf("partition %s: cleanup children failed: %w", p.Geohash, errC)
				}

				return nil
			})
		})
	}

	return g.Wait()
}

func CleanupChildren(db *gorm.DB, tableName string, parentHash string, level int16, bound projectBound) error {
	return db.Table(tableName).
		Where(fmt.Sprintf("parent_hash like '%s%%' AND level >= ? AND need_refine = false",
			parentHash)+" AND ST_Intersects(bbox, ST_MakeEnvelope(?, ?, ?, ?, 4326))",
			append([]interface{}{level}, bound.args()...)...).
		Delete(nil).Error
}

// GetNeedRefinePartitions 返回当前需要细分的分片
func GetNeedRefinePartitions(db *gorm.DB, tableName string, level int16, bound projectBound) ([]TileSetPartition, error) {
	var partitions []TileSetPartition

	err := db.Table(tableName).Where(
		"need_refine = ? AND level = ? AND ST_Intersects(bbox, ST_MakeEnvelope(?, ?, ?, ?, 4326))",
		append([]interface{}{true, level}, bound.args()...)...).
		Find(&partitions).Error
	if err != nil {
		return nil, err
	}
	return partitions, nil
}

// GetMinLevelPartition 获取 tileset_partition 表当前最小 level
func GetMinLevelPartition(db *gorm.DB, tableName string, bound projectBound) (int16, error) {
	var minLevel int16
	err := db.Table(tableName).
		Select("MIN(level)").
		Where("ST_Intersects(bbox, ST_MakeEnvelope(?, ?, ?, ?, 4326))", bound.args()...).
		Scan(&minLevel).Error
	if err != nil {
		return 0, err
	}
	return minLevel, nil
}

// geohashBase32 字符表
const geohashBase32 = "0123456789bcdefghjkmnpqrstuvwxyz"

// GenerateChildGeohashes 返回父 geohash 的所有 32 个子 geohash
func GenerateChildGeohashes(parent string) []string {
	children := make([]string, 0, 32)
	for _, c := range geohashBase32 {
		child := parent + string(c)
		children = append(children, child)
	}
	return children
}

// CreateChildPartitions 生成父分片的子分片
func CreateChildPartitions(db *gorm.DB, tableName string, parent TileSetPartition, bound projectBound) error {
	parentLevel := parent.Level
	childLevel := parentLevel + 1 // 下一层级

	// 获取父分片的子 geohash
	childGeohashes := GenerateChildGeohashes(parent.Geohash) // 返回 []string

	// 重置子分片计数
	if errC := db.Raw(fmt.Sprintf(`
			UPDATE %s
			SET total_count = 0, refined = false
			WHERE parent_hash = ?;
			`, tableName), parent.ParentHash).Error; errC != nil {
		return errC
	}

	var children []TileSetPartition
	for _, gh := range childGeohashes {
		if !geohashIntersectsBound(gh, bound) {
			continue
		}
		child := TileSetPartition{
			Geohash:    gh,
			Level:      childLevel,
			BBox:       geohashToBBoxWKT(gh),
			ParentHash: &parent.Geohash,
		}
		children = append(children, child)
	}

	// 批量插入子分片，忽略已存在的冲突
	if err := db.Clauses(
		clause.OnConflict{DoNothing: true},
	).
		Table(tableName).
		Create(&children).Error; err != nil {
		return err
	}

	// 更新父分片状态
	if err := db.Table(tableName).
		Where("id = ?", parent.ID).
		Updates(map[string]interface{}{
			"refined":     true,
			"need_refine": false,
		}).Error; err != nil {
		return err
	}

	return nil
}

// 检测是否需要分割
func CheckShouldSplit(db *gorm.DB, geoTables []string, parentHash string, level int16, threshold int, bound projectBound) bool {
	// Step1: 聚合 表
	// 合并统计结果
	countMap := make(map[string]map[string]int)
	sum := 0
	for _, table := range geoTables {
		// 统计 device 表
		var counts []GeoCount
		result := db.Raw(fmt.Sprintf(`
			SELECT ST_GeoHash(geom, ?) AS geohash,
				   COUNT(*) AS count
			FROM %s
			WHERE ST_GeoHash(geom, ?) LIKE ?
			  AND ST_Intersects(ST_Transform(geom, 4326), ST_MakeEnvelope(?, ?, ?, ?, 4326))
			GROUP BY geohash
		`, table), append([]interface{}{level, level, parentHash + "%"}, bound.args()...)...).Scan(&counts)
		if result.Error != nil {
			return false
		}

		for _, d := range counts {
			if countMap[d.Geohash] == nil {
				countMap[d.Geohash] = make(map[string]int)
			}
			countMap[d.Geohash][table] = d.Count
			sum += d.Count
		}
	}

	nonEmpty := 0
	total := 0
	maxChild := 0

	// 控制总数
	if sum > MAX_TILE_MODEL_COUNT {
		return true
	}

	for _, tables := range countMap {
		count := 0
		for _, cnt := range tables {
			count = count + cnt
		}
		if count > 0 {
			nonEmpty++
			total += count
			if count > maxChild {
				maxChild = count
			}
		}
	}

	avg := 0
	if nonEmpty > 0 {
		avg = int(float64(total) / float64(nonEmpty))
	}

	// 关键判断条件
	should := nonEmpty >= 1 &&
		avg >= int(float64(threshold)*0.2) && // 平均密度不能太低
		maxChild >= int(float64(total)*0.2) // 有明显集中度
	return should
}

// 更新分片函数
func UpdatePartitionCount(db *gorm.DB, geoTables []string, tableName string, parentHash string, level int16, threshold int, bound projectBound) error {
	// Step1: 聚合 表
	// 合并统计结果
	countMap := make(map[string]map[string]int)
	for _, table := range geoTables {
		// 统计 device 表
		var counts []GeoCount
		result := db.Raw(fmt.Sprintf(`
			SELECT ST_GeoHash(geom, ?) AS geohash,
				   COUNT(*) AS count
			FROM %s
			WHERE ST_GeoHash(geom, ?) LIKE ?
			  AND (model like '%%glb' or model like '%%gltf')
			  AND ST_Intersects(ST_Transform(geom, 4326), ST_MakeEnvelope(?, ?, ?, ?, 4326))
			GROUP BY geohash
		`, table), append([]interface{}{level, level, parentHash + "%"}, bound.args()...)...).Scan(&counts)
		if result.Error != nil {
			return result.Error
		}

		for _, d := range counts {
			if countMap[d.Geohash] == nil {
				countMap[d.Geohash] = make(map[string]int)
			}
			countMap[d.Geohash][table] = d.Count
		}
	}

	if level > MinTileSetLevel {
		if !CheckShouldSplit(db, geoTables, parentHash, level, threshold, bound) {
			if errC := setTableGeohashNotRefine(db, tableName, parentHash); errC != nil {
				return errC
			}
			return nil
		}
	}

	// Step2: 遍历聚合结果，更新 tileset_partition
	for hash, tables := range countMap {
		count := 0
		for _, cnt := range tables {
			count = count + cnt
		}

		err2 := setTableGeohashRefine(db, tableName, level, hash, count)
		if err2 != nil {
			return err2
		}
	}

	return nil
}

func setTableGeohashNotRefine(db *gorm.DB, tableName string, hash string) error {
	// 更新父分片状态
	if err := db.Table(tableName).
		Where("geohash = ?", hash).
		Updates(map[string]interface{}{
			"refined":     false,
			"need_refine": false,
		}).Error; err != nil {
		return err
	}

	return nil
}

func setTableGeohashRefine(db *gorm.DB, tableName string, level int16, hash string, cnt int) error {

	parentHash := ""
	if len(hash) > 0 {
		parentHash = hash[:len(hash)-1]
	}
	bbox := geohashToBBoxWKT(hash)
	data := map[string]interface{}{
		"geohash":     hash,
		"level":       level,
		"total_count": cnt,
		"need_refine": true,
		"refined":     false,
		"parent_hash": parentHash,
		"bbox":        bbox,
		"update_time": time.Now(),
	}

	err := db.Table(tableName).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "geohash"}, {Name: "level"}}, // 冲突条件
		DoUpdates: clause.AssignmentColumns([]string{"total_count", "need_refine", "refined", "update_time"}),
	}).Create(data).Error

	if err != nil {
		return err
	}
	return nil
}
