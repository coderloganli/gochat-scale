package tools

import (
	"context"
	"sync"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/sirupsen/logrus"
)

type RabbitMQClient struct {
	conn    *amqp.Connection
	channel *amqp.Channel
	url     string
	mu      sync.RWMutex
	pubMu   sync.Mutex
	closed  bool
}

var (
	rabbitMQClient *RabbitMQClient
	rabbitMQOnce   sync.Once
)

func GetRabbitMQInstance(url string) *RabbitMQClient {
	rabbitMQOnce.Do(func() {
		rabbitMQClient = &RabbitMQClient{url: url}
	})
	return rabbitMQClient
}

func (c *RabbitMQClient) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	conn, err := amqp.Dial(c.url)
	if err != nil {
		return err
	}
	c.conn = conn

	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		return err
	}
	c.channel = ch
	c.closed = false

	go c.handleReconnect()

	return nil
}

func (c *RabbitMQClient) handleReconnect() {
	notifyClose := c.conn.NotifyClose(make(chan *amqp.Error))
	for {
		reason, ok := <-notifyClose
		if !ok {
			c.mu.RLock()
			closed := c.closed
			c.mu.RUnlock()
			if closed {
				return
			}
			logrus.Info("RabbitMQ connection closed normally")
			return
		}
		logrus.Warnf("RabbitMQ connection closed: %v, reconnecting...", reason)
		for {
			time.Sleep(5 * time.Second)
			c.mu.Lock()
			if c.closed {
				c.mu.Unlock()
				return
			}
			conn, err := amqp.Dial(c.url)
			if err != nil {
				c.mu.Unlock()
				logrus.Errorf("RabbitMQ reconnect failed: %v", err)
				continue
			}
			ch, err := conn.Channel()
			if err != nil {
				conn.Close()
				c.mu.Unlock()
				logrus.Errorf("RabbitMQ channel creation failed: %v", err)
				continue
			}
			c.conn = conn
			c.channel = ch
			c.mu.Unlock()
			logrus.Info("RabbitMQ reconnected")
			notifyClose = conn.NotifyClose(make(chan *amqp.Error))
			break
		}
	}
}

func (c *RabbitMQClient) Channel() *amqp.Channel {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.channel
}

// NewChannel creates a new independent amqp.Channel from the underlying connection.
// Each goroutine that performs Consume/Ack should use its own channel since amqp.Channel is not concurrency-safe.
func (c *RabbitMQClient) NewChannel() (*amqp.Channel, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.conn == nil {
		return nil, amqp.ErrClosed
	}
	return c.conn.Channel()
}

// Publish safely publishes a message, serializing access to the shared channel.
func (c *RabbitMQClient) Publish(ctx context.Context, exchange, key string, mandatory, immediate bool, msg amqp.Publishing) error {
	c.pubMu.Lock()
	defer c.pubMu.Unlock()
	c.mu.RLock()
	ch := c.channel
	c.mu.RUnlock()
	return ch.PublishWithContext(ctx, exchange, key, mandatory, immediate, msg)
}

func (c *RabbitMQClient) Connection() *amqp.Connection {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

func (c *RabbitMQClient) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.channel != nil {
		c.channel.Close()
	}
	if c.conn != nil {
		c.conn.Close()
	}
}
