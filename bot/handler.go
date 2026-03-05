package bot

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/go-github/v84/github"
)

// GithubHandler 处理 GitHub Webhook 请求
func GithubHandler(c *gin.Context) {
	// 验证签名
	payload, err := github.ValidatePayload(c.Request, []byte(C.Github.WebhookKey))
	if err != nil {
		slog.Error("签名验证失败", "error", err)
		c.AbortWithStatusJSON(400, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	eventType := github.WebHookType(c.Request)
	event, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		slog.Error("解析 Webhook 失败", "error", err)
		c.AbortWithStatusJSON(400, gin.H{"code": 2, "msg": err.Error()})
		return
	}

	detail := parseEvent(event, eventType, payload)

	// 获取仓库和发送者信息
	var m map[string]any
	_ = json.Unmarshal(payload, &m)
	repo := ext(m, "repository", "full_name")
	repoUrl := ext(m, "repository", "html_url")
	sender := ext(m, "sender", "login")
	senderUrl := ext(m, "sender", "html_url")

	// 构建飞书卡片
	card := buildCard(repo, repoUrl, sender, senderUrl, detail)

	if err := SendCard(card); err != nil {
		slog.Error("发送飞书消息失败", "error", err)
	}

	c.JSON(200, gin.H{"code": 0, "msg": "ok"})
}

type eventDetail struct {
	Title string
	Text  string
	URL   string
	Ref   string
}

func parseEvent(event any, eventType string, payload []byte) eventDetail {
	d := eventDetail{Title: fmt.Sprintf("🔔 GitHub 推送: %s", eventType)}

	switch e := event.(type) {
	case *github.PushEvent:
		ref := e.GetRef()
		var lines []string
		isTag := strings.HasPrefix(ref, "refs/tags/")
		repoUrl := e.GetRepo().GetHTMLURL()

		if isTag {
			tag := strings.TrimPrefix(ref, "refs/tags/")
			d.Title = "🏷️ 标签推送"
			d.Ref = fmt.Sprintf("🏷️ [%s](%s/releases/tag/%s)", tag, repoUrl, tag)
			d.URL = fmt.Sprintf("%s/releases/tag/%s", repoUrl, tag)
		} else if strings.HasPrefix(ref, "refs/heads/") {
			branch := strings.TrimPrefix(ref, "refs/heads/")
			d.Title = "🌿 分支推送到"
			d.Ref = fmt.Sprintf("🌿 [%s](%s/tree/%s)", branch, repoUrl, branch)
		}

		if len(e.Commits) > 0 {
			for i, c := range e.Commits {
				emoji := "🔹"
				if i%2 != 0 {
					emoji = "🔸"
				}
				msg := strings.Split(c.GetMessage(), "\n")[0]
				lines = append(lines, fmt.Sprintf("%s [%s](%s) %s - %s", emoji, c.GetID()[:7], c.GetURL(), msg, c.GetAuthor().GetName()))
			}
		} else if e.GetDeleted() {
			d.Title = "🗑️ 引用已删除"
			lines = append(lines, "_删除了此引用_")
		} else if e.GetCreated() {
			d.Title = "🆕 引用已创建"
			lines = append(lines, "_创建了新引用_")
		}
		d.Text = strings.Join(lines, "\n")
		if hc := e.GetHeadCommit(); hc != nil {
			d.URL = hc.GetURL()
		}

	case *github.PullRequestEvent:
		pr := e.GetPullRequest()
		action := e.GetAction()
		icon, actionZh := "🔄", action
		switch action {
		case "opened":
			actionZh = "已开启"
		case "closed":
			if pr.GetMerged() {
				actionZh, icon = "已合并", "💜"
			} else {
				actionZh, icon = "已关闭", "❌"
			}
		}
		d.Title = fmt.Sprintf("%s 合并请求 %s", icon, actionZh)
		d.Text = fmt.Sprintf("**标题**: %s\n**状态**: %s\n> %s", pr.GetTitle(), pr.GetState(), truncate(pr.GetBody()))
		d.URL = pr.GetHTMLURL()

	case *github.IssuesEvent:
		action := e.GetAction()
		icon, actionZh := "🐛", action
		if action == "closed" {
			icon, actionZh = "✅", "已关闭"
		}
		d.Title = fmt.Sprintf("%s 问题 %s", icon, actionZh)
		iss := e.GetIssue()
		d.Text = fmt.Sprintf("**标题**: %s\n**状态**: %s\n> %s", iss.GetTitle(), iss.GetState(), truncate(iss.GetBody()))
		d.URL = iss.GetHTMLURL()

	case *github.WorkflowRunEvent:
		wr := e.GetWorkflowRun()
		conclusion, icon := wr.GetConclusion(), "🚀"
		if conclusion == "success" {
			icon = "✅"
		} else if conclusion == "failure" {
			icon = "💥"
		}
		d.Title = fmt.Sprintf("%s 工作流 %s", icon, e.GetAction())
		d.Text = fmt.Sprintf("**名称**: %s\n**状态**: `%s`\n**结论**: `%s`", wr.GetName(), wr.GetStatus(), conclusion)
		d.Ref = fmt.Sprintf("🌿 [%s](%s/tree/%s)", wr.GetHeadBranch(), e.GetRepo().GetHTMLURL(), wr.GetHeadBranch())
		d.URL = wr.GetHTMLURL()

	default:
		// 其他事件简略处理
		var m map[string]any
		_ = json.Unmarshal(payload, &m)
		if act, ok := m["action"].(string); ok {
			d.Text = fmt.Sprintf("**动作**: `%s`", act)
		}
	}
	return d
}

func buildCard(repo, repoUrl, sender, senderUrl string, detail eventDetail) *Card {
	card := &Card{
		Header: &CardHeader{
			Title:    Text{Tag: "plain_text", Content: detail.Title},
			Template: getTemplate(detail.Title),
		},
	}

	repoLink := fmt.Sprintf("[%s](%s)", repo, repoUrl)
	senderLink := fmt.Sprintf("[%s](%s)", sender, senderUrl)

	fields := []CardField{
		{IsShort: true, Text: Text{Tag: "lark_md", Content: fmt.Sprintf("**目标仓库**\n%s", repoLink)}},
	}
	if detail.Ref != "" {
		fields = append(fields, CardField{IsShort: true, Text: Text{Tag: "lark_md", Content: fmt.Sprintf("**分支/引用**\n%s", detail.Ref)}})
	}
	fields = append(fields, CardField{IsShort: true, Text: Text{Tag: "lark_md", Content: fmt.Sprintf("**触发者**\n%s", senderLink)}})

	// 组装卡片内容
	card.AddElement("div", nil, fields)
	card.AddDivider()

	if detail.Text != "" {
		card.AddElement("div", &Text{Tag: "lark_md", Content: detail.Text}, nil)
	}

	if detail.URL != "" {
		card.AddAction(Button{
			Tag:  "button",
			Text: Text{Tag: "plain_text", Content: "查看详情"},
			Url:  detail.URL,
			Type: "primary",
		})
	}

	return card
}

func getTemplate(title string) string {
	if containsAny(title, "❌", "💥", "💔", "🔴") {
		return "red"
	}
	if containsAny(title, "✅", "💜", "🟢") {
		return "green"
	}
	if containsAny(title, "⚠️", "🏃", "🟡") {
		return "orange"
	}
	return "blue"
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func truncate(s string) string {
	if len(s) > 100 {
		return s[:100] + "..."
	}
	return s
}

func ext(m map[string]any, keys ...string) string {
	var cur any = m
	for _, k := range keys {
		cm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = cm[k]
	}
	s, _ := cur.(string)
	return s
}
