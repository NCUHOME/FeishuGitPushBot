package middlewares

import (
	"github.com/Mmx233/secure"
	"github.com/gin-gonic/gin"
	"github.com/ncuhome/FeishuGitPushBot/api/callback"
)

func Secure() gin.HandlerFunc {
	return secure.New(&secure.Config{
		RateLimit: 180,
		CallBack: func(c *gin.Context) {
			callback.Error(c, 6, nil)
		},
	})
}
