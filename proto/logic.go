/**
 * Created by lock
 * Date: 2019-08-10
 * Time: 18:38
 */
package proto

type LoginRequest struct {
	Name     string
	Password string
}

type LoginResponse struct {
	Code      int
	AuthToken string
}

type GetUserInfoRequest struct {
	UserId int
}

type GetUserInfoResponse struct {
	Code     int
	UserId   int
	UserName string
}

type RegisterRequest struct {
	Name     string
	Password string
}

type RegisterReply struct {
	Code      int
	AuthToken string
}

type LogoutRequest struct {
	AuthToken string
}

type LogoutResponse struct {
	Code int
}

type CheckAuthRequest struct {
	AuthToken string
}

type CheckAuthResponse struct {
	Code     int
	UserId   int
	UserName string
}

type ConnectRequest struct {
	AuthToken string `json:"authToken"`
	RoomId    int    `json:"roomId"`
	ServerId  string `json:"serverId"`
}

type ConnectReply struct {
	UserId int
}

type DisConnectRequest struct {
	RoomId int
	UserId int
}

type DisConnectReply struct {
	Has bool
}

type Send struct {
	Code         int    `json:"code"`
	Msg          string `json:"msg"`
	FromUserId   int    `json:"fromUserId"`
	FromUserName string `json:"fromUserName"`
	ToUserId     int    `json:"toUserId"`
	ToUserName   string `json:"toUserName"`
	RoomId       int    `json:"roomId"`
	Op           int    `json:"op"`
	CreateTime   string `json:"createTime"`
	ContentType  string `json:"contentType,omitempty"` // "text" or "image"
}

type SendTcp struct {
	Code         int    `json:"code"`
	Msg          string `json:"msg"`
	FromUserId   int    `json:"fromUserId"`
	FromUserName string `json:"fromUserName"`
	ToUserId     int    `json:"toUserId"`
	ToUserName   string `json:"toUserName"`
	RoomId       int    `json:"roomId"`
	Op           int    `json:"op"`
	CreateTime   string `json:"createTime"`
	AuthToken    string `json:"authToken"`            // TCP only, include when sending msg
	ContentType  string `json:"contentType,omitempty"` // "text" or "image"
}

// GetSingleChatHistoryRequest is the request for retrieving single chat history
type GetSingleChatHistoryRequest struct {
	CurrentUserId int `json:"currentUserId"` // The current user's ID
	OtherUserId   int `json:"otherUserId"`   // The other user's ID
	Limit         int `json:"limit"`
	Offset        int `json:"offset"`
}

// GetRoomHistoryRequest is the request for retrieving room chat history
type GetRoomHistoryRequest struct {
	RoomId int `json:"roomId"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// MessageItem represents a single message in the history response
type MessageItem struct {
	Id           int    `json:"id"`
	FromUserId   int    `json:"fromUserId"`
	FromUserName string `json:"fromUserName"`
	ToUserId     int    `json:"toUserId"`
	ToUserName   string `json:"toUserName"`
	RoomId       int    `json:"roomId"`
	Content      string `json:"content"`
	ContentType  string `json:"contentType"`
	CreateTime   string `json:"createTime"`
}

// GetMessageHistoryResponse is the response for message history requests
type GetMessageHistoryResponse struct {
	Code     int           `json:"code"`
	Messages []MessageItem `json:"messages"`
}
