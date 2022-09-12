package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/ncuhome/FeishuGitPushBot/api/callback"
	githubCall "github.com/ncuhome/FeishuGitPushBot/api/callback/github"
	"github.com/ncuhome/FeishuGitPushBot/modules/feishu"
	log "github.com/sirupsen/logrus"
	"strings"
)

func genMadeByElements(repo, content, author string) *feishu.CardMsgContentElement {
	return &feishu.CardMsgContentElement{
		Tag: "div",
		Fields: []feishu.CardMsgElementField{
			{
				IsShort: true,
				Text: feishu.CardMsgElementText{
					Tag:     "lark_md",
					Content: fmt.Sprintf("**目标**\n%s : %s", repo, content),
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

func sendMsg(conf *feishu.ReqCardMsg) {
	if e := feishu.SendCardMsg(conf); e != nil {
		log.Errorf("发送消息失败：%v\n", e)
	}
}

func Event(c *gin.Context) {
	i, _ := c.Get("body")
	body := i.(*bytes.Buffer)
	switch c.GetHeader("X-GitHub-Event") {
	case "push":
		var f githubCall.PushEvent
		if e := json.NewDecoder(body).Decode(&f); e != nil {
			callback.Error(c, 8, e)
			return
		}

		if len(f.Commits) == 0 {
			break
		}

		var content string
		for index, commit := range f.Commits {
			if index != 0 {
				content += "\n"
			}
			content += fmt.Sprintf(
				"%s %s - %s",
				func() string {
					if index%2 == 0 {
						return "🔸"
					}
					return "🔹"
				}(),
				commit.Message,
				commit.Committer.Name,
			)
		}

		sendMsg(&feishu.ReqCardMsg{
			Header: &feishu.CardMsgHeader{
				Title: feishu.CardMsgElementText{
					Tag:     "plain_text",
					Content: "🍏 New commits",
				},
			},
			Elements: []interface{}{
				genMadeByElements(f.Repository.Name, strings.Split(f.Ref, "/")[2], f.Sender.Login),
				feishu.CardMsgContentElement{
					Tag: "div",
					Fields: []feishu.CardMsgElementField{
						{
							Text: feishu.CardMsgElementText{
								Tag:     "lark_md",
								Content: content,
							},
						},
					},
				},
				genUrlButton("查看", f.HeadCommit.Url),
			},
		})
	case "create":
		var f githubCall.CreateEvent
		if e := json.NewDecoder(body).Decode(&f); e != nil {
			callback.Error(c, 8, e)
			return
		}

		sendMsg(&feishu.ReqCardMsg{
			Header: &feishu.CardMsgHeader{
				Title: feishu.CardMsgElementText{
					Tag:     "plain_text",
					Content: fmt.Sprintf("🍊 New %s", f.RefType),
				},
			},
			Elements: []interface{}{
				genMadeByElements(f.Repository.Name, f.Ref, f.Sender.Login),
				genUrlButton("查看", f.Repository.Url),
			},
		})
	case "delete":
		var f githubCall.DeleteEvent
		if e := json.NewDecoder(body).Decode(&f); e != nil {
			callback.Error(c, 8, e)
			return
		}

		sendMsg(&feishu.ReqCardMsg{
			Header: &feishu.CardMsgHeader{
				Title: feishu.CardMsgElementText{
					Tag:     "plain_text",
					Content: fmt.Sprintf("🍅 %s deleted", f.RefType),
				},
			},
			Elements: []interface{}{
				genMadeByElements(f.Repository.Name, f.Ref, f.Sender.Login),
				genUrlButton("查看", f.Repository.Url),
			},
		})
	default:
		callback.Error(c, 10, nil)
		return
	}

	callback.Default(c)
}
