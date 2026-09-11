package main

import (
	"bufio"
	"cesium-tileset-tool/config"
	"compress/gzip"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

func getMergedTileSets(c *gin.Context) {
	rootPath := filepath.Join(config.Instance().NetworkFolder, "tilesets")
	tilesetRoot, err := filepath.Abs(rootPath)
	if err != nil {
		panic(err)
	}

	reqPath := c.Param("filepath")
	if reqPath == "" || reqPath == "/" {
		reqPath = "/tileset.json"
	}

	cleaned := filepath.Clean(reqPath)
	fullPath := filepath.Join(tilesetRoot, cleaned)

	if !strings.HasPrefix(fullPath, filepath.Clean(tilesetRoot)) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Invalid path"})
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "File not found"})
		return
	}

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

	acceptEncoding := c.GetHeader("Accept-Encoding")
	supportsGzip := strings.Contains(acceptEncoding, "gzip")

	ext := strings.ToLower(filepath.Ext(fullPath))
	contentType := mime.TypeByExtension(ext)
	if ext == ".glb" {
		isGzipFile := false
		contentType = "model/gltf-binary"

		// 判断是否已经是压缩文件
		if tp, errD := detectFileType(fullPath); errD != nil {
			log.Error(errD)
		} else {
			if tp == "gzip" {
				isGzipFile = true
			}
		}

		switch {
		case isGzipFile:
			// 文件本身已经是 gzip 内容
			if supportsGzip {
				c.Header("Content-Encoding", "gzip")
				c.DataFromReader(http.StatusOK, -1, contentType, f, nil)
				return
			}

			// 客户端不支持 gzip，理论上应该解压后再发
			gr, errR := gzip.NewReader(f)
			if errR != nil {
				log.Error(errR)
				c.Status(http.StatusInternalServerError)
				return
			}
			defer gr.Close()

			c.DataFromReader(http.StatusOK, -1, contentType, gr, nil)
			return

		case supportsGzip:
			// 文件不是 gzip，现场压缩后发送
			c.Header("Content-Encoding", "gzip")

			gzw := gzip.NewWriter(c.Writer)
			defer gzw.Close()

			c.Status(http.StatusOK)
			if _, err := io.Copy(gzw, f); err != nil {
				log.Error(err)
			}
			return
		}
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	c.Header("Content-Type", contentType)
	c.DataFromReader(http.StatusOK, -1, contentType, f, nil)
}

func detectFileType(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "err", err
	}
	defer f.Close()

	reader := bufio.NewReader(f)

	header, err := reader.Peek(4)
	if err != nil {
		return "err", err
	}

	// GZIP
	if header[0] == 0x1f && header[1] == 0x8b {
		return "gzip", nil
	}

	// GLB ("glTF")
	if string(header[:4]) == "glTF" {
		return "glb", nil
	}

	return "unknown", nil
}
