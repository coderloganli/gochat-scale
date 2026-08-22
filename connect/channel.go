/**
 * Created by lock
 * Date: 2019-08-09
 * Time: 15:18
 */
package connect

import (
	"github.com/gorilla/websocket"
	"gochat/proto"
	"net"
	"sync/atomic"
)

// channelSeq spreads channels across the registry's shards. It is assigned once
// at creation and never changes, so Add and Remove agree on where a channel
// lives without writing to it.
var channelSeq uint32

// in fact, Channel it's a user Connect session
type Channel struct {
	Room      *Room
	Next      *Channel
	Prev      *Channel
	broadcast chan *proto.Msg
	done      chan struct{}
	userId    int
	conn      *websocket.Conn
	connTcp   *net.TCPConn
	shard     uint32
}

func NewChannel(size int) (c *Channel) {
	c = new(Channel)
	c.broadcast = make(chan *proto.Msg, size)
	c.done = make(chan struct{})
	c.Next = nil
	c.Prev = nil
	c.shard = atomic.AddUint32(&channelSeq, 1)
	return
}

func (ch *Channel) Push(msg *proto.Msg) (err error) {
	select {
	case ch.broadcast <- msg:
	default:
	}
	return
}
