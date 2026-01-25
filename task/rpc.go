/**
 * Created by lock
 * Date: 2019-08-13
 * Time: 10:13
 */
package task

import (
	"context"
	"encoding/json"
	"errors"
	"gochat/config"
	"gochat/pkg/middleware"
	"gochat/proto"
	"gochat/tools"
	"strings"
	"sync"
	"time"

	"github.com/rpcxio/libkv/store"
	etcdV3 "github.com/rpcxio/rpcx-etcd/client"
	"github.com/sirupsen/logrus"
	"github.com/smallnest/rpcx/client"
	"github.com/smallnest/rpcx/protocol"
)

var RClient = &RpcConnectClient{
	ServerInsMap: make(map[string][]Instance),
	IndexMap:     make(map[string]int),
}

const roomInfoMinInterval = 250 * time.Millisecond

var roomInfoMu sync.Mutex
var roomInfoEntries = make(map[int]*roomInfoEntry)

type roomInfoEntry struct {
	lastSent time.Time
	pending  map[string]string
	timer    *time.Timer
}

type Instance struct {
	ServerType string
	ServerId   string
	Client     client.XClient
}

type RpcConnectClient struct {
	lock         sync.RWMutex
	ServerInsMap map[string][]Instance //serverId--[]ins
	IndexMap     map[string]int        //serverId--index
}

func (rc *RpcConnectClient) GetRpcClientByServerId(serverId string) (c client.XClient, err error) {
	rc.lock.Lock()
	defer rc.lock.Unlock()
	if _, ok := rc.ServerInsMap[serverId]; !ok || len(rc.ServerInsMap[serverId]) <= 0 {
		return nil, errors.New("no connect layer ip:" + serverId)
	}
	if _, ok := rc.IndexMap[serverId]; !ok {
		rc.IndexMap = map[string]int{
			serverId: 0,
		}
	}
	idx := rc.IndexMap[serverId] % len(rc.ServerInsMap[serverId])
	ins := rc.ServerInsMap[serverId][idx]
	rc.IndexMap[serverId] = (rc.IndexMap[serverId] + 1) % len(rc.ServerInsMap[serverId])
	return ins.Client, nil
}

func (rc *RpcConnectClient) GetAllConnectTypeRpcClient() (rpcClientList []client.XClient) {
	rc.lock.RLock()
	serverIds := make([]string, 0, len(rc.ServerInsMap))
	for serverId := range rc.ServerInsMap {
		serverIds = append(serverIds, serverId)
	}
	rc.lock.RUnlock()

	for _, serverId := range serverIds {
		c, err := rc.GetRpcClientByServerId(serverId)
		if err != nil {
			logrus.Debugf("GetAllConnectTypeRpcClient err:%s", err.Error())
			continue
		}
		rpcClientList = append(rpcClientList, c)
	}
	return
}

func getParamByKey(s string, key string) string {
	params := strings.Split(s, "&")
	for _, p := range params {
		kv := strings.Split(p, "=")
		if len(kv) == 2 && kv[0] == key {
			return kv[1]
		}
	}
	return ""
}

func (task *Task) InitConnectRpcClient() (err error) {
	etcdConfigOption := &store.Config{
		ClientTLS:         nil,
		TLS:               nil,
		ConnectionTimeout: time.Duration(config.Conf.Common.CommonEtcd.ConnectionTimeout) * time.Second,
		Bucket:            "",
		PersistConnection: true,
		Username:          config.Conf.Common.CommonEtcd.UserName,
		Password:          config.Conf.Common.CommonEtcd.Password,
	}
	etcdConfig := config.Conf.Common.CommonEtcd
	d, e := etcdV3.NewEtcdV3Discovery(
		etcdConfig.BasePath,
		etcdConfig.ServerPathConnect,
		[]string{etcdConfig.Host},
		true,
		etcdConfigOption,
	)
	if e != nil {
		logrus.Fatalf("init task rpc etcd discovery client fail:%s", e.Error())
	}
	if len(d.GetServices()) <= 0 {
		logrus.Debugf("no etcd server find!")
	}
	// watch connect server change && update RpcConnectClientList
	go task.watchServicesChange(d)
	return
}

func (task *Task) watchServicesChange(d client.ServiceDiscovery) {
	etcdConfig := config.Conf.Common.CommonEtcd
	for kvChan := range d.WatchService() {
		if len(kvChan) <= 0 {
			logrus.Errorf("connect services change, connect alarm, no abailable ip")
		}
		logrus.Debugf("connect services change trigger...")
		insMap := make(map[string][]Instance)
		for _, kv := range kvChan {
			logrus.Debugf("connect services change,key is:%s,value is:%s", kv.Key, kv.Value)
			serverType := getParamByKey(kv.Value, "serverType")
			serverId := getParamByKey(kv.Value, "serverId")
			logrus.Debugf("serverType is:%s,serverId is:%s", serverType, serverId)
			if serverType == "" || serverId == "" {
				continue
			}
			d, e := client.NewPeer2PeerDiscovery(kv.Key, "")
			if e != nil {
				logrus.Errorf("init task client.NewPeer2PeerDiscovery watch client fail:%s", e.Error())
				continue
			}
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
			c := client.NewXClient(etcdConfig.ServerPathConnect, client.Failtry, client.RandomSelect, d, opt)
			ins := Instance{
				ServerType: serverType,
				ServerId:   serverId,
				Client:     c,
			}
			if _, ok := insMap[serverId]; !ok {
				insMap[serverId] = []Instance{ins}
			} else {
				insMap[serverId] = append(insMap[serverId], ins)
			}
		}
		RClient.lock.Lock()
		RClient.ServerInsMap = insMap
		RClient.lock.Unlock()

	}
}

func (task *Task) pushSingleToConnect(serverId string, userId int, msg []byte) {
	logrus.Debugf("pushSingleToConnect Body %s", string(msg))
	pushMsgReq := &proto.PushMsgRequest{
		UserId: userId,
		Msg: proto.Msg{
			Ver:       config.MsgVersion,
			Operation: config.OpSingleSend,
			SeqId:     tools.GetSnowflakeId(),
			Body:      msg,
		},
	}
	reply := &proto.SuccessReply{}
	connectRpc, err := RClient.GetRpcClientByServerId(serverId)
	if err != nil {
		logrus.Errorf("get rpc client err %v", err)
		return
	}
	err = middleware.InstrumentedCall(context.Background(), connectRpc, "task", "connect", "PushSingleMsg", pushMsgReq, reply)
	if err != nil {
		logrus.Errorf("pushSingleToConnect Call err %v", err)
		return
	}
	logrus.Debugf("reply %s", reply.Msg)
}

func (task *Task) broadcastRoomToConnect(roomId int, msg []byte) {
	pushRoomMsgReq := &proto.PushRoomMsgRequest{
		RoomId: roomId,
		Msg: proto.Msg{
			Ver:       config.MsgVersion,
			Operation: config.OpRoomSend,
			SeqId:     tools.GetSnowflakeId(),
			Body:      msg,
		},
	}
	reply := &proto.SuccessReply{}
	rpcList := RClient.GetAllConnectTypeRpcClient()
	for _, rpc := range rpcList {
		logrus.Debugf("broadcastRoomToConnect rpc %v", rpc)
		middleware.InstrumentedCall(context.Background(), rpc, "task", "connect", "PushRoomMsg", pushRoomMsgReq, reply)
		logrus.Debugf("reply %s", reply.Msg)
	}
}

func (task *Task) broadcastRoomCountToConnect(roomId, count int) {
	msg := &proto.RedisRoomCountMsg{
		Count: count,
		Op:    config.OpRoomCountSend,
	}
	var body []byte
	var err error
	if body, err = json.Marshal(msg); err != nil {
		logrus.Warnf("broadcastRoomCountToConnect  json.Marshal err :%s", err.Error())
		return
	}
	pushRoomMsgReq := &proto.PushRoomMsgRequest{
		RoomId: roomId,
		Msg: proto.Msg{
			Ver:       config.MsgVersion,
			Operation: config.OpRoomCountSend,
			SeqId:     tools.GetSnowflakeId(),
			Body:      body,
		},
	}
	reply := &proto.SuccessReply{}
	rpcList := RClient.GetAllConnectTypeRpcClient()
	for _, rpc := range rpcList {
		logrus.Debugf("broadcastRoomCountToConnect rpc %v", rpc)
		middleware.InstrumentedCall(context.Background(), rpc, "task", "connect", "PushRoomCount", pushRoomMsgReq, reply)
		logrus.Debugf("reply %s", reply.Msg)
	}
}

func (task *Task) broadcastRoomInfoToConnect(roomId int, roomUserInfo map[string]string) {
	now := time.Now()
	roomInfoMu.Lock()
	entry := roomInfoEntries[roomId]
	if entry == nil {
		entry = &roomInfoEntry{}
		roomInfoEntries[roomId] = entry
	}
	if entry.timer == nil && now.Sub(entry.lastSent) >= roomInfoMinInterval {
		entry.lastSent = now
		roomInfoMu.Unlock()
		task.sendRoomInfoToConnect(roomId, roomUserInfo)
		return
	}
	entry.pending = roomUserInfo
	if entry.timer == nil {
		wait := roomInfoMinInterval - now.Sub(entry.lastSent)
		if wait < 0 {
			wait = roomInfoMinInterval
		}
		entry.timer = time.AfterFunc(wait, func() {
			task.flushRoomInfo(roomId)
		})
	}
	roomInfoMu.Unlock()
}

func (task *Task) flushRoomInfo(roomId int) {
	var pending map[string]string
	roomInfoMu.Lock()
	entry := roomInfoEntries[roomId]
	if entry == nil {
		roomInfoMu.Unlock()
		return
	}
	pending = entry.pending
	entry.pending = nil
	entry.timer = nil
	entry.lastSent = time.Now()
	roomInfoMu.Unlock()
	if pending != nil {
		task.sendRoomInfoToConnect(roomId, pending)
	}
}

func (task *Task) sendRoomInfoToConnect(roomId int, roomUserInfo map[string]string) {
	msg := &proto.RedisRoomInfo{
		Count:        len(roomUserInfo),
		Op:           config.OpRoomInfoSend,
		RoomUserInfo: roomUserInfo,
		RoomId:       roomId,
	}
	var body []byte
	var err error
	if body, err = json.Marshal(msg); err != nil {
		logrus.Warnf("broadcastRoomInfoToConnect  json.Marshal err :%s", err.Error())
		return
	}
	pushRoomMsgReq := &proto.PushRoomMsgRequest{
		RoomId: roomId,
		Msg: proto.Msg{
			Ver:       config.MsgVersion,
			Operation: config.OpRoomInfoSend,
			SeqId:     tools.GetSnowflakeId(),
			Body:      body,
		},
	}
	reply := &proto.SuccessReply{}
	rpcList := RClient.GetAllConnectTypeRpcClient()
	for _, rpc := range rpcList {
		logrus.Debugf("broadcastRoomInfoToConnect rpc %v", rpc)
		middleware.InstrumentedCall(context.Background(), rpc, "task", "connect", "PushRoomInfo", pushRoomMsgReq, reply)
		logrus.Debugf("broadcastRoomInfoToConnect rpc reply %v", reply)
	}
}
