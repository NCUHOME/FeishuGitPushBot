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
	"sync"
	"time"

	"resty.dev/v3"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

var httpClient = resty.New().
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

// GetImageKey 从缓存中获取飞书 img_key（纯 DB 查询，不阻塞）
func GetImageKey(ctx context.Context, url string) string {
	if url == "" || DB == nil {
		return ""
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var cache ImageCache
	if err := DB.NewSelect().Model(&cache).Where("url = ?", url).Scan(ctx); err == nil {
		return cache.ImgKey
	}
	return ""
}

// syncUploadImage 同步下载并上传图片到飞书，返回 img_key
func syncUploadImage(ctx context.Context, url string) string {
	// 1. 下载图片 (使用传入的 ctx，受限时控制)
	imageRes, err := httpClient.R().SetContext(ctx).Get(url)
	if err != nil || imageRes == nil || imageRes.IsError() {
		return ""
	}
	defer imageRes.Body.Close()
	imgData, err := io.ReadAll(imageRes.Body)
	if err != nil || len(imgData) == 0 {
		return ""
	}
	newHash := fmt.Sprintf("%x", md5.Sum(imgData))

	// 2. 核心优化：检查缓存，如果内容没变（Hash 一致），则不重复上传飞书
	if DB != nil {
		var oldCache ImageCache
		if err := DB.NewSelect().Model(&oldCache).Where("url = ?", url).Scan(ctx); err == nil {
			if oldCache.Hash == newHash && oldCache.ImgKey != "" {
				// 内容未变，直接更新时间并返回旧 Key
				_, _ = DB.NewUpdate().Model(&oldCache).
					Set("updated_at = ?", time.Now()).
					WherePK().Exec(context.Background())
				return oldCache.ImgKey
			}
		}
	}

	client := GetLarkClient()
	var resp *larkim.CreateImageResp

	// 3. 上传图片到飞书 (仅在内容有变化或无缓存时执行)
	for i := 0; i < 3; i++ {
		if ctx.Err() != nil {
			return ""
		}
		uploadCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
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
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(time.Duration(i+1) * 1 * time.Second):
		}
	}

	if err != nil || resp == nil || !resp.Success() || resp.Data == nil || resp.Data.ImageKey == nil {
		return ""
	}

	imgKey := *resp.Data.ImageKey
	if DB != nil {
		cache := ImageCache{
			URL:       url,
			ImgKey:    imgKey,
			Hash:      newHash,
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

// SendToChat 发送消息到指定群组，返回消息 ID
func SendToChat(chatID string, card *Card) (string, error) {
	if chatID == "" {
		chatID = C.Feishu.ChatID
	}
	if chatID == "" {
		return "", fmt.Errorf("target chat ID (CHAT_ID) not specified")
	}
	return sendMessage(chatID, "", card)
}

// UpdateMessage 更新已发送的消息卡片
func UpdateMessage(messageID string, card *Card) error {
	client := GetLarkClient()
	var (
		resp *larkim.PatchMessageResp
		err  error
	)
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
		return fmt.Errorf("update message failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

// ReplyToMessage 以话题方式回复指定消息
func ReplyToMessage(parentID string, card *Card) (string, error) {
	return sendMessage("", parentID, card)
}

func sendMessage(chatID, parentID string, card *Card) (string, error) {
	client := GetLarkClient()

	if parentID != "" {
		var (
			resp *larkim.ReplyMessageResp
			err  error
		)
		for i := 0; i < 3; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			resp, err = client.Im.Message.Reply(ctx, larkim.NewReplyMessageReqBuilder().
				MessageId(parentID).
				Body(larkim.NewReplyMessageReqBodyBuilder().
					MsgType(larkim.MsgTypeInteractive).
					Content(card.String()).
					ReplyInThread(true).
					Build()).
				Build())
			cancel()
			if err == nil && resp.Success() && resp.Data != nil {
				return *resp.Data.MessageId, nil
			}
			time.Sleep(time.Duration(i+1) * 2 * time.Second)
		}
		if err != nil {
			return "", err
		}
		if resp != nil && !resp.Success() {
			return "", fmt.Errorf("reply message failed code=%d msg=%s", resp.Code, resp.Msg)
		}
		return "", fmt.Errorf("reply message failed: resp is nil or data is nil")
	}

	if chatID == "" {
		chatID = C.Feishu.ChatID
	}
	if chatID == "" {
		return "", fmt.Errorf("target chat ID (CHAT_ID) not specified")
	}

	var (
		resp *larkim.CreateMessageResp
		err  error
	)
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
		if err == nil && resp.Success() && resp.Data != nil {
			return *resp.Data.MessageId, nil
		}
		time.Sleep(time.Duration(i+1) * 2 * time.Second)
	}
	if err != nil {
		return "", err
	}
	if resp != nil && !resp.Success() {
		return "", fmt.Errorf("send message failed code=%d msg=%s", resp.Code, resp.Msg)
	}
	return "", fmt.Errorf("send message failed: resp is nil or data is nil")
}

// SendCard 通过 Webhook 发送卡片（兜底兼容模式）
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

	var result struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}

	res, err := httpClient.R().
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]any{
			"msg_type":  "interactive",
			"card":      card,
			"timestamp": fmt.Sprint(ts),
			"sign":      sign,
		}).
		SetResult(&result).
		Post(C.Feishu.Webhook)

	if err != nil {
		return err
	}
	if res.StatusCode() > 299 {
		return fmt.Errorf("HTTP %d: %s", res.StatusCode(), res.String())
	}
	if result.Code != 0 {
		return fmt.Errorf("feishu error code=%d msg=%s", result.Code, result.Msg)
	}
	return nil
}

func genSign(secret string, ts int64) (string, error) {
	str := fmt.Sprintf("%v\n%s", ts, secret)
	h := hmac.New(sha256.New, []byte(secret))
	if _, err := h.Write([]byte(str)); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(h.Sum(nil)), nil
}

// ---------------------------------------------------------------------------
// Card V2 数据结构
// 参考: https://open.feishu.cn/document/feishu-cards/card-json-v2-structure
// ---------------------------------------------------------------------------

// Card 飞书消息卡片顶层结构 (schema 2.0)
type Card struct {
	Schema string      `json:"schema"`
	Config *CardConfig `json:"config,omitempty"`
	Header *CardHeader `json:"header,omitempty"`
	Body   *CardBody   `json:"body,omitempty"`
}

// CardConfig 卡片全局配置
// V2 规范字段：enable_forward / update_multi
type CardConfig struct {
	// 是否允许转发卡片
	EnableForward bool `json:"enable_forward"`
	// 是否允许多端同步更新（替代旧版 wide_screen_mode）
	UpdateMulti bool `json:"update_multi"`
}

// CardHeader 卡片标题区
type CardHeader struct {
	// title 必须为 plain_text 或 lark_md
	Title CardText `json:"title"`
	// template 控制标题栏背景色: blue/green/red/orange/grey/purple/indigo/wathet/turquoise/yellow/lime/pink/carmine
	Template string `json:"template,omitempty"`
	// subtitle 副标题（可选）
	Subtitle *CardText `json:"subtitle,omitempty"`
}

// CardBody 卡片正文
type CardBody struct {
	Elements []any `json:"elements"`
}

// CardText 通用文本对象
// tag 可为 plain_text 或 lark_md
type CardText struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

// NewCard 创建符合 V2 规范的新卡片
func NewCard() *Card {
	return &Card{
		Schema: "2.0",
		Config: &CardConfig{
			EnableForward: true,
			UpdateMulti:   true,
		},
		Header: &CardHeader{},
		Body:   &CardBody{Elements: []any{}},
	}
}

// String 将卡片序列化为 JSON 字符串（供飞书 API content 字段使用）
func (c *Card) String() string {
	b, _ := json.Marshal(c)
	return string(b)
}

// ---------------------------------------------------------------------------
// 卡片 Body 元素构造方法
// ---------------------------------------------------------------------------

// AddDivider 添加分割线
func (c *Card) AddDivider() {
	c.Body.Elements = append(c.Body.Elements, map[string]string{"tag": "hr"})
}

// AddMarkdown 添加 lark_md Markdown 块
func (c *Card) AddMarkdown(content string) {
	c.Body.Elements = append(c.Body.Elements, map[string]any{
		"tag":     "markdown",
		"content": content,
	})
}

// AddCollapsiblePanel 添加折叠面板
// V2 规范：header.title 必须为 lark_md；expanded 控制默认展开状态
func (c *Card) AddCollapsiblePanel(title, content string) {
	if title == "" {
		title = "📝 展开查看完整内容"
	}
	c.Body.Elements = append(c.Body.Elements, map[string]any{
		"tag":      "collapsible_panel",
		"expanded": false,
		"header": map[string]any{
			"title": map[string]string{
				"tag":     "lark_md",
				"content": title,
			},
			"padding": "4px 0px",
		},
		"elements": []any{
			map[string]any{
				"tag":     "markdown",
				"content": content,
			},
		},
		"border": map[string]any{
			"color":         "grey",
			"corner_radius": "4px",
		},
	})
}

// AddActions 添加操作按钮容器（V2 规范：按钮必须放在 actions 容器内）
// layout 可为 "bisected"、"trisection"、"flow"（默认 flow）
func (c *Card) AddActions(layout string, buttons ...ActionButton) {
	if len(buttons) == 0 {
		return
	}
	if layout == "" {
		layout = "flow"
	}
	actions := make([]any, 0, len(buttons))
	for _, b := range buttons {
		btn := map[string]any{
			"tag":  "button",
			"text": map[string]string{"tag": "plain_text", "content": b.Text},
			"type": b.Type,
		}
		if b.URL != "" {
			btn["url"] = b.URL
		}
		if b.Disabled {
			btn["disabled"] = true
		}
		actions = append(actions, btn)
	}
	c.Body.Elements = append(c.Body.Elements, map[string]any{
		"tag":     "action",
		"layout":  layout,
		"actions": actions,
	})
}

// ActionButton 按钮描述
type ActionButton struct {
	Text string
	URL  string
	// Type: primary / danger / default
	Type     string
	Disabled bool
}

// AddNote 添加备注（V2 规范：note 组件，elements 内可含 img / plain_text / lark_md）
func (c *Card) AddNote(elements ...any) {
	if len(elements) == 0 {
		return
	}
	c.Body.Elements = append(c.Body.Elements, map[string]any{
		"tag":      "note",
		"elements": elements,
	})
}

// AddNoteText 添加纯文本备注（快捷方法）
func (c *Card) AddNoteText(content string) {
	c.AddNote(map[string]any{
		"tag":     "lark_md",
		"content": content,
	})
}

// AddColumnSet 添加分栏布局容器
// flexMode: none / stretch / flow / bisect / trisect
func (c *Card) AddColumnSet(flexMode string, horizontalSpacing string, columns ...any) {
	if flexMode == "" {
		flexMode = "none"
	}
	if horizontalSpacing == "" {
		horizontalSpacing = "small"
	}
	c.Body.Elements = append(c.Body.Elements, map[string]any{
		"tag":                "column_set",
		"flex_mode":          flexMode,
		"horizontal_spacing": horizontalSpacing,
		"columns":            columns,
	})
}

// NewColumn 创建分栏列
// width: "auto" | "weighted" | 像素值字符串
func NewColumn(width string, weight int, verticalAlign string, elements ...any) map[string]any {
	col := map[string]any{
		"tag":      "column",
		"width":    width,
		"elements": elements,
	}
	if weight > 0 {
		col["weight"] = weight
	}
	if verticalAlign != "" {
		col["vertical_align"] = verticalAlign
	}
	return col
}

// NewImageElement 创建图片元素（用于列、备注等容器内）
// mode: crop_center / fit_horizontal / stretch / large / medium / small / tiny
func NewImageElement(imgKey string, altText string, customWidth int, mode string) map[string]any {
	el := map[string]any{
		"tag":     "img",
		"img_key": imgKey,
		"alt": map[string]string{
			"tag":     "plain_text",
			"content": altText,
		},
		"mode": mode,
	}
	if customWidth > 0 {
		el["custom_width"] = customWidth
	}
	return el
}

// NewMarkdownElement 创建 Markdown 内联元素（用于列等容器内）
func NewMarkdownElement(content string) map[string]any {
	return map[string]any{
		"tag":     "markdown",
		"content": content,
	}
}

// AddImage 在卡片正文中添加图片块
func (c *Card) AddImage(imgKey, altText string, mode string) {
	el := map[string]any{
		"tag":     "img",
		"img_key": imgKey,
		"alt": map[string]string{
			"tag":     "plain_text",
			"content": altText,
		},
		"mode": mode,
	}
	c.Body.Elements = append(c.Body.Elements, el)
}

// ---------------------------------------------------------------------------
// 废弃兼容层（避免其他文件编译报错，后续可删除）
// ---------------------------------------------------------------------------

// CardField 卡片字段（旧版 div fields，V2 已改用 column_set）
type CardField struct {
	IsShort bool      `json:"is_short"`
	Text    *CardText `json:"text"`
}

// CardElement 旧版通用元素（兼容保留）
type CardElement struct {
	Tag     string      `json:"tag"`
	Content string      `json:"content,omitempty"`
	Text    *CardText   `json:"text,omitempty"`
	Fields  []CardField `json:"fields,omitempty"`
}

// Text 兼容旧版 Text 类型（与 CardText 等价）
type Text = CardText
