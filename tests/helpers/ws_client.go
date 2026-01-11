// Package helpers provides test utilities for integration tests.
package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSClient wraps WebSocket client for testing
type WSClient struct {
	conn     *websocket.Conn
	URL      string
	Messages chan []byte
	Errors   chan error
	done     chan struct{}
	mu       sync.Mutex
	closed   bool
}

// ConnectRequest matches proto.ConnectRequest for WebSocket authentication
type ConnectRequest struct {
	AuthToken string `json:"authToken"`
	RoomId    int    `json:"roomId"`
	ServerId  string `json:"serverId"`
}

// NewWSClient creates and connects a WebSocket client
func NewWSClient(url string) (*WSClient, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		return nil, fmt.Errorf("websocket dial: %w", err)
	}

	client := &WSClient{
		conn:     conn,
		URL:      url,
		Messages: make(chan []byte, 100),
		Errors:   make(chan error, 10),
		done:     make(chan struct{}),
	}

	go client.readPump()

	return client, nil
}

// Connect sends auth token and room ID to establish session
func (c *WSClient) Connect(authToken string, roomId int) error {
	req := ConnectRequest{
		AuthToken: authToken,
		RoomId:    roomId,
	}
	return c.SendJSON(req)
}

// SendJSON marshals and sends JSON message
func (c *WSClient) SendJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("connection closed")
	}
	return c.conn.WriteJSON(v)
}

// SendText sends a text message
func (c *WSClient) SendText(msg string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("connection closed")
	}
	return c.conn.WriteMessage(websocket.TextMessage, []byte(msg))
}

// WaitForMessage waits for any message with timeout
func (c *WSClient) WaitForMessage(timeout time.Duration) ([]byte, error) {
	select {
	case msg := <-c.Messages:
		return msg, nil
	case err := <-c.Errors:
		return nil, err
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout waiting for message after %v", timeout)
	}
}

// WaitForMessageContaining waits for message containing substring
func (c *WSClient) WaitForMessageContaining(substring string, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case msg := <-c.Messages:
			if bytes.Contains(msg, []byte(substring)) {
				return msg, nil
			}
			// Message doesn't match, continue waiting
		case err := <-c.Errors:
			return nil, err
		case <-time.After(100 * time.Millisecond):
			continue
		}
	}
	return nil, fmt.Errorf("timeout waiting for message containing %q", substring)
}

// DrainMessages reads and discards all pending messages
func (c *WSClient) DrainMessages(timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-c.Messages:
			// Discard message
		case <-time.After(100 * time.Millisecond):
			return
		}
	}
}

// Close cleanly closes the connection
func (c *WSClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	close(c.done)
	err := c.conn.Close()
	c.mu.Unlock()
	return err
}

// IsClosed returns whether the connection is closed
func (c *WSClient) IsClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *WSClient) readPump() {
	defer func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
	}()

	for {
		select {
		case <-c.done:
			return
		default:
			c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			_, message, err := c.conn.ReadMessage()
			if err != nil {
				select {
				case <-c.done:
					return
				case c.Errors <- err:
				default:
					// Error channel full, drop error
				}
				return
			}
			select {
			case c.Messages <- message:
			default:
				// Message channel full, drop message
			}
		}
	}
}

// ReceivedMessage represents a parsed message from the server
type ReceivedMessage struct {
	Code         int    `json:"code"`
	Msg          string `json:"msg"`
	FromUserId   int    `json:"fromUserId"`
	FromUserName string `json:"fromUserName"`
	ToUserId     int    `json:"toUserId"`
	ToUserName   string `json:"toUserName"`
	RoomId       int    `json:"roomId"`
	Op           int    `json:"op"`
	CreateTime   string `json:"createTime"`
}

// ParseMessage parses a raw message into ReceivedMessage
func ParseMessage(data []byte) (*ReceivedMessage, error) {
	var msg ReceivedMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}
	return &msg, nil
}
