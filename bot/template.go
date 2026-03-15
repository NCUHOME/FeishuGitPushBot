package bot

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

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
	SHA          string `json:"sha"`
	IsTag        bool   `json:"is_tag"`
}

// ParseEvent 解析 GitHub 事件为极简明了的 Detail
func ParseEvent(event any, eventType string) EventDetail {
	d := EventDetail{
		Title: fmt.Sprintf("🔔 GitHub Event: %s", eventType),
		Skip:  false, // 默认不跳过任何事件
	}

	// 屏蔽已知无实质内容的冗余事件类型 (可选)
	if eventType == "member" {
		// d.Skip = true
	}

	switch e := event.(type) {
	case *github.PushEvent:
		ref := e.GetRef()
		// 更鲁棒的标签检测：检查 refs/tags/ 前缀或 ref 本身
		isTag := strings.HasPrefix(ref, "refs/tags/")
		refShort := ""
		if isTag {
			refShort = strings.TrimPrefix(ref, "refs/tags/")
			d.IsTag = true
		} else {
			refShort = strings.TrimPrefix(ref, "refs/heads/")
		}
		repoUrl := e.GetRepo().GetHTMLURL()

		if isTag {
			d.Title = fmt.Sprintf("🏷️ New Tag: %s", refShort)
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
			if isTag {
				d.Title = fmt.Sprintf("🗑️ Tag Deleted: %s", refShort)
			} else {
				d.Title = fmt.Sprintf("🗑️ Branch Deleted: %s", refShort)
			}
			d.Text = ""
		} else if e.GetCreated() {
			if isTag {
				d.Title = fmt.Sprintf("🏷️ New Tag: %s", refShort)
			} else {
				d.Title = fmt.Sprintf("🆕 New Branch: %s", refShort)
			}
			d.Text = ""
		}
		if hc := e.GetHeadCommit(); hc != nil {
			d.URL = hc.GetURL()
			sha := hc.GetID()
			if len(sha) > 7 {
				d.SHA = sha[:7]
			} else {
				d.SHA = sha
			}
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
			d.Text = fmt.Sprintf("**%s**\n\n**Content**\n%s", iss.GetTitle(), commentBody)
		} else {
			d.Text = fmt.Sprintf("**%s**", iss.GetTitle())
		}
		d.URL = e.GetComment().GetHTMLURL()

	case *github.WorkflowRunEvent:
		wr := e.GetWorkflowRun()
		status := wr.GetStatus()
		conclusion := wr.GetConclusion()
		workflowName := wr.GetName()
		ref := wr.GetHeadBranch()
		sha := wr.GetHeadSHA()
		shortSHA := sha
		if len(sha) > 7 {
			shortSHA = sha[:7]
		}

		icon := "⚙️"
		stateVerb := "started"
		switch conclusion {
		case "success":
			icon = "✅"
			stateVerb = "succeeded"
		case "failure", "cancelled", "timed_out":
			icon = "❌"
			if conclusion == "failure" {
				stateVerb = "failed"
			} else {
				stateVerb = conclusion
			}
		default:
			if status == "in_progress" {
				icon = "⏳"
				stateVerb = "running"
			}
		}

		d.SHA = shortSHA
		repoUrl := e.GetRepo().GetHTMLURL()
		if repoUrl != "" && ref != "" {
			d.RefURL = fmt.Sprintf("%s/tree/%s", repoUrl, ref)
		}
		d.Title = fmt.Sprintf("%s Workflow %s: %s", icon, strings.Title(stateVerb), workflowName)

		var lines []string
		commitPart := ""
		if sha != "" && repoUrl != "" {
			commitPart = fmt.Sprintf(" ([%s](%s/commit/%s))", shortSHA, repoUrl, sha)
		}

		durationPart := ""
		if conclusion != "" {
			start := wr.GetRunStartedAt().Time
			end := wr.GetUpdatedAt().Time
			if !start.IsZero() && !end.IsZero() {
				durationPart = fmt.Sprintf(" in %s", FormatDuration(end.Sub(start)))
			}
		}
		lines = append(lines, fmt.Sprintf("%s **%s** workflow run %s%s%s", icon, workflowName, stateVerb, durationPart, commitPart))

		d.Text = strings.Join(lines, "\n")
		d.RefName = ref
		d.URL = wr.GetHTMLURL()

	case *github.WorkflowJobEvent:
		wj := e.GetWorkflowJob()
		status := wj.GetStatus()
		conclusion := wj.GetConclusion()
		jobName := wj.GetName()
		shortSHA := wj.GetHeadSHA()
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		d.SHA = shortSHA

		icon := "⚙️"
		stateVerb := "started"
		switch conclusion {
		case "success":
			icon = "✅"
			stateVerb = "succeeded"
		case "failure", "cancelled", "timed_out":
			icon = "❌"
			stateVerb = conclusion
		default:
			if status == "in_progress" {
				icon = "⏳"
				stateVerb = "running"
			}
		}

		d.Title = fmt.Sprintf("%s Job %s: %s", icon, strings.Title(stateVerb), jobName)

		var lines []string
		// 如果有 workflow_name 则显示为 Workflow / Job 格式
		displayJobName := jobName
		if wj.GetWorkflowName() != "" {
			displayJobName = fmt.Sprintf("%s / %s", wj.GetWorkflowName(), jobName)
		}

		durationPart := ""
		if conclusion != "" {
			start := wj.GetStartedAt().Time
			end := wj.GetCompletedAt().Time
			if !start.IsZero() && !end.IsZero() {
				durationPart = fmt.Sprintf(" in %s", FormatDuration(end.Sub(start)))
			}
		}
		lines = append(lines, fmt.Sprintf("%s job **%s** %s%s", icon, displayJobName, stateVerb, durationPart))

		repoUrl := e.GetRepo().GetHTMLURL()
		sha := wj.GetHeadSHA()
		if sha != "" && repoUrl != "" {
			lines = append(lines, fmt.Sprintf("Commit: [%s](%s/commit/%s)", shortSHA, repoUrl, sha))
		}

		d.Text = strings.Join(lines, "\n")
		d.URL = wj.GetHTMLURL()

	case *github.WatchEvent:
		d.Title = "⭐ New Star!"
		d.Text = "Your repository has a new follower."

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
		d.Text = fmt.Sprintf("Repository forked to [%s](%s)", forkee.GetFullName(), forkee.GetHTMLURL())

	case *github.GollumEvent:
		d.Title = "📖 Wiki Updated"
		var pages []string
		for _, p := range e.Pages {
			pages = append(pages, fmt.Sprintf("• [%s](%s) (%s)", p.GetTitle(), p.GetHTMLURL(), p.GetAction()))
		}
		d.Text = strings.Join(pages, "\n")

	case *github.CreateEvent, *github.DeleteEvent:
		// 已经在 GitHub 后台关闭对应 Webhook，且 Push 事件已涵盖此类逻辑，此处统一跳过
		d.Skip = true
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
		shaPart := ""
		if detail.SHA != "" {
			shaPart = fmt.Sprintf(" ([%s](%s/commit/%s))", detail.SHA, repoUrl, detail.SHA)
		}

		if detail.IsTag {
			refPart = fmt.Sprintf(" / 🏷️ **Tag** [%s](%s)%s", detail.RefName, link, shaPart)
		} else {
			refPart = fmt.Sprintf(" / 🌿 **Branch** [%s](%s)%s", detail.RefName, link, shaPart)
		}
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
	if detail.URL != "" && !strings.Contains(detail.Title, "commits") && !strings.Contains(detail.Title, "Created:") && !strings.Contains(detail.Title, "Deleted:") {
		btnText := "View"
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
				Text: Text{Tag: "plain_text", Content: "View Branch"},
				Url:  detail.RefURL,
				Type: "default",
			})
		}
	}

	return card
}

// GetTemplate 根据标题中的 emoji 返回对应的卡片颜色模板
func GetTemplate(title string) string {
	if ContainsAny(title, "❌", "💥", "💔", "🔴") {
		return "red"
	}
	if ContainsAny(title, "✅", "💜", "🟢") {
		return "green"
	}
	if ContainsAny(title, "⚠️", "🏃", "🟡", "⏳") {
		return "orange"
	}
	return "blue"
}

// ContainsAny 检查字符串是否包含任意一个子串
func ContainsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// SafeText 安全地截断字符串到指定长度，避免 UTF8 字节切分问题，
// 并将 < 和 > 替换为全角字符，防止飞书内部 Markdown 解析错误。
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

var conventionalRegex = regexp.MustCompile(`(?i)(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert|ref)(\([^)]+\))?(!?):`)

// ProcessCommitMessage 处理提交信息，转换 emoji 并高亮 Conventional Commit 前缀
func ProcessCommitMessage(msg string) string {
	msg = strings.TrimSpace(msg)
	// 1. 转换 Emoji 短代码
	msg = emoji.Sprint(msg)

	// 2. 高亮 Conventional Commit 并处理换行
	matches := conventionalRegex.FindAllStringIndex(msg, -1)
	if len(matches) == 0 {
		return msg
	}

	var result strings.Builder
	last := 0
	for i, match := range matches {
		start, end := match[0], match[1]

		// 处理当前匹配之前的内容
		if start > last {
			part := msg[last:start]
			// 如果这个匹配不是在行首（即前面有非换行内容），且前面有内容，则注入换行实现“正确换行”
			if i > 0 && !strings.HasSuffix(result.String(), "\n") && strings.TrimSpace(part) != "" {
				result.WriteString(strings.TrimRight(part, " "))
				result.WriteString("\n")
			} else {
				result.WriteString(part)
			}
		} else if i > 0 {
			// 如果紧挨着上一个匹配，直接加换行
			if !strings.HasSuffix(result.String(), "\n") {
				result.WriteString("\n")
			}
		}

		// 加粗匹配的前缀
		result.WriteString("**")
		result.WriteString(msg[start:end])
		result.WriteString("**")
		last = end
	}
	result.WriteString(msg[last:])

	return result.String()
}

// FormatDuration 格式化耗时为人类可读格式 (Xh Ym Zs)
func FormatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	var parts []string
	if h > 0 {
		parts = append(parts, fmt.Sprintf("%d hour", h))
		if h > 1 {
			parts[len(parts)-1] += "s"
		}
	}
	if m > 0 {
		parts = append(parts, fmt.Sprintf("%d minute", m))
		if m > 1 {
			parts[len(parts)-1] += "s"
		}
	}
	if s > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%d second", s))
		if s > 1 {
			parts[len(parts)-1] += "s"
		}
	}
	return strings.Join(parts, " ")
}
