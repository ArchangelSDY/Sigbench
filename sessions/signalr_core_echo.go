package sessions

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type SignalRCoreEcho struct {
	cntInProgress int64
	cntError      int64
	cntSuccess    int64
}

func (s *SignalRCoreEcho) Name() string {
	return "SignalRCore:Echo"
}

func (s *SignalRCoreEcho) Setup(map[string]string) error {
	s.cntInProgress = 0
	s.cntError = 0
	s.cntSuccess = 0
	return nil
}

func (s *SignalRCoreEcho) logError(msg string, err error) {
	log.Println("Error: ", msg, " due to ", err)
	atomic.AddInt64(&s.cntError, 1)
}

func (s *SignalRCoreEcho) Execute(ctx *UserContext) error {
	atomic.AddInt64(&s.cntInProgress, 1)
	defer atomic.AddInt64(&s.cntInProgress, -1)

	host := ctx.Params[ParamHost]
	handshakeReq, err := http.NewRequest(http.MethodOptions, "http://"+host+"/chat", nil)
	if err != nil {
		s.logError("Fail to construct handshake request", err)
		return err
	}

	handshakeResp, err := http.DefaultClient.Do(handshakeReq)
	if err != nil {
		s.logError("Fail to obtain connection id", err)
		return err
	}
	defer handshakeResp.Body.Close()

	decoder := json.NewDecoder(handshakeResp.Body)
	var handshakeContent SignalRCoreHandshakeResp
	err = decoder.Decode(&handshakeContent)
	if err != nil {
		s.logError("Fail to decode connection id", err)
		return err
	}

	wsUrl := "ws://" + host + "/chat?id=" + handshakeContent.ConnectionId
	c, _, err := websocket.DefaultDialer.Dial(wsUrl, nil)
	if err != nil {
		s.logError("Fail to connect to websocket", err)
		return err
	}
	defer c.Close()

	echoReceivedChan := make(chan struct{})
	doneChan := make(chan struct{})

	go func() {
		defer close(doneChan)
		for {
			_, msgWithTerm, err := c.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure) {
					s.logError("Fail to read incoming message", err)
				}
				return
			}

			msg := msgWithTerm[:len(msgWithTerm)-1]
			var content SignalRCoreInvocation
			err = json.Unmarshal(msg, &content)
			if err != nil {
				s.logError("Fail to decode incoming message", err)
				return
			}

			if content.Type == 1 && content.Target == "echo" && content.Arguments[1] == "foobar" {
				close(echoReceivedChan)
			}
		}
	}()

	err = c.WriteMessage(websocket.TextMessage, []byte("{\"protocol\":\"json\"}\x1e"))
	if err != nil {
		s.logError("Fail to set protocol", err)
		return err
	}

	err = c.WriteMessage(websocket.TextMessage, []byte("{\"type\":1,\"invocationId\":\"0\",\"target\":\"echo\",\"arguments\":[\"echo-client\",\"foobar\"],\"nonblocking\":false}\x1e"))
	if err != nil {
		s.logError("Fail to send echo", err)
		return err
	}

	// Wait echo response
	select {
	case <-time.After(1 * time.Minute):
		s.logError("Fail to receive echo within timeout", nil)
		return errors.New("fail to receive echo within timeout")
	case <-echoReceivedChan:
		// Gracefully close
		err = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		if err != nil {
			s.logError("Fail to close websocket gracefully", err)
			return err
		}
	}

	// Wait close response
	select {
	case <-time.After(1 * time.Minute):
		s.logError("Fail to receive close message", nil)
		return errors.New("fail to receive close message")
	case <-doneChan:
		atomic.AddInt64(&s.cntSuccess, 1)
		return nil
	}
}

func (s *SignalRCoreEcho) Counters() map[string]int64 {
	return map[string]int64{
		"signalrcore:echo:inprogress": atomic.LoadInt64(&s.cntInProgress),
		"signalrcore:echo:success":    atomic.LoadInt64(&s.cntSuccess),
		"signalrcore:echo:error":      atomic.LoadInt64(&s.cntError),
	}
}
