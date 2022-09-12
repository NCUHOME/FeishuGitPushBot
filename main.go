package main

import (
	"github.com/ncuhome/FeishuGitPushBot/modules/feishu"
	log "github.com/sirupsen/logrus"
)

func main() {
	log.Infoln("Sys Boost")

	if e := feishu.SendText("text"); e != nil {
		panic(e)
	}
}
