/**
 * Created by lock
 * Date: 2019-08-09
 * Time: 15:18
 */
package connect

import (
	"gochat/proto"
	"sync"
	"sync/atomic"
)

type Bucket struct {
	cLock         sync.RWMutex     // protect the channels for chs
	chs           map[int]*Channel // map sub key to a channel
	bucketOptions BucketOptions
	rooms         map[int]*Room // bucket room channels
	routines      []chan *proto.PushRoomMsgRequest
	routinesNum   uint64
	broadcast     chan []byte
}

type BucketOptions struct {
	ChannelSize   int
	RoomSize      int
	RoutineAmount uint64
	RoutineSize   int
}

func NewBucket(bucketOptions BucketOptions) (b *Bucket) {
	b = new(Bucket)
	b.chs = make(map[int]*Channel, bucketOptions.ChannelSize)
	b.bucketOptions = bucketOptions
	b.routines = make([]chan *proto.PushRoomMsgRequest, bucketOptions.RoutineAmount)
	b.rooms = make(map[int]*Room, bucketOptions.RoomSize)
	for i := uint64(0); i < b.bucketOptions.RoutineAmount; i++ {
		c := make(chan *proto.PushRoomMsgRequest, bucketOptions.RoutineSize)
		b.routines[i] = c
		go b.PushRoom(c)
	}
	return
}

func (b *Bucket) PushRoom(ch chan *proto.PushRoomMsgRequest) {
	for {
		var (
			arg  *proto.PushRoomMsgRequest
			room *Room
		)
		arg = <-ch
		if room = b.Room(arg.RoomId); room != nil {
			room.Push(&arg.Msg)
		}
	}
}

func (b *Bucket) Room(rid int) (room *Room) {
	b.cLock.RLock()
	room, _ = b.rooms[rid]
	b.cLock.RUnlock()
	return
}

func (b *Bucket) Put(userId int, roomId int, ch *Channel) (err error) {
	var (
		room *Room
		ok   bool
	)
	b.cLock.Lock()
	if roomId != NoRoom {
		if room, ok = b.rooms[roomId]; !ok {
			room = NewRoom(roomId)
			b.rooms[roomId] = room
		}
		ch.Room = room
	}
	ch.userId = userId
	b.chs[userId] = ch
	b.cLock.Unlock()

	if room != nil {
		err = room.Put(ch)
	}
	return
}

// DeleteChannel removes a channel from this bucket and from its room.
//
// The two are not the same condition. The user map holds one channel per user
// and Put overwrites it, so clearing it unconditionally would let an older
// connection's teardown remove a newer one's entry and leave that user
// connected but undeliverable — a drain is when that fires, because every
// client reconnects while the departing instance is still tearing their old
// connection down. The room list, in contrast, holds this channel regardless of
// which one the user map points at, so it always has to be cleaned.
func (b *Bucket) DeleteChannel(ch *Channel) {
	b.cLock.Lock()
	// The user map holds one channel per user, so it is only ours to clear when
	// it still points at this channel.
	if mapped, ok := b.chs[ch.userId]; ok && mapped == ch {
		//delete from bucket
		delete(b.chs, ch.userId)
	}
	// The room is not conditional. This channel is in its own room's list
	// whether or not it is still the mapped one, and leaving it there would keep
	// it in every room broadcast and stop the room from ever being dropped.
	room := ch.Room
	if room != nil && room.DeleteChannel(ch) {
		// if room empty delete,will mark room.drop is true
		if room.drop == true {
			delete(b.rooms, room.Id)
		}
	}
	b.cLock.Unlock()
}

func (b *Bucket) Channel(userId int) (ch *Channel) {
	b.cLock.RLock()
	ch = b.chs[userId]
	b.cLock.RUnlock()
	return
}

func (b *Bucket) BroadcastRoom(pushRoomMsgReq *proto.PushRoomMsgRequest) {
	num := atomic.AddUint64(&b.routinesNum, 1) % b.bucketOptions.RoutineAmount
	b.routines[num] <- pushRoomMsgReq
}
