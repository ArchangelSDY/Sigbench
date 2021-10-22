package sessions

import (
	"log"
	"sync/atomic"

	"net/http"
)

type HttpGetSession struct {
	counterInitiated int64
	counterCompleted int64
	counterError     int64
}

func (s *HttpGetSession) Name() string {
	return "HttpGet"
}

func (s *HttpGetSession) Setup() error {
	s.counterInitiated = 0
	s.counterCompleted = 0
	s.counterError = 0
	return nil
}

func (s *HttpGetSession) Execute(ctx *UserContext) error {
	atomic.AddInt64(&s.counterInitiated, 1)

	resp, err := http.Get("https://www.baidu.com")
	if err != nil {
		log.Println("Error: ", err)
		atomic.AddInt64(&s.counterInitiated, -1)
		atomic.AddInt64(&s.counterError, 1)
		return nil
	}
	defer resp.Body.Close()

	atomic.AddInt64(&s.counterInitiated, -1)
	atomic.AddInt64(&s.counterCompleted, 1)

	return nil
}

func (s *HttpGetSession) Counters() map[string]int64 {
	return map[string]int64{
		"initiated": atomic.LoadInt64(&s.counterInitiated),
		"completed": atomic.LoadInt64(&s.counterCompleted),
		"error":     atomic.LoadInt64(&s.counterError),
	}
}
