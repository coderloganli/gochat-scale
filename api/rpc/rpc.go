/**
 * Created by lock
 * Date: 2019-10-06
 * Time: 22:46
 */
package rpc

import (
	"context"
	"errors"
	"sync"
	"time"

	"gochat/config"
	"gochat/pkg/middleware"
	"gochat/proto"
	"gochat/tools"

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
			TCPKeepAlivePeriod:  30 * time.Second, // TCP keepalive
			BackupLatency:       10 * time.Millisecond,
			SerializeType:       protocol.MsgPack, // Use MsgPack serialization
			CompressType:        protocol.None,    // No compression for speed
		}
		LogicRpcClient = client.NewXClient(config.Conf.Common.CommonEtcd.ServerPathLogic, client.Failtry, client.RandomSelect, d, opt)
		RpcLogicObj = new(RpcLogic)
	})
	if LogicRpcClient == nil {
		logrus.Fatalf("get logic rpc client nil")
	}
}

// callFailureCode classifies a failed RPC.
//
// A ServiceError is the remote method returning an error: the call worked, the
// request did not. That is a normal outcome and stays a 200 with a failure code,
// exactly as before. Anything else — a timeout, no reachable instance, a
// transport error — means this service could not do its job, and is reported as
// unavailable so that it is visible as a 503 rather than counted as a success.
func callFailureCode(err error) int {
	var svcErr client.ServiceError
	if errors.As(err, &svcErr) {
		return tools.CodeFail
	}
	return tools.CodeUnavailable
}

// Every wrapper below must check the call error. rpcx leaves reply untouched
// when a call fails, and the zero value of Code is CodeSuccess, so dropping the
// error would report a failed call as a successful one.
func (rpc *RpcLogic) Login(ctx context.Context, req *proto.LoginRequest) (code int, authToken string, msg string) {
	reply := &proto.LoginResponse{}
	if err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "Login", req, reply); err != nil {
		logrus.Errorf("api call logic Login failed: %v", err)
		return callFailureCode(err), "", err.Error()
	}
	code = reply.Code
	authToken = reply.AuthToken
	return
}

func (rpc *RpcLogic) Register(ctx context.Context, req *proto.RegisterRequest) (code int, authToken string, msg string) {
	reply := &proto.RegisterReply{}
	if err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "Register", req, reply); err != nil {
		logrus.Errorf("api call logic Register failed: %v", err)
		return callFailureCode(err), "", err.Error()
	}
	code = reply.Code
	authToken = reply.AuthToken
	return
}

func (rpc *RpcLogic) GetUserNameByUserId(ctx context.Context, req *proto.GetUserInfoRequest) (code int, userName string) {
	reply := &proto.GetUserInfoResponse{}
	if err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "GetUserInfoByUserId", req, reply); err != nil {
		logrus.Errorf("api call logic GetUserInfoByUserId failed: %v", err)
		code = callFailureCode(err)
		return
	}
	code = reply.Code
	userName = reply.UserName
	return
}

func (rpc *RpcLogic) CheckAuth(ctx context.Context, req *proto.CheckAuthRequest) (code int, userId int, userName string) {
	reply := &proto.CheckAuthResponse{}
	if err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "CheckAuth", req, reply); err != nil {
		logrus.Errorf("api call logic CheckAuth failed: %v", err)
		code = callFailureCode(err)
		return
	}
	code = reply.Code
	userId = reply.UserId
	userName = reply.UserName
	return
}

func (rpc *RpcLogic) Logout(ctx context.Context, req *proto.LogoutRequest) (code int) {
	reply := &proto.LogoutResponse{}
	if err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "Logout", req, reply); err != nil {
		logrus.Errorf("api call logic Logout failed: %v", err)
		code = callFailureCode(err)
		return
	}
	code = reply.Code
	return
}

func (rpc *RpcLogic) Push(ctx context.Context, req *proto.Send) (code int, msg string) {
	reply := &proto.SuccessReply{}
	if err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "Push", req, reply); err != nil {
		logrus.Errorf("api call logic Push failed: %v", err)
		code = callFailureCode(err)
		return
	}
	code = reply.Code
	msg = reply.Msg
	return
}

func (rpc *RpcLogic) PushRoom(ctx context.Context, req *proto.Send) (code int, msg string) {
	reply := &proto.SuccessReply{}
	if err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "PushRoom", req, reply); err != nil {
		logrus.Errorf("api call logic PushRoom failed: %v", err)
		code = callFailureCode(err)
		return
	}
	code = reply.Code
	msg = reply.Msg
	return
}

func (rpc *RpcLogic) Count(ctx context.Context, req *proto.Send) (code int, msg string) {
	reply := &proto.SuccessReply{}
	if err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "Count", req, reply); err != nil {
		logrus.Errorf("api call logic Count failed: %v", err)
		code = callFailureCode(err)
		return
	}
	code = reply.Code
	msg = reply.Msg
	return
}

func (rpc *RpcLogic) GetRoomInfo(ctx context.Context, req *proto.Send) (code int, msg string) {
	reply := &proto.SuccessReply{}
	if err := middleware.InstrumentedCall(ctx, LogicRpcClient, "api", "logic", "GetRoomInfo", req, reply); err != nil {
		logrus.Errorf("api call logic GetRoomInfo failed: %v", err)
		code = callFailureCode(err)
		return
	}
	code = reply.Code
	msg = reply.Msg
	return
}
