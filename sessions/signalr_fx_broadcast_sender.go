package sessions

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"net/url"
)

type SignalRFxBroadcastSender struct {
	cntInProgress            int64
	cntConnected             int64
	cntError                 int64
	cntCloseError            int64
	cntSuccess               int64
	cntMessagesRecv          int64
	cntMessagesSend          int64
	cntMessagesSendAck       int64
	cntLatencyLessThan100ms  int64
	cntLatencyLessThan500ms  int64
	cntLatencyLessThan1000ms int64
	cntLatencyMoreThan1000ms int64
}

func (s *SignalRFxBroadcastSender) Name() string {
	return "SignalRFx:Broadcast:Sender"
}

func (s *SignalRFxBroadcastSender) Setup(map[string]string) error {
	s.cntInProgress = 0
	s.cntError = 0
	s.cntConnected = 0
	s.cntCloseError = 0
	s.cntSuccess = 0
	s.cntMessagesRecv = 0
	s.cntMessagesSend = 0
	s.cntMessagesSendAck = 0
	s.cntLatencyLessThan100ms = 0
	s.cntLatencyLessThan500ms = 0
	s.cntLatencyLessThan1000ms = 0
	s.cntLatencyMoreThan1000ms = 0
	return nil
}

func (s *SignalRFxBroadcastSender) logError(ctx *UserContext, msg string, err error) {
	log.Printf("[Error][%s] %s due to %s", ctx.UserId, msg, err)
	atomic.AddInt64(&s.cntError, 1)
}

func (s *SignalRFxBroadcastSender) logLatency(latency int64) {
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

func (s *SignalRFxBroadcastSender) Execute(ctx *UserContext) error {
	atomic.AddInt64(&s.cntInProgress, 1)
	defer atomic.AddInt64(&s.cntInProgress, -1)

	host := ctx.Params[ParamHost]
	broadcastDurationSecs := 10
	if secsStr, ok := ctx.Params[ParamBroadcastDurationSecs]; ok {
		if secs, err := strconv.Atoi(secsStr); err == nil {
			broadcastDurationSecs = secs
		}
	}

	// Handshake phase 1: obtain token
	handshakeReq, err := http.NewRequest(http.MethodGet, "http://"+host+"/signalr/negotiate?clientProtocol=1.4&connectionData=%5B%7B%22name%22%3A%22chat%22%7D%5D", nil)
	if err != nil {
		s.logError(ctx, "Fail to construct handshake request", err)
		return err
	}

	handshakeResp, err := http.DefaultClient.Do(handshakeReq)
	if err != nil {
		s.logError(ctx, "Fail to obtain connection token", err)
		return err
	}
	defer handshakeResp.Body.Close()

	decoder := json.NewDecoder(handshakeResp.Body)
	var handshakeContent SignalRFxHandshakeResp
	err = decoder.Decode(&handshakeContent)
	if err != nil {
		s.logError(ctx, "Fail to decode connection token", err)
		return err
	}

	// Handshake phase 2: connect to websocket
	wsUrl := "ws://" + host + "/signalr/connect?transport=webSockets&clientProtocol=1.4&connectionToken=" + url.QueryEscape(handshakeContent.ConnectionToken) + "&connectionData=%5B%7B%22name%22%3A%22chat%22%7D%5D&tid=0"
	c, _, err := websocket.DefaultDialer.Dial(wsUrl, nil)
	if err != nil {
		s.logError(ctx, "Fail to connect to websocket", err)
		return err
	}
	defer c.Close()

	connectChan := make(chan struct{})
	closeChan := make(chan struct{})
	recvChan := make(chan int64, broadcastDurationSecs)

	go func() {
		defer c.Close()
		defer close(closeChan)
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure) {
					s.logError(ctx, "Fail to read incoming message", err)
				}
				return
			}

			var content SignalRFxServerMessage
			err = json.Unmarshal(msg, &content)
			if err != nil {
				s.logError(ctx, "Fail to decode incoming message", err)
				return
			}

			atomic.AddInt64(&s.cntMessagesRecv, 1)

			// Init message
			if content.S == 1 {
				close(connectChan)
				continue
			}

			// Ack message
			if content.Id != "" {
				atomic.AddInt64(&s.cntMessagesSendAck, 1)
				continue
			}

			for _, frame := range content.Frames {
				if frame.Hub == "Chat" && frame.Method == "send" && frame.Arguments[0] == ctx.UserId {
					sendStart, err := strconv.ParseInt(frame.Arguments[1], 10, 64)
					if err != nil {
						s.logError(ctx, "Fail to decode start timestamp", err)
						continue
					}

					recvChan <- (time.Now().UnixNano() - sendStart) / 1000000
				}
			}
		}
	}()

	// Handshake phase 2: wait for init message
	timeoutChan := time.After(time.Minute)
	select {
	case <-connectChan:
		break
	case <-timeoutChan:
		err = errors.New("no init message")
		s.logError(ctx, "Fail to receive init message within timeout", err)
		return err
	}

	// Handshake phase 3: start receiving
	startReq, err := http.NewRequest(http.MethodGet, "http://"+host+"/signalr/start?transport=webSockets&clientProtocol=1.4&connectionToken="+url.QueryEscape(handshakeContent.ConnectionToken)+"&connectionData=%5B%7B%22name%22%3A%22chat%22%7D%5D&tid=0", nil)
	if err != nil {
		s.logError(ctx, "Fail to construct start request", err)
		return err
	}

	startResp, err := http.DefaultClient.Do(startReq)
	if err != nil {
		s.logError(ctx, "Fail to start", err)
		return err
	}
	defer startResp.Body.Close()

	decoder = json.NewDecoder(startResp.Body)
	var startContent SignalRFxStartResp
	err = decoder.Decode(&startContent)
	if err != nil {
		s.logError(ctx, "Fail to decode start response", err)
		return err
	}

	if startContent.Response != "started" {
		err = errors.New(startContent.Response)
		s.logError(ctx, "Start response not expected", err)
		return err
	}

	atomic.AddInt64(&s.cntConnected, 1)
	defer atomic.AddInt64(&s.cntConnected, -1)

	// Now we can send messages
	for i := 0; i < broadcastDurationSecs; i++ {
		// Send message
		msg, err := json.Marshal(&SignalRFxClientMessage{
			Id:     i,
			Hub:    "chat",
			Method: "Send",
			Arguments: []string{
				ctx.UserId,
				strconv.FormatInt(time.Now().UnixNano(), 10),
			},
		})
		if err != nil {
			s.logError(ctx, "Fail to serialize signalr fx message", err)
			return err
		}

		err = c.WriteMessage(websocket.TextMessage, msg)
		if err != nil {
			s.logError(ctx, "Fail to send broadcast message", err)
			return err
		}

		atomic.AddInt64(&s.cntMessagesSend, 1)

		time.Sleep(time.Second)
	}

	timeoutChan = time.After(time.Minute)
	for i := 0; i < broadcastDurationSecs; i++ {
		select {
		case latency := <-recvChan:
			s.logLatency(latency)
		case <-timeoutChan:
			s.logError(ctx, "Fail to receive all self broadcast messages within timeout", nil)
			return errors.New("fail receive all self broadcast messages within timeout")
			break
		}
	}

	err = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
	if err != nil {
		s.logError(ctx, "Fail to close websocket gracefully", err)
		return err
	}

	// Wait close response
	select {
	case <-time.After(1 * time.Minute):
		log.Println("Warning: Fail to receive close message")
		atomic.AddInt64(&s.cntCloseError, 1)
	case <-closeChan:
		atomic.AddInt64(&s.cntSuccess, 1)
	}

	return nil
}

func (s *SignalRFxBroadcastSender) Counters() map[string]int64 {
	return map[string]int64{
		"signalrfx:broadcast:inprogress":       atomic.LoadInt64(&s.cntInProgress),
		"signalrfx:broadcast:connected":        atomic.LoadInt64(&s.cntConnected),
		"signalrfx:broadcast:success":          atomic.LoadInt64(&s.cntSuccess),
		"signalrfx:broadcast:error":            atomic.LoadInt64(&s.cntError),
		"signalrfx:broadcast:closeerror":       atomic.LoadInt64(&s.cntCloseError),
		"signalrfx:broadcast:messages:recv":    atomic.LoadInt64(&s.cntMessagesRecv),
		"signalrfx:broadcast:messages:send":    atomic.LoadInt64(&s.cntMessagesSend),
		"signalrfx:broadcast:messages:sendack": atomic.LoadInt64(&s.cntMessagesSendAck),
		"signalrfx:broadcast:latency:<100":     atomic.LoadInt64(&s.cntLatencyLessThan100ms),
		"signalrfx:broadcast:latency:<500":     atomic.LoadInt64(&s.cntLatencyLessThan500ms),
		"signalrfx:broadcast:latency:<1000":    atomic.LoadInt64(&s.cntLatencyLessThan1000ms),
		"signalrfx:broadcast:latency:>=1000":   atomic.LoadInt64(&s.cntLatencyMoreThan1000ms),
	}
}
