package bot

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/go-github/v84/github"
)

// GithubHandler 处理 GitHub Webhook 请求
func GithubHandler(c *gin.Context) {
	// 验证签名
	secret := strings.TrimSpace(C.Github.Key)
	payload, err := github.ValidatePayload(c.Request, []byte(secret))
	if err != nil {
		slog.Error("Signature verification failed", "error", err)
		c.AbortWithStatusJSON(400, gin.H{"code": 1, "msg": "signature verification failed"})
		return
	}

	eventType := github.WebHookType(c.Request)
	


	event, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		slog.Error("Failed to parse Webhook", "error", err)
		c.AbortWithStatusJSON(400, gin.H{"code": 2, "msg": err.Error()})
		return
	}

	detail := ParseEvent(event, eventType)
	if detail.Skip {
		c.JSON(200, gin.H{"code": 0, "msg": "ignored"})
		return
	}

	// 将消息存入队列 (原始 Webhook 记录)
	if DB != nil {
		_, err := DB.NewInsert().Model(&WebhookEvent{
			EventType: eventType,
			Payload:   string(payload),
			Status:    "pending",
		}).Exec(c.Request.Context())
		if err != nil {
			slog.Error("Failed to record Webhook event", "error", err)
			c.AbortWithStatusJSON(500, gin.H{"code": 3, "msg": "failed to record event"})
			return
		}
		c.JSON(200, gin.H{"code": 0, "msg": "queued"})
		return
	}

	// 兜底方案：如果数据库不可用，则直接构建并发送 (兼容模式)
	// 获取基本信息
	var m map[string]any
	_ = json.Unmarshal(payload, &m)
	repo := ext(m, "repository", "full_name")
	repoUrl := ext(m, "repository", "html_url")
	sender := ext(m, "sender", "login")
	senderUrl := ext(m, "sender", "html_url")
	avatarUrl := ext(m, "sender", "avatar_url")
	card := BuildCard(c.Request.Context(), repo, repoUrl, sender, senderUrl, avatarUrl, detail)
	if _, err := SendToChat("", card); err != nil {
		slog.Error("Fallback send failed", "repo", repo, "event", eventType, "error", err)
		c.AbortWithStatusJSON(500, gin.H{"code": 3, "msg": "failed to send message"})
		return
	}

	c.JSON(200, gin.H{"code": 0, "msg": "ok"})
}

func ext(m map[string]any, keys ...string) string {
	var cur any = m
	for _, k := range keys {
		cm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = cm[k]
	}
	switch v := cur.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprintf("%d", v)
	}
	return ""
}
