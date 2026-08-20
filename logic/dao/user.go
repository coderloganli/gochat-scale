/**
 * Created by lock
 * Date: 2019-09-22
 * Time: 22:53
 */
package dao

import (
	"strings"
	"time"

	"gochat/db"

	"github.com/jinzhu/gorm"
	"github.com/pkg/errors"
)

// ErrUserNameTaken is returned when the user name is already registered. It is
// raised by the unique index on users.user_name, so two concurrent registrations
// of the same name cannot both succeed.
var ErrUserNameTaken = errors.New("user name already taken")

type User struct {
	Id         int `gorm:"primary_key"`
	UserName   string
	Password   string
	CreateTime time.Time
	db.DbGoChat
}

func (u *User) TableName() string {
	return "users"
}

// dbIns resolves the connection lazily. Resolving it in a package-level
// variable would bind it before db.Init has run.
func dbIns() *gorm.DB {
	return db.GetDb(db.DefaultDbName)
}

// Add inserts a new user. Password must already be hashed by the caller.
func (u *User) Add() (userId int, err error) {
	if u.UserName == "" || u.Password == "" {
		return 0, errors.New("user_name or password empty!")
	}
	u.CreateTime = time.Now()
	if err = dbIns().Table(u.TableName()).Create(u).Error; err != nil {
		if isUniqueViolation(err) {
			return 0, ErrUserNameTaken
		}
		return 0, errors.Wrap(err, "create user")
	}
	return u.Id, nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key value")
}

func (u *User) CheckHaveUserName(userName string) (data User, err error) {
	err = dbIns().Table(u.TableName()).Where("user_name = ?", userName).Take(&data).Error
	if gorm.IsRecordNotFoundError(err) {
		return data, nil
	}
	if err != nil {
		return data, errors.Wrap(err, "query user by name")
	}
	return data, nil
}

func (u *User) GetUserNameByUserId(userId int) (userName string, err error) {
	var data User
	err = dbIns().Table(u.TableName()).Where("id = ?", userId).Take(&data).Error
	if gorm.IsRecordNotFoundError(err) {
		return "", nil
	}
	if err != nil {
		return "", errors.Wrap(err, "query user by id")
	}
	return data.UserName, nil
}
