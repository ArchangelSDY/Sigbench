package sessions

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type WebPubSubEcho struct {
	dialer *websocket.Dialer

	cntInProgress int64
	cntError      int64
	cntSuccess    int64
}

func (s *WebPubSubEcho) Name() string {
	return "WebPubSub:Echo"
}

func (s *WebPubSubEcho) Setup(map[string]string) error {
	s.dialer = &websocket.Dialer{
		Subprotocols: []string{
			"json.webpubsub.azure.v1",
		},
	}

	s.cntInProgress = 0
	s.cntError = 0
	s.cntSuccess = 0
	return nil
}

func (s *WebPubSubEcho) logError(msg string, err error) {
	log.Println("Error: ", msg, " due to ", err)
	atomic.AddInt64(&s.cntError, 1)
}

type WebPubSubEvent struct {
	Type         string `json:"type,omitempty"`
	Event        string `json:"event,omitempty"`
	UserId       string `json:"userId,omitempty"`
	ConnectionId string `json:"connectionId,omitempty"`
	Message      string `json:"message,omitempty"`
	From         string `json:"from,omitempty"`
	AckId        int    `json:"ackId,omitempty"`
	Group        string `json:"group,omitempty"`
	DataType     string `json:"dataType,omitempty"`
	Data         string `json:"data,omitempty"`
}

func (s *WebPubSubEcho) Execute(ctx *UserContext) error {
	atomic.AddInt64(&s.cntInProgress, 1)
	defer atomic.AddInt64(&s.cntInProgress, -1)

	c, _, err := s.dialer.Dial(ctx.Params["url"], nil)
	if err != nil {
		s.logError("Fail to connect to websocket", err)
		return err
	}
	defer c.Close()

	onConnChan := make(chan struct{})
	ackChan := make(chan int)
	msgChan := make(chan string)
	errChan := make(chan error)
	closeChan := make(chan struct{})

	go func() {
		for {
			_, msg, err := c.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure) {
					s.logError("Fail to read incoming message", err)
				}
				closeChan <- struct{}{}
				return
			}

			var ev WebPubSubEvent
			err = json.Unmarshal(msg, &ev)
			if err != nil {
				s.logError("Fail to decode incoming message", err)
				return
			}

			if ev.Type == "system" && ev.Event == "connected" {
				onConnChan <- struct{}{}
				continue
			}

			if ev.Type == "ack" {
				ackChan <- ev.AckId
				continue
			}

			if ev.Type == "message" && ev.From == "group" {
				msgChan <- ev.Data
				continue
			}

			errChan <- fmt.Errorf("Unexpected message: %+v", ev)
		}
	}()

	select {
	case <-time.After(time.Minute):
		s.logError("On connected timeout", nil)
		return errors.New("On connected timeout")
	case <-onConnChan:
		break
	case err = <-errChan:
		s.logError("Error", err)
		return err
	case <-closeChan:
		err = errors.New("Unexpected close")
		s.logError("Error", err)
		return err
	}

	var joinGroupEv WebPubSubEvent = WebPubSubEvent{
		Type:  "joinGroup",
		Group: ctx.UserId,
		AckId: 1,
	}
	joinGroupData, _ := json.Marshal(joinGroupEv)
	err = c.WriteMessage(websocket.TextMessage, joinGroupData)
	if err != nil {
		s.logError("Fail to join group", err)
		return err
	}
	select {
	case <-time.After(time.Minute):
		err = fmt.Errorf("Join group %s ack timeout", ctx.UserId)
		s.logError("Join group ack timeout", err)
		return err
	case ackId := <-ackChan:
		if ackId != joinGroupEv.AckId {
			s.logError("Unexpected ack id", nil)
			return errors.New("Unexpected ack id")
		}
		break
	case err = <-errChan:
		s.logError("Error", err)
		return err
	case <-closeChan:
		err = errors.New("Unexpected close")
		s.logError("Error", err)
		return err
	}

	var sendToGroupEv WebPubSubEvent = WebPubSubEvent{
		Type:     "sendToGroup",
		Group:    ctx.UserId,
		AckId:    2,
		DataType: "text",
		Data:     "foobar",
	}
	sendToGroupEvData, _ := json.Marshal(sendToGroupEv)
	err = c.WriteMessage(websocket.TextMessage, sendToGroupEvData)
	if err != nil {
		s.logError("Fail to join group", err)
		return err
	}
	for {
		select {
		case <-time.After(time.Minute):
			s.logError("Echo timeout", nil)
			return errors.New("Echo timeout")
		case ackId := <-ackChan:
			if ackId != sendToGroupEv.AckId {
				s.logError("Unexpected ack id", nil)
				return errors.New("Unexpected ack id")
			}
			continue
		case data := <-msgChan:
			if data != sendToGroupEv.Data {
				s.logError("Unexpected data", nil)
				return errors.New("Unexpected data")
			}
			atomic.AddInt64(&s.cntSuccess, 1)
			return nil
		case err = <-errChan:
			s.logError("Error", err)
			return err
		case <-closeChan:
			err = errors.New("Unexpected close")
			s.logError("Error", err)
			return err
		}
	}
}

func (s *WebPubSubEcho) Counters() map[string]int64 {
	return map[string]int64{
		"webpubsub:echo:inprogress": atomic.LoadInt64(&s.cntInProgress),
		"webpubsub:echo:success":    atomic.LoadInt64(&s.cntSuccess),
		"webpubsub:echo:error":      atomic.LoadInt64(&s.cntError),
	}
}
