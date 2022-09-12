package main

import (
	"github.com/ncuhome/FeishuGitPushBot/api/router"
	log "github.com/sirupsen/logrus"
)

func main() {
	log.Infoln("Sys Boost")

	if e := router.G.Run(":80"); e != nil {
		log.Fatalln(e)
	}
}
