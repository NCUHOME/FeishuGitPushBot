package router

import (
	"github.com/gin-gonic/gin"
	"github.com/ncuhome/FeishuGitPushBot/api/middlewares"
)

var G *gin.Engine

func init() {
	gin.SetMode(gin.ReleaseMode)
	G = gin.Default()

	G.Use(middlewares.Cors(), middlewares.Options())
	G.Use(middlewares.Secure())
}
