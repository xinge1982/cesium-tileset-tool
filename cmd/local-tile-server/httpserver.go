package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"map-tool/config"
	_ "map-tool/docs"
	"map-tool/httpserver/ginprometheus"
	"net/http"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-contrib/gzip"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

var TempFilePath = "./tmp"

// 启动基础http服务
func StartHttp() error {
	cfg := config.Instance()

	gin.ForceConsoleColor()
	gin.SetMode(gin.ReleaseMode)
	var r *gin.Engine
	r = gin.New()

	origins := []string{"*"}
	r.Use(cors.New(cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{"GET", "POST", "DELETE", "PUT", "OPTIONS", "HEAD"},
		AllowHeaders:     []string{"Origin", "authorization", "Connection", "Upgrade", "Authorization", "Content-Type", "Content-Length", "PGContextID"},
		ExposeHeaders:    []string{"Content-Length", "X-Requested-With", "Content-Type", "X-Codingpedia", "Authorization", "PGContextID"},
		AllowCredentials: false,
		AllowOriginFunc: func(origin string) bool {
			return true
		},
		MaxAge: 12 * time.Hour,
	}))

	r.GET("/version", func(c *gin.Context) {
		gc := cfg.GitCommit
		if len(gc) > 8 {
			gc = gc[:8]
		}
		c.JSON(http.StatusOK, gin.H{
			"version": fmt.Sprintf("Version:%s Build:%s-%s", Version, cfg.BuildDate, gc),
			"verNo":   cfg.AppVersion,
			"date":    cfg.BuildDate,
			"commit":  cfg.GitCommit,
		})
		return
	})

	// Enable gzip for responses
	r.Use(gzip.Gzip(
		gzip.DefaultCompression,
		gzip.WithExcludedExtensions([]string{
			".png", ".jpg", ".jpeg", ".gif", ".webp",
			".mp4", ".mov", ".zip", ".gz", ".br", ".pdf",
			//glb文件单独判断是否是已经压缩的
			".glb",
		}),
	))

	//metrics
	p := ginprometheus.NewPrometheus("gin", []string{})
	p.Use(r, "/actuator/prometheus")

	//// 注册中间件，应用到所有路由
	r.Use(requestLoggerMiddleware())

	// 注册 Swagger 路由
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	//pprof debug information
	pprof.Register(r, "/debug/pprof")

	// api接口注册
	apiRegister(r, "api")

	//ping pong
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"message": "pong",
		})
	})

	staticFS := getFileSystem()

	r.GET("/web", func(c *gin.Context) {
		c.Redirect(http.StatusTemporaryRedirect, "/web/")
	})

	// Serve static assets
	r.StaticFS("/web", staticFS)

	r.NoRoute(func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.URL.Path, "/web") {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		file, err := staticFS.Open("index.html")
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}

		stat, _ := file.Stat()
		c.DataFromReader(http.StatusOK, stat.Size(), "text/html", file, nil)
	})

	// start https server
	go func() {
		var httpServer = "0.0.0.0:" + config.Instance().ManHttpPort
		var tlsServer = "0.0.0.0:" + config.Instance().ManHttpsPort
		for {
			select {
			default:
				if cfg.UsingSSL {
					log.Info("Https server start: " + tlsServer)
					log.Infof("本地三维切片地址：https://localhost:%s/api/tilesets/tileset.json", config.Instance().ManHttpsPort)
					log.Info("请用浏览器打开上述地址，确认本地证书！如果需要在三维场景中展示，请将上述地址加入到页面的用户图层中。")
					log.Infof("本地三维切片浏览地址：https://localhost:%s/web", config.Instance().ManHttpsPort)
					log.Info("请用浏览器打开上述地址，可以查看三维模型。")
					err := r.RunTLS(tlsServer, cfg.SSLSecCrt, cfg.SSLSecKey)
					if err != nil {
						log.Error("Https server error: ", err)
						time.Sleep(5 * time.Second)
					}
				} else {
					log.Info("Http server start: " + httpServer)
					log.Infof("本地三维切片地址：http://localhost:%s/api/tilesets/tileset.json", config.Instance().ManHttpPort)
					log.Infof("本地三维切片浏览地址：http://localhost:%s/web", config.Instance().ManHttpPort)
					log.Info("请用浏览器打开上述地址，可以查看三维模型。")
					err := r.Run(httpServer)
					if err != nil {
						log.Error("Http server error: ", err)
						time.Sleep(5 * time.Second)
					}
				}
			}
			log.Info("Http server stoped: " + tlsServer)
		}
	}()

	return nil
}

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

func apiRegister(g *gin.Engine, prefix string) {
	r := g.Group(prefix)

	// 路网
	r.GET("tilesets", getTileSets)
	r.GET("tilesets/*filepath", getMergedTileSets)
}

func getTileSets(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": config.Instance().Tilesets,
	})
}

func getFileSystem() http.FileSystem {
	// Create sub FS so "/index.html" maps correctly
	subFS, err := fs.Sub(embeddedFiles, "web")
	if err != nil {
		panic(err)
	}
	return http.FS(subFS)
}
