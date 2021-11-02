package sessions

import (
	"context"
	"crypto/tls"
	"io"
	"io/ioutil"
	"log"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"net/http"
	"net/url"

	"microsoft.com/sigbench/base"
)

type HttpGetSession struct {
	client                  *http.Client
	counterInitiated        int64
	counterCompleted        int64
	counterError            int64
	counterDial             int64
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
	s.counterDial = 0
	s.counterDurationLt1000 = 0
	s.counterDurationLt10000 = 0
	s.counterDurationGte10000 = 0

	maxConnsPerHost := 0
	maxConnsPerHostStr := params["maxConnsPerHost"]
	if maxConnsPerHostStr != "" {
		maxConnsPerHost, _ = strconv.Atoi(maxConnsPerHostStr)
	}
	log.Println("Max conns per host", maxConnsPerHost)

	maxIdleConns := 100
	maxIdleConnsStr := params["maxIdleConns"]
	if maxIdleConnsStr != "" {
		maxIdleConns, _ = strconv.Atoi(maxIdleConnsStr)
	}
	log.Println("Max idle conns", maxIdleConns)

	maxIdleConnsPerHost := http.DefaultMaxIdleConnsPerHost
	maxIdleConnsPerHostStr := params["maxIdleConnsPerHost"]
	if maxIdleConnsPerHostStr != "" {
		maxIdleConnsPerHost, _ = strconv.Atoi(maxIdleConnsPerHostStr)
	}
	log.Println("Max idle conns per host", maxIdleConnsPerHost)

	noKeepAlive := false
	if params["keepAlive"] == "false" {
		noKeepAlive = true
	}
	log.Println("Disable keep alive", noKeepAlive)

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	var dialContext func(ctx context.Context, network, addr string) (net.Conn, error)
	if params["tls"] != "true" {
		dialContext = dialer.DialContext
		// dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		// 	log.Println("Dial", network, addr)
		// 	return dialer.DialContext(ctx, network, addr)
		// }
		// dialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		// 	conn, err := dialer.DialContext(ctx, network, addr)
		// 	if err != nil {
		// 		return nil, err
		// 	}
		// 	if err = conn.(*net.TCPConn).SetLinger(0); err != nil {
		// 		log.Println("Fail to set linger", err)
		// 	}
		// 	return conn, nil
		// }
	} else {
		insecure := false
		if params["insecure"] == "true" {
			insecure = true
		}

		log.Printf("Use TLS. Insecure: %tn", insecure)

		var sessCache tls.ClientSessionCache
		if params["tlsClientSessionCache"] != "" {
			capacity, _ := strconv.Atoi(params["tlsClientSessionCache"])
			sessCache = tls.NewLRUClientSessionCache(capacity)
			log.Printf("Use TLS client session cache. Capacity: %d\n", capacity)
		}

		tlsConfig := &tls.Config{
			InsecureSkipVerify: insecure,
			ClientSessionCache: sessCache,
			// Certificates:       []tls.Certificate{loadCertKey("client.crt", "client.key")},
		}
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config:    tlsConfig,
		}
		dialContext = tlsDialer.DialContext
	}

	s.client = &http.Client{
		Transport: &http.Transport{
			// DialContext:           dialContext,
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				atomic.AddInt64(&s.counterDial, 1)
				return dialContext(ctx, network, addr)
			},
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          maxIdleConns,
			MaxIdleConnsPerHost:   maxIdleConnsPerHost,
			MaxConnsPerHost:       maxConnsPerHost,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			DisableKeepAlives:     noKeepAlive,
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
	if params["h2"] == "false" {
		log.Println("Disable HTTP2")
		s.client.Transport.(*http.Transport).TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
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
	io.Copy(ioutil.Discard, resp.Body)
	resp.Body.Close()

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
		"dial":             atomic.LoadInt64(&s.counterDial),
		"duration:<1000":   atomic.LoadInt64(&s.counterDurationLt1000),
		"duration:<10000":  atomic.LoadInt64(&s.counterDurationLt10000),
		"duration:>=10000": atomic.LoadInt64(&s.counterDurationGte10000),
	}
}

func (s *HttpGetSession) AggregateCounters(job *base.Job, counters map[string]int64) {
	completed := counters["completed"]
	duration := job.Duration()
	if duration > 0 {
		durationSecs := int64(duration / time.Second)
		counters["rps"] = completed / durationSecs
	}
}
