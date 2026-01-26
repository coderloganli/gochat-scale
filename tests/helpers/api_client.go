// Package helpers provides test utilities for integration tests.
package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// APIClient wraps HTTP client for API testing
type APIClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewAPIClient creates a new API test client
func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// APIResponse represents a standard API response
type APIResponse struct {
	Code    int         `json:"code"`
	Message interface{} `json:"message"`
	Data    interface{} `json:"data"`
}

// GetDataAsString returns the data field as a string
func (r *APIResponse) GetDataAsString() string {
	if r.Data == nil {
		return ""
	}
	if s, ok := r.Data.(string); ok {
		return s
	}
	return ""
}

// GetDataAsMap returns the data field as a map
func (r *APIResponse) GetDataAsMap() map[string]interface{} {
	if r.Data == nil {
		return nil
	}
	if m, ok := r.Data.(map[string]interface{}); ok {
		return m
	}
	return nil
}

// Register creates a new user
func (c *APIClient) Register(userName, password string) (*APIResponse, error) {
	return c.post("/user/register", map[string]interface{}{
		"userName": userName,
		"passWord": password,
	})
}

// Login authenticates a user
func (c *APIClient) Login(userName, password string) (*APIResponse, error) {
	return c.post("/user/login", map[string]interface{}{
		"userName": userName,
		"passWord": password,
	})
}

// CheckAuth validates an auth token
func (c *APIClient) CheckAuth(authToken string) (*APIResponse, error) {
	return c.post("/user/checkAuth", map[string]interface{}{
		"authToken": authToken,
	})
}

// Logout invalidates a session
func (c *APIClient) Logout(authToken string) (*APIResponse, error) {
	return c.post("/user/logout", map[string]interface{}{
		"authToken": authToken,
	})
}

// Push sends a direct message to a user
func (c *APIClient) Push(authToken, msg, toUserId string, roomId int) (*APIResponse, error) {
	return c.post("/push/push", map[string]interface{}{
		"authToken": authToken,
		"msg":       msg,
		"toUserId":  toUserId,
		"roomId":    roomId,
	})
}

// PushRoom sends a room broadcast message
func (c *APIClient) PushRoom(authToken, msg string, roomId int) (*APIResponse, error) {
	return c.post("/push/pushRoom", map[string]interface{}{
		"authToken": authToken,
		"msg":       msg,
		"roomId":    roomId,
	})
}

// Count gets room online count
func (c *APIClient) Count(roomId int) (*APIResponse, error) {
	return c.post("/push/count", map[string]interface{}{
		"roomId": roomId,
	})
}

// GetRoomInfo retrieves room user information
func (c *APIClient) GetRoomInfo(roomId int) (*APIResponse, error) {
	return c.post("/push/getRoomInfo", map[string]interface{}{
		"roomId": roomId,
	})
}

// GetSingleChatHistory retrieves message history between two users
func (c *APIClient) GetSingleChatHistory(authToken string, otherUserId int, limit, offset int) (*APIResponse, error) {
	return c.post("/push/history/single", map[string]interface{}{
		"authToken":   authToken,
		"otherUserId": otherUserId,
		"limit":       limit,
		"offset":      offset,
	})
}

// GetRoomHistory retrieves message history for a room
func (c *APIClient) GetRoomHistory(authToken string, roomId int, limit, offset int) (*APIResponse, error) {
	return c.post("/push/history/room", map[string]interface{}{
		"authToken": authToken,
		"roomId":    roomId,
		"limit":     limit,
		"offset":    offset,
	})
}

func (c *APIClient) post(path string, body interface{}) (*APIResponse, error) {
	jsonData, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}

	resp, err := c.HTTPClient.Post(
		c.BaseURL+path,
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return nil, fmt.Errorf("HTTP POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w, body: %s", err, string(respBody))
	}

	return &apiResp, nil
}
