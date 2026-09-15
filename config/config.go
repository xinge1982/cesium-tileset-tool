package config

import (
	"cesium-tileset-tool/env"
	"cesium-tileset-tool/utils"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	mapset "github.com/deckarep/golang-set"
	"github.com/gin-gonic/gin"
	"github.com/paulmach/orb"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

type ServiceRole string

var NameRegex = regexp.MustCompile(`[^a-z0-9-]+`)

const (
	ServiceRoleRsu             ServiceRole = "rsu"          //路侧单元服务
	ServiceRoleTrackWebService ServiceRole = "trackweb"     //中心轨迹回放服务，负责在数据中心提供轨迹回放
	ServiceRoleTrackCompute    ServiceRole = "trackcompute" //中心轨迹数据服务，负责在数据中心采集轨迹数据
	ServiceRoleTrackSave       ServiceRole = "tracksave"    //存储轨迹文件
	ServiceRoleTrackFrameWeb   ServiceRole = "trackframe"   //轨迹帧播放
	ServiceRoleAll             ServiceRole = "all"          //采集所有路口数据，提供所有服务
)

const (
	MaxTrackRealPlayCacheSeconds         = 30                                                           //最大缓存秒数
	DefaultTrackFramerate        float32 = 12.5                                                         //默认帧率
	DefaultHistrackInterval              = time.Millisecond * time.Duration(1000/DefaultTrackFramerate) //回放默认帧间隔
	DataFileInterval                     = time.Duration(30) * time.Minute
)

// debug用cross编号对应
var debugCrossIdMap = make(map[string]mapset.Set)
var debugCrossIdMapLocker = sync.Mutex{}

// 合并后路口区域
var combineBounds = make(map[string]orb.Bound)

// 单独显示轨迹线的车辆编号
var trackPathShowTrackId = make(map[int64]bool)
var trackPathShowPlate = make(map[string]bool)

// 转换经纬度到火星坐标系
var TransTrackPointToGcj = false

var UploadMissingMinioObjects = false

var DefaultFontPath = "networks/default/fonts/"

var locker = sync.Mutex{}

const ConfigRootPath = "networks"

var CacheRootFolder = "./cache"

type CrossHttp struct {
	HttpPort           string //http服务端口
	HttpsPort          string //https服务端口
	LocalHost          string //本地网络互通IP
	LocalHttpPort      string //本地网络互通端口
	AdvertiseHost      string //对外地址
	AdvertiseHttpPort  string //对外http服务端口
	AdvertiseHttpsPort string //对外https服务端口
}

type ConfigFileData struct {
	ID            string                   `json:"id"`
	Name          string                   `json:"name"`
	Center        string                   `json:"center"`
	Desc          string                   `json:"desc"`
	NetworkFolder string                   `json:"networkFolder"`
	Bound         interface{}              `json:"bound"`
	Tilesets      map[string]TilesetConfig `json:"tilesets"`
	ImageTile     interface{}              `json:"imageTile"`
}

type PhotoListQueryOption struct {
	LeftSideTable  string
	RightSideTable string
	ImageSortField string
	ImageUrlField  string
	ImageUrlPath   string
}

type TilesetLODLevelConfig struct {
	ModelFolder    string  `yaml:"modelFolder" json:"modelFolder"`
	GeometricError float64 `yaml:"geometricError" json:"geometricError"`
	LocalFirst     bool    `yaml:"localFirst" json:"localFirst"`
	MinioFallback  bool    `yaml:"minioFallback" json:"minioFallback"`
}

type TilesetSourceLODConfig struct {
	LOD2ModelMode     string   `yaml:"lod2ModelMode" json:"lod2ModelMode"`
	LOD2ModelPrefixes []string `yaml:"lod2ModelPrefixes" json:"lod2ModelPrefixes"`
	LOD2UnmatchedMode string   `yaml:"lod2UnmatchedMode" json:"lod2UnmatchedMode"`
}

type TilesetSourceConfig struct {
	ID         string                 `yaml:"id" json:"id"`
	Name       string                 `yaml:"name" json:"name"`
	Match      MatchConfig            `yaml:"match" json:"match"`
	KeyMapping *KeyMappingConfig      `yaml:"keyMapping,omitempty" json:"keyMapping,omitempty"`
	Table      TableConfig            `yaml:"table" json:"table"`
	LOD        TilesetSourceLODConfig `yaml:"lod" json:"lod"`
	Fields     []FieldConfig          `yaml:"fields" json:"fields"`
}

type TilesetLODConfig struct {
	Enabled bool                  `yaml:"enabled" json:"enabled"`
	LOD0    TilesetLODLevelConfig `yaml:"lod0" json:"lod0"`
	LOD1    TilesetLODLevelConfig `yaml:"lod1" json:"lod1"`
	LOD2    TilesetLODLevelConfig `yaml:"lod2" json:"lod2"`
	LOD3    TilesetLODLevelConfig `yaml:"lod3" json:"lod3"`
}

type DefaultView struct {
	Lon     float64 `yaml:"lng" json:"lng"`
	Lat     float64 `yaml:"lat" json:"lat"`
	Height  float64 `yaml:"height" json:"height"`
	Heading float64 `yaml:"heading" json:"heading"`
	Pitch   float64 `yaml:"pitch" json:"pitch"`
	Roll    float64 `yaml:"roll" json:"roll"`
}

type Config struct {
	ManHttpsPort    string                   //服务端口
	Name            string                   //配置名称
	Desc            string                   //配置描述
	AppVersion      string                   //版本信息
	BuildDate       string                   //版本信息
	GitCommit       string                   //版本信息
	NetworkFolder   string                   //路网文件地址
	LocalTileFolder string                   //本地三维切片目录
	WWWFolder       string                   //html目录
	TmpDataFolder   string                   //数据文件目录
	DataProjection  string                   //轨迹数据的投影坐标系
	DataPointType   string                   //轨迹点wgs/gcj
	DeviceId        string                   //路口编号
	CrossIds        map[string]string        //路口编号
	Crossserver     CrossHttp                //服务配置
	Bound           string                   //项目范围
	DefaultView     *DefaultView             `yaml:"DefaultView" json:"defaultView,omitempty"` //默认视角
	Tilesets        map[string]TilesetConfig //三维tileset
	ImageTile       interface{}              //影像瓦片地址
	CardApis        []struct {
		Url string
	}
	WuXiSuoApis []struct {
		Url string
	}
	Mysql struct {
		Host      string
		Port      string
		User      string
		Pass      string
		SysDbName string
	}
	Redis struct {
		Host string
		Port string
		User string
		Pass string
	}
	Postgres struct {
		Host   string
		Port   string
		User   string
		Pass   string
		Schema string
		DbName string
	}
	Kafka struct {
		Host    string
		GroupId string
	}
	Minio struct {
		Host         string
		Port         string
		AccessKey    string
		SecretKey    string
		BucketPrefix string
	}
	MinioOutput struct {
		Host             string
		Port             string
		AccessKey        string
		SecretKey        string
		Bucket           string
		PicBucket        string
		BaseModelBucket  string
		DeviceGltfBucket string
		PoleGltfBucket   string
		GantryGltfBucket string
		SignGltfBucket   string
	}
	RabbitMQ struct {
		Host          string //amqp地址
		Exchange      string //轨迹数据exchange
		StatExchange  string //统计数据exchange
		DataQueueName string //数据队列
	}
	Consul struct { //注册中心
		Host string //consul服务器地址
	}
	ServiceRole   ServiceRole //服务角色
	ServiceOption struct {    //服务功能选项
		CrossRealStat       bool //是否计算路口实时指标
		CrossTrackWebSocket bool //是否提供轨迹websocket播放服务
		CrossTrackSend      bool //是否发送路口轨迹到MQ
		CrossMetricStat     bool //是否计算路口历史指标
		CrossMetricStatSend bool //是否发送路口历史指标到MQ
		CrossRealDataOnReq  bool //是否按请求消费实时数据
		CrossDetectorStat   bool //是否启用虚拟检测器数据
	}
	PhotoFileList             PhotoListQueryOption
	Debug                     bool              //是否调试输出
	HttpDebug                 bool              //是否调试输出
	DebugConsul               bool              //调试consul
	SignalTransDebug          bool              //检测器输出
	LogFileName               string            //外部日志文件
	UsingSSL                  bool              //http是否使用ssl
	SSLSecCrt                 string            //ssl crt文件
	SSLSecKey                 string            //ssl key
	CertFilePath              string            //证书路径
	TrackFileZip              bool              //是否压缩保存轨迹数据
	TrackSaveFile             bool              //是否保存轨迹数据
	TrackPathShow             bool              //是否显示轨迹线
	WebSocketUserLimit        int               //websocket轨迹服务的最大连接数
	MetricStatIntervalSeconds int               //最小统计周期
	Md5HashVehiclePlate       bool              //是否将车牌号做md5哈希处理
	HideVehiclePlate          bool              //是否将车牌号隐藏尾数
	ShowBlindZone             bool              //是否显示车辆盲区
	AllowOrigins              []string          //Allow origin domain names
	HideNomotorExclude        bool              //隐藏不在路口区域的非机轨迹
	HideMotorExclude          bool              //隐藏不在路口区域的机动车轨迹
	WebsocketPlaneCapReplace  bool              //替换车牌首字
	CrossIdTrackDebug         map[string]string //调试路口匹配数据路口名称
	MatchCrossByTrack         bool              //是否按轨迹点匹配路口
	DisableTrackCombine       bool              //禁用轨迹拼接
	TrackCombineSimilar       bool              //启用车牌近似识别
	RadarSpeedOk              bool              //雷达速度可用
	RadarObjectDimensionOk    bool              //雷达目标尺寸可用
	TrackPlayOffsetSeconds    float32           //发送轨迹与当前最新轨迹的最大时差
	ExcludeMinimap            bool              //是否独占地图服务
	TrackViewProtoTrackFolder string            //proto文件路径
	EnableCaptcha             bool              //启用登录验证码
	ProxyUseRabbitmq          bool              //使用rabbitmq做控制请求转发
	TransKafkaGroupIDSuffix   string            //kafka消费的groupId后缀
	PhotoFilePath             string            //照片地址
	PhotoFileWorker           *FileWorkerQueue  //照片远程工作流
	ShareImageUrl             string            //共享照片服务地址
	PublishToList             bool              //是否发布到项目列表中
	SignImageOutputPath       string            //标志牌图片输出地址
	SignImageOutputGetUrl     string            //标志牌通知更新接口地址
	SignModelOutputPath       string            //标志牌模型输出地址
	SignModelPrefixString     string            //应用标志牌时的model名称前缀
	EnableAutoTilesetRebuild  bool              //是否定期编译tileset
}

type TilesetConfig struct {
	Name           string                `yaml:"name" json:"name"`
	URL            string                `yaml:"url" json:"url"`
	Type           string                `yaml:"type" json:"type"`
	Enabled        bool                  `yaml:"enabled" json:"enabled"`
	Maintainable   bool                  `yaml:"maintainable" json:"maintainable"`
	Partition      Partition             `yaml:"partition" json:"partition"`
	ZClip          ZClipConfig           `yaml:"zclip" json:"zclip"`
	Order          int                   `yaml:"order" json:"order"`
	Feature        FeatureConfig         `yaml:"feature" json:"feature"`
	Sources        []TilesetSourceConfig `yaml:"sources" json:"sources"`
	Options        [][]string            `yaml:"options" json:"options"`
	BoundingVolume *BoundingVolumeConfig `yaml:"boundingVolume" json:"boundingVolume"`
	LOD            TilesetLODConfig      `yaml:"lod" json:"lod"` //该Tileset独立的多级模型及切片参数
}

type BoundingVolumeConfig struct {
	Scale                 float32 `yaml:"scale" json:"scale"`
	MinimumRadius         float32 `yaml:"minimumRadius" json:"minimumRadius"`
	DisableBoundingVolume bool    `yaml:"disableBoundingVolume" json:"disableBoundingVolume"`
}

type Partition struct {
	Table string `yaml:"table" json:"table"`
}

type ZClipConfig struct {
	Enabled bool    `yaml:"enabled" json:"enabled"`
	Offset  float32 `yaml:"offset" json:"offset"`
}

type FeatureConfig struct {
	IDField        string `yaml:"idField" json:"idField"`
	FeatureIdField string `yaml:"featureIdField" json:"featureIdField"` //数据对应tileset的字段值列名称
}

type MatchConfig struct {
	Operator string `yaml:"operator" json:"operator"`
	Value    string `yaml:"value" json:"value"`
}

type KeyMappingConfig struct {
	Operator          string `yaml:"operator" json:"operator"`
	Value             string `yaml:"value" json:"value"`
	FeatureValueField string `yaml:"featureValueField" json:"featureValueField"` //数据对应tileset的字段值列名称
}

type TableConfig struct {
	Schema        string `yaml:"schema" json:"schema"`
	Name          string `yaml:"name" json:"name"`
	PrimaryKey    string `yaml:"primaryKey" json:"primaryKey"`
	GeometryField string `yaml:"geometryField" json:"geometryField"`
}

type FieldConfig struct {
	Name     string         `yaml:"name" json:"name"`
	Label    string         `yaml:"label" json:"label"`
	List     bool           `yaml:"list" json:"list"`
	Search   bool           `yaml:"search" json:"search"`
	Editable bool           `yaml:"editable" json:"editable"`
	Submit   bool           `yaml:"submit" json:"submit"`
	Detail   bool           `yaml:"detail" json:"detail"`
	Storage  *StorageConfig `yaml:"storage,omitempty" json:"storage,omitempty"`
	Editor   map[string]any `yaml:"editor,omitempty" json:"editor,omitempty"`
}

type StorageConfig struct {
	Type       string   `yaml:"type" json:"type"`
	Column     string   `yaml:"column" json:"column"`
	Path       []string `yaml:"path,omitempty" json:"path,omitempty"`
	ValueType  string   `yaml:"valueType,omitempty" json:"valueType,omitempty"`
	AutoCreate bool     `yaml:"autoCreate,omitempty" json:"autoCreate,omitempty"`
	ZColumn    string   `yaml:"zColumn,omitempty" json:"zColumn,omitempty"`
}

type FileWorkerQueue struct {
	Queue string
	Path  string
}

var ConfigFilePath = ""
var instantiated *Config
var once sync.Once

func Instance() *Config {
	once.Do(func() {
		if instantiated != nil {
			return
		}
		instantiated = viperConfig()
	})
	return instantiated
}

func ExternalSet(config *Config) {
	instantiated = config
}

func viperConfig() *Config {
	var cfg Config

	// config.yaml
	yaml := viper.New()
	fileName := "config"
	fileExt := "yaml"
	fileFolder := "./"
	if len(ConfigFilePath) > 0 {
		if fi, err := os.Stat(ConfigFilePath); err == nil {
			fileExt = strings.Replace(filepath.Ext(ConfigFilePath), ".", "", 1)
			fileName = strings.Replace(fi.Name(), "."+fileExt, "", 1)
			fileFolder = filepath.Dir(ConfigFilePath)
		}
	}
	yaml.SetConfigName(fileName) // name of config file (without extension)
	yaml.SetConfigType(fileExt)
	yaml.AddConfigPath(fileFolder)

	name := getConfigName(fileName)
	cfg.Name = name

	yaml.SetDefault("ManHttpsPort", "48091")
	yaml.SetDefault("NetworkFolder", "network")
	yaml.SetDefault("LocalTileFolder", "data")
	yaml.SetDefault("WWWFolder", "dist")
	yaml.SetDefault("DataPointType", "gcj")
	yaml.SetDefault("DeviceId", "0")
	yaml.SetDefault("Debug", false)
	yaml.SetDefault("CertFilePath", "cert")
	yaml.SetDefault("HttpDebug", false)
	yaml.SetDefault("EnableAutoTilesetRebuild", true)
	yaml.SetDefault("Postgres", map[string]interface{}{
		"Host":   "localhost",
		"Port":   "5432",
		"User":   "postgres",
		"DbName": "postgres",
		"Schema": "public",
	})
	yaml.SetDefault("Mysql", map[string]interface{}{
		"Host": "",
		"Port": "3306",
	})
	yaml.SetDefault("Redis", map[string]interface{}{
		"Host": "localhost",
		"Port": "6379",
		"User": "default",
	})
	yaml.SetDefault("RabbitMQ", map[string]interface{}{
		"Exchange": "rsu",
	})
	yaml.SetDefault("Minio", map[string]interface{}{
		"Host": "localhost",
		"Port": "9000",
	})

	fmt.Println(fmt.Sprintf("Reading Config file:%s\n", filepath.Join(fileFolder, fileName+"."+fileExt)))
	configFile := filepath.Join(
		fileFolder,
		fileName+"."+fileExt,
	)

	content, err := os.ReadFile(configFile)
	if err != nil {
		log.Errorf("read config file failed: %v", err)
		return nil
	}

	expandedContent := os.ExpandEnv(string(content))

	err = yaml.ReadConfig(
		strings.NewReader(expandedContent),
	)
	if err != nil {
		log.Errorf("parse config file failed: %v", err)
		return nil
	}

	err = yaml.Unmarshal(&cfg)
	if err != nil {
		panic("Fatal error in parse config file: " + err.Error())
	}

	err = yaml.Unmarshal(&cfg)
	if err != nil {
		panic("Fatal error in parse config file: " + err.Error())
	}
	cfg.CrossIds = make(map[string]string)

	if _, err := os.Stat(filepath.Join(cfg.CertFilePath, "server.pem")); err == nil {
		log.Info("File ", "server.pem found for https on "+cfg.CertFilePath)
		cfg.UsingSSL = true
		cfg.SSLSecCrt = filepath.Join(cfg.CertFilePath, "server.pem")
		cfg.SSLSecKey = filepath.Join(cfg.CertFilePath, "server.key")
	}

	return &cfg
}

func GetNetworkConfigByName(name string) (*Config, error) {
	path := getConfigPathByName(name)
	return GetNetworkConfig(path)
}

func GetNetworkConfig(fn string) (*Config, error) {
	var cfg Config

	// config.yaml
	yaml := viper.New()
	fileName := "config"
	fileExt := "yaml"
	fileFolder := ConfigRootPath
	if len(fn) > 0 {
		_, filename := filepath.Split(fn)
		fileExt = strings.Replace(filepath.Ext(filename), ".", "", 1)
		fileName = strings.Replace(filename, "."+fileExt, "", 1)
	} else {
		return nil, errors.New("filename is null")
	}
	yaml.SetConfigName(fileName) // name of config file (without extension)
	yaml.SetConfigType(fileExt)
	yaml.AddConfigPath(fileFolder)

	name := getConfigName(fn)
	cfg.Name = name

	yaml.SetDefault("NetworkFolder", "network")
	yaml.SetDefault("DataPointType", "gcj")
	yaml.SetDefault("Debug", false)
	yaml.SetDefault("HttpDebug", false)
	yaml.SetDefault("PublishToList", true)
	yaml.SetDefault("Postgres", map[string]interface{}{
		"Host":   "localhost",
		"Port":   "5432",
		"User":   "postgres",
		"DbName": "postgres",
		"Schema": "public",
	})
	yaml.SetDefault("Redis", map[string]interface{}{
		"Host": "localhost",
		"Port": "6379",
		"User": "default",
	})
	yaml.SetDefault("RabbitMQ", map[string]interface{}{
		"Exchange": "rsu",
	})
	yaml.SetDefault("Minio", map[string]interface{}{
		"Host": "localhost",
		"Port": "9000",
	})

	fmt.Println(fmt.Sprintf("Reading Config file:%s\n", filepath.Join(fileFolder, fileName+"."+fileExt)))
	configFile := filepath.Join(
		fileFolder,
		fileName+"."+fileExt,
	)

	content, err := os.ReadFile(configFile)
	if err != nil {
		log.Errorf("read config file failed: %v", err)
		return nil, nil
	}

	expandedContent := os.ExpandEnv(string(content))

	err = yaml.ReadConfig(
		strings.NewReader(expandedContent),
	)
	if err != nil {
		log.Errorf("parse config file failed: %v", err)
		return nil, nil
	}

	err = yaml.Unmarshal(&cfg)
	if err != nil {
		panic("Fatal error in parse config file: " + err.Error())
	}

	err = yaml.Unmarshal(&cfg)
	if err != nil {
		panic("Fatal error in parse config file: " + err.Error())
	}
	cfg.CrossIds = make(map[string]string)

	osCacheRoot := os.Getenv("CACHE_ROOT")
	if len(osCacheRoot) > 0 {
		CacheRootFolder = osCacheRoot
		fmt.Println("Environment CACHE_ROOT:" + osCacheRoot)
	}

	if _, errS := os.Stat(cfg.NetworkFolder); os.IsNotExist(errS) {
		// 目录不存在，创建
		if errC := os.MkdirAll(cfg.NetworkFolder, 0755); errC != nil {
			return nil, errC
		}
	}

	if len(cfg.SignImageOutputPath) > 0 {
		if _, errS := os.Stat(cfg.SignImageOutputPath); os.IsNotExist(errS) {
			// 目录不存在，创建
			if errC := os.MkdirAll(cfg.SignImageOutputPath, 0755); errC != nil {
				return nil, errC
			}
		}
	}

	return &cfg, nil
}

func (c *Config) GetMinioBucket(name string) string {

	// 替换所有不合法字符为 "-"
	prefix := NameRegex.ReplaceAllString(c.Minio.BucketPrefix, "-")

	// 确保开头和结尾不是 "-"
	prefix = strings.Trim(prefix, "-")

	return prefix + "-" + name
}

func GetDeviceCrossId(devId string) string {
	locker.Lock()
	defer locker.Unlock()

	if c, ok := instantiated.CrossIds[devId]; ok {
		return c
	}
	return ""
}

// 本机ip地址
func getIpAddr(cfg *Config) (addr string, err error) {
	conn, err := net.Dial("udp", cfg.Crossserver.AdvertiseHost+":80")
	if err != nil {
		return "", err
	}
	defer func(conn net.Conn) {
		_ = conn.Close()
	}(conn)

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	fmt.Println(fmt.Sprintf("Local Address: %s", localAddr.IP))
	return localAddr.IP.String(), nil
}

func SetDebugCrossIds(cross map[string]string) {
	debugCrossIdMapLocker.Lock()
	defer debugCrossIdMapLocker.Unlock()

	for cid, ids := range cross {
		cid = strings.ToUpper(cid)
		debugCrossIdMap[cid] = mapset.NewSet()
		keys := strings.Split(ids, ",")
		for _, key := range keys {
			debugCrossIdMap[cid].Add(key)
		}
	}
}

func GetDebugCrossIdLen() int {
	debugCrossIdMapLocker.Lock()
	defer debugCrossIdMapLocker.Unlock()
	return len(debugCrossIdMap)
}

func GetDebugCrossId(crossId string, devId string) bool {
	debugCrossIdMapLocker.Lock()
	defer debugCrossIdMapLocker.Unlock()

	if c, ok := debugCrossIdMap[crossId]; ok {
		return c.Contains(devId)
	}
	return false
}

func SetCombineBounds(bds map[string]orb.Bound) {
	locker.Lock()
	defer locker.Unlock()

	combineBounds = make(map[string]orb.Bound)
	for s, bd := range bds {
		combineBounds[s] = bd
	}
}

func GetCombineBounds(bd orb.Bound) orb.Bound {
	locker.Lock()
	defer locker.Unlock()

	rst := orb.Bound{}
	for _, bound := range combineBounds {
		if bound.Intersects(bd) {
			if rst.IsZero() {
				rst = bound
			} else {
				rst = rst.Union(bound)
			}
		}
	}

	if rst.IsZero() {
		rst = bd
	}
	return rst
}

func SetTrackPathShowTrackId(ids map[int64]bool) {
	locker.Lock()
	defer locker.Unlock()
	trackPathShowTrackId = make(map[int64]bool)
	for s, b := range ids {
		trackPathShowTrackId[s] = b
	}
}

func GetTrackPathShowTrackIdMap() map[int64]bool {
	locker.Lock()
	defer locker.Unlock()

	tps := make(map[int64]bool)
	for i, b := range trackPathShowTrackId {
		tps[i] = b
	}
	return tps
}

func TestTrackPathShowTrackId(id int64) bool {
	locker.Lock()
	defer locker.Unlock()

	if b, ok := trackPathShowTrackId[id]; ok {
		return b
	}
	return false
}

func SetTrackPathShowPlate(ids map[string]bool) {
	locker.Lock()
	defer locker.Unlock()
	trackPathShowPlate = make(map[string]bool)
	for s, b := range ids {
		trackPathShowPlate[s] = b
	}
}

func TestTrackPathShowPlate(id string) bool {
	locker.Lock()
	defer locker.Unlock()

	if b, ok := trackPathShowPlate[id]; ok {
		return b
	}
	return false
}

func GetNetworkConfigs() ([]ConfigFileData, error) {
	if _, err := os.Stat(ConfigRootPath); err != nil {
		if os.IsNotExist(err) {
			return nil, err
		}
	} else {
		files, err := os.ReadDir(ConfigRootPath)
		if err != nil {
			panic(err)
		}
		sort.Slice(files, func(i, j int) bool {
			return strings.Compare(files[i].Name(), files[j].Name()) < 0
		})

		var cfgs []ConfigFileData
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			if strings.HasSuffix(f.Name(), ".yaml") {
				filePath := filepath.Join(ConfigRootPath, f.Name())
				cfg, errL := GetNetworkConfig(filePath)
				if errL != nil {
					continue
				}
				if !cfg.PublishToList {
					continue
				}

				log.Info("config loaded file:" + filePath)

				var bound interface{} = gin.H{
					"west":  73.0,  // 西经
					"south": 18.0,  // 南纬
					"east":  135.0, // 东经
					"north": 53.5,  // 北纬
				}
				if len(cfg.Bound) > 0 {
					values := strings.Split(cfg.Bound, ",")
					if len(values) == 4 {
						bound = gin.H{
							"west":  utils.ToFloat64(values[0]), // 西经
							"south": utils.ToFloat64(values[1]), // 南纬
							"east":  utils.ToFloat64(values[2]), // 东经
							"north": utils.ToFloat64(values[3]), // 北纬
						}
					}
				}

				cfgs = append(cfgs, ConfigFileData{
					Desc:          cfg.Desc,
					Name:          cfg.Name,
					ID:            EncodeContext(cfg.Name, ""),
					NetworkFolder: cfg.NetworkFolder,
					Bound:         bound,
					Tilesets:      cfg.Tilesets,
					ImageTile:     cfg.ImageTile,
				})
			}
		}
		return cfgs, nil
	}

	return nil, nil
}

func getConfigName(filePath string) string {
	_, fn := filepath.Split(filePath)
	name := strings.Replace(fn, ".yaml", "", 1)
	return name
}

func getConfigPathByName(name string) string {
	return name + ".yaml"
}

// 生成数据库地址和表名的哈希字符串
func generateHash(dbAddr, tableName string) string {
	// 拼接字符串（可以加分隔符，保证清晰）
	combined := fmt.Sprintf("%s:%s", dbAddr, tableName)

	// 创建哈希
	hash := sha256.Sum256([]byte(combined))

	// 转成十六进制字符串
	return hex.EncodeToString(hash[:])
}

// 用 MD5 生成数据库地址和表名的哈希字符串
func generateMD5Hash(dbAddr, tableName string) string {
	// 拼接数据库地址和表名
	combined := fmt.Sprintf("%s:%s", dbAddr, tableName)

	// 计算 MD5 哈希
	hash := md5.Sum([]byte(combined))

	// 转换为十六进制字符串
	return hex.EncodeToString(hash[:])
}

// 编码配置地址和表名为哈希字符串
func EncodeContext(configName, tableName string) string {
	combined := fmt.Sprintf("%s||%s", configName, tableName)
	encrypted, err := encrypt(combined, env.ConfigKey)
	if err != nil {
		log.Error(err)
		return ""
	}
	return encrypted
}

// 解码哈希字符串，解析出配置地址和表名
func DecodeContext(hash string) (string, string, error) {
	decrypted, err := decrypt(hash, env.ConfigKey)
	if err != nil {
		log.Error(err)
		return "", "", err
	}

	parts := strings.Split(decrypted, "||")
	if len(parts) == 1 {
		return parts[0], "", nil
	} else if len(parts) == 2 {
		return parts[0], parts[1], nil
	} else {
		return "", "", fmt.Errorf("invalid encoded string format")
	}
}

// 加密数据
func encrypt(data, key string) (string, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}

	// 创建一个GCM
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	// 创建一个nonce
	nonce := make([]byte, gcm.NonceSize())

	// 加密数据
	ciphertext := gcm.Seal(nonce, nonce, []byte(data), nil)

	return hex.EncodeToString(ciphertext), nil
}

// 解密数据
func decrypt(encryptedData, key string) (string, error) {
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	data, err := hex.DecodeString(encryptedData)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("invalid data length")
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

func (field FieldConfig) EffectiveStorage() StorageConfig {
	if field.Storage != nil {
		return *field.Storage
	}

	return StorageConfig{
		Type:   "column",
		Column: field.Name,
	}
}
