/**
 * Created by lock
 * Date: 2019-08-13
 * Time: 10:13
 */
package task

import (
	"github.com/sirupsen/logrus"
	"gochat/config"
	"gochat/tools"
)

var RabbitMQClient *tools.RabbitMQClient

func (task *Task) InitRabbitMQConsumer() error {
	RabbitMQClient = tools.GetRabbitMQInstance(config.Conf.Common.CommonRabbitMQ.URL)
	if err := RabbitMQClient.Connect(); err != nil {
		return err
	}

	ch := RabbitMQClient.Channel()

	// Set QoS (prefetch count)
	if err := ch.Qos(config.Conf.Common.CommonRabbitMQ.PrefetchCount, 0, false); err != nil {
		return err
	}

	// Declare exchange
	if err := ch.ExchangeDeclare(
		config.RabbitMQExchange,
		"direct",
		true,  // durable
		false, // auto-deleted
		false, // internal
		false, // no-wait
		nil,
	); err != nil {
		return err
	}

	// Define queues and their routing keys
	queues := []struct {
		name string
		keys []string
	}{
		{config.RabbitMQQueueSingle, []string{config.RoutingKeySingleSend}},
		{config.RabbitMQQueueRoom, []string{config.RoutingKeyRoomSend}},
		{config.RabbitMQQueueMeta, []string{config.RoutingKeyRoomCount, config.RoutingKeyRoomInfo}},
	}

	// Declare and bind queues
	for _, q := range queues {
		_, err := ch.QueueDeclare(
			q.name,
			true,  // durable
			false, // delete when unused
			false, // exclusive
			false, // no-wait
			nil,
		)
		if err != nil {
			return err
		}

		for _, key := range q.keys {
			if err := ch.QueueBind(q.name, key, config.RabbitMQExchange, false, nil); err != nil {
				return err
			}
		}

		go task.consumeQueue(q.name)
	}

	return nil
}

func (task *Task) consumeQueue(queueName string) {
	ch := RabbitMQClient.Channel()
	msgs, err := ch.Consume(
		queueName,
		"",    // consumer tag
		false, // auto-ack
		false, // exclusive
		false, // no-local
		false, // no-wait
		nil,
	)
	if err != nil {
		logrus.Fatalf("Failed to consume from %s: %v", queueName, err)
	}

	logrus.Infof("Started consuming from queue: %s", queueName)

	for msg := range msgs {
		task.Push(string(msg.Body))
		msg.Ack(false)
	}
}
