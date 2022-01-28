package sessions

import (
	"log"
	"sync/atomic"

	"microsoft.com/sigbench/base"
)

type Session interface {
	Name() string
	Setup(sessionParams map[string]string) error
	Execute(*UserContext) error
	Counters() map[string]int64
}

type CounterAggregator interface {
	AggregateCounters(*base.Job, map[string]int64)
}

var SessionMap = map[string]Session{
	"signalrcore:echo":             &SignalRCoreEcho{},
	"signalrcore:broadcast:sender": &SignalRCoreBroadcastSender{},
	"signalrfx:broadcast:sender":   &SignalRFxBroadcastSender{},
	"redis:pubsub":                 &RedisPubSub{},
	"http:get":                     &HttpGetSession{},
	"webpubsub:echo":               &WebPubSubEcho{},
	"rabbitmq:massive_queue":       &RabbitMQMassiveQueue{},
	"kafka:producer":               &KafkaProducer{},
}

type DummySession struct {
	counterA int64
	counterB int64
}

func (s *DummySession) Name() string {
	return "Dummy"
}

func (s *DummySession) Setup(map[string]string) error {
	log.Println("> Dummy setup")
	s.counterA = 0
	s.counterB = 0
	return nil
}

func (s *DummySession) Execute(ctx *UserContext) error {
	log.Println("> Dummy at phase: " + ctx.Phase)
	atomic.AddInt64(&s.counterA, 1)
	atomic.AddInt64(&s.counterB, 2)
	return nil
}

func (s *DummySession) Counters() map[string]int64 {
	return map[string]int64{
		"a": atomic.LoadInt64(&s.counterA),
		"b": atomic.LoadInt64(&s.counterB),
	}
}
