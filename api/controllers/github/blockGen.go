package controllers

import (
	"fmt"
	"github.com/ncuhome/FeishuGitPushBot/modules/feishu"
	log "github.com/sirupsen/logrus"
)

func sendMsg(conf *feishu.ReqCardMsg) {
	if e := feishu.SendCardMsg(conf); e != nil {
		log.Errorf("发送消息失败：%v\n", e)
	}
}

func genMadeByElements(repo, content, author string) *feishu.CardMsgContentElement {
	return &feishu.CardMsgContentElement{
		Tag: "div",
		Fields: []feishu.CardMsgElementField{
			{
				IsShort: true,
				Text: feishu.CardMsgElementText{
					Tag: "lark_md",
					Content: func() string {
						if content != "" {
							return fmt.Sprintf("**目标**\n%s | %s", repo, content)
						}
						return fmt.Sprintf("**目标**\n%s", repo)
					}(),
				},
			},
			{
				IsShort: true,
				Text: feishu.CardMsgElementText{
					Tag:     "lark_md",
					Content: fmt.Sprintf("**创建人**\n%s", author),
				},
			},
		},
	}
}

func genUrlButton(content, url string) *feishu.CardMsgActionElement {
	return &feishu.CardMsgActionElement{
		Tag: "action",
		Actions: []feishu.CardMsgElementButton{
			{
				Tag: "button",
				Text: feishu.CardMsgElementText{
					Tag:     "plain_text",
					Content: content,
				},
				Url: url,
			},
		},
	}
}
