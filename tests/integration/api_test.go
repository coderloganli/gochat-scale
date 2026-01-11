package integration

import (
	"fmt"
	"testing"

	"gochat/tests/helpers"
	"gochat/tests/testdata"
)

func TestAPIEndpoints(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := helpers.DefaultTestConfig()
	client := helpers.NewAPIClient(cfg.APIBaseURL)

	t.Run("User_Registration", func(t *testing.T) {
		t.Run("successful_registration", func(t *testing.T) {
			username := testdata.GenerateTestUserName()
			resp, err := client.Register(username, testdata.TestPassword)

			if err != nil {
				t.Fatalf("Register failed: %v", err)
			}
			if resp.Code != testdata.CodeSuccess {
				t.Errorf("Expected code %d, got %d: %v", testdata.CodeSuccess, resp.Code, resp.Message)
			}
			authToken := resp.GetDataAsString()
			if authToken == "" {
				t.Error("Expected authToken in response data")
			}
		})

		t.Run("duplicate_username_rejected", func(t *testing.T) {
			username := testdata.GenerateTestUserName()

			// First registration
			_, err := client.Register(username, testdata.TestPassword)
			if err != nil {
				t.Fatalf("First register failed: %v", err)
			}

			// Duplicate registration
			resp, err := client.Register(username, testdata.TestPassword)
			if err != nil {
				t.Fatalf("Second register request failed: %v", err)
			}
			if resp.Code == testdata.CodeSuccess {
				t.Error("Expected failure code for duplicate registration")
			}
		})

		t.Run("empty_username_rejected", func(t *testing.T) {
			resp, err := client.Register("", testdata.TestPassword)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			if resp.Code == testdata.CodeSuccess {
				t.Error("Expected failure for empty username")
			}
		})

		t.Run("empty_password_rejected", func(t *testing.T) {
			username := testdata.GenerateTestUserName()
			resp, err := client.Register(username, "")
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			if resp.Code == testdata.CodeSuccess {
				t.Error("Expected failure for empty password")
			}
		})
	})

	t.Run("User_Login", func(t *testing.T) {
		// Setup: register a user first
		username := testdata.GenerateTestUserName()
		_, err := client.Register(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}

		t.Run("successful_login", func(t *testing.T) {
			resp, err := client.Login(username, testdata.TestPassword)

			if err != nil {
				t.Fatalf("Login failed: %v", err)
			}
			if resp.Code != testdata.CodeSuccess {
				t.Errorf("Expected code %d, got %d", testdata.CodeSuccess, resp.Code)
			}
			authToken := resp.GetDataAsString()
			if authToken == "" {
				t.Error("Expected authToken in response data")
			}
		})

		t.Run("wrong_password_rejected", func(t *testing.T) {
			resp, err := client.Login(username, "wrong_password")

			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			if resp.Code == testdata.CodeSuccess {
				t.Error("Expected failure for wrong password")
			}
		})

		t.Run("nonexistent_user_rejected", func(t *testing.T) {
			resp, err := client.Login("nonexistent_user_12345", testdata.TestPassword)

			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			if resp.Code == testdata.CodeSuccess {
				t.Error("Expected failure for non-existent user")
			}
		})

		t.Run("empty_username_rejected", func(t *testing.T) {
			resp, err := client.Login("", testdata.TestPassword)

			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			if resp.Code == testdata.CodeSuccess {
				t.Error("Expected failure for empty username")
			}
		})
	})

	t.Run("Auth_Token_Validation", func(t *testing.T) {
		// Setup: register and get token
		username := testdata.GenerateTestUserName()
		regResp, err := client.Register(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}
		authToken := regResp.GetDataAsString()

		t.Run("valid_token_accepted", func(t *testing.T) {
			resp, err := client.CheckAuth(authToken)

			if err != nil {
				t.Fatalf("CheckAuth failed: %v", err)
			}
			if resp.Code != testdata.CodeSuccess {
				t.Errorf("Expected code %d, got %d", testdata.CodeSuccess, resp.Code)
			}
			data := resp.GetDataAsMap()
			if data == nil {
				t.Fatal("Expected data map in response")
			}
			if _, ok := data["userId"]; !ok {
				t.Error("Expected userId in response data")
			}
			if _, ok := data["userName"]; !ok {
				t.Error("Expected userName in response data")
			}
		})

		t.Run("invalid_token_rejected", func(t *testing.T) {
			resp, err := client.CheckAuth("invalid_token_12345")

			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			if resp.Code == testdata.CodeSuccess {
				t.Error("Expected failure for invalid token")
			}
		})

		t.Run("empty_token_rejected", func(t *testing.T) {
			resp, err := client.CheckAuth("")

			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			if resp.Code == testdata.CodeSuccess {
				t.Error("Expected failure for empty token")
			}
		})
	})

	t.Run("Logout", func(t *testing.T) {
		// Setup: register and get token
		username := testdata.GenerateTestUserName()
		regResp, err := client.Register(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Setup failed: %v", err)
		}
		authToken := regResp.GetDataAsString()

		t.Run("successful_logout", func(t *testing.T) {
			resp, err := client.Logout(authToken)

			if err != nil {
				t.Fatalf("Logout failed: %v", err)
			}
			if resp.Code != testdata.CodeSuccess {
				t.Errorf("Expected code %d, got %d", testdata.CodeSuccess, resp.Code)
			}
		})

		t.Run("token_invalid_after_logout", func(t *testing.T) {
			// Token should be invalidated after logout
			resp, err := client.CheckAuth(authToken)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			if resp.Code == testdata.CodeSuccess {
				t.Error("Expected token to be invalid after logout")
			}
		})
	})

	t.Run("Push_Endpoints", func(t *testing.T) {
		// Setup: register users
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
		receiverUserId := "0"
		if receiverData != nil {
			if uid, ok := receiverData["userId"].(float64); ok {
				receiverUserId = fmt.Sprintf("%.0f", uid)
			}
		}

		t.Run("push_single_message", func(t *testing.T) {
			resp, err := client.Push(sender.AuthToken, "hello", receiverUserId, testdata.DefaultRoomID)

			if err != nil {
				t.Fatalf("Push failed: %v", err)
			}
			// Note: Push may succeed even if receiver is not connected
			t.Logf("Push response: code=%d, message=%v", resp.Code, resp.Message)
		})

		t.Run("push_room_message", func(t *testing.T) {
			resp, err := client.PushRoom(sender.AuthToken, "hello room", testdata.DefaultRoomID)

			if err != nil {
				t.Fatalf("PushRoom failed: %v", err)
			}
			t.Logf("PushRoom response: code=%d, message=%v", resp.Code, resp.Message)
		})

		t.Run("push_with_invalid_token_rejected", func(t *testing.T) {
			resp, err := client.Push("invalid_token", "hello", receiverUserId, testdata.DefaultRoomID)

			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			if resp.Code == testdata.CodeSuccess {
				t.Error("Expected failure for invalid token")
			}
		})

		t.Run("get_room_count", func(t *testing.T) {
			resp, err := client.Count(testdata.DefaultRoomID)

			if err != nil {
				t.Fatalf("Count failed: %v", err)
			}
			t.Logf("Room count response: code=%d, data=%v", resp.Code, resp.Data)
		})

		t.Run("get_room_info", func(t *testing.T) {
			resp, err := client.GetRoomInfo(testdata.DefaultRoomID)

			if err != nil {
				t.Fatalf("GetRoomInfo failed: %v", err)
			}
			t.Logf("Room info response: code=%d, data=%v", resp.Code, resp.Data)
		})
	})
}
