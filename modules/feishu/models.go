package feishu

type ReqBase struct {
	MsgType   string `json:"msg_type"`
	Content   any    `json:"content"`
	Timestamp string `json:"timestamp"`
	Sign      string `json:"sign"`
}

type ReqSendText struct {
	Text string `json:"text"`
}

type ReqSendPostTextContent struct {
	Tag    string `json:"tag"`
	Text   string `json:"text,omitempty"`
	Href   string `json:"href,omitempty"`
	UserID string `json:"user_id,omitempty"`
}
