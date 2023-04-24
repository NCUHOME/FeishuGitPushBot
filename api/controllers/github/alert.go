package controllers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/ncuhome/FeishuGitPushBot/api/callback"
	form "github.com/ncuhome/FeishuGitPushBot/api/models/form/github"
	"github.com/ncuhome/FeishuGitPushBot/modules/feishu"
	"strings"
)

func Event(c *gin.Context) {
	i, _ := c.Get("body")
	body := i.(*bytes.Buffer)
	switch c.GetHeader("X-GitHub-Event") {
	case "push":
		var f form.PushEvent
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
				genMadeByElements(f.Repository.Name, strings.Join(strings.Split(f.Ref, "/")[2:], "/"), f.Sender.Login),
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
		var f form.CreateEvent
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
		var f form.DeleteEvent
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
	case "issues":
		var f form.IssueEvent
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
				feishu.CardMsgContentElement{
					Tag: "div",
					Fields: []feishu.CardMsgElementField{
						{
							Text: feishu.CardMsgElementText{
								Tag:     "lark_md",
								Content: fmt.Sprintf("**%s**", f.Issue.Title),
							},
						},
					},
				},
			}
			if f.Changes.Title.From != "" && f.Changes.Title.From != f.Issue.Title {
				els = append(els, feishu.CardMsgContentElement{
					Tag: "div",
					Fields: []feishu.CardMsgElementField{
						{
							Text: feishu.CardMsgElementText{
								Tag:     "lark_md",
								Content: fmt.Sprintf("**旧标题**\n%s", f.Changes.Title.From),
							},
						},
					},
				})
			}
			if f.Changes.Body.From != "" && f.Changes.Body.From != f.Issue.Body {
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
									Tag:     "lark_md",
									Content: fmt.Sprintf("**%s**", f.Issue.Title),
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
	case "issue_comment":
		var f form.IssueCommentEvent
		if e := json.NewDecoder(body).Decode(&f); e != nil {
			callback.Error(c, 8, e)
			return
		}

		sendMsg(&feishu.ReqCardMsg{
			Header: &feishu.CardMsgHeader{
				Title: feishu.CardMsgElementText{
					Tag:     "plain_text",
					Content: fmt.Sprintf("🌻 Comment %s", f.Action),
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
								Content: fmt.Sprintf("**%s**", f.Issue.Title),
							},
						},
					},
				},
				feishu.CardMsgContentElement{
					Tag: "div",
					Fields: []feishu.CardMsgElementField{
						{
							Text: feishu.CardMsgElementText{
								Tag:     "lark_md",
								Content: fmt.Sprintf("**内容**\n%s", f.Comment.Body),
							},
						},
					},
				},
				genUrlButton("查看", f.Comment.HtmlUrl),
			},
		})
	case "pull_request":
		var f form.PullRequestEvent
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
						Content: "🥕 New PullRequest",
					},
				},
				Elements: []interface{}{
					genMadeByElements(f.Repository.Name, "", f.Sender.Login),
					feishu.CardMsgContentElement{
						Tag: "div",
						Fields: []feishu.CardMsgElementField{
							{
								IsShort: true,
								Text: feishu.CardMsgElementText{
									Tag:     "lark_md",
									Content: fmt.Sprintf("**来源分支**\n%s", f.PullRequest.Head.Ref),
								},
							},
							{
								IsShort: true,
								Text: feishu.CardMsgElementText{
									Tag:     "lark_md",
									Content: fmt.Sprintf("**目标分支**\n%s", f.PullRequest.Base.Ref),
								},
							},
						},
					},
					feishu.CardMsgContentElement{
						Tag: "div",
						Fields: []feishu.CardMsgElementField{
							{
								Text: feishu.CardMsgElementText{
									Tag:     "lark_md",
									Content: fmt.Sprintf("**%s**\n%s", f.PullRequest.Title, f.PullRequest.Body),
								},
							},
						},
					},
					genUrlButton("查看", f.PullRequest.HtmlUrl),
				},
			})
		case "edited":
			var els = []interface{}{
				genMadeByElements(f.Repository.Name, "", f.Sender.Login),
				feishu.CardMsgContentElement{
					Tag: "div",
					Fields: []feishu.CardMsgElementField{
						{
							Text: feishu.CardMsgElementText{
								Tag:     "lark_md",
								Content: fmt.Sprintf("**%s**", f.PullRequest.Title),
							},
						},
					},
				},
			}
			if f.Changes.Title.From != "" && f.Changes.Title.From != f.PullRequest.Title {
				els = append(els, feishu.CardMsgContentElement{
					Tag: "div",
					Fields: []feishu.CardMsgElementField{
						{
							Text: feishu.CardMsgElementText{
								Tag:     "lark_md",
								Content: fmt.Sprintf("**原标题**\n%s", f.Changes.Title.From),
							},
						},
					},
				})
			}
			if f.Changes.Body.From != "" && f.Changes.Body.From != f.PullRequest.Body {
				els = append(els, feishu.CardMsgContentElement{
					Tag: "div",
					Fields: []feishu.CardMsgElementField{
						{
							Text: feishu.CardMsgElementText{
								Tag:     "lark_md",
								Content: fmt.Sprintf("**内容已变更**\n%s", f.PullRequest.Body),
							},
						},
					},
				})
			}
			sendMsg(&feishu.ReqCardMsg{
				Header: &feishu.CardMsgHeader{
					Title: feishu.CardMsgElementText{
						Tag:     "plain_text",
						Content: "🥕 PullRequest edited",
					},
				},
				Elements: append(els, genUrlButton("查看", f.PullRequest.HtmlUrl)),
			})
		case "closed":
			if f.PullRequest.Merged {
				f.Action = "merged"
			}
			fallthrough
		case "reopened":
			sendMsg(&feishu.ReqCardMsg{
				Header: &feishu.CardMsgHeader{
					Title: feishu.CardMsgElementText{
						Tag:     "plain_text",
						Content: fmt.Sprintf("🥕 PullRequest %s", f.Action),
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
									Content: fmt.Sprintf("**%s**", f.PullRequest.Title),
								},
							},
						},
					},
					genUrlButton("查看", f.PullRequest.HtmlUrl),
				},
			})
		default:
			callback.Error(c, 10, nil)
			return
		}
	default:
		callback.Error(c, 10, nil)
		return
	}

	callback.Default(c)
}
