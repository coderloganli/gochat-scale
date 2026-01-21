/**
 * Created by lock
 * Date: 2021/4/5
 */
package task

import (
	"gochat/config"
	"gochat/tools"
	"testing"
	"time"
)

func Test_TestQueue(t *testing.T) {
	// Skip in short mode - this is an integration test requiring RabbitMQ
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	RabbitMQClient = tools.GetRabbitMQInstance(config.Conf.Common.CommonRabbitMQ.URL)
	if err := RabbitMQClient.Connect(); err != nil {
		t.Skipf("Skipping test - RabbitMQ not available: %v", err)
	}
	defer RabbitMQClient.Close()

	ch := RabbitMQClient.Channel()

	// Declare test queue
	q, err := ch.QueueDeclare(
		config.RabbitMQQueueSingle,
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to declare queue: %v", err)
	}

	// Try to consume one message with a timeout
	msgs, err := ch.Consume(
		q.Name,
		"",
		true,  // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to register a consumer: %v", err)
	}

	select {
	case msg := <-msgs:
		t.Logf("Received message: %s", string(msg.Body))
	case <-time.After(5 * time.Second):
		t.Log("No message received within timeout (queue may be empty)")
	}
}
