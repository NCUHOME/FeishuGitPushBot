package middlewares

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"github.com/gin-gonic/gin"
	"github.com/ncuhome/FeishuGitPushBot/api/callback"
	"github.com/ncuhome/FeishuGitPushBot/global"
	"io"
	"strings"
)

func GithubWebhookAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("X-Hub-Signature-256")
		if !strings.HasPrefix(token, "sha256=") {
			callback.Error(c, 5, nil)
			return
		}

		defer c.Request.Body.Close()
		s, e := io.ReadAll(c.Request.Body)
		if e != nil {
			callback.Error(c, 15, nil)
			return
		}

		h := hmac.New(sha256.New, []byte(global.Config.Github.WebhookKey))
		h.Write(s)
		if "sha256="+hex.EncodeToString(h.Sum(nil)) != token {
			callback.Error(c, 5, nil)
			return
		}

		c.Set("body", bytes.NewBuffer(s))
	}
}
