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
	"microsoft.com/sigbench/util"
	"strings"
)

const MaxInstances = 256

type SignalRCoreBroadcastSender struct {
	userIdx                  int64
	cntInProgress            int64
	cntConnected             int64
	cntError                 int64
	cntCloseError            int64
	cntSuccess               int64
	cntMessagesRecv          int64
	cntMessagesSend          int64
	cntLatencyLessThan100ms  int64
	cntLatencyLessThan500ms  int64
	cntLatencyLessThan1000ms int64
	cntLatencyMoreThan1000ms int64
	cntInstances             []int64
}

func (s *SignalRCoreBroadcastSender) Name() string {
	return "SignalRCore:Broadcast:Sender"
}

func (s *SignalRCoreBroadcastSender) Setup(map[string]string) error {
	s.userIdx = 0
	s.cntInProgress = 0
	s.cntConnected = 0
	s.cntError = 0
	s.cntCloseError = 0
	s.cntSuccess = 0
	s.cntMessagesRecv = 0
	s.cntMessagesSend = 0
	s.cntLatencyLessThan100ms = 0
	s.cntLatencyLessThan500ms = 0
	s.cntLatencyLessThan1000ms = 0
	s.cntLatencyMoreThan1000ms = 0
	s.cntInstances = make([]int64, MaxInstances, MaxInstances)
	return nil
}

func (s *SignalRCoreBroadcastSender) logError(ctx *UserContext, msg string, err error) {
	log.Printf("[Error][%s] %s due to %s", ctx.UserId, msg, err)
	atomic.AddInt64(&s.cntError, 1)
}

func (s *SignalRCoreBroadcastSender) logLatency(latency int64) {
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

func (s *SignalRCoreBroadcastSender) logHostInstance(ctx *UserContext, hostName string) error {
	hostInstanceId, err := util.GetVMSSInstanceId(hostName)
	if err != nil {
		s.logError(ctx, "Fail to decode host name "+hostName, err)
		return err
	}
	atomic.AddInt64(&s.cntInstances[hostInstanceId], 1)
	return nil
}

func (s *SignalRCoreBroadcastSender) Execute(ctx *UserContext) error {
	atomic.AddInt64(&s.cntInProgress, 1)
	defer atomic.AddInt64(&s.cntInProgress, -1)

	// Select a host using round-robin
	hosts := strings.Split(ctx.Params[ParamHost], ",")
	host := hosts[atomic.AddInt64(&s.userIdx, 1)%int64(len(hosts))]

	broadcastDurationSecs := 10
	if secsStr, ok := ctx.Params[ParamBroadcastDurationSecs]; ok {
		if secs, err := strconv.Atoi(secsStr); err == nil {
			broadcastDurationSecs = secs
		}
	}

	handshakeReq, err := http.NewRequest(http.MethodOptions, "http://"+host+"/chat", nil)
	if err != nil {
		s.logError(ctx, "Fail to construct handshake request", err)
		return err
	}

	handshakeResp, err := http.DefaultClient.Do(handshakeReq)
	if err != nil {
		s.logError(ctx, "Fail to obtain connection id", err)
		return err
	}
	defer handshakeResp.Body.Close()

	// Record host instance
	if err = s.logHostInstance(ctx, handshakeResp.Header.Get("X-HostName")); err != nil {
		return err
	}

	decoder := json.NewDecoder(handshakeResp.Body)
	var handshakeContent SignalRCoreHandshakeResp
	err = decoder.Decode(&handshakeContent)
	if err != nil {
		s.logError(ctx, "Fail to decode connection id", err)
		return err
	}

	wsUrl := "ws://" + host + "/chat?id=" + handshakeContent.ConnectionId
	c, _, err := websocket.DefaultDialer.Dial(wsUrl, nil)
	if err != nil {
		s.logError(ctx, "Fail to connect to websocket", err)
		return err
	}
	defer c.Close()

	closeChan := make(chan struct{})
	recvChan := make(chan int64, broadcastDurationSecs)

	go func() {
		defer c.Close()
		defer close(closeChan)
		for {
			_, msgWithTerm, err := c.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure) {
					s.logError(ctx, "Fail to read incoming message", err)
				}
				return
			}

			msg := msgWithTerm[:len(msgWithTerm)-1]
			var content SignalRCoreInvocation
			err = json.Unmarshal(msg, &content)
			if err != nil {
				s.logError(ctx, "Fail to decode incoming message", err)
				return
			}

			atomic.AddInt64(&s.cntMessagesRecv, 1)

			if content.Type == 1 && content.Target == "broadcastMessage" && content.Arguments[0] == ctx.UserId {
				sendStart, err := strconv.ParseInt(content.Arguments[1], 10, 64)
				if err != nil {
					s.logError(ctx, "Fail to decode start timestamp", err)
					continue
				}

				recvChan <- (time.Now().UnixNano() - sendStart) / 1000000
			}
		}
	}()

	err = c.WriteMessage(websocket.TextMessage, []byte("{\"protocol\":\"json\"}\x1e"))
	if err != nil {
		s.logError(ctx, "Fail to set protocol", err)
		return err
	}

	atomic.AddInt64(&s.cntConnected, 1)
	defer atomic.AddInt64(&s.cntConnected, -1)

	for i := 0; i < broadcastDurationSecs; i++ {
		// Send message
		msg, err := SerializeSignalRCoreMessage(&SignalRCoreInvocation{
			Type:         1,
			InvocationId: "0",
			Target:       "send",
			Arguments: []string{
				ctx.UserId,
				strconv.FormatInt(time.Now().UnixNano(), 10),
			},
			NonBlocking: false,
		})
		if err != nil {
			s.logError(ctx, "Fail to serialize signalr core message", err)
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

	timeoutChan := time.After(time.Minute)
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

func (s *SignalRCoreBroadcastSender) Counters() map[string]int64 {
	counters := map[string]int64{
		"signalrcore:broadcast:inprogress":     atomic.LoadInt64(&s.cntInProgress),
		"signalrcore:broadcast:connected":      atomic.LoadInt64(&s.cntConnected),
		"signalrcore:broadcast:success":        atomic.LoadInt64(&s.cntSuccess),
		"signalrcore:broadcast:error":          atomic.LoadInt64(&s.cntError),
		"signalrcore:broadcast:closeerror":     atomic.LoadInt64(&s.cntCloseError),
		"signalrcore:broadcast:messages:recv":  atomic.LoadInt64(&s.cntMessagesRecv),
		"signalrcore:broadcast:messages:send":  atomic.LoadInt64(&s.cntMessagesSend),
		"signalrcore:broadcast:latency:<100":   atomic.LoadInt64(&s.cntLatencyLessThan100ms),
		"signalrcore:broadcast:latency:<500":   atomic.LoadInt64(&s.cntLatencyLessThan500ms),
		"signalrcore:broadcast:latency:<1000":  atomic.LoadInt64(&s.cntLatencyLessThan1000ms),
		"signalrcore:broadcast:latency:>=1000": atomic.LoadInt64(&s.cntLatencyMoreThan1000ms),
	}

	for i := 0; i < MaxInstances; i++ {
		if val := atomic.LoadInt64(&s.cntInstances[i]); val > 0 {
			counters["signalrcore:broadcast:instancehit:"+strconv.Itoa(i)] = val
		}
	}

	return counters
}
