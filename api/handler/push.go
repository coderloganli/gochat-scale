/**
 * Created by lock
 * Date: 2019-10-06
 * Time: 23:40
 */
package handler

import (
	"strconv"

	"gochat/api/ctxutil"
	"gochat/api/rpc"
	"gochat/config"
	"gochat/proto"
	"gochat/tools"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

type FormPush struct {
	Msg         string `form:"msg" json:"msg" binding:"required"`
	ToUserId    string `form:"toUserId" json:"toUserId" binding:"required"`
	RoomId      int    `form:"roomId" json:"roomId" binding:"required"`
	AuthToken   string `form:"authToken" json:"authToken" binding:"required"`
	ContentType string `form:"contentType" json:"contentType"` // "text" or "image", defaults to "text"
}

func Push(c *gin.Context) {
	var formPush FormPush
	if err := c.ShouldBindBodyWith(&formPush, binding.JSON); err != nil {
		tools.FailWithMsg(c, err.Error())
		return
	}
	ctx := c.Request.Context()
	msg := formPush.Msg
	toUserId := formPush.ToUserId
	toUserIdInt, _ := strconv.Atoi(toUserId)
	getUserNameReq := &proto.GetUserInfoRequest{UserId: toUserIdInt}
	code, toUserName := rpc.RpcLogicObj.GetUserNameByUserId(ctx, getUserNameReq)
	if code == tools.CodeFail {
		tools.FailWithMsg(c, "rpc fail get friend userName")
		return
	}
	// Reuse auth info from middleware instead of making another RPC call
	fromUserId, fromUserName, ok := ctxutil.GetAuthFromContext(c)
	if !ok {
		tools.FailWithMsg(c, "auth info not found in context")
		return
	}
	roomId := formPush.RoomId
	contentType := formPush.ContentType
	if contentType == "" {
		contentType = config.ContentTypeText
	}
	req := &proto.Send{
		Msg:          msg,
		FromUserId:   fromUserId,
		FromUserName: fromUserName,
		ToUserId:     toUserIdInt,
		ToUserName:   toUserName,
		RoomId:       roomId,
		Op:           config.OpSingleSend,
		ContentType:  contentType,
	}
	code, rpcMsg := rpc.RpcLogicObj.Push(ctx, req)
	if code == tools.CodeFail {
		tools.FailWithMsg(c, rpcMsg)
		return
	}
	tools.SuccessWithMsg(c, "ok", nil)
	return
}

type FormRoom struct {
	AuthToken   string `form:"authToken" json:"authToken" binding:"required"`
	Msg         string `form:"msg" json:"msg" binding:"required"`
	RoomId      int    `form:"roomId" json:"roomId" binding:"required"`
	ContentType string `form:"contentType" json:"contentType"` // "text" or "image", defaults to "text"
}

func PushRoom(c *gin.Context) {
	var formRoom FormRoom
	if err := c.ShouldBindBodyWith(&formRoom, binding.JSON); err != nil {
		tools.FailWithMsg(c, err.Error())
		return
	}
	ctx := c.Request.Context()
	msg := formRoom.Msg
	roomId := formRoom.RoomId
	// Reuse auth info from middleware instead of making another RPC call
	fromUserId, fromUserName, ok := ctxutil.GetAuthFromContext(c)
	if !ok {
		tools.FailWithMsg(c, "auth info not found in context")
		return
	}
	contentType := formRoom.ContentType
	if contentType == "" {
		contentType = config.ContentTypeText
	}
	req := &proto.Send{
		Msg:          msg,
		FromUserId:   fromUserId,
		FromUserName: fromUserName,
		RoomId:       roomId,
		Op:           config.OpRoomSend,
		ContentType:  contentType,
	}
	code, msg := rpc.RpcLogicObj.PushRoom(ctx, req)
	if code == tools.CodeFail {
		tools.FailWithMsg(c, "rpc push room msg fail!")
		return
	}
	tools.SuccessWithMsg(c, "ok", msg)
	return
}

type FormCount struct {
	RoomId int `form:"roomId" json:"roomId" binding:"required"`
}

func Count(c *gin.Context) {
	var formCount FormCount
	if err := c.ShouldBindBodyWith(&formCount, binding.JSON); err != nil {
		tools.FailWithMsg(c, err.Error())
		return
	}
	ctx := c.Request.Context()
	roomId := formCount.RoomId
	req := &proto.Send{
		RoomId: roomId,
		Op:     config.OpRoomCountSend,
	}
	code, msg := rpc.RpcLogicObj.Count(ctx, req)
	if code == tools.CodeFail {
		tools.FailWithMsg(c, "rpc get room count fail!")
		return
	}
	tools.SuccessWithMsg(c, "ok", msg)
	return
}

type FormRoomInfo struct {
	RoomId int `form:"roomId" json:"roomId" binding:"required"`
}

func GetRoomInfo(c *gin.Context) {
	var formRoomInfo FormRoomInfo
	if err := c.ShouldBindBodyWith(&formRoomInfo, binding.JSON); err != nil {
		tools.FailWithMsg(c, err.Error())
		return
	}
	ctx := c.Request.Context()
	roomId := formRoomInfo.RoomId
	req := &proto.Send{
		RoomId: roomId,
		Op:     config.OpRoomInfoSend,
	}
	code, msg := rpc.RpcLogicObj.GetRoomInfo(ctx, req)
	if code == tools.CodeFail {
		tools.FailWithMsg(c, "rpc get room info fail!")
		return
	}
	tools.SuccessWithMsg(c, "ok", msg)
	return
}
