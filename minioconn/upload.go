package minioconn

import (
	"bytes"
	"cesium-tileset-tool/config"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/srwiley/oksvg"
)

const UserTempUploadBucketName = "upload-sign-temp"

func (p *MinioConn) uploadHandler(c *gin.Context) {
	p.locker.Lock()
	defer p.locker.Unlock()

	// 获取文件
	bucketName := config.Instance().GetMinioBucket(UserTempUploadBucketName)
	if bucketName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "upload-file/:bucketName 缺少参数"})
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "接收文件失败：" + err.Error()})
		return
	}

	// 校验文件类型（只允许 JPEG 和 PNG）
	if !isAllowedImageType(file) {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "仅支持 JPEG 或 PNG 或 SVG 格式"})
		return
	}

	// 打开文件
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"msg": "无法打开文件：" + err.Error()})
		return
	}
	defer func(src multipart.File) {
		_ = src.Close()
	}(src)

	buf, errR := io.ReadAll(src)
	if errR != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"msg": "无法读取文件：" + err.Error()})
		return
	}

	// 解码图片

	var srcWidth int
	var srcHeight int
	if strings.Contains(strings.ToLower(filepath.Ext(file.Filename)), "svg") {
		if width, height, errS := GetSvgSize(buf); errS != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": "无法解码源图片：" + errS.Error()})
			return
		} else {
			srcWidth = width
			srcHeight = height
		}
	} else {
		srcImg, _, err := image.Decode(bytes.NewReader(buf))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": "无法解码源图片：" + err.Error()})
			return
		}
		// 获取源图像的宽度和高度
		srcBounds := srcImg.Bounds()
		srcWidth = srcBounds.Dx()
		srcHeight = srcBounds.Dy()
	}

	// 生成唯一文件名
	filename := generateFileName(file.Filename)

	// 上传文件到 MinIO
	ctx := context.Background()
	exists, errBucketExists := instantiated.client.BucketExists(context.Background(), bucketName)
	if errBucketExists == nil && exists {
	} else {
		err = instantiated.client.MakeBucket(context.Background(), bucketName, minio.MakeBucketOptions{})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"msg": "上传文件失败: " + err.Error()})
			return
		}
	}

	_, err = instantiated.client.PutObject(ctx, bucketName, filename, bytes.NewReader(buf), file.Size, minio.PutObjectOptions{
		ContentType: getContentType(file.Filename),
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"msg": "上传文件失败: " + err.Error()})
		return
	}

	// 生成 MinIO 文件 URL
	fileURL := fmt.Sprintf("%s/%s/%s", ApiFetchPrefix, bucketName, filename)

	// 返回成功响应
	c.JSON(http.StatusOK, gin.H{
		"msg": "文件上传成功",
		"data": gin.H{
			"url":      fileURL,
			"width":    srcWidth,
			"height":   srcHeight,
			"fileName": file.Filename,
		},
	})
}

func (p *MinioConn) UploadBase64(bucketName string, base64Image string) (string, error) {
	p.locker.Lock()
	defer p.locker.Unlock()

	// 获取文件
	if bucketName == "" {
		bucketName = "upload"
	}

	if !p.Ready {
		return "", fmt.Errorf("存储服务不在线")
	}

	// 解析 Base64 数据
	data, mimeType, err := DecodeBase64Image(base64Image)
	if err != nil {
		return "", fmt.Errorf("Error decoding image: %v", err)
	}

	// 生成唯一文件名
	filename := generateFileName("upload")

	// 上传文件到 MinIO
	ctx := context.Background()
	exists, errBucketExists := instantiated.client.BucketExists(context.Background(), bucketName)
	if errBucketExists == nil && exists {
	} else {
		err = instantiated.client.MakeBucket(context.Background(), bucketName, minio.MakeBucketOptions{})
		if err != nil {
			return "", errors.New("上传文件失败: " + err.Error())
		}
	}

	_, err = instantiated.client.PutObject(ctx, bucketName, filename, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: mimeType,
	})
	if err != nil {
		return "", errors.New("上传文件失败: " + err.Error())
	}

	// 生成 MinIO 文件 URL
	fileURL := fmt.Sprintf("%s/%s/%s", ApiFetchPrefix, bucketName, filename)

	// 返回成功响应
	return fileURL, nil
}

// 校验是否为允许的图片类型
func isAllowedImageType(file *multipart.FileHeader) bool {
	ext := strings.ToLower(filepath.Ext(file.Filename))
	return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".svg"
}

// 生成唯一文件名
func generateFileName(originalName string) string {
	timestamp := time.Now().Format("20060102150405.999")                              // 时间戳
	return fmt.Sprintf("%s_%s", strings.ReplaceAll(timestamp, ".", ""), originalName) // 生成唯一文件名
}

// 获取图片的 Content-Type
func getContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == ".jpg" || ext == ".jpeg" {
		return "image/jpeg"
	} else if ext == ".png" {
		return "image/png"
	}
	return "application/octet-stream"
}

// DecodeBase64Image 解析 Base64 图片数据并返回二进制数据及 MIME 类型
func DecodeBase64Image(base64Str string) ([]byte, string, error) {
	// 正则匹配 Base64 图片的 MIME 类型和数据部分
	re := regexp.MustCompile(`^data:(image/\w+);base64,(.+)$`)
	matches := re.FindStringSubmatch(base64Str)

	if len(matches) != 3 {
		return nil, "", fmt.Errorf("invalid base64 image format")
	}

	mimeType := matches[1]   // 获取 MIME 类型
	base64Data := matches[2] // 获取 Base64 编码数据

	// 解码 Base64 数据
	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return nil, "", fmt.Errorf("failed to decode base64: %w", err)
	}

	return data, mimeType, nil
}

// GetFileExtension 根据 MIME 类型获取文件扩展名
func GetFileExtension(mimeType string) (string, error) {
	exts, err := mime.ExtensionsByType(mimeType)
	if err != nil || len(exts) == 0 {
		return "", fmt.Errorf("unknown MIME type: %s", mimeType)
	}
	return exts[0], nil
}

func GetSvgSize(buf []byte) (width int, height int, err error) {
	// 读取 SVG 文件
	icon, err := oksvg.ReadIconStream(bytes.NewReader(buf))
	if err != nil {
		return 0, 0, err
	}
	// 获取 SVG 的尺寸
	width, height = int(icon.ViewBox.W), int(icon.ViewBox.H)
	return width, height, nil
}
