/**
 * Created by lock
 * Date: 2019-09-22
 * Time: 22:37
 */
package db

import (
	"fmt"
	"sync"
	"time"

	"gochat/config"

	"github.com/jinzhu/gorm"
	// PostgreSQL dialect for GORM.
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	// DefaultDbName is the logical name used by the application to look up its
	// connection. It is not the PostgreSQL database name, which comes from config.
	DefaultDbName = "gochat"

	connectRetries = 10
	retryInterval  = 2 * time.Second
)

var (
	dbMap    = map[string]*gorm.DB{}
	syncLock sync.Mutex
)

// Init opens the connection pool and verifies the database is reachable.
// It is called explicitly by the services that need a database (logic), rather
// than from an init function, so that packages importing this one for its types
// do not open a connection as a side effect.
func Init() error {
	syncLock.Lock()
	defer syncLock.Unlock()
	if _, ok := dbMap[DefaultDbName]; ok {
		return nil
	}

	cfg := config.Conf.Common.CommonDB
	conn, err := connectWithRetry(cfg)
	if err != nil {
		return err
	}

	sqlDB := conn.DB()
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)
	conn.LogMode(config.GetMode() == "dev")

	dbMap[DefaultDbName] = conn
	logrus.Infof("connected to postgres %s:%d/%s", cfg.Host, cfg.Port, cfg.DbName)
	return nil
}

// connectWithRetry waits for the database to accept connections. Under Docker
// Compose or Kubernetes the database may still be starting when a service comes
// up, and a healthcheck on the database alone does not guarantee it is ready by
// the time the first query runs.
func connectWithRetry(cfg config.CommonDB) (*gorm.DB, error) {
	dsn := buildDSN(cfg)
	var lastErr error
	for attempt := 1; attempt <= connectRetries; attempt++ {
		conn, err := gorm.Open("postgres", dsn)
		if err == nil {
			if err = conn.DB().Ping(); err == nil {
				return conn, nil
			}
			_ = conn.Close()
		}
		lastErr = err
		logrus.Warnf("connect db failed (attempt %d/%d): %v", attempt, connectRetries, err)
		time.Sleep(retryInterval)
	}
	return nil, errors.Wrap(lastErr, "connect db")
}

func buildDSN(cfg config.CommonDB) string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.DbName, cfg.SslMode,
	)
}

// GetDb returns the connection pool for a logical database name, or nil if Init
// has not run successfully.
func GetDb(dbName string) *gorm.DB {
	syncLock.Lock()
	defer syncLock.Unlock()
	return dbMap[dbName]
}

// Close releases the connection pool. Used on shutdown and by tests.
func Close() error {
	syncLock.Lock()
	defer syncLock.Unlock()
	var err error
	for name, conn := range dbMap {
		if cerr := conn.Close(); cerr != nil {
			err = cerr
		}
		delete(dbMap, name)
	}
	return err
}

type DbGoChat struct {
}

func (*DbGoChat) GetDbName() string {
	return DefaultDbName
}
