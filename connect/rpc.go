/**
 * Created by lock
 * Date: 2019-08-12
 * Time: 23:36
 */
package connect

import (
	"context"
	"fmt"
	"gochat/config"
	"gochat/pkg/middleware"
	"gochat/proto"
	"gochat/tools"
	"strings"
	"sync"
	"time"

	"github.com/rcrowley/go-metrics"
	"github.com/rpcxio/libkv/store"
	etcdV3 "github.com/rpcxio/rpcx-etcd/client"
	"github.com/rpcxio/rpcx-etcd/serverplugin"
	"github.com/sirupsen/logrus"
	"github.com/smallnest/rpcx/client"
	"github.com/smallnest/rpcx/protocol"
	"github.com/smallnest/rpcx/server"
)

var logicRpcClient client.XClient
var once sync.Once

type RpcConnect struct {
}

func (c *Connect) InitLogicRpcClient() (err error) {
	etcdConfigOption := &store.Config{
		ClientTLS:         nil,
		TLS:               nil,
		ConnectionTimeout: time.Duration(config.Conf.Common.CommonEtcd.ConnectionTimeout) * time.Second,
		Bucket:            "",
		PersistConnection: true,
		Username:          config.Conf.Common.CommonEtcd.UserName,
		Password:          config.Conf.Common.CommonEtcd.Password,
	}
	once.Do(func() {
		d, e := etcdV3.NewEtcdV3Discovery(
			config.Conf.Common.CommonEtcd.BasePath,
			config.Conf.Common.CommonEtcd.ServerPathLogic,
			[]string{config.Conf.Common.CommonEtcd.Host},
			true,
			etcdConfigOption,
		)
		if e != nil {
			logrus.Fatalf("init connect rpc etcd discovery client fail:%s", e.Error())
		}
		// Kept so the readiness check can ask whether any logic instance is
		// actually registered, rather than finding out on the first call.
		logicDiscovery = d
		// Optimized client options for better connection reuse
		opt := client.Option{
			Retries:             3,
			ConnectTimeout:      500 * time.Millisecond,
			IdleTimeout:         0,
			Heartbeat:           true,
			HeartbeatInterval:   10 * time.Second,
			MaxWaitForHeartbeat: 30 * time.Second,
			TCPKeepAlivePeriod:  30 * time.Second,
			BackupLatency:       10 * time.Millisecond,
			SerializeType:       protocol.MsgPack,
			CompressType:        protocol.None,
		}
		logicRpcClient = client.NewXClient(config.Conf.Common.CommonEtcd.ServerPathLogic, client.Failtry, client.RandomSelect, d, opt)
	})
	if logicRpcClient == nil {
		logrus.Fatalf("get rpc client nil")
	}
	return
}

func (rpc *RpcConnect) Connect(connReq *proto.ConnectRequest) (uid int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	reply := &proto.ConnectReply{}
	err = middleware.InstrumentedCall(ctx, logicRpcClient, "connect", "logic", "Connect", connReq, reply)
	if err != nil {
		logrus.Errorf("Connect RPC call failed: %v", err)
		return
	}
	uid = reply.UserId
	return
}

func (rpc *RpcConnect) DisConnect(disConnReq *proto.DisConnectRequest) (err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	reply := &proto.DisConnectReply{}
	if err = middleware.InstrumentedCall(ctx, logicRpcClient, "connect", "logic", "DisConnect", disConnReq, reply); err != nil {
		logrus.Errorf("DisConnect RPC call failed: %v", err)
	}
	return
}

func (c *Connect) InitConnectWebsocketRpcServer() (err error) {
	var network, addr string
	connectRpcAddress := strings.Split(config.Conf.Connect.ConnectRpcAddressWebSockts.Address, ",")
	for _, bind := range connectRpcAddress {
		if network, addr, err = tools.ParseNetwork(bind); err != nil {
			logrus.Panicf("InitConnectWebsocketRpcServer ParseNetwork error : %s", err)
		}
		logrus.Infof("Connect start run at-->%s:%s", network, addr)
		// Built here rather than inside the goroutine, so that Stop cannot race
		// the slice it has to walk to deregister.
		s := c.newConnectRpcServer(network, addr, "ws")
		if s == nil {
			// Registration failed; there is nothing to serve on and readiness
			// will keep this instance out of the Service.
			continue
		}
		go func(network, addr string) { _ = s.Serve(network, addr) }(network, addr)
	}
	return
}

func (c *Connect) InitConnectTcpRpcServer() (err error) {
	var network, addr string
	connectRpcAddress := strings.Split(config.Conf.Connect.ConnectRpcAddressTcp.Address, ",")
	for _, bind := range connectRpcAddress {
		if network, addr, err = tools.ParseNetwork(bind); err != nil {
			logrus.Panicf("InitConnectTcpRpcServer ParseNetwork error : %s", err)
		}
		logrus.Infof("Connect start run at-->%s:%s", network, addr)
		s := c.newConnectRpcServer(network, addr, "tcp")
		if s == nil {
			// Registration failed; there is nothing to serve on and readiness
			// will keep this instance out of the Service.
			continue
		}
		go func(network, addr string) { _ = s.Serve(network, addr) }(network, addr)
	}
	return
}

type RpcConnectPush struct {
}

func (rpc *RpcConnectPush) PushSingleMsg(ctx context.Context, pushMsgReq *proto.PushMsgRequest, successReply *proto.SuccessReply) (err error) {
	var (
		bucket  *Bucket
		channel *Channel
	)
	if pushMsgReq == nil {
		logrus.Errorf("rpc PushSingleMsg() args:(%v)", pushMsgReq)
		return
	}
	bucket = DefaultServer.Bucket(pushMsgReq.UserId)
	if channel = bucket.Channel(pushMsgReq.UserId); channel != nil {
		err = channel.Push(&pushMsgReq.Msg)
		return
	}
	successReply.Code = config.SuccessReplyCode
	successReply.Msg = config.SuccessReplyMsg
	return
}

func (rpc *RpcConnectPush) PushRoomMsg(ctx context.Context, pushRoomMsgReq *proto.PushRoomMsgRequest, successReply *proto.SuccessReply) (err error) {
	successReply.Code = config.SuccessReplyCode
	successReply.Msg = config.SuccessReplyMsg
	for _, bucket := range DefaultServer.Buckets {
		bucket.BroadcastRoom(pushRoomMsgReq)
	}
	return
}

func (rpc *RpcConnectPush) PushRoomCount(ctx context.Context, pushRoomMsgReq *proto.PushRoomMsgRequest, successReply *proto.SuccessReply) (err error) {
	successReply.Code = config.SuccessReplyCode
	successReply.Msg = config.SuccessReplyMsg
	for _, bucket := range DefaultServer.Buckets {
		bucket.BroadcastRoom(pushRoomMsgReq)
	}
	return
}

func (rpc *RpcConnectPush) PushRoomInfo(ctx context.Context, pushRoomMsgReq *proto.PushRoomMsgRequest, successReply *proto.SuccessReply) (err error) {
	successReply.Code = config.SuccessReplyCode
	successReply.Msg = config.SuccessReplyMsg
	for _, bucket := range DefaultServer.Buckets {
		bucket.BroadcastRoom(pushRoomMsgReq)
	}
	return
}

// newConnectRpcServer builds an rpcx server, registers it, and remembers it so
// that shutdown can deregister it from etcd.
//
// There is deliberately no RegisterOnShutdown hook here. In rpcx v1.7.4 the
// onShutdown slice is appended to and never read (server/server.go:92,865), so
// the s.UnregisterAll() this code used to register had never run. What actually
// removes the etcd node is Shutdown itself, through Plugins.DoUnregister.
func (c *Connect) newConnectRpcServer(network, addr, serverType string) *server.Server {
	s := server.NewServer()
	addRegistryPlugin(s, network, addr)
	if err := s.RegisterName(config.Conf.Common.CommonEtcd.ServerPathConnect, new(RpcConnectPush),
		fmt.Sprintf("serverId=%s&serverType=%s", c.ServerId, serverType)); err != nil {
		logrus.Errorf("register connect rpc server: %v", err)
		// Deliberately not calling etcdRegistered(): the readiness gate must stay
		// shut. A connect instance that is serving but not in the registry is a
		// black hole - task resolves recipients through etcd and silently drops
		// what it cannot route.
		return nil
	}
	// Registration is synchronous here, before Serve is spawned, so this is a
	// fact rather than a race: task can now resolve this address.
	etcdRegistered()
	c.rpcServers = append(c.rpcServers, s)
	return s
}

func addRegistryPlugin(s *server.Server, network string, addr string) {
	r := &serverplugin.EtcdV3RegisterPlugin{
		ServiceAddress: tools.GetServiceAddress(network, addr),
		EtcdServers:    []string{config.Conf.Common.CommonEtcd.Host},
		BasePath:       config.Conf.Common.CommonEtcd.BasePath,
		Metrics:        metrics.NewRegistry(),
		UpdateInterval: time.Minute,
	}
	err := r.Start()
	if err != nil {
		logrus.Fatal(err)
	}
	s.Plugins.Add(r)
}
