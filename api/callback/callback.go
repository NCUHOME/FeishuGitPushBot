package callback

import (
	"github.com/gin-gonic/gin"
	"github.com/ncuhome/FeishuGitPushBot/api/models/response"
	log "github.com/sirupsen/logrus"
)

func Error(c *gin.Context, code uint, e error) {
	log.Debugln("Response Code: ", code, " Error: ", e)
	c.AsciiJSON(400, response.WithDynamicData{
		Code: code,
	})
	c.Abort()
}

func Success(c *gin.Context, data interface{}) {
	c.AsciiJSON(200, response.WithDynamicData{
		Data: data,
	})
}

func SuccessWithCode(c *gin.Context, code uint, data interface{}) {
	c.AsciiJSON(200, response.WithDynamicData{
		Code: code,
		Data: data,
	})
}

func Default(c *gin.Context) {
	Success(c, nil)
}
