package sessions

import (
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/streadway/amqp"
)

type RabbitMQChannelPool struct {
	url   string
	conns []*amqp.Connection
	lock  sync.Mutex
}

func NewRabbitMQChannelPool(url string) *RabbitMQChannelPool {
	return &RabbitMQChannelPool{
		url:   url,
		conns: []*amqp.Connection{},
	}
}

func (p *RabbitMQChannelPool) Channel() (*amqp.Channel, error) {
	for _, conn := range p.conns {
		ch, err := conn.Channel()
		if err == nil {
			return ch, nil
		}
	}

	p.lock.Lock()
	for _, conn := range p.conns {
		ch, err := conn.Channel()
		if err == nil {
			p.lock.Unlock()
			return ch, nil
		}
	}

	conn, err := amqp.Dial(p.url)
	if err != nil {
		p.lock.Unlock()
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		conn.Close()
		p.lock.Unlock()
		return nil, err
	}

	p.conns = append(p.conns, conn)
	p.lock.Unlock()

	return ch, nil
}

func (p *RabbitMQChannelPool) Close() {
	p.lock.Lock()
	for _, conn := range p.conns {
		conn.Close()
	}
	p.conns = []*amqp.Connection{}
	p.lock.Unlock()
}

type RabbitMQMassiveQueue struct {
	cntInProgress int64
	cntError      int64
	cntSuccess    int64
	cntPublished  int64
	cntReceived   int64

	connPool        *RabbitMQChannelPool
	payload         []byte
	queues          int
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

	if s.connPool == nil {
		s.connPool = NewRabbitMQChannelPool(params["url"])
	} else {
		s.connPool.Close()
	}

	var err error

	payloadSize := 100
	if payloadSizeStr := params["messageSize"]; payloadSizeStr != "" {
		if payloadSize, err = strconv.Atoi(payloadSizeStr); err != nil {
			return err
		}
	}
	log.Println("Payload size", payloadSize)
	s.payload = make([]byte, payloadSize)
	rand.Read(s.payload)

	s.queues = 1
	if queuesStr := params["queues"]; queuesStr != "" {
		if s.queues, err = strconv.Atoi(queuesStr); err != nil {
			return err
		}
	}
	log.Println("Queues per user", s.queues)

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
	for i := 0; i < s.queues; i++ {
		err := s.doQueue(ctx, i)
		if err != nil {
			s.logError(fmt.Sprintf("Fail to test queue %d", i), err)
			return err
		} else {
			atomic.AddInt64(&s.cntSuccess, 1)
		}
	}

	return nil
}

func (s *RabbitMQMassiveQueue) doQueue(ctx *UserContext, idx int) error {
	atomic.AddInt64(&s.cntInProgress, 1)
	defer atomic.AddInt64(&s.cntInProgress, -1)

	pubCh, err := s.connPool.Channel()
	if err != nil {
		s.logError("Fail to open a channel", err)
		return err
	}
	defer pubCh.Close()

	qArgs := amqp.Table{}
	if s.queueType != "" {
		qArgs["x-queue-type"] = s.queueType
	}

	q, err := pubCh.QueueDeclare(
		fmt.Sprintf("q-%s-%d", ctx.UserId, idx),
		true,  // durable
		false, // delete when unused
		false, // exclusive
		false, // no-wait
		qArgs,
	)
	if err != nil {
		return fmt.Errorf("Fail to declare a queue: %s", err)
	}
	defer func() {
		ch, err := s.connPool.Channel()
		if err == nil {
			ch.QueueDelete(q.Name, false, false, false)
		} else {
			s.logError("Fail to delete queue: "+q.Name, err)
		}
	}()

	var msgs <-chan amqp.Delivery
	if s.queueMessages > 0 {
		subCh, err := s.connPool.Channel()
		if err != nil {
			s.logError("Fail to open a channel", err)
			return err
		}
		defer subCh.Close()

		cid := fmt.Sprintf("c-%s-%d", ctx.UserId, idx)
		msgs, err = subCh.Consume(
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
			return err
		}
	}

	exitChan := make(chan error)
	go func() {
		// Sub
		for i := 0; i < s.queueMessages; i++ {
			msg, ok := <-msgs
			if !ok {
				exitChan <- fmt.Errorf("Queue closed too early", err)
				return
			}

			// TODO: Batch ack?
			if err = msg.Ack(false); err != nil {
				exitChan <- fmt.Errorf("Fail to ack", err)
				return
			}
			atomic.AddInt64(&s.cntReceived, 1)
		}

		exitChan <- nil
	}()

	for i := 0; i < s.queueMessages; i++ {
		// Pub
		err := pubCh.Publish(
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
			return err
		}
		atomic.AddInt64(&s.cntPublished, 1)
	}

	return <-exitChan
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
