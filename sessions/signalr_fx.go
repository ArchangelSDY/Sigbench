package sessions

type SignalRFxHandshakeResp struct {
	Url                        string  `json:"Url"`
	ConnectionToken            string  `json:"ConnectionToken"`
	ConnectionId               string  `json:"ConnectionId"`
	TryWebSockets              bool    `json:"TryWebSockets"`
	ProtocolVersion            string  `json:"ProtocolVersion"`
	KeepAliveTimeout           float64 `json:"KeepAliveTimeout"`
	DisconnectTimeout          float64 `json:"DisconnectTimeout"`
	ConnectionTimeout          float64 `json:"ConnectionTimeout"`
	TransportConnectionTimeout float64 `json:"TransportConnectionTimeout"`
	LongPollDelay              float64 `json:"LongPollDelay"`
}

type SignalRFxStartResp struct {
	Response string `json:"Response"`
}

type SignalRFxServerMessageFrame struct {
	Hub       string   `json:"H"`
	Method    string   `json:"M"`
	Arguments []string `json:"A"`
}

type SignalRFxServerMessage struct {
	C      string                        `json:"C"`
	S      int                           `json:"S"`
	Id     string                        `json:"I"`
	Frames []SignalRFxServerMessageFrame `json:"M"`
}

type SignalRFxClientMessage struct {
	Hub       string   `json:"H"`
	Method    string   `json:"M"`
	Arguments []string `json:"A"`
	Id        int      `json:"I"`
}
