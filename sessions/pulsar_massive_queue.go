package sessions

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/apache/pulsar-client-go/pulsar"
)

type PulsarMassiveQueue struct {
	cntInProgress int64
	cntError      int64
	cntSuccess    int64
	cntPublished  int64
	cntReceived   int64

	client pulsar.Client

	payload       []byte
	queues        int
	queueMessages int
}

func (s *PulsarMassiveQueue) Name() string {
	return "Pulsar:MassiveQueue"
}

func (s *PulsarMassiveQueue) Setup(params map[string]string) error {
	s.cntInProgress = 0
	s.cntError = 0
	s.cntSuccess = 0
	s.cntPublished = 0
	s.cntReceived = 0

	var err error

	if s.client != nil {
		s.client.Close()
	}
	s.client, err = pulsar.NewClient(pulsar.ClientOptions{
		URL:                     params["url"],
		OperationTimeout:        30 * time.Second,
		ConnectionTimeout:       30 * time.Second,
		MaxConnectionsPerBroker: 10,
	})
	if err != nil {
		return err
	}

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

	return nil
}

func (s *PulsarMassiveQueue) logError(msg string, err error) {
	log.Println("Error: ", msg, " due to ", err)
	atomic.AddInt64(&s.cntError, 1)
}

func (s *PulsarMassiveQueue) Execute(ctx *UserContext) error {
	var wg sync.WaitGroup

	for i := 0; i < s.queues; i++ {
		wg.Add(1)
		go func(wg *sync.WaitGroup, i int) {
			err := s.doQueue(ctx, i)
			if err != nil {
				s.logError(fmt.Sprintf("Fail to test queue %d", i), err)
			} else {
				atomic.AddInt64(&s.cntSuccess, 1)
			}
			wg.Done()
		}(&wg, i)
	}
	wg.Wait()

	return nil
}

func sendMessageWithTimeout(producer pulsar.Producer, msg *pulsar.ProducerMessage) (pulsar.MessageID, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return producer.Send(ctx, msg)
}

func receiveMessageWithTimeout(consumer pulsar.Consumer) (pulsar.Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return consumer.Receive(ctx)
}

func (s *PulsarMassiveQueue) doQueue(ctx *UserContext, idx int) error {
	atomic.AddInt64(&s.cntInProgress, 1)
	defer atomic.AddInt64(&s.cntInProgress, -1)

	topic := fmt.Sprintf("q-%s-%d", ctx.UserId, idx)

	producer, err := s.client.CreateProducer(pulsar.ProducerOptions{
		Topic: topic,
	})
	if err != nil {
		return err
	}
	defer producer.Close()

	consumer, err := s.client.Subscribe(pulsar.ConsumerOptions{
		Topic:            topic,
		SubscriptionName: fmt.Sprintf("c-%s-%d", ctx.UserId, idx),
	})
	if err != nil {
		return err
	}
	defer func() {
		if err := consumer.Unsubscribe(); err != nil {
			s.logError("Fail to unsubscribe", err)
		}
		consumer.Close()
	}()

	exitChan := make(chan error)
	go func() {
		// Sub
		timeStart := time.Now()

		for i := 0; i < s.queueMessages; i++ {
			if time.Now().Sub(timeStart) > time.Second*time.Duration(s.queueMessages) {
				exitChan <- fmt.Errorf("Subscriber timeout")
				return
			}

			msg, err := receiveMessageWithTimeout(consumer)
			if err != nil {
				exitChan <- fmt.Errorf("Fail to consume", err)
				continue
			}

			consumer.Ack(msg)

			atomic.AddInt64(&s.cntReceived, 1)
		}

		exitChan <- nil
	}()

	for i := 0; i < s.queueMessages; i++ {
		// Pub
		_, err := sendMessageWithTimeout(producer, &pulsar.ProducerMessage{
			Payload: s.payload,
		})
		if err != nil {
			s.logError("Fail to publish", err)
			continue
		}
		atomic.AddInt64(&s.cntPublished, 1)
	}

	return <-exitChan
}

func (s *PulsarMassiveQueue) Counters() map[string]int64 {
	return map[string]int64{
		"pulsar:massive_queue:inprogress":    atomic.LoadInt64(&s.cntInProgress),
		"pulsar:massive_queue:success":       atomic.LoadInt64(&s.cntSuccess),
		"pulsar:massive_queue:error":         atomic.LoadInt64(&s.cntError),
		"pulsar:massive_queue:msg_published": atomic.LoadInt64(&s.cntPublished),
		"pulsar:massive_queue:msg_received":  atomic.LoadInt64(&s.cntReceived),
	}
}
