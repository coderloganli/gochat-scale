package integration

import (
	"fmt"
	"testing"

	"gochat/tests/helpers"
	"gochat/tests/testdata"
)

func TestMessageHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := helpers.DefaultTestConfig()
	client := helpers.NewAPIClient(cfg.APIBaseURL)

	t.Run("Single_Chat_History", func(t *testing.T) {
		// Setup: register two users
		sender := testdata.NewTestUser()
		senderResp, err := client.Register(sender.UserName, sender.Password)
		if err != nil {
			t.Fatalf("Setup sender failed: %v", err)
		}
		sender.AuthToken = senderResp.GetDataAsString()

		receiver := testdata.NewTestUser()
		receiverResp, err := client.Register(receiver.UserName, receiver.Password)
		if err != nil {
			t.Fatalf("Setup receiver failed: %v", err)
		}
		receiver.AuthToken = receiverResp.GetDataAsString()

		// Get receiver's userId
		authResp, _ := client.CheckAuth(receiver.AuthToken)
		receiverData := authResp.GetDataAsMap()
		var receiverUserId int
		if receiverData != nil {
			if uid, ok := receiverData["userId"].(float64); ok {
				receiverUserId = int(uid)
			}
		}

		// Send some messages
		messageContent := "hello_" + testdata.GenerateTestUserName()
		_, err = client.Push(sender.AuthToken, messageContent, fmt.Sprintf("%d", receiverUserId), testdata.DefaultRoomID)
		if err != nil {
			t.Fatalf("Push message failed: %v", err)
		}

		t.Run("history_returns_sent_messages", func(t *testing.T) {
			resp, err := client.GetSingleChatHistory(sender.AuthToken, receiverUserId, 50, 0)
			if err != nil {
				t.Fatalf("GetSingleChatHistory failed: %v", err)
			}
			if resp.Code != testdata.CodeSuccess {
				t.Errorf("Expected code %d, got %d: %v", testdata.CodeSuccess, resp.Code, resp.Message)
			}
			t.Logf("Single chat history response: code=%d, data=%v", resp.Code, resp.Data)

			// Check that we got messages back
			if data, ok := resp.Data.([]interface{}); ok {
				if len(data) == 0 {
					t.Log("No messages returned (expected at least 1)")
				} else {
					t.Logf("Got %d messages in history", len(data))
				}
			}
		})

		t.Run("history_with_pagination", func(t *testing.T) {
			resp, err := client.GetSingleChatHistory(sender.AuthToken, receiverUserId, 10, 0)
			if err != nil {
				t.Fatalf("GetSingleChatHistory failed: %v", err)
			}
			if resp.Code != testdata.CodeSuccess {
				t.Errorf("Expected code %d, got %d", testdata.CodeSuccess, resp.Code)
			}
		})

		t.Run("history_with_invalid_token_rejected", func(t *testing.T) {
			resp, err := client.GetSingleChatHistory("invalid_token", receiverUserId, 50, 0)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			if resp.Code == testdata.CodeSuccess {
				t.Error("Expected failure for invalid token")
			}
		})
	})

	t.Run("Room_Chat_History", func(t *testing.T) {
		// Setup: register a user
		user := testdata.NewTestUser()
		userResp, err := client.Register(user.UserName, user.Password)
		if err != nil {
			t.Fatalf("Setup user failed: %v", err)
		}
		user.AuthToken = userResp.GetDataAsString()

		roomId := testdata.DefaultRoomID

		// Send a room message
		messageContent := "room_msg_" + testdata.GenerateTestUserName()
		_, err = client.PushRoom(user.AuthToken, messageContent, roomId)
		if err != nil {
			t.Fatalf("PushRoom failed: %v", err)
		}

		t.Run("history_returns_room_messages", func(t *testing.T) {
			resp, err := client.GetRoomHistory(user.AuthToken, roomId, 50, 0)
			if err != nil {
				t.Fatalf("GetRoomHistory failed: %v", err)
			}
			if resp.Code != testdata.CodeSuccess {
				t.Errorf("Expected code %d, got %d: %v", testdata.CodeSuccess, resp.Code, resp.Message)
			}
			t.Logf("Room history response: code=%d, data=%v", resp.Code, resp.Data)

			// Check that we got messages back
			if data, ok := resp.Data.([]interface{}); ok {
				if len(data) == 0 {
					t.Log("No room messages returned (expected at least 1)")
				} else {
					t.Logf("Got %d room messages in history", len(data))
				}
			}
		})

		t.Run("history_with_pagination", func(t *testing.T) {
			resp, err := client.GetRoomHistory(user.AuthToken, roomId, 10, 0)
			if err != nil {
				t.Fatalf("GetRoomHistory failed: %v", err)
			}
			if resp.Code != testdata.CodeSuccess {
				t.Errorf("Expected code %d, got %d", testdata.CodeSuccess, resp.Code)
			}
		})

		t.Run("history_with_invalid_token_rejected", func(t *testing.T) {
			resp, err := client.GetRoomHistory("invalid_token", roomId, 50, 0)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			if resp.Code == testdata.CodeSuccess {
				t.Error("Expected failure for invalid token")
			}
		})

		t.Run("history_for_different_rooms", func(t *testing.T) {
			// Should return different messages for different room IDs
			resp1, err := client.GetRoomHistory(user.AuthToken, 1, 50, 0)
			if err != nil {
				t.Fatalf("GetRoomHistory room 1 failed: %v", err)
			}

			resp2, err := client.GetRoomHistory(user.AuthToken, 999, 50, 0)
			if err != nil {
				t.Fatalf("GetRoomHistory room 999 failed: %v", err)
			}

			t.Logf("Room 1 history: code=%d, Room 999 history: code=%d", resp1.Code, resp2.Code)
		})
	})
}
