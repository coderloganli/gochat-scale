/**
 * Created by lock
 * Date: 2019-08-12
 * Time: 15:44
 */
package logic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/rcrowley/go-metrics"
	"github.com/rpcxio/rpcx-etcd/serverplugin"
	"github.com/sirupsen/logrus"
	"github.com/smallnest/rpcx/server"
	"gochat/config"
	"gochat/pkg/middleware"
	"gochat/proto"
	"gochat/tools"
	"strings"
)

var RedisClient *redis.Client
var RedisSessClient *redis.Client
var RabbitMQClient *tools.RabbitMQClient

func (logic *Logic) InitPublishRedisClient() (err error) {
	redisOpt := tools.RedisOption{
		Address:  config.Conf.Common.CommonRedis.RedisAddress,
		Password: config.Conf.Common.CommonRedis.RedisPassword,
		Db:       config.Conf.Common.CommonRedis.Db,
	}
	RedisClient = tools.GetRedisInstance(redisOpt)
	if pong, err := RedisClient.Ping().Result(); err != nil {
		logrus.Infof("RedisCli Ping Result pong: %s,  err: %s", pong, err)
	}
	//this can change use another redis save session data
	RedisSessClient = RedisClient
	return err
}

func (logic *Logic) InitRabbitMQClient() error {
	RabbitMQClient = tools.GetRabbitMQInstance(config.Conf.Common.CommonRabbitMQ.URL)
	if err := RabbitMQClient.Connect(); err != nil {
		return err
	}

	ch := RabbitMQClient.Channel()
	return ch.ExchangeDeclare(
		config.RabbitMQExchange,
		"direct",
		true,  // durable
		false, // auto-deleted
		false, // internal
		false, // no-wait
		nil,
	)
}

func (logic *Logic) InitRpcServer() (err error) {
	var network, addr string
	// a host multi port case
	rpcAddressList := strings.Split(config.Conf.Logic.LogicBase.RpcAddress, ",")
	for _, bind := range rpcAddressList {
		if network, addr, err = tools.ParseNetwork(bind); err != nil {
			logrus.Panicf("InitLogicRpc ParseNetwork error : %s", err.Error())
		}
		logrus.Infof("logic start run at-->%s:%s", network, addr)
		// Built here rather than inside the goroutine, so that Stop cannot race
		// the slice it has to walk to deregister.
		s := logic.newRpcServer(network, addr)
		if s == nil {
			// Registration failed; readiness keeps this instance out of the
			// Service rather than letting it serve unreachable.
			continue
		}
		go func(network, addr string) { _ = s.Serve(network, addr) }(network, addr)
	}
	return
}

// newRpcServer builds an rpcx server, registers it, and remembers it so that
// shutdown can deregister it from etcd.
//
// There is deliberately no RegisterOnShutdown hook. In rpcx v1.7.4 the
// onShutdown slice is appended to and never read (server/server.go:92,865), so
// the s.UnregisterAll() this code used to register had never run. What actually
// removes the etcd node is Shutdown itself, through Plugins.DoUnregister.
func (logic *Logic) newRpcServer(network string, addr string) *server.Server {
	s := server.NewServer()
	s.Plugins.Add(middleware.NewPrometheusRPCPlugin("logic"))
	logic.addRegistryPlugin(s, network, addr)
	// serverId must be unique
	if err := s.RegisterName(config.Conf.Common.CommonEtcd.ServerPathLogic, new(RpcLogic), logic.ServerId); err != nil {
		logrus.Errorf("register error:%s", err.Error())
		return nil
	}
	// Registered in etcd, so api and connect can now discover this address, and
	// readiness can stop reporting this instance as unreachable.
	etcdRegistered()
	logic.rpcServers = append(logic.rpcServers, s)
	return s
}

func (logic *Logic) addRegistryPlugin(s *server.Server, network string, addr string) {
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

func (logic *Logic) PublishToUser(serverId string, toUserId int, msg []byte) (err error) {
	redisMsg := proto.RedisMsg{
		Op:       config.OpSingleSend,
		ServerId: serverId,
		UserId:   toUserId,
		Msg:      msg,
	}
	body, err := json.Marshal(redisMsg)
	if err != nil {
		logrus.Errorf("logic,PublishToUser Marshal err:%s", err.Error())
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return RabbitMQClient.Publish(
		ctx,
		config.RabbitMQExchange,
		config.RoutingKeySingleSend,
		false, // mandatory
		false, // immediate
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         body,
		},
	)
}

func (logic *Logic) PublishToRoom(roomId int, count int, RoomUserInfo map[string]string, msg []byte) (err error) {
	var redisMsg = &proto.RedisMsg{
		Op:           config.OpRoomSend,
		RoomId:       roomId,
		Count:        count,
		Msg:          msg,
		RoomUserInfo: RoomUserInfo,
	}
	body, err := json.Marshal(redisMsg)
	if err != nil {
		logrus.Errorf("logic,PublishToRoom redisMsg error : %s", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return RabbitMQClient.Publish(
		ctx,
		config.RabbitMQExchange,
		config.RoutingKeyRoomSend,
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         body,
		},
	)
}

func (logic *Logic) PublishRoomCount(roomId int, count int) (err error) {
	var redisMsg = &proto.RedisMsg{
		Op:     config.OpRoomCountSend,
		RoomId: roomId,
		Count:  count,
	}
	body, err := json.Marshal(redisMsg)
	if err != nil {
		logrus.Errorf("logic,PublishRoomCount redisMsg error : %s", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return RabbitMQClient.Publish(
		ctx,
		config.RabbitMQExchange,
		config.RoutingKeyRoomCount,
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         body,
		},
	)
}

func (logic *Logic) PublishRoomInfo(roomId int, count int, roomUserInfo map[string]string) (err error) {
	var redisMsg = &proto.RedisMsg{
		Op:           config.OpRoomInfoSend,
		RoomId:       roomId,
		Count:        count,
		RoomUserInfo: roomUserInfo,
	}
	body, err := json.Marshal(redisMsg)
	if err != nil {
		logrus.Errorf("logic,PublishRoomInfo redisMsg error : %s", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	return RabbitMQClient.Publish(
		ctx,
		config.RabbitMQExchange,
		config.RoutingKeyRoomInfo,
		false,
		false,
		amqp.Publishing{
			DeliveryMode: amqp.Persistent,
			ContentType:  "application/json",
			Body:         body,
		},
	)
}

func (logic *Logic) getRoomUserKey(authKey string) string {
	var returnKey bytes.Buffer
	returnKey.WriteString(config.RedisRoomPrefix)
	returnKey.WriteString(authKey)
	return returnKey.String()
}

func (logic *Logic) getRoomOnlineCountKey(authKey string) string {
	var returnKey bytes.Buffer
	returnKey.WriteString(config.RedisRoomOnlinePrefix)
	returnKey.WriteString(authKey)
	return returnKey.String()
}

func (logic *Logic) getUserKey(authKey string) string {
	var returnKey bytes.Buffer
	returnKey.WriteString(config.RedisPrefix)
	returnKey.WriteString(authKey)
	return returnKey.String()
}

// Outcomes of clearUserServerIdScript. They also answer the question the rest
// of DisConnect needs answered: is this user still ours to clean up?
const (
	// DisconnectStale means the routing key names a different instance, so the
	// user has already reconnected elsewhere and none of their state is ours.
	DisconnectStale = 0
	// DisconnectCleared means the key named this instance and was removed.
	DisconnectCleared = 1
	// DisconnectAbsent means there was no key — cleared already, or expired.
	DisconnectAbsent = 2
)

// clearUserServerIdScript deletes the userId -> serverId routing key when it
// still holds the serverId given, and reports what it found.
//
// The compare and the delete have to be one step. A plain DEL would race a user
// who has already reconnected somewhere else: the departing instance's late
// DisConnect would delete the fresh mapping and black-hole that user until they
// reconnected again.
var clearUserServerIdScript = redis.NewScript(`
local current = redis.call('GET', KEYS[1])
if not current then
	return 2
end
if current == ARGV[1] then
	redis.call('DEL', KEYS[1])
	return 1
end
return 0
`)

// clearUserServerId removes a user's routing key if it still names serverId,
// and reports whether the user still belongs to that instance.
//
// An unknown serverId — a caller that has not been updated — reports Absent, so
// the old unconditional behaviour is what happens rather than a silent skip.
func (logic *Logic) clearUserServerId(userId int, serverId string) (int, error) {
	if userId == 0 || serverId == "" {
		return DisconnectAbsent, nil
	}
	userKey := logic.getUserKey(fmt.Sprintf("%d", userId))
	result, err := clearUserServerIdScript.Run(RedisClient, []string{userKey}, serverId).Result()
	if err != nil {
		return DisconnectAbsent, err
	}
	code, ok := result.(int64)
	if !ok {
		return DisconnectAbsent, fmt.Errorf("unexpected script result %T", result)
	}
	return int(code), nil
}
