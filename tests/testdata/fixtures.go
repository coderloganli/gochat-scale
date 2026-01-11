// Package testdata provides test data constants and generators.
package testdata

import (
	"fmt"
	"time"
)

// Test user credentials
const (
	TestUserPrefix  = "test_user_"
	TestPassword    = "test_password_123"
	DefaultRoomID   = 1
	AlternateRoomID = 2
)

// Response codes
const (
	CodeSuccess = 0
	CodeFail    = 1
)

// Operation codes (matching config/op.go)
const (
	OpSingleSend    = 2 // Single user message
	OpRoomSend      = 3 // Room broadcast
	OpRoomCountSend = 4 // Room count update
	OpRoomInfoSend  = 5 // Room info update
)

// GenerateTestUserName creates a unique test username
func GenerateTestUserName() string {
	return fmt.Sprintf("%s%d", TestUserPrefix, time.Now().UnixNano())
}

// GenerateTestUserNameWithSuffix creates a unique test username with custom suffix
func GenerateTestUserNameWithSuffix(suffix string) string {
	return fmt.Sprintf("%s%s_%d", TestUserPrefix, suffix, time.Now().UnixNano())
}

// TestMessage generates a test message with prefix
func TestMessage(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
}

// TestUser holds test user credentials and tokens
type TestUser struct {
	UserName  string
	Password  string
	AuthToken string
	UserId    int
}

// NewTestUser creates a new test user data holder
func NewTestUser() *TestUser {
	return &TestUser{
		UserName: GenerateTestUserName(),
		Password: TestPassword,
	}
}

// NewTestUserWithName creates a test user with specific name suffix
func NewTestUserWithName(suffix string) *TestUser {
	return &TestUser{
		UserName: GenerateTestUserNameWithSuffix(suffix),
		Password: TestPassword,
	}
}
