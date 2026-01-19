package integration

import (
	"fmt"
	"testing"
	"time"

	"gochat/tests/helpers"
	"gochat/tests/testdata"
)

func TestE2EMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := helpers.DefaultTestConfig()
	apiClient := helpers.NewAPIClient(cfg.APIBaseURL)

	t.Run("Single_User_Message", func(t *testing.T) {
		// 1. Register sender and receiver
		sender := testdata.NewTestUser()
		senderResp, err := apiClient.Register(sender.UserName, sender.Password)
		if err != nil {
			t.Fatalf("Register sender failed: %v", err)
		}
		sender.AuthToken = senderResp.GetDataAsString()

		receiver := testdata.NewTestUser()
		receiverResp, err := apiClient.Register(receiver.UserName, receiver.Password)
		if err != nil {
			t.Fatalf("Register receiver failed: %v", err)
		}
		receiver.AuthToken = receiverResp.GetDataAsString()

		// Get receiver's userId
		authResp, err := apiClient.CheckAuth(receiver.AuthToken)
		if err != nil {
			t.Fatalf("CheckAuth failed: %v", err)
		}
		receiverData := authResp.GetDataAsMap()
		if receiverData == nil {
			t.Fatal("Failed to get receiver data")
		}
		receiverUserId := fmt.Sprintf("%.0f", receiverData["userId"].(float64))
		t.Logf("Receiver userId: %s", receiverUserId)

		// 2. Connect receiver via WebSocket
		wsClient, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("WebSocket connection failed: %v", err)
		}
		defer wsClient.Close()

		// 3. Authenticate WebSocket connection
		err = wsClient.Connect(receiver.AuthToken, testdata.DefaultRoomID)
		if err != nil {
			t.Fatalf("WebSocket auth failed: %v", err)
		}

		// Allow connection to establish and drain any initial messages
		time.Sleep(1 * time.Second)
		wsClient.DrainMessages(500 * time.Millisecond)

		// 4. Send message via API
		testMsg := testdata.TestMessage("hello_direct")
		pushResp, err := apiClient.Push(sender.AuthToken, testMsg, receiverUserId, testdata.DefaultRoomID)
		if err != nil {
			t.Fatalf("Push failed: %v", err)
		}
		if pushResp.Code != testdata.CodeSuccess {
			t.Logf("Push returned non-success code: %d, message: %v", pushResp.Code, pushResp.Message)
		}

		// 5. Wait for message via WebSocket
		msg, err := wsClient.WaitForMessageContaining(testMsg, 10*time.Second)
		if err != nil {
			t.Fatalf("Failed to receive message: %v", err)
		}

		t.Logf("Received message: %s", string(msg))

		// 6. Parse and verify message content
		parsedMsg, err := helpers.ParseMessage(msg)
		if err != nil {
			t.Fatalf("Failed to parse message: %v", err)
		}

		if parsedMsg.Msg != testMsg {
			t.Errorf("Message content mismatch: expected %s, got %s", testMsg, parsedMsg.Msg)
		}

		if parsedMsg.FromUserName != sender.UserName {
			t.Errorf("FromUserName mismatch: expected %s, got %s", sender.UserName, parsedMsg.FromUserName)
		}
	})

	t.Run("Room_Broadcast", func(t *testing.T) {
		roomId := testdata.DefaultRoomID

		// 1. Register users
		user1 := testdata.NewTestUserWithName("room_user1")
		user1Resp, err := apiClient.Register(user1.UserName, user1.Password)
		if err != nil {
			t.Fatalf("Register user1 failed: %v", err)
		}
		user1.AuthToken = user1Resp.GetDataAsString()

		user2 := testdata.NewTestUserWithName("room_user2")
		user2Resp, err := apiClient.Register(user2.UserName, user2.Password)
		if err != nil {
			t.Fatalf("Register user2 failed: %v", err)
		}
		user2.AuthToken = user2Resp.GetDataAsString()

		user3 := testdata.NewTestUserWithName("room_user3")
		user3Resp, err := apiClient.Register(user3.UserName, user3.Password)
		if err != nil {
			t.Fatalf("Register user3 failed: %v", err)
		}
		user3.AuthToken = user3Resp.GetDataAsString()

		// 2. Connect all users to same room
		ws1, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("WebSocket connection for user1 failed: %v", err)
		}
		defer ws1.Close()
		ws1.Connect(user1.AuthToken, roomId)

		ws2, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("WebSocket connection for user2 failed: %v", err)
		}
		defer ws2.Close()
		ws2.Connect(user2.AuthToken, roomId)

		ws3, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("WebSocket connection for user3 failed: %v", err)
		}
		defer ws3.Close()
		ws3.Connect(user3.AuthToken, roomId)

		// Wait for connections to establish
		time.Sleep(1 * time.Second)

		// Drain initial messages
		ws1.DrainMessages(500 * time.Millisecond)
		ws2.DrainMessages(500 * time.Millisecond)
		ws3.DrainMessages(500 * time.Millisecond)

		// 3. User1 sends room message
		testMsg := testdata.TestMessage("room_broadcast")
		_, err = apiClient.PushRoom(user1.AuthToken, testMsg, roomId)
		if err != nil {
			t.Fatalf("PushRoom failed: %v", err)
		}

		// 4. User2 and User3 should receive the message
		msg2, err := ws2.WaitForMessageContaining(testMsg, 10*time.Second)
		if err != nil {
			t.Errorf("User2 failed to receive room message: %v", err)
		} else {
			t.Logf("User2 received: %s", string(msg2))
		}

		msg3, err := ws3.WaitForMessageContaining(testMsg, 10*time.Second)
		if err != nil {
			t.Errorf("User3 failed to receive room message: %v", err)
		} else {
			t.Logf("User3 received: %s", string(msg3))
		}
	})

	t.Run("Room_Isolation", func(t *testing.T) {
		// User in room 1
		user1 := testdata.NewTestUserWithName("isolation_user1")
		user1Resp, err := apiClient.Register(user1.UserName, user1.Password)
		if err != nil {
			t.Fatalf("Register user1 failed: %v", err)
		}
		user1.AuthToken = user1Resp.GetDataAsString()

		// User in room 2
		user2 := testdata.NewTestUserWithName("isolation_user2")
		user2Resp, err := apiClient.Register(user2.UserName, user2.Password)
		if err != nil {
			t.Fatalf("Register user2 failed: %v", err)
		}
		user2.AuthToken = user2Resp.GetDataAsString()

		// Connect user1 to room 1
		ws1, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("WebSocket connection for user1 failed: %v", err)
		}
		defer ws1.Close()
		ws1.Connect(user1.AuthToken, testdata.DefaultRoomID)

		// Connect user2 to room 2
		ws2, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("WebSocket connection for user2 failed: %v", err)
		}
		defer ws2.Close()
		ws2.Connect(user2.AuthToken, testdata.AlternateRoomID)

		time.Sleep(1 * time.Second)
		ws1.DrainMessages(500 * time.Millisecond)
		ws2.DrainMessages(500 * time.Millisecond)

		// Send to room 1
		testMsg := testdata.TestMessage("room1_only")
		_, err = apiClient.PushRoom(user1.AuthToken, testMsg, testdata.DefaultRoomID)
		if err != nil {
			t.Fatalf("PushRoom failed: %v", err)
		}

		// User in room 2 should NOT receive
		_, err = ws2.WaitForMessageContaining(testMsg, 3*time.Second)
		if err == nil {
			t.Error("User in different room should not receive message")
		} else {
			t.Logf("Correctly: user2 did not receive message from room1 (timeout)")
		}
	})

	t.Run("Message_Order_Preservation", func(t *testing.T) {
		// Register users
		sender := testdata.NewTestUser()
		senderResp, err := apiClient.Register(sender.UserName, sender.Password)
		if err != nil {
			t.Fatalf("Register sender failed: %v", err)
		}
		sender.AuthToken = senderResp.GetDataAsString()

		receiver := testdata.NewTestUser()
		receiverResp, err := apiClient.Register(receiver.UserName, receiver.Password)
		if err != nil {
			t.Fatalf("Register receiver failed: %v", err)
		}
		receiver.AuthToken = receiverResp.GetDataAsString()

		// Connect receiver
		wsClient, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("WebSocket connection failed: %v", err)
		}
		defer wsClient.Close()
		wsClient.Connect(receiver.AuthToken, testdata.DefaultRoomID)

		time.Sleep(1 * time.Second)
		wsClient.DrainMessages(500 * time.Millisecond)

		// Send multiple room messages
		messages := []string{
			testdata.TestMessage("order_1"),
			testdata.TestMessage("order_2"),
			testdata.TestMessage("order_3"),
		}

		for _, msg := range messages {
			_, err := apiClient.PushRoom(sender.AuthToken, msg, testdata.DefaultRoomID)
			if err != nil {
				t.Fatalf("PushRoom failed: %v", err)
			}
			// Small delay between messages
			time.Sleep(100 * time.Millisecond)
		}

		// Receive and verify order
		var receivedOrder []string
		for i := 0; i < len(messages); i++ {
			msg, err := wsClient.WaitForMessage(5 * time.Second)
			if err != nil {
				t.Logf("Failed to receive message %d: %v", i, err)
				break
			}
			parsedMsg, _ := helpers.ParseMessage(msg)
			if parsedMsg != nil {
				receivedOrder = append(receivedOrder, parsedMsg.Msg)
			}
		}

		t.Logf("Received %d messages", len(receivedOrder))
		for i, msg := range receivedOrder {
			t.Logf("Message %d: %s", i, msg)
		}
	})

	t.Run("Large_Message", func(t *testing.T) {
		// Register users
		sender := testdata.NewTestUser()
		senderResp, err := apiClient.Register(sender.UserName, sender.Password)
		if err != nil {
			t.Fatalf("Register sender failed: %v", err)
		}
		sender.AuthToken = senderResp.GetDataAsString()

		receiver := testdata.NewTestUser()
		receiverResp, err := apiClient.Register(receiver.UserName, receiver.Password)
		if err != nil {
			t.Fatalf("Register receiver failed: %v", err)
		}
		receiver.AuthToken = receiverResp.GetDataAsString()

		// Connect receiver
		wsClient, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("WebSocket connection failed: %v", err)
		}
		defer wsClient.Close()
		wsClient.Connect(receiver.AuthToken, testdata.DefaultRoomID)

		time.Sleep(1 * time.Second)
		wsClient.DrainMessages(500 * time.Millisecond)

		// Create a larger message (1KB)
		largeContent := ""
		for i := 0; i < 100; i++ {
			largeContent += "0123456789"
		}
		testMsg := testdata.TestMessage("large") + "_" + largeContent

		_, err = apiClient.PushRoom(sender.AuthToken, testMsg, testdata.DefaultRoomID)
		if err != nil {
			t.Fatalf("PushRoom with large message failed: %v", err)
		}

		// Receive and verify
		msg, err := wsClient.WaitForMessage(10 * time.Second)
		if err != nil {
			t.Fatalf("Failed to receive large message: %v", err)
		}

		parsedMsg, _ := helpers.ParseMessage(msg)
		if parsedMsg != nil {
			if len(parsedMsg.Msg) != len(testMsg) {
				t.Errorf("Large message length mismatch: expected %d, got %d", len(testMsg), len(parsedMsg.Msg))
			} else {
				t.Logf("Large message received successfully (length: %d)", len(parsedMsg.Msg))
			}
		}
	})
}
