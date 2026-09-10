package pg

import (
	"cesium-tileset-tool/common"
	"cesium-tileset-tool/config"
	"errors"
	"fmt"
	"net/http"

	"github.com/enriquebris/goconcurrentqueue"
	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

func GetConn(cfgName string) *PGConn {
	locker.Lock()
	defer locker.Unlock()
	if dbc, ok := store[cfgName]; ok {
		return dbc
	} else {
		if cfg, err := config.GetNetworkConfigByName(cfgName); err != nil {
			log.Error(err)
			return nil
		} else {
			conn := newConn(cfg)
			return conn
		}
	}
}

func RemoveConn(cfgName string) {
	locker.Lock()
	defer locker.Unlock()
	if dbc, ok := store[cfgName]; ok {
		dbc.Close()
		delete(store, cfgName)
	}
}

func newConn(cfg *config.Config) *PGConn {
	if dbc, ok := store[cfg.Name]; ok {
		return dbc
	}

	conn := &PGConn{
		Cfg:       cfg,
		tables:    make(map[string]bool),
		dataQueue: goconcurrentqueue.NewFIFO(),
	}

	conn.run()

	store[cfg.Name] = conn
	return conn
}

// 从context中取数据库连接
func GormDB(c *gin.Context) *gorm.DB {
	if db, ok := c.Get(common.DBName); ok {
		if geom, okk := db.(*PGConn); okk && geom.DB != nil {
			return geom.DB
		}
	}
	c.JSON(http.StatusForbidden, gin.H{"msg": fmt.Sprintf("database connection not exist.")})
	c.Abort()
	return nil
}

// 从context中取数据库连接
func PubDB(c *gin.Context) *gorm.DB {
	if db, ok := c.Get(common.PubDBName); ok {
		if geom, okk := db.(*PGConn); okk && geom.DB != nil {
			return geom.DB
		}
	}
	c.JSON(http.StatusForbidden, gin.H{"msg": fmt.Sprintf("public connection not exist.")})
	c.Abort()
	return nil
}

// 从context中取数据库表名
func GormTableName(c *gin.Context) string {
	if tn, ok := c.Get(common.TableName); ok {
		if name, okk := tn.(string); okk {
			return name
		}
	}
	return ""
}

// 从context中取数据库配置
func CheckAndSetConfigConn(c *gin.Context, defaultConfigName string) (string, error) {
	pubDbc := GetConn(defaultConfigName)
	if pubDbc == nil {
		c.JSON(http.StatusForbidden, gin.H{"msg": fmt.Sprintf("public database %s not exist", defaultConfigName)})
		c.Abort()
		return "", errors.New(fmt.Sprintf("public database %s not exist", defaultConfigName))
	}
	c.Set(common.PubDBName, pubDbc)

	code := c.GetHeader(common.ConfigContextCode)
	if len(code) > 0 {
		configName, tableName, errD := config.DecodeContext(code)
		if errD != nil {
			c.JSON(http.StatusForbidden, gin.H{"msg": fmt.Sprintf("tableId decode error " + errD.Error())})
			c.Abort()
			return "", errors.New(fmt.Sprintf("tableId decode error " + errD.Error()))
		}
		if configName == "" {
			c.JSON(http.StatusForbidden, gin.H{"msg": fmt.Sprintf("configName is empty")})
			c.Abort()
			return "", errors.New(fmt.Sprintf("configName is empty"))
		}
		log.Infof("Request config config:%s table:%s", configName, tableName)
		dbc := GetConn(configName)
		if dbc == nil {
			c.JSON(http.StatusForbidden, gin.H{"msg": fmt.Sprintf("database %s not exist", configName)})
			c.Abort()
			return "", errors.New(fmt.Sprintf("database %s not exist", configName))
		}
		c.Set(common.ConfigName, configName)
		c.Set(common.DBName, dbc)
		c.Set(common.TableName, tableName)
		return configName, nil
	} else {
		dbc := GetConn(defaultConfigName)
		log.Infof("Request config %s", defaultConfigName)
		if dbc == nil {
			c.JSON(http.StatusForbidden, gin.H{"msg": fmt.Sprintf("database %s not exist", defaultConfigName)})
			c.Abort()
			return "", errors.New(fmt.Sprintf("database %s not exist", defaultConfigName))
		}
		c.Set(common.ConfigName, defaultConfigName)
		c.Set(common.DBName, dbc)
		return defaultConfigName, nil
	}
}
