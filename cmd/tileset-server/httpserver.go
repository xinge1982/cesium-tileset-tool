package main

import (
	"bytes"
	"cesium-tileset-tool/config"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

var TempFilePath = "./tmp"

// 自定义 ResponseWriter，用于捕获响应体和状态码
type ResponseWriterWrapper struct {
	gin.ResponseWriter
	body       *bytes.Buffer
	StatusCode int
}

func (rw *ResponseWriterWrapper) Write(p []byte) (n int, err error) {
	n, err = rw.body.Write(p) // 将响应体写入到缓存
	if err == nil {
		_, err = rw.ResponseWriter.Write(p) // 继续写入原始响应
	}
	return n, err
}

func (rw *ResponseWriterWrapper) WriteHeader(statusCode int) {
	rw.StatusCode = statusCode                // 捕获状态码
	rw.ResponseWriter.WriteHeader(statusCode) // 调用原始 WriteHeader
}

// 定义一个中间件，用于记录请求
func requestLoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 记录请求开始时间
		startTime := time.Now()

		// 在这里可以添加其他操作，比如权限校验、请求头校验等

		// 处理请求前的钩子
		log.Infof("Starting request to %s", c.Request.URL.Path)

		// 用一个缓冲区来接收响应内容
		body := bytes.NewBufferString("")
		// 创建一个包装过的 ResponseWriter
		rw := &ResponseWriterWrapper{
			ResponseWriter: c.Writer,
			body:           body,
			StatusCode:     200, // 默认状态码为 200
		}
		// 替换 Gin 的默认 ResponseWriter
		c.Writer = rw

		// 继续执行其他中间件和路由处理函数
		c.Next()

		// 请求处理完成后，获取捕获的响应内容和状态码
		statusCode := rw.StatusCode

		// 处理请求后的钩子
		duration := time.Since(startTime)
		log.Infof("Completed request [%d] to %s in %v",
			statusCode, c.Request.URL.Path, duration)
	}
}

func getTileSets(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": config.Instance().Tilesets,
	})
}
