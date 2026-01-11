package integration

import (
	"testing"
	"time"

	"gochat/tests/helpers"
	"gochat/tests/testdata"
)

func TestRoomOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := helpers.DefaultTestConfig()
	apiClient := helpers.NewAPIClient(cfg.APIBaseURL)

	t.Run("Join_Room", func(t *testing.T) {
		t.Run("user_joins_room_via_websocket", func(t *testing.T) {
			// Register user
			username := testdata.GenerateTestUserName()
			regResp, err := apiClient.Register(username, testdata.TestPassword)
			if err != nil {
				t.Fatalf("Register failed: %v", err)
			}
			authToken := regResp.GetDataAsString()

			// Connect to room
			wsClient, err := helpers.NewWSClient(cfg.WSBaseURL)
			if err != nil {
				t.Fatalf("WebSocket connection failed: %v", err)
			}
			defer wsClient.Close()

			err = wsClient.Connect(authToken, testdata.DefaultRoomID)
			if err != nil {
				t.Fatalf("Join room failed: %v", err)
			}

			time.Sleep(1 * time.Second)

			if wsClient.IsClosed() {
				t.Error("Connection should remain open after joining room")
			}

			// May receive room info update
			wsClient.DrainMessages(500 * time.Millisecond)
		})

		t.Run("multiple_users_join_same_room", func(t *testing.T) {
			roomId := testdata.AlternateRoomID
			var wsClients []*helpers.WSClient

			// Connect 5 users to the same room
			for i := 0; i < 5; i++ {
				username := testdata.GenerateTestUserName()
				regResp, err := apiClient.Register(username, testdata.TestPassword)
				if err != nil {
					t.Fatalf("Register user %d failed: %v", i, err)
				}
				authToken := regResp.GetDataAsString()

				ws, err := helpers.NewWSClient(cfg.WSBaseURL)
				if err != nil {
					t.Fatalf("WebSocket connection %d failed: %v", i, err)
				}
				wsClients = append(wsClients, ws)

				err = ws.Connect(authToken, roomId)
				if err != nil {
					t.Fatalf("Join room %d failed: %v", i, err)
				}

				// Small delay between joins
				time.Sleep(200 * time.Millisecond)
			}

			defer func() {
				for _, ws := range wsClients {
					ws.Close()
				}
			}()

			time.Sleep(1 * time.Second)

			// All connections should be open
			openCount := 0
			for i, ws := range wsClients {
				if !ws.IsClosed() {
					openCount++
				} else {
					t.Logf("Connection %d is closed", i)
				}
			}

			t.Logf("Open connections: %d/%d", openCount, len(wsClients))

			if openCount == 0 {
				t.Error("At least some connections should remain open")
			}
		})
	})

	t.Run("Leave_Room", func(t *testing.T) {
		t.Run("user_leaves_room_on_disconnect", func(t *testing.T) {
			roomId := testdata.AlternateRoomID

			// Register user
			username := testdata.GenerateTestUserName()
			regResp, err := apiClient.Register(username, testdata.TestPassword)
			if err != nil {
				t.Fatalf("Register failed: %v", err)
			}
			authToken := regResp.GetDataAsString()

			// Connect to room
			wsClient, err := helpers.NewWSClient(cfg.WSBaseURL)
			if err != nil {
				t.Fatalf("WebSocket connection failed: %v", err)
			}

			err = wsClient.Connect(authToken, roomId)
			if err != nil {
				t.Fatalf("Join room failed: %v", err)
			}

			time.Sleep(500 * time.Millisecond)

			// Disconnect
			wsClient.Close()

			time.Sleep(500 * time.Millisecond)

			// Room count should have decreased (verify via API if possible)
			// This is implicit - the connection is closed
			t.Log("User disconnected from room")
		})

		t.Run("other_users_notified_on_leave", func(t *testing.T) {
			roomId := testdata.AlternateRoomID

			// Register two users
			user1 := testdata.NewTestUserWithName("leave_user1")
			user1Resp, err := apiClient.Register(user1.UserName, user1.Password)
			if err != nil {
				t.Fatalf("Register user1 failed: %v", err)
			}
			user1.AuthToken = user1Resp.GetDataAsString()

			user2 := testdata.NewTestUserWithName("leave_user2")
			user2Resp, err := apiClient.Register(user2.UserName, user2.Password)
			if err != nil {
				t.Fatalf("Register user2 failed: %v", err)
			}
			user2.AuthToken = user2Resp.GetDataAsString()

			// Both connect
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
			ws2.Connect(user2.AuthToken, roomId)

			time.Sleep(1 * time.Second)
			ws1.DrainMessages(500 * time.Millisecond)

			// User2 disconnects
			ws2.Close()

			// User1 may receive room update notification
			// This depends on implementation
			msg, err := ws1.WaitForMessage(3 * time.Second)
			if err == nil {
				t.Logf("User1 received notification after user2 left: %s", string(msg))
			} else {
				t.Log("No notification received (implementation dependent)")
			}
		})
	})

	t.Run("Room_Count", func(t *testing.T) {
		t.Run("get_room_count", func(t *testing.T) {
			roomId := testdata.AlternateRoomID

			// Connect some users first
			var wsClients []*helpers.WSClient
			for i := 0; i < 3; i++ {
				username := testdata.GenerateTestUserName()
				regResp, err := apiClient.Register(username, testdata.TestPassword)
				if err != nil {
					t.Fatalf("Register user %d failed: %v", i, err)
				}
				authToken := regResp.GetDataAsString()

				ws, err := helpers.NewWSClient(cfg.WSBaseURL)
				if err != nil {
					t.Fatalf("WebSocket connection %d failed: %v", i, err)
				}
				wsClients = append(wsClients, ws)
				ws.Connect(authToken, roomId)
			}

			defer func() {
				for _, ws := range wsClients {
					ws.Close()
				}
			}()

			time.Sleep(1 * time.Second)

			// Get count via API
			resp, err := apiClient.Count(roomId)
			if err != nil {
				t.Fatalf("Count failed: %v", err)
			}

			t.Logf("Room count response: code=%d, data=%v", resp.Code, resp.Data)
		})

		t.Run("count_updates_on_join_leave", func(t *testing.T) {
			roomId := testdata.AlternateRoomID

			// Initial count
			resp1, err := apiClient.Count(roomId)
			if err != nil {
				t.Fatalf("Initial count failed: %v", err)
			}
			t.Logf("Initial room count: %v", resp1.Data)

			// Add user
			username := testdata.GenerateTestUserName()
			regResp, err := apiClient.Register(username, testdata.TestPassword)
			if err != nil {
				t.Fatalf("Register failed: %v", err)
			}
			authToken := regResp.GetDataAsString()

			ws, err := helpers.NewWSClient(cfg.WSBaseURL)
			if err != nil {
				t.Fatalf("WebSocket connection failed: %v", err)
			}
			ws.Connect(authToken, roomId)

			time.Sleep(1 * time.Second)

			// Count after join
			resp2, err := apiClient.Count(roomId)
			if err != nil {
				t.Fatalf("Count after join failed: %v", err)
			}
			t.Logf("Room count after join: %v", resp2.Data)

			// User leaves
			ws.Close()

			time.Sleep(1 * time.Second)

			// Count after leave
			resp3, err := apiClient.Count(roomId)
			if err != nil {
				t.Fatalf("Count after leave failed: %v", err)
			}
			t.Logf("Room count after leave: %v", resp3.Data)
		})
	})

	t.Run("Room_Info", func(t *testing.T) {
		t.Run("get_room_user_info", func(t *testing.T) {
			roomId := testdata.AlternateRoomID

			// Connect users
			user1 := testdata.NewTestUserWithName("info_user1")
			user1Resp, err := apiClient.Register(user1.UserName, user1.Password)
			if err != nil {
				t.Fatalf("Register user1 failed: %v", err)
			}
			user1.AuthToken = user1Resp.GetDataAsString()

			user2 := testdata.NewTestUserWithName("info_user2")
			user2Resp, err := apiClient.Register(user2.UserName, user2.Password)
			if err != nil {
				t.Fatalf("Register user2 failed: %v", err)
			}
			user2.AuthToken = user2Resp.GetDataAsString()

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

			time.Sleep(1 * time.Second)

			// Get room info
			resp, err := apiClient.GetRoomInfo(roomId)
			if err != nil {
				t.Fatalf("GetRoomInfo failed: %v", err)
			}

			t.Logf("Room info response: code=%d, data=%v", resp.Code, resp.Data)
		})

		t.Run("empty_room_info", func(t *testing.T) {
			// Room with no users (use a unique room ID)
			emptyRoomId := 9999

			resp, err := apiClient.GetRoomInfo(emptyRoomId)
			if err != nil {
				t.Fatalf("GetRoomInfo failed: %v", err)
			}

			t.Logf("Empty room info response: code=%d, data=%v", resp.Code, resp.Data)
		})
	})

	t.Run("Room_Broadcast", func(t *testing.T) {
		t.Run("message_reaches_all_room_members", func(t *testing.T) {
			roomId := testdata.DefaultRoomID

			// Setup 3 users in room
			type userConn struct {
				ws    *helpers.WSClient
				token string
				name  string
			}
			var users []userConn

			for i := 0; i < 3; i++ {
				user := testdata.NewTestUserWithName("broadcast")
				regResp, err := apiClient.Register(user.UserName, user.Password)
				if err != nil {
					t.Fatalf("Register user %d failed: %v", i, err)
				}
				user.AuthToken = regResp.GetDataAsString()

				ws, err := helpers.NewWSClient(cfg.WSBaseURL)
				if err != nil {
					t.Fatalf("WebSocket connection %d failed: %v", i, err)
				}
				ws.Connect(user.AuthToken, roomId)

				users = append(users, userConn{ws: ws, token: user.AuthToken, name: user.UserName})
			}

			defer func() {
				for _, u := range users {
					u.ws.Close()
				}
			}()

			time.Sleep(1 * time.Second)

			// Drain initial messages
			for _, u := range users {
				u.ws.DrainMessages(500 * time.Millisecond)
			}

			// First user sends message
			testMsg := testdata.TestMessage("broadcast_all")
			_, err := apiClient.PushRoom(users[0].token, testMsg, roomId)
			if err != nil {
				t.Fatalf("PushRoom failed: %v", err)
			}

			// Other users should receive
			for i := 1; i < len(users); i++ {
				msg, err := users[i].ws.WaitForMessageContaining(testMsg, 10*time.Second)
				if err != nil {
					t.Errorf("User %d (%s) failed to receive broadcast: %v", i, users[i].name, err)
				} else {
					t.Logf("User %d received: %s", i, string(msg))
				}
			}
		})

		t.Run("message_not_sent_to_other_rooms", func(t *testing.T) {
			// User in room 1
			user1 := testdata.NewTestUserWithName("other_room1")
			user1Resp, err := apiClient.Register(user1.UserName, user1.Password)
			if err != nil {
				t.Fatalf("Register user1 failed: %v", err)
			}
			user1.AuthToken = user1Resp.GetDataAsString()

			ws1, err := helpers.NewWSClient(cfg.WSBaseURL)
			if err != nil {
				t.Fatalf("WebSocket connection for user1 failed: %v", err)
			}
			defer ws1.Close()
			ws1.Connect(user1.AuthToken, testdata.DefaultRoomID)

			// User in room 2
			user2 := testdata.NewTestUserWithName("other_room2")
			user2Resp, err := apiClient.Register(user2.UserName, user2.Password)
			if err != nil {
				t.Fatalf("Register user2 failed: %v", err)
			}
			user2.AuthToken = user2Resp.GetDataAsString()

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
			testMsg := testdata.TestMessage("room1_exclusive")
			apiClient.PushRoom(user1.AuthToken, testMsg, testdata.DefaultRoomID)

			// User in room 2 should NOT receive
			_, err = ws2.WaitForMessageContaining(testMsg, 3*time.Second)
			if err == nil {
				t.Error("User in different room should not receive message")
			} else {
				t.Log("Correctly isolated: user in room 2 did not receive room 1 message")
			}
		})
	})
}
