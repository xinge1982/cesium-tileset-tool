package main

import (
	"bytes"
	"cesium-tileset-tool/config"
	"cesium-tileset-tool/pg"
	"cesium-tileset-tool/service"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"github.com/sirupsen/logrus/hooks/writer"
)

type MyFormatter struct {
}

var configPathPtr = flag.String("c", "", "File path of config.yaml")
var logFile = flag.Bool("logfile", false, "log into file")

var GitCommit string
var BuildDate string

const Version string = "2.0"

type Runner interface {
	Init([]string) error
	Run() error
	Name() string
}

func main() {
	subCmds := []Runner{}
	if len(os.Args) > 1 {
		for _, cmd := range subCmds {
			if strings.EqualFold(os.Args[1], cmd.Name()) {
				err := cmd.Init(os.Args[2:])
				if err != nil {
					log.Error(err)
					return
				}
				_ = cmd.Run()
				return
			}
		}
	}

	flag.Parse()
	// config file
	if len(*configPathPtr) > 0 {
		if _, err := os.Stat(*configPathPtr); err == nil {
			config.ConfigFilePath = *configPathPtr
		} else {
			fmt.Println(fmt.Errorf("error in config file, file not exists: %s", *configPathPtr))
		}
	} else {
		fn := "./config.yaml"
		configPathPtr = &fn
		config.ConfigFilePath = *configPathPtr
		if _, err := os.Stat(*configPathPtr); err != nil {
			if os.IsNotExist(err) {
				if errW := os.WriteFile(fn, []byte{}, 0777); errW != nil {
					log.Fatal(errW)
				} else {
					fmt.Println(fmt.Sprintf("created config file: %s", config.ConfigFilePath))
				}
			}
			fmt.Println(fmt.Errorf("error in config file, file not exists: %s %s", *configPathPtr, err.Error()))
		}
	}

	fmt.Print(`

   /$$     /$$ /$$                                                                              
  | $$    |__/| $$                                                                              
 /$$$$$$   /$$| $$  /$$$$$$           /$$$$$$$  /$$$$$$   /$$$$$$  /$$    /$$ /$$$$$$   /$$$$$$ 
|_  $$_/  | $$| $$ /$$__  $$ /$$$$$$ /$$_____/ /$$__  $$ /$$__  $$|  $$  /$$//$$__  $$ /$$__  $$
  | $$    | $$| $$| $$$$$$$$|______/|  $$$$$$ | $$$$$$$$| $$  \__/ \  $$/$$/| $$$$$$$$| $$  \__/
  | $$ /$$| $$| $$| $$_____/         \____  $$| $$_____/| $$        \  $$$/ | $$_____/| $$      
  |  $$$$/| $$| $$|  $$$$$$$         /$$$$$$$/|  $$$$$$$| $$         \  $/  |  $$$$$$$| $$      
   \___/  |__/|__/ \_______/        |_______/  \_______/|__/          \_/    \_______/|__/      
                                                                                                
                                                                                    
`)

	fmt.Println(fmt.Sprintf("Start tile-server, Version:%s Date:%s Build:%s", Version, BuildDate, GitCommit))

	if *logFile {
		fn := "logrus.log"
		if _, err := os.Stat(fn); err == nil {
			log.Info("Backup log: " + fn)
			newPath := fmt.Sprintf("logrus_%s.log", time.Now().Format("20060102150405"))
			e := os.Rename(fn, newPath)
			if e != nil {
				log.Error(e)
			} else {
				log.Info("Backup log done:" + newPath)
			}
		}

		file, err := os.OpenFile(fn, os.O_CREATE|os.O_WRONLY, 0666)
		if err == nil {
			log.SetOutput(file)

			log.AddHook(&writer.Hook{ // Send logs with level higher than warning to stderr
				Writer: os.Stderr,
				LogLevels: []log.Level{
					log.PanicLevel,
					log.FatalLevel,
					log.ErrorLevel,
					log.WarnLevel,
				},
			})

			log.AddHook(&writer.Hook{ // Send info and debug logs to stdout
				Writer: os.Stdout,
				LogLevels: []log.Level{
					log.InfoLevel,
					log.DebugLevel,
				},
			})
		} else {
			log.Info("Failed to log to file, using default stderr")
		}
	}

	log.SetFormatter(&MyFormatter{})

	log.Println(fmt.Sprintf("Program start. Version %s Date:%s build:%s", Version, BuildDate, GitCommit))
	log.Println(fmt.Sprintf("Env config-path:%s", *configPathPtr))

	var cfg = config.Instance()
	cfg.AppVersion = Version
	cfg.BuildDate = BuildDate
	cfg.GitCommit = GitCommit
	if cfg.Debug {
		log.SetLevel(log.DebugLevel)
	}

	loadConfig()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc,
		syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
		syscall.SIGKILL)
OUTLOOP:
	for {
		select {
		case s := <-sigc:
			log.Info("signal received:" + s.String())
			break OUTLOOP
		case <-ticker.C:
		}
	}
	log.Println("exit.")
}

func (m *MyFormatter) Format(entry *log.Entry) ([]byte, error) {
	var b *bytes.Buffer
	if entry.Buffer != nil {
		b = entry.Buffer
	} else {
		b = &bytes.Buffer{}
	}

	timestamp := entry.Time.Local().Format("2006-01-02 15:04:05")
	var newLog string
	newLog = fmt.Sprintf("%s\t%s\t%s\n", timestamp, entry.Level, entry.Message)

	b.WriteString(newLog)
	return b.Bytes(), nil
}

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization, Range")
		c.Header("Access-Control-Expose-Headers", "Content-Length, Content-Range, Content-Disposition")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

func loadConfig() {
	// config file
	if _, err := os.Stat(*configPathPtr); err == nil {
		config.ConfigFilePath = *configPathPtr
	} else {
		log.Fatal(fmt.Errorf("error in config file, file not exists: %s", *configPathPtr))
	}

	var cfg = config.Instance()
	if cfg.Debug {
		log.SetLevel(log.DebugLevel)
	}

	configName := ""
	_, fn := filepath.Split(*configPathPtr)
	ext := filepath.Ext(fn)
	configName = strings.ReplaceAll(fn, ext, "")

	conn := pg.GetConn(configName)
	if conn == nil {
		log.Fatal("Error initial DB connection of project " + configName)
		return
	}
	retries := 0
	maxRetries := 10
	for {
		if !conn.Ready {
			time.Sleep(3 * time.Second)
			if retries += 1; retries > maxRetries {
				log.Fatal("Error Project DB connect failed")
			}
		} else {
			break
		}
	}

	dc := service.NewDataAccess(conn)

	r := gin.Default()

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

	r.Use(CORSMiddleware())

	r.StaticFile("/", "./dist/index.html")
	r.Static("/assets", "./dist/assets")
	r.Static("/cesium", "./dist/cesium")

	// 路网
	r.GET("tilesets/*filepath", getMergedTileSets)

	dc.ApiRegister(r, "api")

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

	go func() {
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
	}()
}
