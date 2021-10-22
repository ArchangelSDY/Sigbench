package sessions

import "encoding/json"

const SignalRCoreTerminator = '\x1e'

type SignalRCoreHandshakeResp struct {
	AvailableTransports []string `json:"availableTransports"`
	ConnectionId        string   `json:"connectionId"`
}

type SignalRCoreInvocation struct {
	InvocationId string   `json:"invocationId"`
	Type         int      `json:"type"`
	Target       string   `json:"target"`
	NonBlocking  bool     `json:"nonBlocking"`
	Arguments    []string `json:"arguments"`
}

func SerializeSignalRCoreMessage(body interface{}) ([]byte, error) {
	msg, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	return append(msg, SignalRCoreTerminator), nil
}
