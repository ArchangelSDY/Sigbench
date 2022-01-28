package sessions

import (
	"context"
	"log"
	"math/rand"
	"strconv"
	"sync/atomic"

	"github.com/segmentio/kafka-go"
)

type KafkaProducer struct {
	cntInProgress int64
	cntError      int64
	cntSuccess    int64
	cntWritten    int64

	payload []byte
	msgCnt  int
}

func (s *KafkaProducer) Name() string {
	return "Kafka:Producer"
}

func (s *KafkaProducer) Setup(params map[string]string) error {
	s.cntInProgress = 0
	s.cntError = 0
	s.cntSuccess = 0
	s.cntWritten = 0

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

	s.msgCnt = 1
	if msgCntStr := params["messageCount"]; msgCntStr != "" {
		if s.msgCnt, err = strconv.Atoi(msgCntStr); err != nil {
			return err
		}
	}
	log.Println("Per client message count", s.msgCnt)

	return nil
}

func (s *KafkaProducer) logError(msg string, err error) {
	log.Println("Error: ", msg, " due to ", err)
	atomic.AddInt64(&s.cntError, 1)
}

func (s *KafkaProducer) Execute(ctx *UserContext) error {
	atomic.AddInt64(&s.cntInProgress, 1)
	defer atomic.AddInt64(&s.cntInProgress, -1)

	partition, err := strconv.Atoi(ctx.Params["partition"])
	if err != nil {
		s.logError("Invalid partition", err)
		return err
	}

	conn, err := kafka.DialLeader(context.Background(), "tcp", ctx.Params["url"], ctx.Params["topic"], partition)
	if err != nil {
		s.logError("Fail to connect", err)
		return err
	}

	for i := 0; i < s.msgCnt; i++ {
		n, err := conn.WriteMessages(
			kafka.Message{Value: s.payload},
		)
		if err != nil {
			s.logError("Fail to send message", err)
			return err
		}
		atomic.AddInt64(&s.cntWritten, int64(n))
	}

	atomic.AddInt64(&s.cntSuccess, 1)

	return nil
}

func (s *KafkaProducer) Counters() map[string]int64 {
	return map[string]int64{
		"kafka:producer:inprogress": atomic.LoadInt64(&s.cntInProgress),
		"kafka:producer:success":    atomic.LoadInt64(&s.cntSuccess),
		"kafka:producer:error":      atomic.LoadInt64(&s.cntError),
		"kafka:producer:written":    atomic.LoadInt64(&s.cntWritten),
	}
}
