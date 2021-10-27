package sessions

import (
	"log"
	"net"
	"sync/atomic"
	"time"

	"net/http"
	"net/url"
)

type HttpGetSession struct {
	client                  *http.Client
	counterInitiated        int64
	counterCompleted        int64
	counterError            int64
	counterDurationLt1000   int64
	counterDurationLt10000  int64
	counterDurationGte10000 int64
}

func (s *HttpGetSession) Name() string {
	return "HttpGet"
}

func (s *HttpGetSession) Setup(params map[string]string) error {
	s.counterInitiated = 0
	s.counterCompleted = 0
	s.counterError = 0
	s.counterDurationLt1000 = 0
	s.counterDurationLt10000 = 0
	s.counterDurationGte10000 = 0

	s.client = &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DisableKeepAlives:     true,
		},
		Timeout: time.Minute,
	}
	proxyUrl := params["proxy"]
	if proxyUrl != "" {
		if u, err := url.Parse(proxyUrl); err == nil {
			log.Println("Use proxy", u)
			s.client.Transport.(*http.Transport).Proxy = http.ProxyURL(u)
		}
	}

	return nil
}

func (s *HttpGetSession) Execute(ctx *UserContext) error {
	atomic.AddInt64(&s.counterInitiated, 1)

	start := time.Now()

	urlStr := ctx.Params["url"]
	resp, err := s.client.Get(urlStr)
	if err != nil {
		log.Println("Error: ", err)
		atomic.AddInt64(&s.counterInitiated, -1)
		atomic.AddInt64(&s.counterError, 1)
		return nil
	}
	defer resp.Body.Close()

	duration := time.Now().Sub(start)
	if duration < time.Second {
		atomic.AddInt64(&s.counterDurationLt1000, 1)
	} else if duration < 10*time.Second {
		atomic.AddInt64(&s.counterDurationLt10000, 1)
	} else {
		atomic.AddInt64(&s.counterDurationGte10000, 1)
	}

	atomic.AddInt64(&s.counterInitiated, -1)
	atomic.AddInt64(&s.counterCompleted, 1)

	return nil
}

func (s *HttpGetSession) Counters() map[string]int64 {
	return map[string]int64{
		"initiated":        atomic.LoadInt64(&s.counterInitiated),
		"completed":        atomic.LoadInt64(&s.counterCompleted),
		"error":            atomic.LoadInt64(&s.counterError),
		"duration:<1000":   atomic.LoadInt64(&s.counterDurationLt1000),
		"duration:<10000":  atomic.LoadInt64(&s.counterDurationLt10000),
		"duration:>=10000": atomic.LoadInt64(&s.counterDurationGte10000),
	}
}
