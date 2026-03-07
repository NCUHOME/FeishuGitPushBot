package bot

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"resty.dev/v3"
)

var http = resty.New().
	SetTimeout(30 * time.Second).
	SetRetryCount(2).
	SetRetryWaitTime(2 * time.Second)

// SendCard 发送飞书消息卡片
func SendCard(card *Card) error {
	ts := time.Now().Unix()
	sign, err := genSign(C.Feishu.Secret, ts)
	if err != nil {
		return err
	}

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}

	res, err := http.R().
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]any{
			"msg_type":  "interactive",
			"card":      card,
			"timestamp": fmt.Sprint(ts),
			"sign":      sign,
		}).
		SetResult(&resp).
		Post(C.Feishu.Webhook)

	if err != nil {
		return err
	}
	if res.StatusCode() > 299 {
		return fmt.Errorf("HTTP %d: %s", res.StatusCode(), res.String())
	}
	if resp.Code != 0 {
		return fmt.Errorf("飞书错误 %d: %s", resp.Code, resp.Msg)
	}
	return nil
}

func genSign(secret string, ts int64) (string, error) {
	str := fmt.Sprintf("%v\n%s", ts, secret)
	h := hmac.New(sha256.New, []byte(str))
	if _, err := h.Write(nil); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

// 飞书消息卡片模型
type Card struct {
	Header   *CardHeader `json:"header,omitempty"`
	Elements []any       `json:"elements"`
	Config   *CardConfig `json:"config,omitempty"`
}

type CardConfig struct {
	WideScreenMode bool `json:"wide_screen_mode"`
	EnableForward  bool `json:"enable_forward"`
}

func (c *Card) AddDivider() {
	c.Elements = append(c.Elements, map[string]string{"tag": "hr"})
}

func (c *Card) AddMarkdown(content string) {
	c.Elements = append(c.Elements, CardElement{
		Tag:     "markdown",
		Content: content,
	})
}

func (c *Card) AddDiv(content string, fields []CardField) {
	el := CardElement{
		Tag: "div",
	}
	if content != "" {
		el.Text = &Text{Tag: "lark_md", Content: content}
	}
	if len(fields) > 0 {
		el.Fields = fields
	}
	c.Elements = append(c.Elements, el)
}

func (c *Card) AddAction(btn Button) {
	c.Elements = append(c.Elements, CardAction{
		Tag:     "action",
		Actions: []Button{btn},
	})
}

func (c *Card) AddNote(content string) {
	c.Elements = append(c.Elements, map[string]any{
		"tag": "note",
		"elements": []any{
			map[string]string{
				"tag":     "lark_md",
				"content": content,
			},
		},
	})
}

type CardHeader struct {
	Title    Text   `json:"title"`
	Template string `json:"template,omitempty"`
}

type Text struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type CardField struct {
	IsShort bool  `json:"is_short"`
	Text    *Text `json:"text"`
}

type CardElement struct {
	Tag     string      `json:"tag"`
	Content string      `json:"content,omitempty"`
	Text    *Text       `json:"text,omitempty"`
	Fields  []CardField `json:"fields,omitempty"`
}

type CardAction struct {
	Tag     string   `json:"tag"`
	Actions []Button `json:"actions"`
}

type Button struct {
	Tag  string `json:"tag"`
	Text Text   `json:"text"`
	Url  string `json:"url,omitempty"`
	Type string `json:"type,omitempty"`
}
