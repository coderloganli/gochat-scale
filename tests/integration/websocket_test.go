package integration

import (
	"testing"
	"time"

	"gochat/tests/helpers"
	"gochat/tests/testdata"
)

func TestWebSocketLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := helpers.DefaultTestConfig()
	apiClient := helpers.NewAPIClient(cfg.APIBaseURL)

	t.Run("Connection_Establishment", func(t *testing.T) {
		t.Run("successful_connection_with_valid_token", func(t *testing.T) {
			// Register user
			username := testdata.GenerateTestUserName()
			regResp, err := apiClient.Register(username, testdata.TestPassword)
			if err != nil {
				t.Fatalf("Register failed: %v", err)
			}
			authToken := regResp.GetDataAsString()

			// Connect WebSocket
			wsClient, err := helpers.NewWSClient(cfg.WSBaseURL)
			if err != nil {
				t.Fatalf("WebSocket connection failed: %v", err)
			}
			defer wsClient.Close()

			// Authenticate
			err = wsClient.Connect(authToken, testdata.DefaultRoomID)
			if err != nil {
				t.Fatalf("WebSocket auth failed: %v", err)
			}

			// Connection should remain open
			time.Sleep(500 * time.Millisecond)

			if wsClient.IsClosed() {
				t.Error("Connection should remain open after authentication")
			}
		})

		t.Run("connection_with_invalid_token", func(t *testing.T) {
			wsClient, err := helpers.NewWSClient(cfg.WSBaseURL)
			if err != nil {
				t.Fatalf("WebSocket connection failed: %v", err)
			}
			defer wsClient.Close()

			err = wsClient.Connect("invalid_token_12345", testdata.DefaultRoomID)
			if err != nil {
				t.Fatalf("Send connect message failed: %v", err)
			}

			// Server may close connection or send error
			// Wait and check connection state
			time.Sleep(2 * time.Second)

			// The behavior depends on implementation - log the result
			t.Logf("Connection closed after invalid token: %v", wsClient.IsClosed())
		})

		t.Run("connection_with_empty_token", func(t *testing.T) {
			wsClient, err := helpers.NewWSClient(cfg.WSBaseURL)
			if err != nil {
				t.Fatalf("WebSocket connection failed: %v", err)
			}
			defer wsClient.Close()

			err = wsClient.Connect("", testdata.DefaultRoomID)
			if err != nil {
				t.Fatalf("Send connect message failed: %v", err)
			}

			// Wait and check
			time.Sleep(1 * time.Second)
			t.Logf("Connection closed after empty token: %v", wsClient.IsClosed())
		})
	})

	t.Run("Graceful_Disconnect", func(t *testing.T) {
		// Register user
		username := testdata.GenerateTestUserName()
		regResp, err := apiClient.Register(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		authToken := regResp.GetDataAsString()

		// Connect
		wsClient, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("WebSocket connection failed: %v", err)
		}

		err = wsClient.Connect(authToken, testdata.DefaultRoomID)
		if err != nil {
			t.Fatalf("WebSocket auth failed: %v", err)
		}

		time.Sleep(500 * time.Millisecond)

		// Close gracefully
		err = wsClient.Close()
		if err != nil {
			t.Errorf("Error closing connection: %v", err)
		}

		if !wsClient.IsClosed() {
			t.Error("Connection should be marked as closed")
		}
	})

	t.Run("Multiple_Connections_Same_User", func(t *testing.T) {
		// Register user
		username := testdata.GenerateTestUserName()
		regResp, err := apiClient.Register(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		authToken := regResp.GetDataAsString()

		// First connection
		ws1, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("First WebSocket connection failed: %v", err)
		}
		defer ws1.Close()

		err = ws1.Connect(authToken, testdata.DefaultRoomID)
		if err != nil {
			t.Fatalf("First WebSocket auth failed: %v", err)
		}

		time.Sleep(300 * time.Millisecond)

		// Second connection with same token
		ws2, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("Second WebSocket connection failed: %v", err)
		}
		defer ws2.Close()

		err = ws2.Connect(authToken, testdata.DefaultRoomID)
		if err != nil {
			t.Fatalf("Second WebSocket auth failed: %v", err)
		}

		time.Sleep(500 * time.Millisecond)

		// Log the state of both connections
		t.Logf("First connection closed: %v", ws1.IsClosed())
		t.Logf("Second connection closed: %v", ws2.IsClosed())
	})

	t.Run("Connection_To_Different_Rooms", func(t *testing.T) {
		// Register user
		username := testdata.GenerateTestUserName()
		regResp, err := apiClient.Register(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		authToken := regResp.GetDataAsString()

		// Connect to room 1
		ws1, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("WebSocket connection to room 1 failed: %v", err)
		}
		defer ws1.Close()

		err = ws1.Connect(authToken, testdata.DefaultRoomID)
		if err != nil {
			t.Fatalf("WebSocket auth for room 1 failed: %v", err)
		}

		time.Sleep(300 * time.Millisecond)

		// Connect to room 2
		ws2, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("WebSocket connection to room 2 failed: %v", err)
		}
		defer ws2.Close()

		err = ws2.Connect(authToken, testdata.AlternateRoomID)
		if err != nil {
			t.Fatalf("WebSocket auth for room 2 failed: %v", err)
		}

		time.Sleep(500 * time.Millisecond)

		// Both should be open (depending on implementation)
		t.Logf("Room 1 connection closed: %v", ws1.IsClosed())
		t.Logf("Room 2 connection closed: %v", ws2.IsClosed())
	})

	t.Run("Reconnection_After_Disconnect", func(t *testing.T) {
		// Register user
		username := testdata.GenerateTestUserName()
		regResp, err := apiClient.Register(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		authToken := regResp.GetDataAsString()

		// First connection
		ws1, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("First connection failed: %v", err)
		}

		err = ws1.Connect(authToken, testdata.DefaultRoomID)
		if err != nil {
			t.Fatalf("First auth failed: %v", err)
		}

		time.Sleep(300 * time.Millisecond)

		// Disconnect
		ws1.Close()

		time.Sleep(300 * time.Millisecond)

		// Reconnect
		ws2, err := helpers.NewWSClient(cfg.WSBaseURL)
		if err != nil {
			t.Fatalf("Reconnection failed: %v", err)
		}
		defer ws2.Close()

		err = ws2.Connect(authToken, testdata.DefaultRoomID)
		if err != nil {
			t.Fatalf("Reconnection auth failed: %v", err)
		}

		time.Sleep(500 * time.Millisecond)

		if ws2.IsClosed() {
			t.Error("Reconnection should succeed")
		}
	})
}
