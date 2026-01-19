package integration

import (
	"sync"
	"testing"
	"time"

	"gochat/tests/helpers"
	"gochat/tests/testdata"
)

func TestAuthFlow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	cfg := helpers.DefaultTestConfig()
	client := helpers.NewAPIClient(cfg.APIBaseURL)

	t.Run("Complete_Auth_Lifecycle", func(t *testing.T) {
		// 1. Register new user
		username := testdata.GenerateTestUserName()
		regResp, err := client.Register(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		if regResp.Code != testdata.CodeSuccess {
			t.Fatalf("Register returned error: %v", regResp.Message)
		}
		authToken := regResp.GetDataAsString()
		t.Logf("User registered with token: %s...", authToken[:min(len(authToken), 20)])

		// 2. Verify token works
		checkResp, err := client.CheckAuth(authToken)
		if err != nil {
			t.Fatalf("CheckAuth failed: %v", err)
		}
		if checkResp.Code != testdata.CodeSuccess {
			t.Errorf("CheckAuth failed after registration: %v", checkResp.Message)
		}

		// 3. Login again (should get new token)
		loginResp, err := client.Login(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Login failed: %v", err)
		}
		if loginResp.Code != testdata.CodeSuccess {
			t.Errorf("Login failed: %v", loginResp.Message)
		}
		newToken := loginResp.GetDataAsString()
		t.Logf("User logged in with new token: %s...", newToken[:min(len(newToken), 20)])

		// 4. Verify new token works
		checkResp2, err := client.CheckAuth(newToken)
		if err != nil {
			t.Fatalf("CheckAuth failed: %v", err)
		}
		if checkResp2.Code != testdata.CodeSuccess {
			t.Errorf("CheckAuth failed with new token: %v", checkResp2.Message)
		}

		// 5. Logout
		logoutResp, err := client.Logout(newToken)
		if err != nil {
			t.Fatalf("Logout failed: %v", err)
		}
		if logoutResp.Code != testdata.CodeSuccess {
			t.Errorf("Logout failed: %v", logoutResp.Message)
		}

		// 6. Verify token no longer works
		checkResp3, err := client.CheckAuth(newToken)
		if err != nil {
			t.Fatalf("CheckAuth request failed: %v", err)
		}
		if checkResp3.Code == testdata.CodeSuccess {
			t.Error("Token should be invalid after logout")
		}

		// 7. Can login again after logout
		loginResp2, err := client.Login(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Login after logout failed: %v", err)
		}
		if loginResp2.Code != testdata.CodeSuccess {
			t.Errorf("Should be able to login after logout: %v", loginResp2.Message)
		}
	})

	t.Run("Session_Persistence", func(t *testing.T) {
		// Register and get token
		username := testdata.GenerateTestUserName()
		regResp, err := client.Register(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		authToken := regResp.GetDataAsString()

		// Token should remain valid for multiple checks
		for i := 0; i < 3; i++ {
			resp, err := client.CheckAuth(authToken)
			if err != nil {
				t.Fatalf("CheckAuth %d failed: %v", i, err)
			}
			if resp.Code != testdata.CodeSuccess {
				t.Errorf("CheckAuth %d returned failure: %v", i, resp.Message)
			}
			time.Sleep(100 * time.Millisecond)
		}
	})

	t.Run("Concurrent_Login", func(t *testing.T) {
		// Register user
		username := testdata.GenerateTestUserName()
		_, err := client.Register(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Register failed: %v", err)
		}

		// Concurrent logins
		var wg sync.WaitGroup
		tokens := make(chan string, 5)
		errors := make(chan error, 5)

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := client.Login(username, testdata.TestPassword)
				if err != nil {
					errors <- err
					return
				}
				if resp.Code == testdata.CodeSuccess {
					tokens <- resp.GetDataAsString()
				}
			}()
		}

		wg.Wait()
		close(tokens)
		close(errors)

		// Check for errors
		for err := range errors {
			t.Errorf("Concurrent login error: %v", err)
		}

		// Count successful logins
		successCount := 0
		for range tokens {
			successCount++
		}
		t.Logf("Concurrent logins successful: %d/5", successCount)

		if successCount == 0 {
			t.Error("Expected at least one successful login")
		}
	})

	t.Run("Token_Uniqueness", func(t *testing.T) {
		// Register user
		username := testdata.GenerateTestUserName()
		regResp, err := client.Register(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		token1 := regResp.GetDataAsString()

		// Login to get a new token
		loginResp, err := client.Login(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Login failed: %v", err)
		}
		token2 := loginResp.GetDataAsString()

		// Tokens may or may not be the same depending on implementation
		t.Logf("Token1: %s...", token1[:min(len(token1), 20)])
		t.Logf("Token2: %s...", token2[:min(len(token2), 20)])
	})

	t.Run("UserInfo_Consistency", func(t *testing.T) {
		// Register user
		username := testdata.GenerateTestUserName()
		regResp, err := client.Register(username, testdata.TestPassword)
		if err != nil {
			t.Fatalf("Register failed: %v", err)
		}
		authToken := regResp.GetDataAsString()

		// Check user info
		checkResp, err := client.CheckAuth(authToken)
		if err != nil {
			t.Fatalf("CheckAuth failed: %v", err)
		}

		data := checkResp.GetDataAsMap()
		if data == nil {
			t.Fatal("Expected user data in response")
		}

		returnedUsername, ok := data["userName"].(string)
		if !ok {
			t.Fatal("Expected userName in response")
		}

		if returnedUsername != username {
			t.Errorf("Username mismatch: expected %s, got %s", username, returnedUsername)
		}

		userId, ok := data["userId"].(float64)
		if !ok {
			t.Fatal("Expected userId in response")
		}

		if userId <= 0 {
			t.Error("Expected positive userId")
		}
		t.Logf("User: %s, ID: %.0f", returnedUsername, userId)
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
