package feishu

type ReqBase struct {
	MsgType   string `json:"msg_type"`
	Content   any    `json:"content,omitempty"`
	Card      any    `json:"card,omitempty"`
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

type ReqCardMsg struct {
	//https://open.feishu.cn/document/ukTMukTMukTM/uAjNwUjLwYDM14CM2ATN
	Config map[string]interface{} `json:"config,omitempty"`
	//https://open.feishu.cn/document/ukTMukTMukTM/ukTNwUjL5UDM14SO1ATN
	Header *CardMsgHeader `json:"header,omitempty"`
	//https://open.feishu.cn/document/ukTMukTMukTM/uEjNwUjLxYDM14SM2ATN
	Elements []interface{} `json:"elements"`
}

// CardMsgContentElement https://open.feishu.cn/document/ukTMukTMukTM/uMjNwUjLzYDM14yM2ATN
type CardMsgContentElement struct {
	Tag    string                `json:"tag"`
	Text   *CardMsgElementText   `json:"text,omitempty"`
	Fields []CardMsgElementField `json:"fields,omitempty"`
	Extra  interface{}           `json:"extra,omitempty"`
}

// CardMsgDividerElement https://open.feishu.cn/document/ukTMukTMukTM/uQjNwUjL0YDM14CN2ATN
type CardMsgDividerElement struct {
	Tag string `json:"tag"`
}

// CardMsgImageElement https://open.feishu.cn/document/ukTMukTMukTM/uUjNwUjL1YDM14SN2ATN
type CardMsgImageElement struct {
	Tag          string              `json:"tag"`
	ImgKey       string              `json:"img_key"`
	Alt          CardMsgElementText  `json:"alt"`
	Title        *CardMsgElementText `json:"title,omitempty"`
	CustomWidth  int                 `json:"custom_width,omitempty"`
	CompactWidth *bool               `json:"compact_width,omitempty"`
	Mode         string              `json:"mode,omitempty"`
	Preview      *bool               `json:"preview,omitempty"`
}

// CardMsgActionElement https://open.feishu.cn/document/ukTMukTMukTM/uYjNwUjL2YDM14iN2ATN
type CardMsgActionElement struct {
	Tag     string                 `json:"tag"`
	Actions []CardMsgElementButton `json:"actions"`
	Layout  string                 `json:"layout,omitempty"`
}

// CardMsgNoteElement https://open.feishu.cn/document/ukTMukTMukTM/ucjNwUjL3YDM14yN2ATN
type CardMsgNoteElement struct {
	Tag      string        `json:"tag"`
	Elements []interface{} `json:"elements"`
}

// CardMsgHeader https://open.feishu.cn/document/ukTMukTMukTM/ukTNwUjL5UDM14SO1ATN
type CardMsgHeader struct {
	Title CardMsgElementText `json:"title"`
	//卡片标题的主题色
	Template string `json:"template,omitempty"`
}

// CardMsgElementField https://open.feishu.cn/document/ukTMukTMukTM/uYzNwUjL2cDM14iN3ATN
type CardMsgElementField struct {
	IsShort bool               `json:"is_short"`
	Text    CardMsgElementText `json:"text"`
}

// CardMsgElementText https://open.feishu.cn/document/ukTMukTMukTM/uUzNwUjL1cDM14SN3ATN
type CardMsgElementText struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
	Lines   int    `json:"lines,omitempty"`
}

// CardMsgElementMultiUrl https://open.feishu.cn/document/ukTMukTMukTM/uczNwUjL3cDM14yN3ATN
type CardMsgElementMultiUrl struct {
	Url        string `json:"url"`
	AndroidUrl string `json:"android_url"`
	IosUrl     string `json:"ios_url"`
	PcUrl      string `json:"pc_url"`
}

// CardMsgElementConfirm https://open.feishu.cn/document/ukTMukTMukTM/ukzNwUjL5cDM14SO3ATN
type CardMsgElementConfirm struct {
	Title CardMsgElementText `json:"title"`
	Text  CardMsgElementText `json:"text"`
}

// CardMsgElementButton https://open.feishu.cn/document/ukTMukTMukTM/uEzNwUjLxcDM14SM3ATN
type CardMsgElementButton struct {
	Tag      string                  `json:"tag"`
	Text     CardMsgElementText      `json:"text"`
	Url      string                  `json:"url,omitempty"`
	MultiUrl *CardMsgElementMultiUrl `json:"multi_url,omitempty"`
	Type     string                  `json:"type,omitempty"`
	Value    map[string]interface{}  `json:"value,omitempty"`
}
