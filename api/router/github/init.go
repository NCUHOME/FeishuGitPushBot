package github

import (
	"github.com/gin-gonic/gin"
	"github.com/ncuhome/FeishuGitPushBot/api/middlewares"
)

func Router(G *gin.RouterGroup) {
	G.Use(middlewares.GithubWebhookAuth())
}
