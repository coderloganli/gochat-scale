/**
 * Created by lock
 * Date: 2019-08-13
 * Time: 10:13
 */
package task

import (
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
	"gochat/config"
	"gochat/tools"
)

var RabbitMQClient *tools.RabbitMQClient

// consumerRetryInterval bounds how fast a consumer retries after a failed or
// very short-lived attach. consumerMinAttachDuration is what counts as
// short-lived: below it, the attach is treated as a failure for backoff
// purposes even if Consume itself succeeded.
const (
	consumerRetryInterval     = 2 * time.Second
	consumerMinAttachDuration = 1 * time.Second
)

// wantedConsumers is how many queues this service consumes; consumersRunning is
// how many are currently attached. Readiness compares the two, because "the
// connection is open" and "messages are being consumed" turned out to be very
// different things.
var (
	wantedConsumers  atomic.Int64
	consumersRunning atomic.Int64
)

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

		wantedConsumers.Add(1)
		go task.consumeQueue(q.name)
	}

	return nil
}

// consumeQueue consumes one queue for the lifetime of the process, re-attaching
// whenever the channel goes away.
//
// The retry loop is the point. A consumer's delivery channel closes whenever the
// broker restarts or the connection drops, and the client underneath reconnects
// on its own - so without this the process stays up, stays connected, reports
// healthy, and never delivers another message as long as it runs. It has to be
// restarted by hand to recover, which is the worst kind of failure: silent, and
// invisible to everything that was watching.
func (task *Task) consumeQueue(queueName string) {
	for {
		start := time.Now()
		task.consumeOnce(queueName)
		// Back off unless the attach actually lasted. Attaching successfully is
		// not enough: a broker that is mid-restart, or a queue that has been
		// deleted, can accept the Consume and close the delivery channel
		// immediately, and retrying that with no pause is a hot loop that opens
		// and closes a channel as fast as the CPU allows.
		if time.Since(start) < consumerMinAttachDuration {
			time.Sleep(consumerRetryInterval)
		}
	}
}

// consumeOnce attaches to the queue and consumes until the delivery channel
// closes.
func (task *Task) consumeOnce(queueName string) {
	ch, err := RabbitMQClient.NewChannel()
	if err != nil {
		logrus.Warnf("consumer %s: cannot open channel: %v", queueName, err)
		return
	}
	defer ch.Close()

	if err := ch.Qos(config.Conf.Common.CommonRabbitMQ.PrefetchCount, 0, false); err != nil {
		logrus.Warnf("consumer %s: cannot set QoS: %v", queueName, err)
		return
	}

	// The queue is redeclared on every attach. After a broker restart that lost
	// its state, consuming a queue that no longer exists fails; declaring is
	// idempotent and costs nothing when it is already there.
	if _, err := ch.QueueDeclare(queueName, true, false, false, false, nil); err != nil {
		logrus.Warnf("consumer %s: cannot declare queue: %v", queueName, err)
		return
	}

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
		logrus.Warnf("consumer %s: cannot consume: %v", queueName, err)
		return
	}

	logrus.Debugf("Started consuming from queue: %s", queueName)
	consumersRunning.Add(1)
	defer consumersRunning.Add(-1)

	for msg := range msgs {
		task.Push(string(msg.Body))
		msg.Ack(false)
	}

	logrus.Warnf("Consumer channel closed for queue: %s, reattaching", queueName)
}
