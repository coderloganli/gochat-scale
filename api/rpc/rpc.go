/**
 * Created by lock
 * Date: 2019-10-06
 * Time: 22:46
 */
package rpc

import (
	"context"
	"sync"
	"time"

	"gochat/config"
	"gochat/pkg/middleware"
	"gochat/proto"

	"github.com/rpcxio/libkv/store"
	etcdV3 "github.com/rpcxio/rpcx-etcd/client"
	"github.com/sirupsen/logrus"
	"github.com/smallnest/rpcx/client"
	"github.com/smallnest/rpcx/protocol"
)

var LogicRpcClient client.XClient
var once sync.Once

type RpcLogic struct {
}

var RpcLogicObj *RpcLogic

func InitLogicRpcClient() {
	once.Do(func() {
		etcdConfigOption := &store.Config{
			ClientTLS:         nil,
			TLS:               nil,
			ConnectionTimeout: time.Duration(config.Conf.Common.CommonEtcd.ConnectionTimeout) * time.Second,
			Bucket:            "",
			PersistConnection: true,
			Username:          config.Conf.Common.CommonEtcd.UserName,
			Password:          config.Conf.Common.CommonEtcd.Password,
		}
		d, err := etcdV3.NewEtcdV3Discovery(
			config.Conf.Common.CommonEtcd.BasePath,
			config.Conf.Common.CommonEtcd.ServerPathLogic,
			[]string{config.Conf.Common.CommonEtcd.Host},
			true,
			etcdConfigOption,
		)
		if err != nil {
			logrus.Fatalf("init connect rpc etcd discovery client fail:%s", err.Error())
		}
		// Optimized client options for better connection reuse
		opt := client.Option{
			Retries:             3,
			ConnectTimeout:      500 * time.Millisecond, // Faster connection timeout
			IdleTimeout:         0,                      // No idle timeout, keep connections alive
			Heartbeat:           true,                   // Enable heartbeat to keep connections alive
			HeartbeatInterval:   10 * time.Second,       // Heartbeat every 10s
			MaxWaitForHeartbeat: 30 * time.Second,
			TCPKeepAlivePeriod:  30 * time.Second,       // TCP keepalive
			BackupLatency:       10 * time.Millisecond,
			SerializeType:       protocol.MsgPack,       // Use MsgPack serialization
			CompressType:        protocol.None,          // No compression for speed
		}
		LogicRpcClient = client.NewXClient(config.Conf.Common.CommonEtcd.ServerPathLogic, client.Failtry, client.RandomSelect, d, opt)
		RpcLogicObj = new(RpcLogic)
	})
	if LogicRpcClient == nil {
		logrus.Fatalf("get logic rpc client nil")
	}
}

func (rpc *RpcLogic) Login(ctx context.Context, req *proto.LoginRequest) (code int, authToken string, msg string) {
	reply := &proto.LoginResponse{}
	err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "Login", req, reply)
	if err != nil {
		return config.FailReplyCode, "", err.Error()
	}
	code = reply.Code
	authToken = reply.AuthToken
	return
}

func (rpc *RpcLogic) Register(ctx context.Context, req *proto.RegisterRequest) (code int, authToken string, msg string) {
	reply := &proto.RegisterReply{}
	err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "Register", req, reply)
	if err != nil {
		return config.FailReplyCode, "", err.Error()
	}
	code = reply.Code
	authToken = reply.AuthToken
	return
}

func (rpc *RpcLogic) GetUserNameByUserId(ctx context.Context, req *proto.GetUserInfoRequest) (code int, userName string) {
	reply := &proto.GetUserInfoResponse{}
	err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "GetUserInfoByUserId", req, reply)
	if err != nil {
		return config.FailReplyCode, ""
	}
	code = reply.Code
	userName = reply.UserName
	return
}

func (rpc *RpcLogic) CheckAuth(ctx context.Context, req *proto.CheckAuthRequest) (code int, userId int, userName string) {
	reply := &proto.CheckAuthResponse{}
	err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "CheckAuth", req, reply)
	if err != nil {
		return config.FailReplyCode, 0, ""
	}
	code = reply.Code
	userId = reply.UserId
	userName = reply.UserName
	return
}

func (rpc *RpcLogic) Logout(ctx context.Context, req *proto.LogoutRequest) (code int) {
	reply := &proto.LogoutResponse{}
	err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "Logout", req, reply)
	if err != nil {
		return config.FailReplyCode
	}
	code = reply.Code
	return
}

func (rpc *RpcLogic) Push(ctx context.Context, req *proto.Send) (code int, msg string) {
	reply := &proto.SuccessReply{}
	err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "Push", req, reply)
	if err != nil {
		return config.FailReplyCode, err.Error()
	}
	code = reply.Code
	msg = reply.Msg
	return
}

func (rpc *RpcLogic) PushRoom(ctx context.Context, req *proto.Send) (code int, msg string) {
	reply := &proto.SuccessReply{}
	err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "PushRoom", req, reply)
	if err != nil {
		return config.FailReplyCode, err.Error()
	}
	code = reply.Code
	msg = reply.Msg
	return
}

func (rpc *RpcLogic) Count(ctx context.Context, req *proto.Send) (code int, msg string) {
	reply := &proto.SuccessReply{}
	err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "Count", req, reply)
	if err != nil {
		return config.FailReplyCode, err.Error()
	}
	code = reply.Code
	msg = reply.Msg
	return
}

func (rpc *RpcLogic) GetRoomInfo(ctx context.Context, req *proto.Send) (code int, msg string) {
	reply := &proto.SuccessReply{}
	err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "GetRoomInfo", req, reply)
	if err != nil {
		return config.FailReplyCode, err.Error()
	}
	code = reply.Code
	msg = reply.Msg
	return
}

func (rpc *RpcLogic) GetSingleChatHistory(ctx context.Context, req *proto.GetSingleChatHistoryRequest) (code int, messages []proto.MessageItem) {
	reply := &proto.GetMessageHistoryResponse{}
	err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "GetSingleChatHistory", req, reply)
	if err != nil {
		return config.FailReplyCode, nil
	}
	code = reply.Code
	messages = reply.Messages
	return
}

func (rpc *RpcLogic) GetRoomHistory(ctx context.Context, req *proto.GetRoomHistoryRequest) (code int, messages []proto.MessageItem) {
	reply := &proto.GetMessageHistoryResponse{}
	err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "GetRoomHistory", req, reply)
	if err != nil {
		return config.FailReplyCode, nil
	}
	code = reply.Code
	messages = reply.Messages
	return
}
