/**
 * Message history handlers
 */
package handler

import (
	"gochat/api/ctxutil"
	"gochat/api/rpc"
	"gochat/proto"
	"gochat/tools"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

type FormSingleChatHistory struct {
	AuthToken   string `form:"authToken" json:"authToken" binding:"required"`
	OtherUserId int    `form:"otherUserId" json:"otherUserId" binding:"required"`
	Limit       int    `form:"limit" json:"limit"`
	Offset      int    `form:"offset" json:"offset"`
}

// GetSingleChatHistory retrieves message history between the current user and another user
func GetSingleChatHistory(c *gin.Context) {
	var form FormSingleChatHistory
	if err := c.ShouldBindBodyWith(&form, binding.JSON); err != nil {
		tools.FailWithMsg(c, err.Error())
		return
	}

	// Get current user from context (set by auth middleware)
	currentUserId, _, ok := ctxutil.GetAuthFromContext(c)
	if !ok {
		tools.FailWithMsg(c, "auth info not found in context")
		return
	}

	ctx := c.Request.Context()
	req := &proto.GetSingleChatHistoryRequest{
		CurrentUserId: currentUserId,
		OtherUserId:   form.OtherUserId,
		Limit:         form.Limit,
		Offset:        form.Offset,
	}

	code, messages := rpc.RpcLogicObj.GetSingleChatHistory(ctx, req)
	if code == tools.CodeFail {
		tools.FailWithMsg(c, "rpc get single chat history fail!")
		return
	}
	tools.SuccessWithMsg(c, "ok", messages)
}

type FormRoomHistory struct {
	AuthToken string `form:"authToken" json:"authToken" binding:"required"`
	RoomId    int    `form:"roomId" json:"roomId" binding:"required"`
	Limit     int    `form:"limit" json:"limit"`
	Offset    int    `form:"offset" json:"offset"`
}

// GetRoomHistory retrieves message history for a room
func GetRoomHistory(c *gin.Context) {
	var form FormRoomHistory
	if err := c.ShouldBindBodyWith(&form, binding.JSON); err != nil {
		tools.FailWithMsg(c, err.Error())
		return
	}

	ctx := c.Request.Context()
	req := &proto.GetRoomHistoryRequest{
		RoomId: form.RoomId,
		Limit:  form.Limit,
		Offset: form.Offset,
	}

	code, messages := rpc.RpcLogicObj.GetRoomHistory(ctx, req)
	if code == tools.CodeFail {
		tools.FailWithMsg(c, "rpc get room history fail!")
		return
	}
	tools.SuccessWithMsg(c, "ok", messages)
}
