package connect

import "testing"

func testBucket() *Bucket {
	return NewBucket(BucketOptions{
		ChannelSize:   16,
		RoomSize:      4,
		RoutineAmount: 2,
		RoutineSize:   4,
	})
}

// Test case 21: after a second connection replaces the first for one user,
// tearing the first one down must not remove the second one's entry.
//
// A drain is exactly when this fires: every client reconnects while the
// departing instance is still running teardown for their old connection.
func TestDeleteChannelLeavesAReplacementAlone(t *testing.T) {
	const userId = 42

	b := testBucket()

	first := NewChannel(8)
	if err := b.Put(userId, NoRoom, first); err != nil {
		t.Fatalf("Put first: %v", err)
	}

	second := NewChannel(8)
	if err := b.Put(userId, NoRoom, second); err != nil {
		t.Fatalf("Put second: %v", err)
	}

	b.DeleteChannel(first)

	got := b.Channel(userId)
	if got == nil {
		t.Fatal("deleting the superseded connection removed the live one; that user " +
			"is connected and will never be delivered to")
	}
	if got != second {
		t.Fatalf("Channel(%d) returned an unexpected channel after deleting the superseded one", userId)
	}
}

// Test case 22: deleting the currently mapped channel removes it.
func TestDeleteChannelRemovesTheMappedChannel(t *testing.T) {
	const userId = 43

	b := testBucket()

	ch := NewChannel(8)
	if err := b.Put(userId, NoRoom, ch); err != nil {
		t.Fatalf("Put: %v", err)
	}

	b.DeleteChannel(ch)

	if got := b.Channel(userId); got != nil {
		t.Fatal("Channel returned a channel after it was deleted, want nil")
	}
}

// From the stage 8 review: cases 21 and 22 use NoRoom, so neither covers what
// the identity check does to the room. A superseded connection must still leave
// its room, or it stays in every room broadcast and the room is never dropped.
func TestDeleteChannelStillLeavesTheRoomWhenSuperseded(t *testing.T) {
	const userId = 44
	const roomId = 7

	b := testBucket()

	first := NewChannel(8)
	if err := b.Put(userId, roomId, first); err != nil {
		t.Fatalf("Put first: %v", err)
	}
	second := NewChannel(8)
	if err := b.Put(userId, roomId, second); err != nil {
		t.Fatalf("Put second: %v", err)
	}

	room := b.Room(roomId)
	if room == nil {
		t.Fatal("the room was not created")
	}
	before := room.OnlineCount

	b.DeleteChannel(first)

	if got := room.OnlineCount; got != before-1 {
		t.Errorf("room online count is %d after deleting the superseded connection, want %d — "+
			"a channel left in the room list keeps receiving every broadcast", got, before-1)
	}
	if b.Channel(userId) != second {
		t.Error("deleting the superseded connection disturbed the live one")
	}
}
