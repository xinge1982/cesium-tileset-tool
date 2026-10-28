package main

import (
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

var glbGzip = flag.Bool("glb-gzip-compress", false, "gzip glb file")

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, Range")
		c.Header("Access-Control-Expose-Headers", "Content-Length, Content-Range")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func main() {
	flag.Parse()

	r := gin.Default()

	r.Use(CORSMiddleware())

	r.GET("/*filepath", func(c *gin.Context) {
		path := "./public" + c.Param("filepath")

		if strings.HasSuffix(path, ".glb") {
			if *glbGzip {
				c.Header("Content-Encoding", "gzip")
			}
			c.Header("Content-Type", "model/gltf-binary")
		}

		c.File(path)
	})

	var certFilePath = "cert"
	var useSSL = false
	var sSLSecCrt = ""
	var sSLSecKey = ""
	if _, err := os.Stat(filepath.Join(certFilePath, "server.pem")); err == nil {
		log.Info("File ", "server.pem found for https on "+certFilePath)
		useSSL = true
		sSLSecCrt = filepath.Join(certFilePath, "server.pem")
		sSLSecKey = filepath.Join(certFilePath, "server.key")
	}
	if useSSL {
		var tlsServer = "0.0.0.0:8080"
		err := r.RunTLS(tlsServer, sSLSecCrt, sSLSecKey)
		if err != nil {
			log.Error("Https server error: ", err)
			time.Sleep(5 * time.Second)
		}
		log.Info("Https server start: " + tlsServer)

	} else {
		var server = "0.0.0.0:8080"
		err := r.Run(server)
		if err != nil {
			log.Error("Https server error: ", err)
			time.Sleep(5 * time.Second)
		}
		log.Info("Http server start: " + server)
	}

}
