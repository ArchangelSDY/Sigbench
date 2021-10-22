package sessions

import (
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/garyburd/redigo/redis"
)

type RedisPubSub struct {
	pool *redis.Pool

	cntInProgress            int64
	cntConnected             int64
	cntError                 int64
	cntErrorNotRecvAll       int64
	cntSuccess               int64
	cntMessagesRecv          int64
	cntMessagesSend          int64
	cntLatencyLessThan100ms  int64
	cntLatencyLessThan500ms  int64
	cntLatencyLessThan1000ms int64
	cntLatencyMoreThan1000ms int64
}

type RedisPubSubMessage struct {
	Uid       string
	Timestamp int64
}

func (s *RedisPubSub) Name() string {
	return "Redis:PubSub"
}

func (s *RedisPubSub) Setup(sessionParams map[string]string) error {
	if err := s.setupRedisPool(sessionParams); err != nil {
		return err
	}

	s.cntInProgress = 0
	s.cntConnected = 0
	s.cntError = 0
	s.cntErrorNotRecvAll = 0
	s.cntSuccess = 0
	s.cntMessagesRecv = 0
	s.cntMessagesSend = 0
	s.cntLatencyLessThan100ms = 0
	s.cntLatencyLessThan500ms = 0
	s.cntLatencyLessThan1000ms = 0
	s.cntLatencyMoreThan1000ms = 0
	return nil
}

func (s *RedisPubSub) setupRedisPool(sessionParams map[string]string) error {
	if s.pool != nil {
		s.pool.Close()
	}

	s.pool = &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial: func() (redis.Conn, error) {
			c, err := redis.Dial("tcp", sessionParams[ParamHost])
			if err != nil {
				return nil, err
			}

			if password, ok := sessionParams[ParamPassword]; ok && password != "" {
				if _, err := c.Do("AUTH", sessionParams[ParamPassword]); err != nil {
					c.Close()
					return nil, err
				}
			}

			return c, nil
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
	}

	return nil
}

func (s *RedisPubSub) logError(ctx *UserContext, msg string, err error) {
	log.Printf("[Error][%s] %s due to %s", ctx.UserId, msg, err)
	atomic.AddInt64(&s.cntError, 1)
}

func (s *RedisPubSub) logLatency(latency int64) {
	// log.Println("Latency: ", latency)
	if latency < 100 {
		atomic.AddInt64(&s.cntLatencyLessThan100ms, 1)
	} else if latency < 500 {
		atomic.AddInt64(&s.cntLatencyLessThan500ms, 1)
	} else if latency < 1000 {
		atomic.AddInt64(&s.cntLatencyLessThan1000ms, 1)
	} else {
		atomic.AddInt64(&s.cntLatencyMoreThan1000ms, 1)
	}
}

func (s *RedisPubSub) Execute(ctx *UserContext) error {
	atomic.AddInt64(&s.cntInProgress, 1)
	defer atomic.AddInt64(&s.cntInProgress, -1)

	broadcastDurationSecs := 10
	if secsStr, ok := ctx.Params[ParamBroadcastDurationSecs]; ok {
		if secs, err := strconv.Atoi(secsStr); err == nil {
			broadcastDurationSecs = secs
		}
	}
	publishInterval, err := strconv.Atoi(ctx.Params[ParamPublishInterval])
	if err != nil {
		s.logError(ctx, "Invalid publish interval", err)
		return err
	}
	totalMsgCnt := broadcastDurationSecs * 1000 * 1000 / publishInterval

	startChan := make(chan struct{})
	recvChan := make(chan int64, totalMsgCnt)
	exit := int64(0)

	go func() {
		sc := redis.PubSubConn{Conn: s.pool.Get()}
		defer sc.Close()

		if err := sc.Subscribe("sigbench"); err != nil {
			s.logError(ctx, "Fail to subscribe", err)
			return
		}

		close(startChan)

		for atomic.LoadInt64(&exit) == 0 {
			switch n := sc.Receive().(type) {
			case redis.Message:
				atomic.AddInt64(&s.cntMessagesRecv, 1)

				var msg RedisPubSubMessage
				err := json.Unmarshal(n.Data, &msg)
				if err != nil {
					s.logError(ctx, "Fail to unmarshal message", err)
					continue
				}

				if msg.Uid == ctx.UserId {
					recvChan <- (time.Now().UnixNano() - msg.Timestamp) / 1000000
				}
			case error:
				if n.Error() != "redigo: connection closed" {
					s.logError(ctx, "Received error message", n)
				}
				return
			}

		}
	}()

	<-startChan

	atomic.AddInt64(&s.cntConnected, 1)
	defer atomic.AddInt64(&s.cntConnected, -1)

	for i := 0; i < totalMsgCnt; i++ {
		msg := &RedisPubSubMessage{
			Uid:       ctx.UserId,
			Timestamp: time.Now().UnixNano(),
		}

		msgEncoded, err := json.Marshal(msg)
		if err != nil {
			s.logError(ctx, "Fail to marshal message", err)
			return err
		}

		pconn := s.pool.Get()
		_, err = pconn.Do("PUBLISH", "sigbench", msgEncoded)
		if err != nil {
			s.logError(ctx, "Fail to publish message", err)
			pconn.Close()
			return err
		}
		pconn.Flush()
		pconn.Close()

		atomic.AddInt64(&s.cntMessagesSend, 1)

		time.Sleep(time.Duration(publishInterval) * time.Microsecond)
	}

	defer atomic.StoreInt64(&exit, 1)

	timeoutChan := time.After(time.Minute)
	for i := 0; i < totalMsgCnt; i++ {
		select {
		case latency := <-recvChan:
			s.logLatency(latency)
		case <-timeoutChan:
			log.Printf("[Error][%s] Fail to receive all messages within timeout. Current i: %d", ctx.UserId, i)
			atomic.AddInt64(&s.cntErrorNotRecvAll, 1)
			return errors.New("fail to receive all messages within timeout")
		}
	}

	atomic.AddInt64(&s.cntSuccess, 1)

	return nil
}

func (s *RedisPubSub) Counters() map[string]int64 {
	return map[string]int64{
		"redis:pubsub:inprogress":       atomic.LoadInt64(&s.cntInProgress),
		"redis:pubsub:connected":        atomic.LoadInt64(&s.cntConnected),
		"redis:pubsub:success":          atomic.LoadInt64(&s.cntSuccess),
		"redis:pubsub:error":            atomic.LoadInt64(&s.cntError),
		"redis:pubsub:error:notrecvall": atomic.LoadInt64(&s.cntErrorNotRecvAll),
		"redis:pubsub:messages:recv":    atomic.LoadInt64(&s.cntMessagesRecv),
		"redis:pubsub:messages:send":    atomic.LoadInt64(&s.cntMessagesSend),
		"redis:pubsub:latency:<100":     atomic.LoadInt64(&s.cntLatencyLessThan100ms),
		"redis:pubsub:latency:<500":     atomic.LoadInt64(&s.cntLatencyLessThan500ms),
		"redis:pubsub:latency:<1000":    atomic.LoadInt64(&s.cntLatencyLessThan1000ms),
		"redis:pubsub:latency:>=1000":   atomic.LoadInt64(&s.cntLatencyMoreThan1000ms),
	}
}
