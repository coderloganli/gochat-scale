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

	"github.com/jinzhu/gorm"
	_ "github.com/jinzhu/gorm/dialects/postgres"
	"github.com/sirupsen/logrus"
	"gochat/config"
)

var dbMap = map[string]*gorm.DB{}
var syncLock sync.Mutex

func init() {
	initDB("gochat")
}

// User struct for auto-migration (avoids circular import with dao package)
type User struct {
	Id         int       `gorm:"primary_key;auto_increment"`
	UserName   string    `gorm:"type:varchar(255);unique_index"`
	Password   string    `gorm:"type:varchar(255)"`
	CreateTime time.Time
}

func (User) TableName() string {
	return "user"
}

// Message struct for auto-migration (avoids circular import with dao package)
type Message struct {
	Id           int       `gorm:"primary_key;auto_increment"`
	FromUserId   int       `gorm:"index"`
	FromUserName string    `gorm:"type:varchar(255)"`
	ToUserId     int       `gorm:"index"`
	ToUserName   string    `gorm:"type:varchar(255)"`
	RoomId       int       `gorm:"index"`
	MessageType  int
	Content      string    `gorm:"type:text"`
	ContentType  string    `gorm:"type:varchar(20);default:'text'"`
	CreateTime   time.Time `gorm:"index"`
}

func (Message) TableName() string {
	return "message"
}

func initDB(dbName string) {
	var err error
	pgConfig := config.Conf.Common.CommonPostgreSQL

	// Build PostgreSQL connection string
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		pgConfig.Host,
		pgConfig.Port,
		pgConfig.User,
		pgConfig.Password,
		pgConfig.DBName,
		pgConfig.SSLMode,
	)

	syncLock.Lock()
	defer syncLock.Unlock()

	dbMap[dbName], err = gorm.Open("postgres", dsn)
	if err != nil {
		logrus.Errorf("connect postgresql fail:%s", err.Error())
		return
	}

	// Configure connection pool
	maxIdleConns := pgConfig.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = 10
	}
	maxOpenConns := pgConfig.MaxOpenConns
	if maxOpenConns <= 0 {
		maxOpenConns = 100
	}
	connMaxLifetime := pgConfig.ConnMaxLifetime
	if connMaxLifetime <= 0 {
		connMaxLifetime = 3600
	}

	dbMap[dbName].DB().SetMaxIdleConns(maxIdleConns)
	dbMap[dbName].DB().SetMaxOpenConns(maxOpenConns)
	dbMap[dbName].DB().SetConnMaxLifetime(time.Duration(connMaxLifetime) * time.Second)

	if config.GetMode() == "dev" {
		dbMap[dbName].LogMode(true)
	}

	// Auto-migrate tables
	if err := dbMap[dbName].AutoMigrate(&User{}, &Message{}).Error; err != nil {
		logrus.Errorf("auto migrate tables fail:%s", err.Error())
	}

	logrus.Infof("postgresql connected successfully: %s:%d/%s", pgConfig.Host, pgConfig.Port, pgConfig.DBName)
}

func GetDb(dbName string) (db *gorm.DB) {
	if db, ok := dbMap[dbName]; ok {
		return db
	} else {
		return nil
	}
}

type DbGoChat struct {
}

func (*DbGoChat) GetDbName() string {
	return "gochat"
}
