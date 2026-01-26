/**
 * Message DAO for persisting chat messages
 */
package dao

import (
	"time"

	"gochat/db"
)

// Message represents a chat message stored in the database
type Message struct {
	Id           int       `gorm:"primary_key;auto_increment"`
	FromUserId   int       `gorm:"index"`
	FromUserName string
	ToUserId     int       `gorm:"index"` // 0 for room messages
	ToUserName   string
	RoomId       int       `gorm:"index"` // 0 for single messages
	MessageType  int       // OpSingleSend (2) or OpRoomSend (3)
	Content      string    `gorm:"type:text"`
	CreateTime   time.Time `gorm:"index"`
	db.DbGoChat
}

func (m *Message) TableName() string {
	return "message"
}

// Add inserts a new message into the database
func (m *Message) Add() (messageId int, err error) {
	if m.CreateTime.IsZero() {
		m.CreateTime = time.Now()
	}
	if err = dbIns.Table(m.TableName()).Create(&m).Error; err != nil {
		return 0, err
	}
	return m.Id, nil
}

// GetSingleChatHistory retrieves message history between two users
func (m *Message) GetSingleChatHistory(userId1, userId2, limit, offset int) (messages []Message, err error) {
	// Get messages where (from=userId1 AND to=userId2) OR (from=userId2 AND to=userId1)
	// and RoomId=0 (single chat messages only)
	err = dbIns.Table(m.TableName()).
		Where("((from_user_id = ? AND to_user_id = ?) OR (from_user_id = ? AND to_user_id = ?)) AND room_id = 0",
			userId1, userId2, userId2, userId1).
		Order("create_time DESC").
		Limit(limit).
		Offset(offset).
		Find(&messages).Error
	return
}

// GetRoomHistory retrieves message history for a room
func (m *Message) GetRoomHistory(roomId, limit, offset int) (messages []Message, err error) {
	err = dbIns.Table(m.TableName()).
		Where("room_id = ?", roomId).
		Order("create_time DESC").
		Limit(limit).
		Offset(offset).
		Find(&messages).Error
	return
}
