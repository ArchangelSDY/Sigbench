package sessions

import (
	"log"
	"math/rand"
	"strconv"
	"sync/atomic"

	"github.com/streadway/amqp"
)

type RabbitMQMassiveQueue struct {
	cntInProgress int64
	cntError      int64
	cntSuccess    int64
	cntPublished  int64
	cntReceived   int64

	conn            *amqp.Connection
	payload         []byte
	queueMessages   int
	queueType       string
	msgDeliveryMode uint8
}

func (s *RabbitMQMassiveQueue) Name() string {
	return "RabbitMQ:MassiveQueue"
}

func (s *RabbitMQMassiveQueue) Setup(params map[string]string) error {
	s.cntInProgress = 0
	s.cntError = 0
	s.cntSuccess = 0
	s.cntPublished = 0
	s.cntReceived = 0

	if s.conn != nil {
		s.conn.Close()
	}

	conn, err := amqp.Dial(params["url"])
	if err != nil {
		return err
	}
	s.conn = conn

	payloadSize := 100
	if payloadSizeStr := params["messageSize"]; payloadSizeStr != "" {
		if payloadSize, err = strconv.Atoi(payloadSizeStr); err != nil {
			return err
		}
	}
	log.Println("Payload size", payloadSize)
	s.payload = make([]byte, payloadSize)
	rand.Read(s.payload)

	s.queueMessages = 1
	if queueMsgsStr := params["queueMessages"]; queueMsgsStr != "" {
		if s.queueMessages, err = strconv.Atoi(queueMsgsStr); err != nil {
			return err
		}
	}
	log.Println("Queue messages", s.queueMessages)

	s.queueType = ""
	if qt := params["queueType"]; qt != "" {
		s.queueType = qt
	}
	log.Println("Queue type", s.queueType)

	s.msgDeliveryMode = 0
	if msgDeliveryModeStr := params["deliveryMode"]; msgDeliveryModeStr != "" {
		if deliveryMode, err := strconv.Atoi(msgDeliveryModeStr); err == nil {
			s.msgDeliveryMode = uint8(deliveryMode)
		} else {
			return err
		}
	}
	log.Println("Message delivery mode", s.msgDeliveryMode)

	return nil
}

func (s *RabbitMQMassiveQueue) logError(msg string, err error) {
	log.Println("Error: ", msg, " due to ", err)
	atomic.AddInt64(&s.cntError, 1)
}

func (s *RabbitMQMassiveQueue) Execute(ctx *UserContext) error {
	atomic.AddInt64(&s.cntInProgress, 1)
	defer atomic.AddInt64(&s.cntInProgress, -1)

	ch, err := s.conn.Channel()
	if err != nil {
		s.logError("Fail to open a channel", err)
		return err
	}
	defer ch.Close()

	chArgs := amqp.Table{}
	if s.queueType != "" {
		chArgs["x-queue-type"] = s.queueType
	}

	q, err := ch.QueueDeclare(
		"q-"+ctx.UserId,
		true,  // durable
		true,  // delete when unused
		false, // exclusive
		false, // no-wait
		chArgs,
	)
	if err != nil {
		s.logError("Fail to declare a queue", err)
		return err
	}

	exitCh := make(chan struct{})
	go func() {
		ch, err := s.conn.Channel()
		if err != nil {
			s.logError("Fail to open a channel", err)
			exitCh <- struct{}{}
			return
		}
		defer ch.Close()

		cid := "c-" + ctx.UserId
		msgs, err := ch.Consume(
			q.Name,
			cid,   // consumer
			false, // auto-acknowledgments
			false, // exclusive
			false, // non-local
			false, // no-wait
			nil,   // args
		)
		if err != nil {
			s.logError("Fail to subscribe", err)
			exitCh <- struct{}{}
			return
		}

		for i := 0; i < s.queueMessages; i++ {
			msg, ok := <-msgs
			if !ok {
				s.logError("Queue closed too early", err)
				exitCh <- struct{}{}
				return
			}

			// TODO: Delay
			// time.Sleep(100 * time.Millisecond)

			// TODO: Batch ack?
			if err = msg.Ack(false); err != nil {
				s.logError("Fail to ack", err)
			}

			atomic.AddInt64(&s.cntReceived, 1)
		}

		exitCh <- struct{}{}
	}()

	for i := 0; i < s.queueMessages; i++ {
		err := ch.Publish(
			"",     // exchange
			q.Name, // routing key
			false,  // mandatory
			false,  // immediate
			amqp.Publishing{
				ContentType:  "text/plain",
				Body:         s.payload,
				DeliveryMode: s.msgDeliveryMode,
			},
		)
		if err != nil {
			s.logError("Fail to publish", err)
		}
		atomic.AddInt64(&s.cntPublished, 1)
	}

	<-exitCh
	atomic.AddInt64(&s.cntSuccess, 1)

	return nil
}

func (s *RabbitMQMassiveQueue) Counters() map[string]int64 {
	return map[string]int64{
		"rabbitmq:massive_queue:inprogress":    atomic.LoadInt64(&s.cntInProgress),
		"rabbitmq:massive_queue:success":       atomic.LoadInt64(&s.cntSuccess),
		"rabbitmq:massive_queue:error":         atomic.LoadInt64(&s.cntError),
		"rabbitmq:massive_queue:msg_published": atomic.LoadInt64(&s.cntPublished),
		"rabbitmq:massive_queue:msg_received":  atomic.LoadInt64(&s.cntReceived),
	}
}
