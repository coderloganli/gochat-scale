// Package integration provides integration tests for the GoChat API
package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
	"time"

	"gochat/tests/helpers"
)

const testAPIURL = "http://localhost:7070"
const testMinIOURL = "http://localhost:9000"

// TestImageUpload tests the image upload functionality
func TestImageUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := helpers.NewAPIClient(testAPIURL)

	// Register and login a user
	userName := fmt.Sprintf("imagetest_%d", time.Now().UnixNano())
	resp, err := client.Register(userName, "testpass123")
	if err != nil {
		t.Fatalf("Failed to register user: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("Register failed with code: %d", resp.Code)
	}

	authToken := resp.GetDataAsMap()["authToken"].(string)
	if authToken == "" {
		t.Fatal("No auth token returned from register")
	}

	// Create a test image (1x1 pixel PNG)
	testImageData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xDE, 0x00, 0x00, 0x00, 0x0C, 0x49, 0x44, 0x41, // IDAT chunk
		0x54, 0x08, 0xD7, 0x63, 0xF8, 0xFF, 0xFF, 0x3F,
		0x00, 0x05, 0xFE, 0x02, 0xFE, 0xDC, 0xCC, 0x59,
		0xE7, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, // IEND chunk
		0x44, 0xAE, 0x42, 0x60, 0x82,
	}

	// Upload the image
	imageURL, err := uploadTestImage(authToken, testImageData, "test.png")
	if err != nil {
		t.Fatalf("Failed to upload image: %v", err)
	}

	if imageURL == "" {
		t.Fatal("No image URL returned")
	}

	t.Logf("Image uploaded successfully: %s", imageURL)
}

// TestImageUploadInvalidType tests that invalid image types are rejected
func TestImageUploadInvalidType(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := helpers.NewAPIClient(testAPIURL)

	// Register and login a user
	userName := fmt.Sprintf("imagetest_%d", time.Now().UnixNano())
	resp, err := client.Register(userName, "testpass123")
	if err != nil {
		t.Fatalf("Failed to register user: %v", err)
	}

	authToken := resp.GetDataAsMap()["authToken"].(string)

	// Try to upload a non-image file
	textData := []byte("This is not an image")
	_, err = uploadTestImageWithContentType(authToken, textData, "test.txt", "text/plain")
	if err == nil {
		t.Fatal("Expected error for invalid file type, got none")
	}
	t.Logf("Correctly rejected invalid file type: %v", err)
}

// TestImageUploadTooLarge tests that oversized images are rejected
func TestImageUploadTooLarge(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := helpers.NewAPIClient(testAPIURL)

	// Register and login a user
	userName := fmt.Sprintf("imagetest_%d", time.Now().UnixNano())
	resp, err := client.Register(userName, "testpass123")
	if err != nil {
		t.Fatalf("Failed to register user: %v", err)
	}

	authToken := resp.GetDataAsMap()["authToken"].(string)

	// Create a large fake image (11MB, exceeds 10MB limit)
	largeData := make([]byte, 11*1024*1024)
	_, err = uploadTestImageWithContentType(authToken, largeData, "large.png", "image/png")
	if err == nil {
		t.Fatal("Expected error for oversized file, got none")
	}
	t.Logf("Correctly rejected oversized file: %v", err)
}

// TestSendImageMessage tests sending an image message in single chat
func TestSendImageMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := helpers.NewAPIClient(testAPIURL)

	// Register two users
	user1Name := fmt.Sprintf("imguser1_%d", time.Now().UnixNano())
	user2Name := fmt.Sprintf("imguser2_%d", time.Now().UnixNano())

	resp1, err := client.Register(user1Name, "testpass123")
	if err != nil {
		t.Fatalf("Failed to register user1: %v", err)
	}
	authToken1 := resp1.GetDataAsMap()["authToken"].(string)

	resp2, err := client.Register(user2Name, "testpass123")
	if err != nil {
		t.Fatalf("Failed to register user2: %v", err)
	}

	// Get user2's ID from the data
	user2Data := resp2.GetDataAsMap()
	var user2Id string
	if id, ok := user2Data["userId"]; ok {
		user2Id = fmt.Sprintf("%v", id)
	} else {
		// Try to get from check auth
		authToken2 := user2Data["authToken"].(string)
		checkResp, _ := client.CheckAuth(authToken2)
		if checkResp != nil && checkResp.Code == 0 {
			checkData := checkResp.GetDataAsMap()
			if id, ok := checkData["userId"]; ok {
				user2Id = fmt.Sprintf("%v", id)
			}
		}
	}

	if user2Id == "" {
		t.Skip("Could not determine user2 ID, skipping test")
	}

	// Send an image message
	imageURL := "http://minio:9000/gochat-images/test/test-image.jpg"
	resp, err := client.PushWithContentType(authToken1, imageURL, user2Id, 1, "image")
	if err != nil {
		t.Fatalf("Failed to send image message: %v", err)
	}

	if resp.Code != 0 {
		t.Fatalf("Send image message failed with code: %d", resp.Code)
	}

	t.Log("Image message sent successfully")
}

// TestSendImageMessageToRoom tests sending an image message to a room
func TestSendImageMessageToRoom(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := helpers.NewAPIClient(testAPIURL)

	// Register a user
	userName := fmt.Sprintf("roomimguser_%d", time.Now().UnixNano())
	resp, err := client.Register(userName, "testpass123")
	if err != nil {
		t.Fatalf("Failed to register user: %v", err)
	}
	authToken := resp.GetDataAsMap()["authToken"].(string)

	// Send an image message to room
	imageURL := "http://minio:9000/gochat-images/test/room-image.jpg"
	roomId := 1
	resp, err = client.PushRoomWithContentType(authToken, imageURL, roomId, "image")
	if err != nil {
		t.Fatalf("Failed to send image to room: %v", err)
	}

	if resp.Code != 0 {
		t.Fatalf("Send room image message failed with code: %d", resp.Code)
	}

	t.Log("Room image message sent successfully")
}

// TestImageMessageInHistory tests that image messages appear in history with correct contentType
func TestImageMessageInHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	client := helpers.NewAPIClient(testAPIURL)

	// Register a user
	userName := fmt.Sprintf("histimguser_%d", time.Now().UnixNano())
	resp, err := client.Register(userName, "testpass123")
	if err != nil {
		t.Fatalf("Failed to register user: %v", err)
	}
	authToken := resp.GetDataAsMap()["authToken"].(string)

	// Send an image message to room
	imageURL := "http://minio:9000/gochat-images/test/history-image.jpg"
	roomId := 100 + int(time.Now().Unix()%1000) // Use unique room ID
	resp, err = client.PushRoomWithContentType(authToken, imageURL, roomId, "image")
	if err != nil {
		t.Fatalf("Failed to send image to room: %v", err)
	}

	// Wait a bit for message to be persisted
	time.Sleep(100 * time.Millisecond)

	// Get room history
	histResp, err := client.GetRoomHistory(authToken, roomId, 10, 0)
	if err != nil {
		t.Fatalf("Failed to get room history: %v", err)
	}

	if histResp.Code != 0 {
		t.Fatalf("Get room history failed with code: %d", histResp.Code)
	}

	// Check that the message has contentType = "image"
	messages := histResp.GetDataAsMap()["messages"]
	if messages == nil {
		t.Fatal("No messages in history response")
	}

	msgList, ok := messages.([]interface{})
	if !ok || len(msgList) == 0 {
		t.Fatal("Empty message list in history")
	}

	// Check the first message (most recent)
	firstMsg := msgList[0].(map[string]interface{})
	contentType := firstMsg["contentType"].(string)
	content := firstMsg["content"].(string)

	if contentType != "image" {
		t.Errorf("Expected contentType 'image', got '%s'", contentType)
	}

	if content != imageURL {
		t.Errorf("Expected content '%s', got '%s'", imageURL, content)
	}

	t.Logf("Image message found in history with contentType: %s", contentType)
}

// Helper function to upload a test image
func uploadTestImage(authToken string, imageData []byte, filename string) (string, error) {
	return uploadTestImageWithContentType(authToken, imageData, filename, "image/png")
}

// Helper function to upload a test image with specific content type
func uploadTestImageWithContentType(authToken string, data []byte, filename, contentType string) (string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add auth token field
	err := writer.WriteField("authToken", authToken)
	if err != nil {
		return "", fmt.Errorf("failed to write authToken field: %w", err)
	}

	// Add file field
	part, err := writer.CreateFormFile("image", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}

	_, err = io.Copy(part, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("failed to copy file data: %w", err)
	}

	err = writer.Close()
	if err != nil {
		return "", fmt.Errorf("failed to close writer: %w", err)
	}

	req, err := http.NewRequest("POST", testAPIURL+"/push/uploadImage", body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var apiResp struct {
		Code    int                    `json:"code"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data"`
	}

	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse response: %w, body: %s", err, string(respBody))
	}

	if apiResp.Code != 0 {
		return "", fmt.Errorf("upload failed: %s", apiResp.Message)
	}

	if apiResp.Data == nil {
		return "", fmt.Errorf("no data in response")
	}

	imageURL, ok := apiResp.Data["imageUrl"].(string)
	if !ok {
		return "", fmt.Errorf("no imageUrl in response data")
	}

	return imageURL, nil
}

// isMinIOAvailable checks if MinIO is available for testing
func isMinIOAvailable() bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(testMinIOURL + "/minio/health/live")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
