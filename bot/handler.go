package bot

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/go-github/v84/github"
)

// GithubHandler 处理 GitHub Webhook 请求
func GithubHandler(c *gin.Context) {
	// 验证签名
	secret := strings.TrimSpace(C.Github.Key)
	payload, err := github.ValidatePayload(c.Request, []byte(secret))
	if err != nil {
		slog.Error("签名验证失败",
			"error", err,
			"secret_len", len(secret),
			"content_type", c.ContentType(),
		)
		c.AbortWithStatusJSON(400, gin.H{"code": 1, "msg": "签名验证失败"})
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

	if detail.Skip {
		c.JSON(200, gin.H{"code": 0, "msg": "ignored"})
		return
	}

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
	Skip  bool
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
			d.Title = "🌿 分支推送"
			d.Ref = fmt.Sprintf("🌿 [%s](%s/tree/%s)", branch, repoUrl, branch)
		}

		if len(e.Commits) > 0 {
			for i, c := range e.Commits {
				emoji := "🔹"
				if i%2 != 0 {
					emoji = "🔸"
				}
				msg := strings.Split(c.GetMessage(), "\n")[0]
				lines = append(lines, fmt.Sprintf("%s [%s](%s) %s", emoji, c.GetID()[:7], c.GetURL(), msg))
			}
		} else if e.GetDeleted() {
			d.Title = "🗑️ 引用已删除"
			lines = append(lines, "🗑️ **删除了此引用**")
		} else if e.GetCreated() {
			d.Title = "🆕 引用已创建"
			lines = append(lines, "🆕 **创建了新引用**")
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
		case "reopened":
			actionZh = "已重新开启"
		case "synchronize":
			actionZh = "已同步更改"
		}
		d.Title = fmt.Sprintf("%s 合并请求 %s", icon, actionZh)
		body := truncate(pr.GetBody())
		if body != "" {
			body = fmt.Sprintf("\n> %s", body)
		}
		stateZh := pr.GetState()
		if stateZh == "open" {
			stateZh = "开启中"
		} else if stateZh == "closed" {
			stateZh = "已关闭"
		}

		d.Text = fmt.Sprintf("**标题**: %s\n**状态**: %s%s", pr.GetTitle(), stateZh, body)
		d.Ref = fmt.Sprintf("🌿 [%s -> %s](%s)", pr.GetHead().GetRef(), pr.GetBase().GetRef(), pr.GetHTMLURL())
		d.URL = pr.GetHTMLURL()

	case *github.IssuesEvent:
		action := e.GetAction()
		icon, actionZh := "🐛", action
		if action == "closed" {
			icon, actionZh = "✅", "已关闭"
		}
		d.Title = fmt.Sprintf("%s 问题 %s", icon, actionZh)
		iss := e.GetIssue()
		body := truncate(iss.GetBody())
		if body != "" {
			body = fmt.Sprintf("\n> %s", body)
		}
		stateZh := iss.GetState()
		if stateZh == "open" {
			stateZh = "开启中"
		} else if stateZh == "closed" {
			stateZh = "已关闭"
		}
		d.Text = fmt.Sprintf("**标题**: %s\n**状态**: %s%s", iss.GetTitle(), stateZh, body)
		d.URL = iss.GetHTMLURL()

	case *github.WorkflowRunEvent:
		wr := e.GetWorkflowRun()
		status := wr.GetStatus()
		conclusion := wr.GetConclusion()

		icon := "🚀"
		if conclusion == "success" {
			icon = "✅"
		} else if conclusion == "failure" {
			icon = "💥"
		} else if status == "in_progress" {
			icon = "🔄"
		}

		actionZh := e.GetAction()
		if actionZh == "completed" {
			actionZh = "已完成"
		} else if actionZh == "requested" {
			actionZh = "已请求"
		} else if actionZh == "in_progress" {
			actionZh = "进行中"
			d.Skip = true // 不推送进行中的状态
		}

		d.Title = fmt.Sprintf("%s 工作流 %s", icon, actionZh)
		conclusionStr := ""
		if conclusion != "" {
			conclusionZh := conclusion
			switch conclusion {
			case "success":
				conclusionZh = "成功"
			case "failure":
				conclusionZh = "失败"
			case "cancelled":
				conclusionZh = "已取消"
			case "timed_out":
				conclusionZh = "超时"
			case "skipped":
				conclusionZh = "已跳过"
			}
			conclusionStr = fmt.Sprintf("\n**结论**: %s", conclusionZh)
		}

		statusZh := status
		switch status {
		case "queued":
			statusZh = "排队中"
		case "in_progress":
			statusZh = "进行中"
		case "completed":
			statusZh = "已完成"
		}

		d.Text = fmt.Sprintf("**工作流**: %s | **状态**: %s%s", wr.GetName(), statusZh, conclusionStr)
		d.Ref = fmt.Sprintf("🌿 [%s](%s/tree/%s)", wr.GetHeadBranch(), e.GetRepo().GetHTMLURL(), wr.GetHeadBranch())
		d.URL = wr.GetHTMLURL()

	case *github.IssueCommentEvent:
		actionZh := "已评论"
		if e.GetAction() != "created" {
			actionZh = e.GetAction()
		}
		d.Title = fmt.Sprintf("💬 问题 %s", actionZh)
		iss := e.GetIssue()
		d.Text = fmt.Sprintf("**问题**: [%s](%s)\n> %s", iss.GetTitle(), iss.GetHTMLURL(), truncate(e.GetComment().GetBody()))
		d.URL = e.GetComment().GetHTMLURL()

	case *github.WorkflowJobEvent:
		wj := e.GetWorkflowJob()
		status := wj.GetStatus()
		conclusion := wj.GetConclusion()

		icon := "⚙️"
		if conclusion == "success" {
			icon = "🟢"
		} else if conclusion == "failure" {
			icon = "🔴"
		} else if status == "in_progress" {
			icon = "🔄"
		}

		actionZh := e.GetAction()
		if actionZh == "completed" {
			actionZh = "已完成"
		} else if actionZh == "queued" {
			actionZh = "已排队"
		} else if actionZh == "in_progress" {
			actionZh = "进行中"
			d.Skip = true // 不推送进行中的状态
		}

		d.Title = fmt.Sprintf("%s 作业 %s", icon, actionZh)
		conclusionStr := ""
		if conclusion != "" {
			conclusionZh := conclusion
			switch conclusion {
			case "success":
				conclusionZh = "成功"
			case "failure":
				conclusionZh = "失败"
			case "cancelled":
				conclusionZh = "已取消"
			}
			conclusionStr = fmt.Sprintf("\n**结论**: %s", conclusionZh)
		}

		statusZh := status
		switch status {
		case "queued":
			statusZh = "排队中"
		case "in_progress":
			statusZh = "进行中"
		case "completed":
			statusZh = "已完成"
		}

		d.Text = fmt.Sprintf("**作业**: %s | **状态**: %s%s | **步骤**: %d", wj.GetName(), statusZh, conclusionStr, len(wj.Steps))
		d.URL = wj.GetHTMLURL()

	case *github.ReleaseEvent:
		actionZh := "已发布"
		if e.GetAction() != "published" {
			actionZh = e.GetAction()
		}
		d.Title = fmt.Sprintf("📦 版本发布 %s", actionZh)
		r := e.GetRelease()
		body := truncate(r.GetBody())
		if body != "" {
			body = fmt.Sprintf("\n> %s", body)
		}
		d.Text = fmt.Sprintf("**标签**: %s\n**名称**: %s%s", r.GetTagName(), r.GetName(), body)
		d.URL = r.GetHTMLURL()

	case *github.CreateEvent:
		d.Skip = true // 与 PushEvent 重复，跳过

	case *github.DeleteEvent:
		d.Skip = true // 与 PushEvent 重复，跳过

	case *github.StarEvent:
		d.Title = "⭐ 仓库收到了 Star"
		if e.GetAction() == "deleted" {
			d.Title = "💔 仓库被取消了 Star"
		}

	case *github.ForkEvent:
		d.Title = "🍴 仓库被 Fork"
		f := e.GetForkee()
		d.Text = fmt.Sprintf("Fork 到 **[%s](%s)**", f.GetFullName(), f.GetHTMLURL())
		d.URL = f.GetHTMLURL()

	case *github.DiscussionEvent:
		actionZh := "已发起"
		if e.GetAction() != "created" {
			actionZh = e.GetAction()
		}
		d.Title = fmt.Sprintf("📢 讨论 %s", actionZh)
		disc := e.GetDiscussion()
		d.Text = fmt.Sprintf("**标题**: %s\n> %s", disc.GetTitle(), truncate(disc.GetBody()))
		d.URL = disc.GetHTMLURL()

	case *github.MemberEvent:
		actionZh := "已添加"
		if e.GetAction() != "added" {
			actionZh = e.GetAction()
		}
		member := e.GetMember()
		d.Title = fmt.Sprintf("👥 成员更新 %s", actionZh)
		d.Text = fmt.Sprintf("**用户**: [%s](%s)", member.GetLogin(), member.GetHTMLURL())

	default:
		// 其他事件提取动作
		var m map[string]any
		_ = json.Unmarshal(payload, &m)
		if act, ok := m["action"].(string); ok {
			d.Text = fmt.Sprintf("**动作**: %s (%s)", act, eventType)
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
		Config: &CardConfig{
			WideScreenMode: true,
			EnableForward:  true,
		},
	}

	// 紧凑的元数据展示
	fields := []CardField{
		{
			IsShort: true,
			Text: &Text{
				Tag:     "lark_md",
				Content: fmt.Sprintf("**📦 仓库**\n[%s](%s)", repo, repoUrl),
			},
		},
		{
			IsShort: true,
			Text: &Text{
				Tag:     "lark_md",
				Content: fmt.Sprintf("**👤 触发者**\n[%s](%s)", sender, senderUrl),
			},
		},
	}

	if detail.Ref != "" {
		fields = append(fields, CardField{
			IsShort: true,
			Text: &Text{
				Tag:     "lark_md",
				Content: fmt.Sprintf("**🌿 引用**\n%s", detail.Ref),
			},
		})
	}

	card.AddDiv("", fields)

	if detail.Text != "" {
		// 如果是工作流或作业，直接在上方展示摘要，不再单独用分割线隔离文本，除非文本内容较多
		if strings.Contains(detail.Title, "工作流") || strings.Contains(detail.Title, "作业") {
			card.AddMarkdown(detail.Text)
		} else {
			card.AddDivider()
			card.AddMarkdown(detail.Text)
		}
	}

	if detail.URL != "" {
		card.AddAction(Button{
			Tag:  "button",
			Text: Text{Tag: "plain_text", Content: "🔗 查看详情"},
			Url:  detail.URL,
			Type: "primary",
		})
	}

	// 增加备注页脚
	card.AddNote(fmt.Sprintf("🕒 %s | 来自 FeishuGitPushBot", time.Now().Format("15:04:05")))

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
	r := []rune(s)
	if len(r) > 100 {
		return string(r[:100]) + "..."
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
