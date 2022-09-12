package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/ncuhome/FeishuGitPushBot/api/callback"
	githubCall "github.com/ncuhome/FeishuGitPushBot/api/callback/github"
	"github.com/ncuhome/FeishuGitPushBot/modules/feishu"
	"strings"
)

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
				genUrlButton("查看", f.Repository.HtmlUrl),
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
				genUrlButton("查看", f.Repository.HtmlUrl),
			},
		})
	case "issue":
		var f githubCall.IssueEvent
		if e := json.NewDecoder(body).Decode(&f); e != nil {
			callback.Error(c, 8, e)
			return
		}

		switch f.Action {
		case "opened":
			sendMsg(&feishu.ReqCardMsg{
				Header: &feishu.CardMsgHeader{
					Title: feishu.CardMsgElementText{
						Tag:     "plain_text",
						Content: "🍄 New Issue",
					},
				},
				Elements: []interface{}{
					genMadeByElements(f.Repository.Name, "", f.Sender.Login),
					feishu.CardMsgContentElement{
						Tag: "div",
						Fields: []feishu.CardMsgElementField{
							{
								Text: feishu.CardMsgElementText{
									Tag:     "lark_md",
									Content: fmt.Sprintf("**%s**\n%s", f.Issue.Title, f.Issue.Body),
								},
							},
						},
					},
					genUrlButton("查看", f.Issue.HtmlUrl),
				},
			})
		case "edited":
			var els = []interface{}{
				genMadeByElements(f.Repository.Name, "", f.Sender.Login),
			}
			if f.Changes.Title.From != f.Issue.Title {
				els = append(els, feishu.CardMsgContentElement{
					Tag: "div",
					Fields: []feishu.CardMsgElementField{
						{
							IsShort: true,
							Text: feishu.CardMsgElementText{
								Tag:     "lark_md",
								Content: fmt.Sprintf("**原标题**\n%s", f.Changes.Title.From),
							},
						},
						{
							IsShort: true,
							Text: feishu.CardMsgElementText{
								Tag:     "lark_md",
								Content: fmt.Sprintf("**新标题**\n%s", f.Issue.Title),
							},
						},
					},
				})
			}
			if f.Changes.Body.From != f.Issue.Body {
				els = append(els, feishu.CardMsgContentElement{
					Tag: "div",
					Fields: []feishu.CardMsgElementField{
						{
							Text: feishu.CardMsgElementText{
								Tag:     "lark_md",
								Content: fmt.Sprintf("**内容已变更**\n%s", f.Issue.Body),
							},
						},
					},
				})
			}
			sendMsg(&feishu.ReqCardMsg{
				Header: &feishu.CardMsgHeader{
					Title: feishu.CardMsgElementText{
						Tag:     "plain_text",
						Content: "🍄 Issue edited",
					},
				},
				Elements: append(els, genUrlButton("查看", f.Issue.HtmlUrl)),
			})
		case "reopened":
			fallthrough
		case "closed":
			sendMsg(&feishu.ReqCardMsg{
				Header: &feishu.CardMsgHeader{
					Title: feishu.CardMsgElementText{
						Tag:     "plain_text",
						Content: fmt.Sprintf("🍄 Issue %s", f.Action),
					},
				},
				Elements: []interface{}{
					genMadeByElements(f.Repository.Name, "", f.Sender.Login),
					feishu.CardMsgContentElement{
						Tag: "div",
						Fields: []feishu.CardMsgElementField{
							{
								Text: feishu.CardMsgElementText{
									Tag:     "plain_text",
									Content: f.Issue.Title,
								},
							},
						},
					},
					genUrlButton("查看", f.Issue.HtmlUrl),
				},
			})
		default:
			callback.Error(c, 10, nil)
		}
	default:
		callback.Error(c, 10, nil)
		return
	}

	callback.Default(c)
}
