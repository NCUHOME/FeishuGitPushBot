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

func doSendMsg(title string, text string, link string) {
	if e := feishu.SendPostText(title,
		[]feishu.ReqSendPostTextContent{
			{
				Tag:  "text",
				Text: text + "\n",
			},
			{
				Tag:  "a",
				Text: "点击查看",
				Href: link,
			},
		}); e != nil {
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

		title := fmt.Sprintf(
			"[%s:%s] %d new commit by %s",
			f.Repository.Name,
			strings.Split(f.Ref, "/")[2],
			len(f.Commits),
			f.Pusher.Name,
		)
		var content string
		for _, commit := range f.Commits {
			content += fmt.Sprintf(
				"%s - %s\n",
				commit.Message,
				commit.Committer.Name,
			)
		}
		doSendMsg(title, content, f.HeadCommit.Url)
	case "create":
		var f githubCall.CreateEvent
		if e := json.NewDecoder(body).Decode(&f); e != nil {
			callback.Error(c, 8, e)
			return
		}

		doSendMsg(fmt.Sprintf(
			"[%s] New %s %s was pushed by %s",
			f.Repository.Name,
			f.RefType,
			f.Ref,
			f.Sender.Login,
		), "", f.Repository.Url)
	case "delete":
		var f githubCall.DeleteEvent
		if e := json.NewDecoder(body).Decode(&f); e != nil {
			callback.Error(c, 8, e)
			return
		}

		doSendMsg(fmt.Sprintf(
			"[%s] The %s %s was deleted by %s",
			f.Repository.Name,
			f.RefType,
			f.Ref,
			f.Sender.Login,
		), "", f.Repository.Url)
	default:
		callback.Error(c, 10, nil)
		return
	}

	callback.Default(c)
}
