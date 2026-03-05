package bot

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// InitRouter 初始化 Gin 路由
func InitRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 基础中间件
	r.Use(cors.Default())

	// 路由
	r.POST("/github/webhook", GithubHandler)

	return r
}
