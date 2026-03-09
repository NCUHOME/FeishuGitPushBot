package bot

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/go-github/v84/github"
	"github.com/kyokomi/emoji/v2"
)

type EventDetail struct {
	Title        string `json:"title"`
	Text         string `json:"text"`
	URL          string `json:"url"`
	Ref          string `json:"ref"`      // 原始 Ref markdown
	RefName      string `json:"ref_name"` // 纯文本引用名 (如 main)
	RefURL       string `json:"ref_url"`  // 引用的 URL
	ReplyToTitle string `json:"reply_to_title"`
	FoldableBody string `json:"foldable_body"`
	Skip         bool   `json:"skip"`
}

// ParseEvent 解析 GitHub 事件为极简明了的 Detail
func ParseEvent(event any, eventType string) EventDetail {
	d := EventDetail{Title: fmt.Sprintf("🔔 GitHub Event: %s", eventType)}

	// 屏蔽无实质内容的冗余事件类型
	if eventType == "create" || eventType == "delete" || eventType == "member" {
		d.Skip = true
		return d
	}

	switch e := event.(type) {
	case *github.PushEvent:
		repo := e.GetRepo().GetFullName()
		ref := e.GetRef()
		isTag := strings.HasPrefix(ref, "refs/tags/")
		refShort := ""
		if isTag {
			refShort = strings.TrimPrefix(ref, "refs/tags/")
		} else {
			refShort = strings.TrimPrefix(ref, "refs/heads/")
		}
		repoUrl := e.GetRepo().GetHTMLURL()

		if isTag {
			d.Title = fmt.Sprintf("🏷️ New Tag: %s/%s", repo, refShort)
			d.RefName = refShort
			d.RefURL = fmt.Sprintf("%s/releases/tag/%s", repoUrl, refShort)
			d.URL = d.RefURL
		} else if strings.HasPrefix(ref, "refs/heads/") {
			d.Title = "🍏 New commits"
			d.RefName = refShort
			d.RefURL = fmt.Sprintf("%s/tree/%s", repoUrl, refShort)
		}

		if len(e.Commits) > 0 {
			authors := make(map[string]bool)
			for _, c := range e.Commits {
				login := c.GetAuthor().GetLogin()
				if login == "" {
					login = c.GetCommitter().GetLogin()
				}
				if login != "" {
					authors[login] = true
				}
			}
			multiAuthor := len(authors) > 1

			var lines []string
			for i, c := range e.Commits {
				emojiIcon := "🔸"
				if i%2 != 0 {
					emojiIcon = "🔹"
				}

				msg := ProcessCommitMessage(c.GetMessage())
				msg = SafeText(msg, 400)

				shortSHA := ""
				if sha := c.GetID(); sha != "" {
					if len(sha) > 7 {
						shortSHA = sha[:7]
					} else {
						shortSHA = sha
					}
				}

				hashPart := ""
				if shortSHA != "" && c.GetURL() != "" {
					hashPart = fmt.Sprintf(" ([%s](%s))", shortSHA, c.GetURL())
				}

				authorPart := ""
				if multiAuthor {
					login := c.GetAuthor().GetLogin()
					if login == "" {
						login = c.GetCommitter().GetLogin()
					}
					name := c.GetAuthor().GetName()
					if name == "" {
						name = login
					}
					if login != "" {
						authorPart = fmt.Sprintf(" ([%s](https://github.com/%s))", name, login)
					} else if name != "" {
						authorPart = fmt.Sprintf(" (%s)", name)
					}
				}

				lines = append(lines, fmt.Sprintf("%s %s%s%s", emojiIcon, msg, hashPart, authorPart))
			}
			d.Text = strings.Join(lines, "\n")
		} else if e.GetDeleted() {
			d.Title = fmt.Sprintf("🗑️ Deleted: %s/%s", repo, refShort)
			d.Text = ""
		} else if e.GetCreated() {
			d.Title = fmt.Sprintf("🆕 Created: %s/%s", repo, refShort)
			d.Text = ""
		}
		if hc := e.GetHeadCommit(); hc != nil {
			d.URL = hc.GetURL()
		}

	case *github.PullRequestEvent:
		pr := e.GetPullRequest()
		action := e.GetAction()
		switch action {
		case "opened":
			d.Title = "🥕 New PullRequest"
		case "closed":
			if pr.GetMerged() {
				d.Title = "🥕 PullRequest merged"
			} else {
				d.Title = "🥕 PullRequest closed"
			}
		case "reopened":
			d.Title = "🥕 PullRequest reopened"
		default:
			d.Title = fmt.Sprintf("📦 PR %s", action)
		}

		body := SafeText(strings.TrimSpace(pr.GetBody()), 1500)
		if body != "" {
			d.Text = fmt.Sprintf("**%s**\n%s", pr.GetTitle(), body)
		} else {
			d.Text = fmt.Sprintf("**%s**", pr.GetTitle())
		}
		d.RefName = fmt.Sprintf("%s ➔ %s", pr.GetHead().GetRef(), pr.GetBase().GetRef())
		d.URL = pr.GetHTMLURL()

	case *github.IssuesEvent:
		action := e.GetAction()
		iss := e.GetIssue()
		switch action {
		case "opened":
			d.Title = "🍄 New Issue"
		case "edited":
			d.Title = "🍄 Issue edited"
		case "closed":
			d.Title = "🍄 Issue closed"
		default:
			d.Title = fmt.Sprintf("🍄 Issue %s", action)
		}
		body := SafeText(strings.TrimSpace(iss.GetBody()), 1500)
		if body != "" {
			d.Text = fmt.Sprintf("**%s**\n%s", iss.GetTitle(), body)
		} else {
			d.Text = fmt.Sprintf("**%s**", iss.GetTitle())
		}
		d.URL = iss.GetHTMLURL()

	case *github.IssueCommentEvent:
		iss := e.GetIssue()
		d.Title = fmt.Sprintf("🌻 Comment %s", e.GetAction())
		commentBody := SafeText(strings.TrimSpace(e.GetComment().GetBody()), 1500)
		if commentBody != "" {
			d.Text = fmt.Sprintf("**%s**\n\n**内容**\n%s", iss.GetTitle(), commentBody)
		} else {
			d.Text = fmt.Sprintf("**%s**", iss.GetTitle())
		}
		d.URL = e.GetComment().GetHTMLURL()

	case *github.WorkflowRunEvent:
		wr := e.GetWorkflowRun()
		status := wr.GetStatus()
		conclusion := wr.GetConclusion()

		icon := "⚙️"
		if conclusion == "success" {
			icon = "✅"
		} else if conclusion == "failure" || conclusion == "cancelled" || conclusion == "timed_out" {
			icon = "❌"
		} else if status == "in_progress" {
			icon = "⏳"
		}

		d.Title = fmt.Sprintf("%s Workflow: %s", icon, wr.GetName())
		stateStr := status
		if conclusion != "" {
			stateStr = conclusion
		}
		d.Text = fmt.Sprintf("Status: **%s**", stateStr)
		d.RefName = wr.GetHeadBranch()
		d.URL = wr.GetHTMLURL()

	case *github.WorkflowJobEvent:
		wj := e.GetWorkflowJob()
		conclusion := wj.GetConclusion()
		icon := "⚙️" // 统一使用 Workflow 的图标感
		if conclusion == "success" {
			icon = "✅"
		} else if conclusion == "failure" || conclusion == "cancelled" || conclusion == "timed_out" {
			icon = "❌"
		}

		// 如果有 WorkflowName 则使用，否则回退
		name := wj.GetName()
		d.Title = fmt.Sprintf("%s Workflow: %s", icon, name)
		d.Text = fmt.Sprintf("Job: **%s** (%s)", name, wj.GetStatus())
		d.URL = wj.GetHTMLURL()

	case *github.WatchEvent:
		d.Title = "⭐ New Star!"
		d.Text = "Your repository has a new stargazer."

	case *github.StarEvent:
		action := e.GetAction()
		if action == "deleted" {
			d.Title = "💔 Star Removed"
		} else {
			d.Title = "⭐ New Star!"
		}
		d.Text = ""

	case *github.ForkEvent:
		forkee := e.GetForkee()
		d.Title = "🍴 Repository Forked"
		d.Text = fmt.Sprintf("仓库被复刻到 [%s](%s)", forkee.GetFullName(), forkee.GetHTMLURL())

	case *github.GollumEvent:
		d.Title = "📖 Wiki Updated"
		var pages []string
		for _, p := range e.Pages {
			pages = append(pages, fmt.Sprintf("• [%s](%s) (%s)", p.GetTitle(), p.GetHTMLURL(), p.GetAction()))
		}
		d.Text = strings.Join(pages, "\n")
	}
	return d
}

// BuildCard 构建现代化、高可读性的飞书卡片 (V2)
func BuildCard(ctx context.Context, repo, repoUrl, sender, senderUrl, avatarUrl string, detail EventDetail) *Card {
	card := NewCard()
	card.Header.Title = Text{Tag: "plain_text", Content: detail.Title}
	card.Header.Template = GetTemplate(detail.Title)

	// --- 0. 回复上下文 (如果存在) ---
	if detail.ReplyToTitle != "" {
		card.AddNoteText(fmt.Sprintf("| Reply to %s", detail.ReplyToTitle))
		card.AddDivider()
	}

	// --- 1. 摘要信息 (仓库 / 分支 / [头像] 提交人) ---
	repoPart := fmt.Sprintf("📦 [%s](%s)", repo, repoUrl)
	refPart := ""
	if detail.RefName != "" {
		link := detail.RefURL
		if link == "" {
			link = repoUrl
		}
		refPart = fmt.Sprintf(" / 🌿 [%s](%s)", detail.RefName, link)
	}
	repoAndBranchText := repoPart + refPart + " / "
	senderText := fmt.Sprintf("[%s](%s)", sender, senderUrl)

	imgKey := ""
	if avatarUrl != "" {
		imgKey = GetImageKey(ctx, avatarUrl)
	}

	if imgKey != "" {
		card.Body.Elements = append(card.Body.Elements, map[string]any{
			"tag":                "column_set",
			"flex_mode":          "none",
			"horizontal_spacing": "small",
			"columns": []map[string]any{
				{
					"tag":            "column",
					"width":          "auto",
					"vertical_align": "center",
					"elements": []map[string]any{
						{
							"tag":     "markdown",
							"content": repoAndBranchText,
						},
					},
				},
				{
					"tag":            "column",
					"width":          "auto",
					"vertical_align": "center",
					"elements": []map[string]any{
						{
							"tag":          "img",
							"img_key":      imgKey,
							"custom_width": 24, // 头像更精简一点
							"mode":         "crop_center",
							"alt": map[string]string{
								"tag":     "plain_text",
								"content": "avatar",
							},
						},
					},
				},
				{
					"tag":            "column",
					"width":          "weight",
					"weight":         1,
					"vertical_align": "center",
					"elements": []map[string]any{
						{
							"tag":     "markdown",
							"content": senderText,
						},
					},
				},
			},
		})
	} else {
		card.AddMarkdown(repoAndBranchText + "👤 " + senderText)
	}

	// --- 2. 详情内容 ---
	if detail.Text != "" {
		card.AddDivider()
		card.AddMarkdown(detail.Text)
	}

	if detail.FoldableBody != "" {
		card.AddCollapsiblePanel(detail.FoldableBody)
	}

	// --- 3. 动态操作按钮 ---
	// 只有非 Push 事件才显示详情按钮 (Commit 不显示)
	if detail.URL != "" && !strings.Contains(detail.Title, "commits") && !strings.Contains(detail.Title, "Created:") && !strings.Contains(detail.Title, "Deleted:") && !strings.Contains(detail.Title, "Tag:") {
		btnText := "查看"
		btnType := "primary"

		if strings.Contains(detail.Title, "Workflow") {
			if strings.Contains(detail.Title, "❌") || strings.Contains(detail.Title, "💥") {
				btnType = "danger"
			}
		}

		card.AddAction(Button{
			Tag:  "button",
			Text: Text{Tag: "plain_text", Content: btnText},
			Url:  detail.URL,
			Type: btnType,
		})

		// 对于失败的任务，可以额外增加一个链接
		if btnType == "danger" && detail.RefURL != "" {
			card.AddAction(Button{
				Tag:  "button",
				Text: Text{Tag: "plain_text", Content: "查看分支"},
				Url:  detail.RefURL,
				Type: "default",
			})
		}
	}

	return card
}

func GetTemplate(title string) string {
	if ContainsAny(title, "❌", "💥", "💔", "🔴") {
		return "red"
	}
	if ContainsAny(title, "✅", "💜", "🟢") {
		return "green"
	}
	if ContainsAny(title, "⚠️", "🏃", "🟡") {
		return "orange"
	}
	return "blue"
}

func ContainsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// SafeText safely truncates a string to maxRunes, avoiding mid-UTF8-byte slicing,
// and replaces < and > with fullwidth variants to prevent Feishu internal markdown parser errors.
func SafeText(s string, maxRunes int) string {
	if s == "" {
		return ""
	}

	s = strings.ReplaceAll(s, "<", "＜")
	s = strings.ReplaceAll(s, ">", "＞")

	runes := []rune(s)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "..."
	}
	return s
}

var conventionalRegex = regexp.MustCompile(`^(?i)(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert|ref)(\([a-z0-9_-]+\))?(!?):`)

// ProcessCommitMessage converts emoji shortcodes and highlights conventional commit prefixes.
func ProcessCommitMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	// 1. Emoji conversion
	msg = emoji.Sprint(msg)
	// 2. Highlighting conventional commits
	if loc := conventionalRegex.FindStringIndex(msg); loc != nil {
		prefix := msg[loc[0]:loc[1]]
		msg = "**" + prefix + "**" + msg[loc[1]:]
	}
	return msg
}
