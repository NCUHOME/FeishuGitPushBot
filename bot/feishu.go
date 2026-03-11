package bot

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"resty.dev/v3"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

var http = resty.New().
	SetTimeout(30 * time.Second).
	SetRetryCount(2).
	SetRetryWaitTime(2 * time.Second)

var (
	larkClient *lark.Client
	larkOnce   sync.Once
)

// GetLarkClient 获取飞书 SDK 客户端单例
func GetLarkClient() *lark.Client {
	larkOnce.Do(func() {
		larkClient = lark.NewClient(C.Feishu.AppID, C.Feishu.AppSecret)
	})
	return larkClient
}



// GetImageKey 获取缓存中的飞书 img_key (极速返回，不阻塞)
func GetImageKey(ctx context.Context, url string) string {
	if url == "" {
		return ""
	}

	if ctx == nil {
		ctx = context.Background()
	}
	var cache ImageCache
	if DB != nil {
		// 仅从数据库读取，不进行任何同步网络操作
		if err := DB.NewSelect().Model(&cache).Where("url = ?", url).Scan(ctx); err == nil {
			return cache.ImgKey
		}
	}

	return ""
}

// syncUploadImage 同步执行下载、哈希计算并上传到飞书
func syncUploadImage(ctx context.Context, url string) string {
	// 下载图片，尊重 context 超时
	imageRes, err := http.R().SetContext(ctx).Get(url)
	if err != nil || imageRes.IsError() {
		return ""
	}
	defer imageRes.Body.Close()
	imgData, _ := io.ReadAll(imageRes.Body)
	hash := fmt.Sprintf("%x", md5.Sum(imgData))

	client := GetLarkClient()
	var resp *larkim.CreateImageResp

	// 重试逻辑
	for i := 0; i < 3; i++ {
		uploadCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		resp, err = client.Im.Image.Create(uploadCtx, larkim.NewCreateImageReqBuilder().
			Body(larkim.NewCreateImageReqBodyBuilder().
				ImageType(larkim.ImageTypeMessage).
				Image(bytes.NewReader(imgData)).
				Build()).
			Build())
		cancel()
		if err == nil && resp.Success() {
			break
		}
		time.Sleep(time.Duration(i+1) * 3 * time.Second)
	}

	if err != nil || (resp != nil && !resp.Success()) {
		return ""
	}

	imgKey := *resp.Data.ImageKey
	if DB != nil {
		cache := ImageCache{
			URL:       url,
			ImgKey:    imgKey,
			Hash:      hash,
			UpdatedAt: time.Now(),
		}
		_, _ = DB.NewInsert().Model(&cache).On("CONFLICT (url) DO UPDATE").
			Set("img_key = EXCLUDED.img_key").
			Set("hash = EXCLUDED.hash").
			Set("updated_at = EXCLUDED.updated_at").
			Exec(context.Background())
	}
	return imgKey
}

// 移除旧的 refreshImageCache 函数，逻辑已迁移到 worker.go 的 imageRefreshWorker

// SendToChat 发送消息到指定群组，返回消息ID
func SendToChat(chatID string, card *Card) (string, error) {
	if chatID == "" {
		chatID = C.Feishu.ChatID
	}
	if chatID == "" {
		return "", fmt.Errorf("未指定目标聊天 ID (CHAT_ID)")
	}

	return sendMessage(chatID, "", card)
}

// UpdateMessage 更新已发送的消息
func UpdateMessage(messageID string, card *Card) error {
	client := GetLarkClient()

	var resp *larkim.PatchMessageResp
	var err error

	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		resp, err = client.Im.Message.Patch(ctx, larkim.NewPatchMessageReqBuilder().
			MessageId(messageID).
			Body(larkim.NewPatchMessageReqBodyBuilder().
				Content(card.String()).
				Build()).
			Build())
		cancel()

		if err == nil && resp.Success() {
			return nil
		}
		time.Sleep(time.Duration(i+1) * 2 * time.Second)
	}

	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("更新消息失败 %d: %s", resp.Code, resp.Msg)
	}
	return nil
}

// ReplyToMessage 回复指定消息
func ReplyToMessage(parentID string, card *Card) (string, error) {
	return sendMessage("", parentID, card)
}

func sendMessage(chatID, parentID string, card *Card) (string, error) {
	client := GetLarkClient()

	if parentID != "" {
		var resp *larkim.ReplyMessageResp
		var err error
		for i := 0; i < 3; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			resp, err = client.Im.Message.Reply(ctx, larkim.NewReplyMessageReqBuilder().
				MessageId(parentID).
				Body(larkim.NewReplyMessageReqBodyBuilder().
					MsgType(larkim.MsgTypeInteractive).
					Content(card.String()).
					Build()).
				Build())
			cancel()

			if err == nil && resp.Success() {
				return *resp.Data.MessageId, nil
			}
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		}
		if err != nil {
			return "", err
		}
		if !resp.Success() {
			return "", fmt.Errorf("回复消息失败 %d: %s", resp.Code, resp.Msg)
		}
		return *resp.Data.MessageId, nil
	}

	if chatID == "" {
		chatID = C.Feishu.ChatID
	}
	if chatID == "" {
		return "", fmt.Errorf("未指定目标聊天 ID (CHAT_ID)")
	}

	var resp *larkim.CreateMessageResp
	var err error
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		resp, err = client.Im.Message.Create(ctx, larkim.NewCreateMessageReqBuilder().
			ReceiveIdType(larkim.ReceiveIdTypeChatId).
			Body(larkim.NewCreateMessageReqBodyBuilder().
				ReceiveId(chatID).
				MsgType(larkim.MsgTypeInteractive).
				Content(card.String()).
				Build()).
			Build())
		cancel()

		if err == nil && resp.Success() {
			return *resp.Data.MessageId, nil
		}
		time.Sleep(time.Duration(i+1) * 2 * time.Second)
	}

	if err != nil {
		return "", err
	}
	if !resp.Success() {
		return "", fmt.Errorf("发送消息失败 %d: %s", resp.Code, resp.Msg)
	}
	return *resp.Data.MessageId, nil
}

// SendCard 发送飞书消息卡片 (保留兼容 Webhook)
func SendCard(card *Card) error {
	if C.Feishu.Webhook == "" {
		_, err := SendToChat("", card)
		return err
	}
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

// Card 飞书消息卡片模型 (V2)
type Card struct {
	Schema string      `json:"schema"`
	Header *CardHeader `json:"header,omitempty"`
	Body   *CardBody   `json:"body,omitempty"`
	Config *CardConfig `json:"config,omitempty"`
}

// CardBody 消息卡片正文
type CardBody struct {
	Elements []any `json:"elements"`
}

// NewCard 创建一个新的消息卡片
func NewCard() *Card {
	return &Card{
		Schema: "2.0",
		Header: &CardHeader{},
		Body:   &CardBody{Elements: []any{}},
		Config: &CardConfig{
			WideScreenMode: true,
			EnableForward:  true,
		},
	}
}

// String 将卡片序列化为 JSON 字符串
func (c *Card) String() string {
	b, _ := json.Marshal(c)
	return string(b)
}

// CardConfig 消息卡片配置
type CardConfig struct {
	WideScreenMode bool `json:"wide_screen_mode"`
	EnableForward  bool `json:"enable_forward"`
}

// AddDivider 添加分割线
func (c *Card) AddDivider() {
	c.Body.Elements = append(c.Body.Elements, map[string]string{"tag": "hr"})
}

// AddMarkdown 添加 Markdown 元素
func (c *Card) AddMarkdown(content string) {
	c.Body.Elements = append(c.Body.Elements, CardElement{
		Tag:     "markdown",
		Content: content,
	})
}

// AddCollapsiblePanel 添加折叠面板
func (c *Card) AddCollapsiblePanel(content string) {
	// 飞书 Schema 2.0 支持 collapsible_panel
	c.Body.Elements = append(c.Body.Elements, map[string]any{
		"tag": "collapsible_panel",
		"header": map[string]any{
			"title": map[string]string{
				"tag": "plain_text",
				"content": "📝 展开查看完整正文",
			},
		},
		"elements": []any{
			map[string]string{
				"tag": "markdown",
				"content": content,
			},
		},
	})
}

// AddDiv 添加普通文本块 (支持多列字段)
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
	c.Body.Elements = append(c.Body.Elements, el)
}

// AddAction 添加操作按钮
func (c *Card) AddAction(btn Button) {
	btn.Tag = "button"
	c.Body.Elements = append(c.Body.Elements, btn)
}

// AddNote 添加备注信息
func (c *Card) AddNote(elements ...any) {
	var markdowns []string
	for _, el := range elements {
		b, _ := json.Marshal(el)
		var m map[string]any
		json.Unmarshal(b, &m)
		if tag, _ := m["tag"].(string); tag == "lark_md" || tag == "plain_text" {
			if content, ok := m["content"].(string); ok {
				markdowns = append(markdowns, content)
			}
		}
	}
	if len(markdowns) > 0 {
		c.AddMarkdown(fmt.Sprintf("%s", strings.Join(markdowns, " | ")))
	}
}

// AddNoteText 添加纯文本备注
func (c *Card) AddNoteText(content string) {
	c.AddNote(map[string]string{
		"tag":     "lark_md",
		"content": content,
	})
}

// AddColumnSet 添加分栏布局
func (c *Card) AddColumnSet(columns ...any) {
	c.Body.Elements = append(c.Body.Elements, map[string]any{
		"tag":       "column_set",
		"flex_mode": "bisect",
		"columns":   columns,
	})
}

// NewColumn 创建一个新的分栏
func NewColumn(width string, elements ...any) map[string]any {
	return map[string]any{
		"tag":      "column",
		"width":    width,
		"elements": elements,
	}
}

// NewTag 创建一个标签
func NewTag(text string, color string) map[string]any {
	return map[string]any{
		"tag": "tag",
		"text": map[string]string{
			"tag":     "plain_text",
			"content": text,
		},
		"color": color,
	}
}

// NewRichText 创建富文本块
func NewRichText(content ...any) map[string]any {
	return map[string]any{
		"tag": "div",
		"text": map[string]any{
			"tag":     "rich_text",
			"content": content,
		},
	}
}

// NewTextElement 创建文本元素
func NewTextElement(content string, isLink bool, url string) map[string]any {
	if isLink {
		return map[string]any{
			"tag":     "a",
			"text":    map[string]string{"tag": "plain_text", "content": content},
			"href":    url,
		}
	}
	return map[string]any{
		"tag":     "text",
		"content": content,
	}
}

// CardHeader 卡片标题部分
type CardHeader struct {
	Title    Text   `json:"title"`
	Template string `json:"template,omitempty"`
}

// Text 文本对象
type Text struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

// CardField 卡片字段部分
type CardField struct {
	IsShort bool  `json:"is_short"`
	Text    *Text `json:"text"`
}

// CardElement 卡片元素基础结构
type CardElement struct {
	Tag     string      `json:"tag"`
	Content string      `json:"content,omitempty"`
	Text    *Text       `json:"text,omitempty"`
	Fields  []CardField `json:"fields,omitempty"`
}

// CardAction 卡片交互操作部分
type CardAction struct {
	Tag     string   `json:"tag"`
	Actions []Button `json:"actions"`
}

// Button 按钮组件
type Button struct {
	Tag  string `json:"tag"`
	Text Text   `json:"text"`
	Url  string `json:"url,omitempty"`
	Type string `json:"type,omitempty"`
}
