package pg

import (
	"cesium-tileset-tool/config"
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/enriquebris/goconcurrentqueue"
	log "github.com/sirupsen/logrus"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var store = make(map[string]*PGConn)
var locker = sync.Mutex{}

type PGConn struct {
	Ready  bool
	Schema string
	DBName string

	Cfg       *config.Config
	DB        *gorm.DB
	sqlDB     *sql.DB
	cancelFun context.CancelFunc
	locker    sync.Mutex
	tableLock sync.RWMutex
	tables    map[string]bool
	dataQueue *goconcurrentqueue.FIFO
}

func (s *PGConn) run() {
	ctx, cfun := context.WithCancel(context.Background())
	s.cancelFun = cfun
	errP := s.initDB()
	if errP != nil {
		panic(errP)
	}

	const maxRetries int = 20
	s.startUpdate(ctx)
	s.fetchTables()
	s.test()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				s.Close()
				return
			case <-ticker.C:
				s.test()

				if s.Ready {
					if err := s.DB.Raw("SELECT 1").Error; err != nil {
						log.Infof("Database %s connection lost, reconnecting...", s.Cfg.Name)
						// 重建连接或处理连接丢失的逻辑
						if err = s.initDB(); err != nil {
							log.Error(err)
						}
					}
				} else {
					if err := s.initDB(); err != nil {
						log.Error(err)
					}
				}
			}
		}
	}()
}

func (s *PGConn) test() {
	if s.sqlDB != nil {
		if err := s.sqlDB.Ping(); err != nil {
			s.Ready = false
			log.Errorf("DB %s ping fail", s.Cfg.Name)
		} else {
			s.Ready = true
			log.Infof("DB %s ping ok", s.Cfg.Name)
		}
	}
}

func (s *PGConn) Close() {
	log.Infof("PGConn close")
	if s.sqlDB != nil {
		_ = s.sqlDB.Close()
	}
}

func (s *PGConn) fetchTables() {
	// 假设 DB 是 *gorm.DB 类型
	tables, err := s.DB.Migrator().GetTables()
	if err != nil {
		log.Errorf("Database %s 获取表列表失败: %s", s.Cfg.Name, err.Error())
		return
	}
	s.tableLock.Lock()
	defer s.tableLock.Unlock()
	for _, table := range tables {
		s.tables[table] = true
	}
}

func (s *PGConn) ContainsTable(name string) bool {
	s.tableLock.RLock()
	defer s.tableLock.RUnlock()
	return s.tables[name]
}

func (s *PGConn) initDB() error {
	s.locker.Lock()
	defer s.locker.Unlock()
	log.Infof("DB %s init", s.Cfg.Name)

	if s.sqlDB != nil {
		_ = s.sqlDB.Close()
	}

	s.DBName = s.Cfg.Postgres.DbName
	s.Schema = s.Cfg.Postgres.Schema

	dsn := fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=disable TimeZone=Asia/Shanghai statement_timeout=10000 search_path=%s",
		s.Cfg.Postgres.Host, s.Cfg.Postgres.User, s.Cfg.Postgres.Pass, s.Cfg.Postgres.DbName, s.Cfg.Postgres.Port, s.Cfg.Postgres.Schema)
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	// 设置连接的最大生命周期
	sqlDB.SetConnMaxLifetime(time.Hour)

	// 设置最大空闲连接数
	sqlDB.SetMaxIdleConns(10)

	// 设置最大打开连接数
	sqlDB.SetMaxOpenConns(100)

	s.DB = db
	s.sqlDB = sqlDB

	return nil
}

func (s *PGConn) startUpdate(ctx context.Context) {
	go func() {
		log.Infof("DB %s start Update", s.Cfg.Name)
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				log.Infof("DB %s exit DB Update", s.Cfg.Name)
				return
			case <-ticker.C:
				if s.dataQueue.GetLen() > 0 {
				}
			}
		}
	}()
}
