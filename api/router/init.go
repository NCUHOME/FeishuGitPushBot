package router

import (
	"github.com/gin-gonic/gin"
	"github.com/ncuhome/FeishuGitPushBot/api/middlewares"
	"github.com/ncuhome/FeishuGitPushBot/api/router/github"
)

var G *gin.Engine

func init() {
	gin.SetMode(gin.ReleaseMode)
	G = gin.Default()

	G.Use(middlewares.Cors(), middlewares.Options())
	G.Use(middlewares.Secure())

	github.Router(G.Group("/github"))
}
