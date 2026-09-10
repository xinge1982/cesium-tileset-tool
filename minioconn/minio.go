package minioconn

import (
	"cesium-tileset-tool/config"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	log "github.com/sirupsen/logrus"
)

type MinioConn struct {
	cfg       *config.Config
	client    *minio.Client
	Ready     bool
	cancelFun context.CancelFunc
	locker    sync.Mutex
	cacheDir  string
}

type objectCacheMeta struct {
	ETag         string    `json:"etag"`
	LastModified time.Time `json:"last_modified"`
	Size         int64     `json:"size"`
}

var instantiated *MinioConn
var once sync.Once
var store = make(map[string]*MinioConn)
var locker = sync.Mutex{}

const (
	ApiFetchPrefix = "fetch-file"
)

func Instance() *MinioConn {
	once.Do(func() {
		instantiated = &MinioConn{}
	})
	return instantiated
}

// 启动minio连接
func (p *MinioConn) Run(cfg *config.Config) {
	p.cfg = cfg

	ctx, cfun := context.WithCancel(context.Background())
	p.cancelFun = cfun

	p.cacheDir = getMinioCacheFolder(p.cfg.Name)
	_ = os.MkdirAll(p.cacheDir, 0755)

	errP := p.initMinio(cfg.Minio.Host, cfg.Minio.Port, cfg.Minio.AccessKey, cfg.Minio.SecretKey)
	if errP != nil {
		panic(errP)
	}
	if p.client != nil {
		p.test()
	}

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if p.client != nil {
					p.test()
				}
			}
		}
	}()
}

// 启动minio连接
func (p *MinioConn) RunOutput(cfg *config.Config) {
	p.cfg = cfg
	ctx, cfun := context.WithCancel(context.Background())
	p.cancelFun = cfun

	p.cacheDir = getMinioCacheFolder(p.cfg.Name)
	_ = os.MkdirAll(p.cacheDir, 0755)

	errP := p.initMinio(cfg.MinioOutput.Host, cfg.MinioOutput.Port, cfg.MinioOutput.AccessKey, cfg.MinioOutput.SecretKey)
	if errP != nil {
		panic(errP)
	}
	if p.client != nil {
		p.test()
	}

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if p.client != nil {
					p.test()
				}
			}
		}
	}()
}

func (p *MinioConn) test() {
	//timeOutCtx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	//defer cancel()
	if _, err := p.client.ListBuckets(context.Background()); err != nil {
		log.Error("Minio bucket list fail ", err)
	} else {
		if p.client.IsOffline() {
			p.Ready = false
			log.Error("Minio offline")
		} else {
			p.Ready = true
			log.Info("Minio online")
		}
	}
}

func (p *MinioConn) initMinio(host, port, accessKey, secretKey string) error {
	p.locker.Lock()
	defer p.locker.Unlock()
	log.Infof("Init Minio connection: %s:%s", host, port)

	addr := fmt.Sprintf("%s:%s", host, port)

	var err error
	p.client, err = minio.New(addr, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: false,
	})
	if err != nil {
		return err
	}

	return nil
}

func getMinioCacheFolder(projectName string) string {
	return filepath.Join(config.CacheRootFolder, "minio-cache", projectName)
}

func (p *MinioConn) Close() {
	locker.Lock()
	defer locker.Unlock()
	delete(store, p.cfg.Name)
}

func GetMinioConn(cfgName string) *MinioConn {
	locker.Lock()
	defer locker.Unlock()
	if dbc, ok := store[cfgName]; ok {
		return dbc
	} else {
		if cfg, err := config.GetNetworkConfigByName(cfgName); err != nil {
			log.Error(err)
			return nil
		} else {
			conn := newOutputConn(cfg)
			return conn
		}
	}
}

func newOutputConn(cfg *config.Config) *MinioConn {
	if dbc, ok := store[cfg.Name]; ok {
		return dbc
	}

	conn := &MinioConn{}
	conn.RunOutput(cfg)

	store[cfg.Name] = conn
	return conn
}

// 上传图片到按日期分 bucket 的 MinIO
func (p *MinioConn) UploadToMinio(file io.Reader, len int64, bucketName string, fileName string, contentType string) (string, error) {
	p.locker.Lock()
	defer p.locker.Unlock()

	if p.client == nil {
		return "", errors.New("connection not ready")
	}

	// 检查存储桶是否存在，如果不存在则创建
	exists, errBucketExists := p.client.BucketExists(context.Background(), bucketName)
	if errBucketExists == nil && exists {
	} else {
		err := p.client.MakeBucket(context.Background(), bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return "", err
		}
	}

	// 上传文件到 MinIO 指定的日期 bucket
	_, err := p.client.PutObject(context.Background(), bucketName, fileName, file, len,
		minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return "", err
	}

	// 返回文件的 URL
	fileURL := fmt.Sprintf("%s/%s", bucketName, fileName)
	return fileURL, nil
}

// 删除过期数据
func (p *MinioConn) deleteOldBuckets() {
	p.locker.Lock()
	defer p.locker.Unlock()

	if p.client == nil {
		return
	}

	retentionPeriod := 30 * 24 * time.Hour // 保留 30 天的数据
	now := time.Now()

	// 获取所有存储桶
	buckets, err := p.client.ListBuckets(context.Background())
	if err != nil {
		log.Fatalf("Failed to list buckets: %v", err)
	}

	for _, bucket := range buckets {
		bucketDate, err := time.Parse("captures-2006-01-02", bucket.Name)
		if err != nil {
			continue // 跳过不符合日期格式的存储桶
		}

		// 如果存储桶的日期超过保留期，则删除
		if now.Sub(bucketDate) > retentionPeriod {
			err := p.client.RemoveBucket(context.Background(), bucket.Name)
			if err != nil {
				log.Printf("Failed to delete bucket %s: %v", bucket.Name, err)
			} else {
				log.Printf("Bucket %s deleted", bucket.Name)
			}
		}
	}
}

// 删除bucket数据
func (p *MinioConn) RemoveBucket(bucketName string) {
	p.locker.Lock()
	defer p.locker.Unlock()

	if p.client == nil {
		return
	}

	ctx := context.Background()

	exists, errBucketExists := p.client.BucketExists(context.Background(), bucketName)
	if errBucketExists == nil && exists {
	} else {
		log.Info("Remove bucket not exists: " + bucketName)
		return
	}

	// 列出并删除 bucket 内的所有对象
	err := p.RemoveAllObjects(ctx, bucketName, minio.ListObjectsOptions{
		Recursive: true,
	})
	if err != nil {
		log.Error("Failed to remove all objects from bucket %s: %v", bucketName, err)
	}

	// 删除 bucket
	err = p.client.RemoveBucket(ctx, bucketName)
	if err != nil {
		log.Error("Failed to remove bucket %s: %v", bucketName, err)
	}

	log.Infof("Bucket %s deleted successfully", bucketName)
}

// 删除 bucket 中的所有对象
func (p *MinioConn) RemoveAllObjects(ctx context.Context, bucketName string, opts minio.ListObjectsOptions) error {
	// 列出所有对象
	objectCh := p.client.ListObjects(ctx, bucketName, opts)

	// 收集所有对象的名称
	var objectsToDelete []minio.ObjectInfo
	for object := range objectCh {
		if object.Err != nil {
			return object.Err
		}
		objectsToDelete = append(objectsToDelete, object)
	}

	// 批量删除对象
	for _, obj := range objectsToDelete {
		err := p.client.RemoveObject(ctx, bucketName, obj.Key, minio.RemoveObjectOptions{})
		if err != nil {
			log.Printf("Failed to delete object %s: %v", obj.Key, err)
		} else {
			log.Printf("Deleted object: %s\n", obj.Key)
		}
	}
	return nil
}

func (p *MinioConn) GetObject(ctx context.Context, bucketName, objectName string) (io.ReadCloser, error) {
	if p.client == nil {
		return nil, errors.New("minio 服务不可用")
	}
	if p.cacheDir == "" {
		return p.client.GetObject(ctx, bucketName, objectName, minio.GetObjectOptions{})
	}

	dataPath, metaPath := p.cachePaths(bucketName, objectName)

	info, err := p.client.StatObject(ctx, bucketName, objectName, minio.StatObjectOptions{})
	if err != nil {
		return nil, err
	}

	p.locker.Lock()
	defer p.locker.Unlock()

	meta, err := readCacheMeta(metaPath)
	if err == nil &&
		meta.ETag == info.ETag &&
		meta.Size == info.Size {
		if f, err := os.Open(dataPath); err == nil {
			return f, nil
		}
	}

	tmpPath := dataPath + ".tmp"

	obj, err := p.client.GetObject(ctx, bucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer obj.Close()

	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return nil, err
	}

	if _, err := io.Copy(tmpFile, obj); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpPath)
		return nil, err
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	if err := os.Rename(tmpPath, dataPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	_ = writeCacheMeta(metaPath, objectCacheMeta{
		ETag:         info.ETag,
		LastModified: info.LastModified,
		Size:         info.Size,
	})

	return os.Open(dataPath)
}

func (p *MinioConn) PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, contentLen int64, contentType string) (
	minio.UploadInfo, error) {
	if p.client == nil {
		return minio.UploadInfo{}, errors.New("minio 服务不可用")
	}

	// 检查存储桶是否存在，如果不存在则创建
	exists, errBucketExists := p.client.BucketExists(context.Background(), bucketName)
	if errBucketExists == nil && exists {
	} else {
		err := p.client.MakeBucket(context.Background(), bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return minio.UploadInfo{}, err
		}
	}

	info, err := p.client.PutObject(ctx, bucketName, objectName, reader, contentLen, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err == nil {
		p.invalidateCache(bucketName, objectName)
	}
	return info, err
}

// ObjectExists 判断对象是否存在
func (p *MinioConn) ObjectExists(ctx context.Context, bucket, objectName string) (bool, error) {
	if p.client == nil {
		return false, errors.New("minio 服务不可用")
	}

	_, err := p.client.StatObject(ctx, bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		// 如果是对象不存在的错误，则返回 false
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" || resp.Code == "NotFound" {
			return false, nil
		}
		return false, err // 其他错误直接返回
	}
	return true, nil
}

func (p *MinioConn) RemoveObject(ctx context.Context, bucketName, objectName string) error {
	if p.client == nil {
		return errors.New("minio 服务不可用")
	}

	err := p.client.RemoveObject(ctx, bucketName, objectName, minio.RemoveObjectOptions{})
	if err == nil {
		p.invalidateCache(bucketName, objectName)
	}
	return err
}

//
//func setBucketLifecycle(client *minio.Client, bucketName string) {
//	// 创建生命周期配置
//	lc := lifecycle.NewConfiguration()
//	lc.Rules = []lifecycle.Rule{
//		{
//			RuleFilter: lifecycle.Filter{
//				Prefix: "", // 针对桶内所有文件
//			},
//			Status: "Enabled",
//			Expiration: lifecycle.Expiration{
//				Days: lifecycle.ExpirationDays(7), // 设置文件过期时间为30天
//			},
//			ID: "auto-delete-after-30-days", // 规则ID
//		},
//	}
//
//	// 应用生命周期规则到桶
//	err := client.SetBucketLifecycle(context.Background(), bucketName, lc)
//	if err != nil {
//		log.Fatalln("Failed to set lifecycle policy:", err)
//	} else {
//		log.Println("Lifecycle policy set successfully, bucket: " + bucketName)
//	}
//}

// 提取文件内容的接口
// @Summary Fetch file content from MinIO
// @Description Retrieve the content of a file stored in MinIO by its bucket and object name
// @Tags Oss
// @Produce text/plain
// @Param bucket_name path string true "Bucket Name"
// @Param object_name path string true "Object Name"
// @Success 200 {string} string "File content"
// @Failure 400 {object} common.HTTPError "请求参数错误"
// @Failure 500 {object} common.HTTPError "服务器内部错误"
// @Router /fetch-file/{bucketName}/{objectName} [get]
func fetchFileContent(c *gin.Context) {
	bucketName := c.Param("bucketName")
	objectName := c.Param("objectName")

	if bucketName == "" || objectName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "Bucket name and object name are required"})
		return
	}

	// 从 MinIO 获取对象
	object, err := instantiated.GetObject(context.Background(), bucketName, objectName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"msg": fmt.Sprintf("Failed to get object: %v", err)})
		return
	}
	defer func() {
		_ = object.Close()
	}()

	// 读取对象内容
	content, err := io.ReadAll(object)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"msg": fmt.Sprintf("Failed to read object content: %v", err)})
		return
	}

	contentType := "application/octet-stream"
	if strings.HasSuffix(strings.ToLower(objectName), ".txt") {
		contentType = "text/plain"
	} else if strings.HasSuffix(strings.ToLower(objectName), ".jpg") ||
		strings.HasSuffix(strings.ToLower(objectName), ".jpeg") {
		contentType = "image/jpeg"
	} else if strings.HasSuffix(strings.ToLower(objectName), ".svg") {
		contentType = "image/svg+xml"
	} else if strings.HasSuffix(strings.ToLower(objectName), ".png") {
		contentType = "image/png"
	} else if strings.Contains(objectName, ".") {
		ext := filepath.Ext(strings.ToLower(objectName))
		// 使用 mime 包获取 MIME 类型
		ct := mime.TypeByExtension(ext)
		if ct != "" {
			contentType = ct
		}
	}
	// 返回文件内容
	c.Data(http.StatusOK, contentType, content)
}

func jwtMiddlewareStatus() gin.HandlerFunc {
	return func(c *gin.Context) {
		if instantiated.client == nil {
			c.JSON(http.StatusForbidden, gin.H{"msg": "OSS存储服务不可用"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func (s *MinioConn) RegistHandler(r *gin.RouterGroup, jwtMiddleware func(string, bool) gin.HandlerFunc) {
	//oss
	r.GET(ApiFetchPrefix+"/:bucketName/:objectName", jwtMiddlewareStatus(), fetchFileContent)
	r.POST("upload-file/:bucketName", jwtMiddlewareStatus(), s.uploadHandler)
}

func (s *MinioConn) GetObjectInfo(bucketName string, objectName string) ([]byte, string, error) {
	// 从 MinIO 获取对象
	object, err := instantiated.GetObject(context.Background(), bucketName, objectName)
	if err != nil {
		return nil, "", err
	}
	defer func() {
		_ = object.Close()
	}()

	// 读取对象内容
	content, err := io.ReadAll(object)
	if err != nil {
		return nil, "", err
	}

	contentType := "application/octet-stream"
	if strings.HasSuffix(strings.ToLower(objectName), ".txt") {
		contentType = "text/plain"
	} else if strings.HasSuffix(strings.ToLower(objectName), ".jpg") ||
		strings.HasSuffix(strings.ToLower(objectName), ".jpeg") {
		contentType = "image/jpeg"
	} else if strings.HasSuffix(strings.ToLower(objectName), ".svg") {
		contentType = "image/svg+xml"
	} else if strings.HasSuffix(strings.ToLower(objectName), ".png") {
		contentType = "image/png"
	} else if strings.Contains(objectName, ".") {
		ext := filepath.Ext(strings.ToLower(objectName))
		// 使用 mime 包获取 MIME 类型
		ct := mime.TypeByExtension(ext)
		if ct != "" {
			contentType = ct
		}
	}

	return content, contentType, nil
}

func GetMinioBaseGltfClient(code string) (*MinioConn, string, string, error) {
	if len(code) > 0 {
		configName, _, errD := config.DecodeContext(code)
		if errD != nil {
			return nil, "", "", errors.New("decode context error:" + errD.Error())
		}
		cfg, errC := config.GetNetworkConfigByName(configName)
		if errC != nil {
			return nil, "", "", errors.New("get config error:" + errC.Error())
		}

		client := GetMinioConn(configName)
		if client == nil {
			return nil, "", "", errors.New("minio client not exists")
		}

		prefix := ""
		bucket := cfg.MinioOutput.Bucket
		if cfg.MinioOutput.BaseModelBucket != "" {
			var keys = strings.Split(cfg.MinioOutput.BaseModelBucket, "/")
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

func (p *MinioConn) cacheKey(bucket, object string) string {
	sum := sha1.Sum([]byte(bucket + "/" + object))
	return hex.EncodeToString(sum[:])
}

func (p *MinioConn) cachePaths(bucket, object string) (dataPath, metaPath string) {
	key := p.cacheKey(bucket, object)
	return filepath.Join(p.cacheDir, key+".data"),
		filepath.Join(p.cacheDir, key+".json")
}

func (p *MinioConn) invalidateCache(bucketName, objectName string) {
	if p.cacheDir == "" {
		return
	}

	dataPath, metaPath := p.cachePaths(bucketName, objectName)
	_ = os.Remove(dataPath)
	_ = os.Remove(metaPath)
}

func readCacheMeta(path string) (*objectCacheMeta, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var meta objectCacheMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func writeCacheMeta(path string, meta objectCacheMeta) error {
	b, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}
