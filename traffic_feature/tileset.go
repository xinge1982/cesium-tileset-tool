package traffic_feature

import (
	"cesium-tileset-tool/cesium"
	"cesium-tileset-tool/common"
	"cesium-tileset-tool/config"
	"cesium-tileset-tool/minioconn"
	"cesium-tileset-tool/pg"
	"cesium-tileset-tool/utils"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	os "os"
	filepath "path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"
	log "github.com/sirupsen/logrus"
	"gorm.io/datatypes"
)

// WGS84 椭球参数
const (
	earth_radius = 6378137.0         // 赤道半径（米）
	earth_radio  = 1 / 298.257223563 // 扁率
)

type TileModel struct {
	Id          string          `gorm:"column:id" json:"id"`
	Type        string          `gorm:"column:type" json:"type"`
	Lng         float64         `gorm:"column:lng" json:"lng"`
	Lat         float64         `gorm:"column:lat" json:"lat"`
	Alt         float64         `gorm:"column:alt" json:"alt"`
	ObjAngle    float64         `gorm:"column:obj_angle" json:"objAngle"`
	Pitch       float64         `gorm:"column:pitch" json:"pitch"`
	Roll        float64         `gorm:"column:roll" json:"roll"`
	ScaleX      float64         `gorm:"column:scale_x" json:"scaleX"`
	ScaleY      float64         `gorm:"column:scale_y" json:"scaleY"`
	ScaleZ      float64         `gorm:"column:scale_z" json:"scaleZ"`
	SphereR     float64         `gorm:"column:sphere_r" json:"sphereR"` //模型包围球体半径，用于判断是否在视野内
	UpdateTime  time.Time       `gorm:"column:update_time" json:"updateTime"`
	Transform   pq.Float64Array `gorm:"column:transform;type:double precision[]" json:"transform"` // PostgreSQL double precision[]
	Metadata    datatypes.JSON  `gorm:"column:metadata;type:jsonb;comment:附加数据" json:"metadata"`
	DeltaLng    float64         `gorm:"column:delta_lng" json:"deltaLng"`
	DeltaLat    float64         `gorm:"column:delta_lat" json:"deltaLat"`
	DelFlag     string          `geom:"column:del_flag" json:"delFlag"`
	ModelerType string          `geom:"column:modeler_type" json:"modelerType"`
	Url         string          `json:"url"`
}

type TileContent struct {
	Uri string `json:"uri"`
}

type TileTransform struct {
	Matrix [16]float64 `json:"matrix"`
}

type TileNode struct {
	BoundingVolume BoundingVolume `json:"boundingVolume,omitempty"`
	Content        *TileContent   `json:"content,omitempty"`
	GeometricError float64        `json:"geometricError"`
	Transform      *[16]float64   `json:"transform,omitempty"`
	Refine         string         `json:"refine,omitempty"`
	Children       []*TileNode    `json:"children,omitempty"`
	Level          int            `json:"level"`
	Geohash        string         `json:"geohash"`
}

type TileChange struct {
	Geohash string       `json:"geohash"`
	Content *TileContent `json:"content,omitempty"`
}

type BoundingVolume struct {
	Box [12]float64 `json:"box,omitempty"`
}

type Tileset struct {
	Asset struct {
		Version string `json:"version"`
	} `json:"asset"`
	GeometricError float64    `json:"geometricError"`
	Root           *TileNode  `json:"root"`
	ExtensionsUsed []string   `json:"extensionsUsed"`
	Extensions     Extensions `json:"extensions"`
}

type Properties struct {
	Project    interface{} `json:"project"`
	LayerType  interface{} `json:"layerType"`
	UpdateTime interface{} `json:"updateTime"`
}
type TilesetInfo struct {
	Properties Properties `json:"properties"`
}
type Classes struct {
	TilesetInfo TilesetInfo `json:"TilesetInfo"`
}
type Schema struct {
	Classes Classes `json:"classes"`
}
type Main struct {
	Class      string     `json:"class"`
	Properties Properties `json:"properties"`
}
type Tilesets struct {
	Main Main `json:"main"`
}
type ThreeDTILESMetadata struct {
	Schema   Schema   `json:"schema"`
	Tilesets Tilesets `json:"tilesets"`
}
type Extensions struct {
	ThreeDTILESMetadata ThreeDTILESMetadata `json:"3DTILES_metadata"`
}

type DBHandlerFunc func(c *gin.Context, dt time.Time) ([]*TileModel, error)

type TIleMetadata struct {
	ModelMatrix map[string]float64 `json:"modelMatrix"`
	Transform   map[string]float64 `json:"transform"`
}

var cacheLock sync.Mutex

func (s *FeatureMan) getCurrentTime(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code": http.StatusOK,
		"time": time.Now().Format("20060102150405"),
	})
	return
}

// 清除tileset缓存模型
func (s *FeatureMan) clearTilesetCache(code, modelType, id string) {
	cacheLock.Lock()
	defer cacheLock.Unlock()

	dir, errC := GetTilesetCacheFolder(code)
	if errC != nil {
		log.Error(errC)
		return
	}
	cacheFilePath := filepath.Join(dir, fmt.Sprintf("%s_%s.glb", modelType, id))
	if err := os.Remove(cacheFilePath); err != nil {
		log.Error(err)
		return
	}
}

func GetTilesetCacheFolder(code string) (string, error) {
	pjName, _, err := config.DecodeContext(code)
	if err != nil {
		return "", err
	}
	if len(pjName) == 0 {
		pjName = "unknown"
	}
	dir := filepath.Join(config.CacheRootFolder, "tilesets-cache", pjName)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		// 目录不存在，创建
		if errM := os.MkdirAll(dir, os.ModePerm); errM != nil {
			return "", fmt.Errorf("创建临时模型目录失败: %s", errM.Error())
		}
	}
	return dir, nil
}

// 清除tileset缓存过期模型
func ClearExpiredTilesetCache(code string) {
	cacheLock.Lock()
	defer cacheLock.Unlock()

	dir, errC := GetTilesetCacheFolder(code)
	if errC != nil {
		log.Error(errC)
		return
	}
	log.Infof("begin remove expired tileset cache file in %s", dir)

	cutoff := time.Now().Add(-24 * time.Hour)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Continue walking even if a single path fails
			return nil
		}

		// Only process regular files
		if d.IsDir() {
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}

		// Compare modification time
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil {
				log.Errorf("failed to remove file %s: %v\n", path, err)
			}
		}

		return nil
	})

	if err != nil {
		log.Errorf("error in remove expired cachel file in %s, %s", dir, err.Error())
	} else {
		log.Infof("success removed expire tileset cache file in %s", dir)
	}
}

func (s *FeatureMan) getFeaturesChangeTileSet(c *gin.Context) {
	code := c.GetHeader(common.ConfigContextCode)
	if len(code) == 0 {
		cid := c.Query("pgcontextid")
		code = cid
	}

	var modifyTime time.Time
	lastModified := c.Query("lastModified")
	if len(lastModified) > 0 {
		dt, err := time.ParseInLocation("20060102150405", lastModified, time.Local)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusBadRequest,
				"msg":  fmt.Sprintf("时间格式错误: 20060102150405"),
			})
			return
		}
		modifyTime = dt
	}

	bound := ""
	if len(code) > 0 {
		configName, _, errD := config.DecodeContext(code)
		if errD != nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("读取项目配置发生错误：%s", errD.Error()),
			})
			return
		}
		cfg, errC := config.GetNetworkConfigByName(configName)
		if errC != nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("读取项目配置发生错误：%s", errC.Error()),
			})
			return
		}
		if cfg == nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("未取到项目配置"),
			})
			return
		}
		bound = cfg.Bound
		if bound == "" {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("项目bound为空"),
			})
			return
		}
	}

	box := cesium.GetBoxFromBound(bound)

	tileset, err := s.GetDynamicTilesetJSON(c, box, s.queryFeaturesTilesetModelsFromDB, modifyTime)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": http.StatusInternalServerError,
			"msg":  fmt.Sprintf("取模型发生错误：%s", err.Error()),
		})
		return
	}

	buf, err := json.MarshalIndent(tileset, "", "  ")
	contentType := "application/json"
	// 返回文件内容
	c.Data(http.StatusOK, contentType, buf)
}

func (s *FeatureMan) getFeaturesDeleted(c *gin.Context) {
	code := c.GetHeader(common.ConfigContextCode)
	if len(code) == 0 {
		cid := c.Query("pgcontextid")
		code = cid
	}

	var modifyTime time.Time
	lastModified := c.Query("lastModified")
	if len(lastModified) > 0 {
		dt, err := time.ParseInLocation("20060102150405", lastModified, time.Local)
		if err != nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusBadRequest,
				"msg":  fmt.Sprintf("时间格式错误: 20060102150405"),
			})
			return
		}
		modifyTime = dt
	}

	// 初始化数据库连接
	db := pg.GormDB(c)

	// 构建查询条件
	// 取Sign正式表信息的
	var devModels []*TileModel
	result := db.Raw(fmt.Sprintf(`
		select id, 'hdSign' as type, update_time, del_flag
		from t_base_sign_edit
		where del_flag = '1'
		  and update_time >= '%s'
		union
		select id, 'hdPole' as type, update_time, del_flag
		from t_base_pole_edit
		where del_flag = '1'
		  and update_time >= '%s'
		union
		select id, 'hdGantry' as type, update_time, del_flag
		from t_base_gantry_edit
		where del_flag = '1'
		  and update_time >= '%s'
		union
		select id, 'hdDevice' as type, update_time, del_flag
		from t_base_device_edit
		where del_flag = '1'
		  and update_time >= '%s'`,
		modifyTime.Format(time.RFC3339), modifyTime.Format(time.RFC3339),
		modifyTime.Format(time.RFC3339), modifyTime.Format(time.RFC3339))).
		Find(&devModels)
	if result.Error != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": http.StatusInternalServerError,
			"msg":  fmt.Errorf("Failed to retrieve model info: %s", result.Error.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": devModels,
	})
}

func (s *FeatureMan) getFeaturesUpdateTime(c *gin.Context) {
	code := c.GetHeader(common.ConfigContextCode)
	if len(code) == 0 {
		cid := c.Query("pgcontextid")
		code = cid
	}

	networkFolder, errC := getNetworkPath(code)
	if errC != nil {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "get config error:" + errC.Error()})
		return
	}

	tilesetRoot := filepath.Join(networkFolder, "tilesets")

	// 获取访问路径
	reqPath := "features/tileset.json"

	// 拼接真实文件路径
	cleaned := filepath.Clean(reqPath)
	fullPath := filepath.Join(tilesetRoot, cleaned)

	// 防止越权路径，比如 ../../../etc/passwd
	if !strings.HasPrefix(fullPath, filepath.Clean(tilesetRoot)) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Invalid path"})
		return
	}

	// 检查文件是否存在
	_, err := os.Stat(fullPath)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

	buf, err := os.ReadFile(fullPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var tileset Tileset
	err = json.Unmarshal(buf, &tileset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var updateTime string
	if tileset.Extensions.ThreeDTILESMetadata.Tilesets.Main.Properties.UpdateTime != nil {
		updateTime = utils.ToString(tileset.Extensions.ThreeDTILESMetadata.Tilesets.Main.Properties.UpdateTime)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"time": updateTime,
	})
}

func (s *FeatureMan) getFeaturesChangeModel(c *gin.Context) {
	modelId := c.Param("modelId")
	code := c.GetHeader(common.ConfigContextCode)
	if len(code) == 0 {
		cid := c.Query("pgcontextid")
		code = cid
	}
	modelType := c.Query("type")
	lng := utils.ToFloat64(c.Query("lng"))
	lat := utils.ToFloat64(c.Query("lat"))
	id := utils.ToString(c.Query("fid"))
	if lng == 0 || lat == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "需要提供lng和lat坐标: " + modelType})
		return
	}
	if len(modelType) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "需要提供type参数: " + modelType})
		return
	}
	if len(id) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "需要提供id参数: " + modelType})
		return
	}

	dir, errC := GetTilesetCacheFolder(code)
	if errC != nil {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "模型存储路径获取错误: " + errC.Error()})
		return
	}

	// 查找缓存文件
	cacheLock.Lock()
	defer cacheLock.Unlock()

	cacheFilePath := filepath.Join(dir, fmt.Sprintf("%s_%s.glb", modelType, id))
	if _, err := os.Stat(cacheFilePath); err == nil {
		buf, errR := os.ReadFile(cacheFilePath)
		if errR != nil {
			log.Errorf("读取模型cache文件错误：%s", errR.Error())
		} else {
			// 返回文件内容
			c.Data(http.StatusOK, "model/gltf-binary", buf)
			return
		}
	}

	//生成cache文件
	var content []byte
	var modelName string
	var tableName string
	switch modelType {
	case "hdDevice":
		// 下载模型
		var client *minioconn.MinioConn
		var bucketName string
		var prefix string
		var errD error

		client, bucketName, prefix, errD = getMinioOutputDeviceGltfPath(code)
		tableName = DeviceTileTableName

		if errD != nil {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "minio连接错误: " + errD.Error()})
			return
		}

		objectName, errJ := url.JoinPath(prefix, modelId)
		if errJ != nil {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "地址错误: " + errJ.Error()})
			return
		}
		modelName = objectName

		// 从 MinIO 获取对象
		object, err := client.GetObject(context.Background(), bucketName, objectName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": fmt.Sprintf("Failed to get object: %v", err)})
			return
		}
		defer func() {
			_ = object.Close()
		}()

		// 读取对象内容
		content, err = io.ReadAll(object)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": fmt.Sprintf("Failed to read object content: %v", err)})
			return
		}
	case "hdSign":
		// 下载模型
		var client *minioconn.MinioConn
		var bucketName string
		var prefix string
		var errD error

		client, bucketName, prefix, errD = getMinioOutputSignGltfClient(code)
		tableName = SignTileTableName

		if errD != nil {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "minio连接错误: " + errD.Error()})
			return
		}

		objectName, errJ := url.JoinPath(prefix, modelId)
		if errJ != nil {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "地址错误: " + errJ.Error()})
			return
		}
		modelName = objectName

		// 从 MinIO 获取对象
		object, err := client.GetObject(context.Background(), bucketName, objectName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": fmt.Sprintf("Failed to get object: %v", err)})
			return
		}
		defer func() {
			_ = object.Close()
		}()

		// 读取对象内容
		content, err = io.ReadAll(object)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": fmt.Sprintf("Failed to read object content: %v", err)})
			return
		}
	case "hdPole":
		// 下载模型
		var client *minioconn.MinioConn
		var bucketName string
		var prefix string
		var errD error

		client, bucketName, prefix, errD = getMinioOutputPoleGltfClient(code)
		tableName = PoleTileTableName

		if errD != nil {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "minio连接错误: " + errD.Error()})
			return
		}

		objectName, errJ := url.JoinPath(prefix, modelId)
		if errJ != nil {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "地址错误: " + errJ.Error()})
			return
		}
		modelName = objectName

		// 从 MinIO 获取对象
		object, err := client.GetObject(context.Background(), bucketName, objectName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": fmt.Sprintf("Failed to get object: %v", err)})
			return
		}
		defer func() {
			_ = object.Close()
		}()

		// 读取对象内容
		content, err = io.ReadAll(object)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": fmt.Sprintf("Failed to read object content: %v", err)})
			return
		}
	case "hdGantry":
		// 下载模型
		var client *minioconn.MinioConn
		var bucketName string
		var prefix string
		var errD error

		client, bucketName, prefix, errD = minioconn.GetMinioBaseGltfClient(code)
		tableName = GantryTileTableName

		if errD != nil {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "minio连接错误: " + errD.Error()})
			return
		}

		objectName, errJ := url.JoinPath(prefix, modelId)
		if errJ != nil {
			c.JSON(http.StatusBadRequest, gin.H{"msg": "地址错误: " + errJ.Error()})
			return
		}
		modelName = objectName

		// 从 MinIO 获取对象
		object, err := client.GetObject(context.Background(), bucketName, objectName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": fmt.Sprintf("Failed to get object: %v", err)})
			return
		}
		defer func() {
			_ = object.Close()
		}()

		// 读取对象内容
		content, err = io.ReadAll(object)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": fmt.Sprintf("Failed to read object content: %v", err)})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"msg": "不支持的类型: " + modelType})
		return
	}

	transform := getDeviceTransform(0, 1.0, 1.0, 1.0)
	var models = make(map[string][]*GeoHashModel)
	device := GeoHashModel{
		TableName: tableName,
		Model:     modelName,
		Id:        id,
		Name:      "",
		Type:      modelType,
		Lng:       lng,
		Lat:       lat,
		Alt:       0.0,
		ObjAngle:  0.0,
		Transform: transform,
		Gltf: &GltfModel{
			Name:        modelName,
			Content:     content,
			ContentType: detectGltfFormat(content),
		},
	}
	models[modelName] = append(models[modelName], &device)

	var center = []float64{lng, lat}
	var region = [4]float64{lng - 0.0005, lat - 0.0005, lng + 0.0005, lat + 0.0005}

	built, errB := GeneratePartitionModel(models, center, region)
	if errB != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"msg": fmt.Sprintf("生成模型错误: %s", errB.Error())})
		return
	}

	if errW := os.WriteFile(cacheFilePath, built.Content, 0644); errW != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"msg": fmt.Sprintf("写入模型cache文件错误: %s", errW.Error())})
		return
	}
	contentType := detectGltfFormat(built.Content)
	// 返回文件内容
	c.Data(http.StatusOK, contentType, built.Content)
}

func (s *FeatureMan) queryFeaturesTilesetModelsFromDB(c *gin.Context, modifyTime time.Time) ([]*TileModel, error) {
	var models []*TileModel
	devs, err := s.queryDeviceTilesetModelsFromDB(c, modifyTime)
	if err != nil {
		return nil, err
	}
	models = append(models, devs...)

	poles, err := s.queryHDPoleTilesetModelsFromDB(c, modifyTime)
	if err != nil {
		return nil, err
	}
	models = append(models, poles...)

	gantrys, err := s.queryHDGantryTilesetModelsFromDB(c, modifyTime)
	if err != nil {
		return nil, err
	}
	models = append(models, gantrys...)

	signs, err := s.querySignTilesetModelsFromDB(c, modifyTime)
	if err != nil {
		return nil, err
	}
	models = append(models, signs...)
	return models, nil
}

func (s *FeatureMan) getHDPoleTilesetJson(c *gin.Context) {
	code := c.GetHeader(common.ConfigContextCode)
	if len(code) == 0 {
		cid := c.Query("pgcontextid")
		code = cid
	}

	bound := ""
	if len(code) > 0 {
		configName, _, errD := config.DecodeContext(code)
		if errD != nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("读取项目配置发生错误：%s", errD.Error()),
			})
			return
		}
		cfg, errC := config.GetNetworkConfigByName(configName)
		if errC != nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("读取项目配置发生错误：%s", errC.Error()),
			})
			return
		}
		if cfg == nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("未取到项目配置"),
			})
			return
		}
		bound = cfg.Bound
		if bound == "" {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("项目bound为空"),
			})
			return
		}
	}

	box := cesium.GetBoxFromBound(bound)

	tileset, err := s.GetDynamicTilesetJSON(c, box, s.queryHDPoleTilesetModelsFromDB, time.Time{})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": http.StatusInternalServerError,
			"msg":  fmt.Sprintf("取模型发生错误：%s", err.Error()),
		})
		return
	}

	buf, err := json.MarshalIndent(tileset, "", "  ")
	contentType := "application/json"
	// 返回文件内容
	c.Data(http.StatusOK, contentType, buf)
}

func (s *FeatureMan) getSignTilesetJson(c *gin.Context) {
	code := c.GetHeader(common.ConfigContextCode)
	if len(code) == 0 {
		cid := c.Query("pgcontextid")
		code = cid
	}

	bound := ""
	if len(code) > 0 {
		configName, _, errD := config.DecodeContext(code)
		if errD != nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("读取项目配置发生错误：%s", errD.Error()),
			})
			return
		}
		cfg, errC := config.GetNetworkConfigByName(configName)
		if errC != nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("读取项目配置发生错误：%s", errC.Error()),
			})
			return
		}
		if cfg == nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("未取到项目配置"),
			})
			return
		}
		bound = cfg.Bound
		if bound == "" {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("项目bound为空"),
			})
			return
		}
	}

	box := cesium.GetBoxFromBound(bound)

	tileset, err := s.GetDynamicTilesetJSON(c, box, s.querySignTilesetModelsFromDB, time.Time{})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": http.StatusInternalServerError,
			"msg":  fmt.Sprintf("取模型发生错误：%s", err.Error()),
		})
		return
	}

	buf, err := json.MarshalIndent(tileset, "", "  ")
	contentType := "application/json"
	// 返回文件内容
	c.Data(http.StatusOK, contentType, buf)
}

func (s *FeatureMan) getDeviceTilesetJson(c *gin.Context) {
	code := c.GetHeader(common.ConfigContextCode)
	if len(code) == 0 {
		cid := c.Query("pgcontextid")
		code = cid
	}

	bound := ""
	if len(code) > 0 {
		configName, _, errD := config.DecodeContext(code)
		if errD != nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("读取项目配置发生错误：%s", errD.Error()),
			})
			return
		}
		cfg, errC := config.GetNetworkConfigByName(configName)
		if errC != nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("读取项目配置发生错误：%s", errC.Error()),
			})
			return
		}
		if cfg == nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("未取到项目配置"),
			})
			return
		}
		bound = cfg.Bound
		if bound == "" {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("项目bound为空"),
			})
			return
		}
	}

	box := cesium.GetBoxFromBound(bound)

	tileset, err := s.GetDynamicTilesetJSON(c, box, s.queryDeviceTilesetModelsFromDB, time.Time{})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": http.StatusInternalServerError,
			"msg":  fmt.Sprintf("取模型发生错误：%s", err.Error()),
		})
		return
	}

	buf, err := json.MarshalIndent(tileset, "", "  ")
	contentType := "application/json"
	// 返回文件内容
	c.Data(http.StatusOK, contentType, buf)
}

func (s *FeatureMan) getHDGantryTilesetJson(c *gin.Context) {
	code := c.GetHeader(common.ConfigContextCode)
	if len(code) == 0 {
		cid := c.Query("pgcontextid")
		code = cid
	}

	bound := ""
	if len(code) > 0 {
		configName, _, errD := config.DecodeContext(code)
		if errD != nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("读取项目配置发生错误：%s", errD.Error()),
			})
			return
		}
		cfg, errC := config.GetNetworkConfigByName(configName)
		if errC != nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("读取项目配置发生错误：%s", errC.Error()),
			})
			return
		}
		if cfg == nil {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("未取到项目配置"),
			})
			return
		}
		bound = cfg.Bound
		if bound == "" {
			c.JSON(http.StatusOK, gin.H{
				"code": http.StatusInternalServerError,
				"msg":  fmt.Sprintf("项目bound为空"),
			})
			return
		}
	}

	box := cesium.GetBoxFromBound(bound)

	tileset, err := s.GetDynamicTilesetJSON(c, box, s.queryHDGantryTilesetModelsFromDB, time.Time{})
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": http.StatusInternalServerError,
			"msg":  fmt.Sprintf("取模型发生错误：%s", err.Error()),
		})
		return
	}

	buf, err := json.MarshalIndent(tileset, "", "  ")
	contentType := "application/json"
	// 返回文件内容
	c.Data(http.StatusOK, contentType, buf)
}

func (s *FeatureMan) GetDynamicTilesetJSON(c *gin.Context, box [12]float64, dbFunc DBHandlerFunc, modifyTime time.Time) (*Tileset, error) {
	code := c.GetHeader(common.ConfigContextCode)
	if len(code) == 0 {
		cid := c.Query("pgcontextid")
		code = cid
	}
	codeHash := ShortHashFNV(code)
	configName, _, _ := config.DecodeContext(code)

	// 从数据库取出所有模型
	models, err := dbFunc(c, modifyTime)
	if err != nil {
		return nil, err
	}

	root := &TileNode{
		BoundingVolume: BoundingVolume{
			Box: box,
		},
		GeometricError: 500,
		Refine:         "ADD",
		Transform:      nil,
	}

	for _, m := range models {
		if m.Transform != nil {
			if len(m.Transform) == 10 {
				m.ScaleX = m.Transform[7]
				m.ScaleY = m.Transform[8]
				m.ScaleZ = m.Transform[9]
			}
		}
		transform := cesium.GenerateTransformMatrixUPScale(m.Lng, m.Lat, m.Alt, m.ObjAngle, m.ScaleX, m.ScaleY, m.ScaleZ)
		if len(m.Metadata) > 0 {
			var meta = TIleMetadata{}
			if errM := json.Unmarshal(m.Metadata, &meta); errM != nil {
				log.Error(errM)
			}
			if meta.Transform != nil && len(meta.Transform) == 16 {
				trans := meta.Transform
				transform = [16]float64{
					trans["0"],
					trans["1"],
					trans["2"],
					trans["3"],
					trans["4"],
					trans["5"],
					trans["6"],
					trans["7"],
					trans["8"],
					trans["9"],
					trans["10"],
					trans["11"],
					trans["12"],
					trans["13"],
					trans["14"],
					trans["15"],
				}
			}
		}

		tile := &TileNode{
			BoundingVolume: BoundingVolume{
				Box: cesium.GetBoundingVolumeBoxDeg(m.Lng, m.Lat, m.Alt, 20), // 近似
			},
			//BoundingVolume: nil,
			Content: &TileContent{
				//Uri: m.Url, // 加上时间戳
				Uri: fmt.Sprintf("%s?v=%s&code=%s&fid=%s&type=%s&lng=%f&lat=%f&alt=%f&modelerType=%s",
					m.Url, m.UpdateTime.Format("20060102150405"), codeHash, m.Id, m.Type, m.Lng, m.Lat, m.Alt, m.ModelerType),
			},
			GeometricError: 500,
			Transform:      &transform,
			Refine:         "ADD",
		}
		root.Children = append(root.Children, tile)
	}
	refreshTileBound(root)

	ts := &Tileset{
		GeometricError: 1000,
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
						UpdateTime: modifyTime.Format("20060102150405"),
					},
				},
			},
		}},
	}
	ts.Asset.Version = "1.1"
	return ts, nil
}

func (s *FeatureMan) queryHDPoleTilesetModelsFromDB(c *gin.Context, modifyTime time.Time) ([]*TileModel, error) {
	var models []*TileModel

	// 初始化数据库连接
	db := pg.GormDB(c)
	// 获取表名
	tableName := pg.GetConfigTableName(c)
	if tableName == "" {
		tableName = "hdpole"
	}
	dbQuery := db.Table(tableName + " as pole")

	orderField := c.DefaultQuery("order", "id")          // 默认为 "id" 字段
	orderDirection := c.DefaultQuery("direction", "asc") // 默认为 "asc" 排序
	// 构建查询条件
	conditions := map[string]interface{}{}
	dbQuery = buildQuery[PoleEdit](orderField, orderDirection, dbQuery, conditions)

	var poleModels []*TileModel
	poleIdColumn := "pole_id"
	hasColumnPoleId := db.Migrator().HasColumn(tableName, "pole_id")
	if !hasColumnPoleId {
		poleIdColumn = "id"
	}
	// 拼接select
	sql := fmt.Sprintf(`pole.%s as id, 'hdPole' as type, 0.0 as obj_angle,  
            1.0 as scale_x, 1.0 as scale_y, 1.0 as scale_z,
			edit.update_time, pole.%s || '.glb' as url,
			ST_Y(ST_Transform(ST_Translate(ST_Transform(pole.geom, 3857), 0, 5), 4326)) - ST_Y(pole.geom) AS delta_lat,
			ST_X(ST_Transform(ST_Translate(ST_Transform(pole.geom, 3857), 5, 0), 4326)) - ST_X(pole.geom) AS delta_lng,
            ST_X(pole.geom) AS lng,ST_Y(pole.geom) AS lat,ST_Z(pole.geom) AS alt`, poleIdColumn, poleIdColumn)
	result := dbQuery.
		Select(sql).
		Joins(fmt.Sprintf("left join %s edit on edit.id::text = pole.%s::text and edit.pole_table = '%s'",
			PoleEdit{}.TableName(), poleIdColumn, tableName)).
		Where("ST_X(pole.geom) != 0 and (edit.del_flag != '1' OR edit.del_flag IS NULL) and char_length(pole.pole_id::text) > 0 and (pole.model like '%.glb' or pole.model like '%.gltf')").
		Where("edit.update_time >= ?", modifyTime.Format(time.RFC3339)).
		Find(&poleModels)
	if result.Error != nil {
		return nil, fmt.Errorf("Failed to retrieve hdPoles: %s", result.Error.Error())
	}

	models = append(models, poleModels...)

	return models, nil
}

func (s *FeatureMan) querySignTilesetModelsFromDB(c *gin.Context, modifyTime time.Time) ([]*TileModel, error) {
	var models []*TileModel

	// 初始化数据库连接
	db := pg.GormDB(c)

	// 获取表名
	var signTableName, signIdColumn string
	signTableName = "hdtraffic_sign"
	signIdColumn = "id"

	dbQuery := db.Table(signTableName + " as sign")

	orderField := c.DefaultQuery("order", "id")          // 默认为 "id" 字段
	orderDirection := c.DefaultQuery("direction", "asc") // 默认为 "asc" 排序
	// 构建查询条件
	conditions := map[string]interface{}{}
	dbQuery = buildQuery[SignEdit](orderField, orderDirection, dbQuery, conditions)

	// 取Sign正式表信息的
	var signModels []*TileModel
	result := db.Table(signTableName+" as sign").
		Select(fmt.Sprintf(`
				sign.%s as id, 'hdSign' as type, sign.obj_angle as obj_angle,  
				1.0 as scale_x, 1.0 as scale_y, 1.0 as scale_z,
				edit.update_time, sign.model as url,
				ST_Y(ST_Transform(ST_Translate(ST_Transform(sign.geom, 3857), 0, 5), 4326)) - ST_Y(sign.geom) AS delta_lat,
				ST_X(ST_Transform(ST_Translate(ST_Transform(sign.geom, 3857), 5, 0), 4326)) - ST_X(sign.geom) AS delta_lng,
            	ST_X(sign.geom) AS lng,ST_Y(sign.geom) AS lat,ST_Z(sign.geom) AS alt`, signIdColumn)).
		Joins(fmt.Sprintf("left join %s edit on sign.%s = edit.id ", SignEdit{}.TableName(), signIdColumn)).
		Where("ST_X(sign.geom) != 0 and (edit.del_flag != '1' OR edit.del_flag IS NULL) and (sign.model like '%.glb' or sign.model like '%.gltf')").
		Where("edit.update_time >= ?", modifyTime.Format(time.RFC3339)).
		Find(&signModels)
	if result.Error != nil {
		return nil, fmt.Errorf("Failed to retrieve sign info: %s", result.Error.Error())
	}

	models = append(models, signModels...)

	return models, nil
}

func (s *FeatureMan) queryHDGantryTilesetModelsFromDB(c *gin.Context, modifyTime time.Time) ([]*TileModel, error) {
	var models []*TileModel

	// 初始化数据库连接
	db := pg.GormDB(c)

	// 获取表名
	tableName := pg.GetConfigTableName(c)
	if tableName == "" {
		tableName = "hdgantry"
	}
	dbQuery := db.Table(tableName + " as gantry")
	gantryIdColumn := "id"

	orderField := c.DefaultQuery("order", "id")          // 默认为 "id" 字段
	orderDirection := c.DefaultQuery("direction", "asc") // 默认为 "asc" 排序
	// 构建查询条件
	conditions := map[string]interface{}{}
	dbQuery = buildQuery[GantryEdit](orderField, orderDirection, dbQuery, conditions)

	// 取Sign正式表信息的
	var gantryModels []*TileModel
	result := db.Table(tableName+" as gantry").
		Select(fmt.Sprintf(`
				gantry.%s as id, 'hdGantry' as type, gantry.obj_angle as obj_angle,  
				1.0 as scale_x, 1.0 as scale_y, 1.0 as scale_z, gantry.transform, 
				edit.update_time, gantry.model as url, edit.model_data->>'modelerType' as modeler_type, 
				ST_Y(ST_Transform(ST_Translate(ST_Transform(gantry.geom, 3857), 0, 50), 4326)) - ST_Y(gantry.geom) AS delta_lat,
				ST_X(ST_Transform(ST_Translate(ST_Transform(gantry.geom, 3857), 50, 0), 4326)) - ST_X(gantry.geom) AS delta_lng,
            	ST_X(gantry.geom) AS lng,ST_Y(gantry.geom) AS lat,ST_Z(gantry.geom) AS alt`, gantryIdColumn)).
		Joins(fmt.Sprintf("left join %s edit on gantry.%s = edit.id ", GantryEdit{}.TableName(), gantryIdColumn)).
		Where("ST_X(gantry.geom) != 0 and (edit.del_flag != '1' OR edit.del_flag IS NULL) and (gantry.model like '%.glb' or gantry.model like '%.gltf')").
		Where("edit.update_time >= ?", modifyTime.Format(time.RFC3339)).
		Find(&gantryModels)
	if result.Error != nil {
		return nil, fmt.Errorf("Failed to retrieve gantry info: %s", result.Error.Error())
	}

	models = append(models, gantryModels...)

	return models, nil
}

func (s *FeatureMan) queryDeviceTilesetModelsFromDB(c *gin.Context, modifyTime time.Time) ([]*TileModel, error) {
	var models []*TileModel

	// 初始化数据库连接
	db := pg.GormDB(c)

	// 获取表名
	var devTableName, devIdColumn string
	devTableName = "hdtraffic_ene"
	devIdColumn = "id"
	dbQuery := db.Table(devTableName + " as dev")

	orderField := c.DefaultQuery("order", "id")          // 默认为 "id" 字段
	orderDirection := c.DefaultQuery("direction", "asc") // 默认为 "asc" 排序
	// 构建查询条件
	conditions := map[string]interface{}{}
	dbQuery = buildQuery[DeviceEdit](orderField, orderDirection, dbQuery, conditions)

	// 取Sign正式表信息的
	var devModels []*TileModel
	result := db.Table(devTableName+" as dev").
		Select(fmt.Sprintf(`
				dev.%s as id, 'hdDevice' as type, dev.obj_angle as obj_angle,  
				1.0 as scale_x, 1.0 as scale_y, 1.0 as scale_z, dev.transform, edit.metadata, 
				edit.update_time, dev.model as url, 
				ST_Y(ST_Transform(ST_Translate(ST_Transform(dev.geom, 3857), 0, 2), 4326)) - ST_Y(dev.geom) AS delta_lat,
				ST_X(ST_Transform(ST_Translate(ST_Transform(dev.geom, 3857), 2, 0), 4326)) - ST_X(dev.geom) AS delta_lng,
            	ST_X(dev.geom) AS lng,ST_Y(dev.geom) AS lat,ST_Z(dev.geom) AS alt`, devIdColumn)).
		Joins(fmt.Sprintf("left join %s edit on dev.%s = edit.id ", DeviceEdit{}.TableName(), devIdColumn)).
		Where("ST_X(dev.geom) != 0 and (edit.del_flag != '1' OR edit.del_flag IS NULL) and (dev.model like '%.glb' or dev.model like '%.gltf')").
		Where("edit.update_time >= ?", modifyTime.Format(time.RFC3339)).
		Find(&devModels)
	if result.Error != nil {
		return nil, fmt.Errorf("Failed to retrieve dev info: %s", result.Error.Error())
	}

	models = append(models, devModels...)

	return models, nil
}

// 返回 FNV-1a 64-bit 的 hex（可截断到更短）
func ShortHashFNV(src string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(src))
	sum := h.Sum64()
	// hex 会是 16 字节长度（8 字符表示 32 bits 可用）
	return fmt.Sprintf("%x", sum) // full 16 hex chars (64-bit)
}

func GetRegion(lng, lat, deltaLng, deltaLat float64, low, high float64) [6]float64 {
	var region []float64
	region = append(region, cesium.CesiumMathToRadians(utils.ToFloat64(lng-deltaLng)))
	region = append(region, cesium.CesiumMathToRadians(utils.ToFloat64(lat-deltaLat)))
	region = append(region, cesium.CesiumMathToRadians(utils.ToFloat64(lng+deltaLng)))
	region = append(region, cesium.CesiumMathToRadians(utils.ToFloat64(lat+deltaLat)))
	region = append(region, low)
	region = append(region, high)

	slice, err := cesium.SliceToArray6(region)
	if err != nil {
		log.Error(err)
		return [6]float64{}
	}
	return slice
}

func getDeviceTransform(bearing float64, scaleX, scaleY, scaleZ float64) []float64 {
	//计算transform
	// Example: heading, pitch, roll in degrees
	headingDegrees := utils.ToFixed(math.Mod(bearing+360.0, 360.0), 6) // Z-axis
	pitchDegrees := 0.0                                                // X-axis
	rollDegrees := 0.0                                                 // Y-axis
	// Generate quaternion from HPR angles
	quaternion := cesium.FromHpr(headingDegrees, float64(pitchDegrees), float64(rollDegrees))
	return []float64{0.0, 0.0, 0.0, quaternion.X, quaternion.Y, quaternion.Z, -quaternion.W, scaleX, scaleY, scaleZ}
}
