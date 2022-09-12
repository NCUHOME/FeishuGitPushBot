package github

import (
	"github.com/gin-gonic/gin"
	controllers "github.com/ncuhome/FeishuGitPushBot/api/controllers/github"
	"github.com/ncuhome/FeishuGitPushBot/api/middlewares"
)

func Router(G *gin.RouterGroup) {
	G.Use(middlewares.GithubWebhookAuth())

	G.POST("/webhook", controllers.Event)
}
