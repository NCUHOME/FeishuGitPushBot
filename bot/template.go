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
	AuthorAvatars []string `json:"author_avatars"` // 提交者或协作者的头像 URL 列表
	Action       string `json:"action"` // 事件具体动作
	ExtraReply    string   `json:"extra_reply"`    // 需要另起一段话题回复的内容
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
			avatarMap := make(map[string]string)
			for _, c := range e.Commits {
				login := c.GetAuthor().GetLogin()
				if login == "" {
					login = c.GetCommitter().GetLogin()
				}
				if login != "" {
					authors[login] = true
					avatarMap[login] = fmt.Sprintf("https://github.com/%s.png", login)
				}
				// 检查 Co-authored-by
				for _, coAuthor := range parseCoAuthors(c.GetMessage()) {
					authors[coAuthor.Login] = true
					if coAuthor.Login != "" {
						avatarMap[coAuthor.Login] = fmt.Sprintf("https://github.com/%s.png", coAuthor.Login)
					}
				}
			}
			multiAuthor := len(authors) > 1

			// 收集所有头像
			for _, url := range avatarMap {
				d.AuthorAvatars = append(d.AuthorAvatars, url)
			}

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
					name := c.GetAuthor().GetName()
					if name == "" {
						name = login
					}
					
					authorList := []string{}
					if name != "" {
						if login != "" {
							authorList = append(authorList, fmt.Sprintf("[%s](https://github.com/%s)", name, login))
						} else {
							authorList = append(authorList, name)
						}
					}
					
					// 添加 Co-authors
					coAuthors := parseCoAuthors(c.GetMessage())
					for _, ca := range coAuthors {
						if ca.Login != "" {
							authorList = append(authorList, fmt.Sprintf("[%s](https://github.com/%s)", ca.Name, ca.Login))
						} else {
							authorList = append(authorList, ca.Name)
						}
					}
					
					if len(authorList) > 0 {
						authorPart = fmt.Sprintf(" (%s)", strings.Join(authorList, ", "))
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
		d.Action = "push"

	case *github.PullRequestEvent:
		pr := e.GetPullRequest()
		action := e.GetAction()
		d.Action = action
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
		case "labeled":
			d.Title = "🏷️ PR Labeled"
		case "unlabeled":
			d.Title = "🏷️ PR Unlabeled"
		default:
			d.Title = fmt.Sprintf("📦 PR %s", action)
		}

		if action == "labeled" || action == "unlabeled" {
			label := e.GetLabel().GetName()
			d.Text = fmt.Sprintf("**%s**\n\nLabel: `%s`", pr.GetTitle(), label)
		} else {
			text, foldable := ProcessGithubMarkdown(pr.GetBody())
			// 如果内容过长 (比如超过 800 字)，则放入 ExtraReply
			if len(text) > 800 {
				d.Text = fmt.Sprintf("**%s**\n*(Content too long, see reply)*", pr.GetTitle())
				d.ExtraReply = text
			} else if text != "" {
				d.Text = fmt.Sprintf("**%s**\n%s", pr.GetTitle(), text)
			} else {
				d.Text = fmt.Sprintf("**%s**", pr.GetTitle())
			}
			d.FoldableBody = foldable
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
		d.Action = action

	case *github.IssueCommentEvent:
		iss := e.GetIssue()
		action := e.GetAction()
		d.Title = fmt.Sprintf("🌻 Comment %s", action)
		d.Action = action

		body := e.GetComment().GetBody()
		if action == "edited" && e.Changes != nil && e.Changes.Body != nil && e.Changes.Body.From != nil {
			body = GetDiffOnlyAdded(*e.Changes.Body.From, body)
		}

		commentBody := SafeText(strings.TrimSpace(body), 1500)
		if len(commentBody) > 1000 {
			d.Text = fmt.Sprintf("**%s**\n*(Comment too long, see reply)*", iss.GetTitle())
			d.ExtraReply = commentBody
		} else if commentBody != "" {
			d.Text = fmt.Sprintf("**%s**\n\n**Content**\n%s", iss.GetTitle(), commentBody)
		} else {
			d.Text = fmt.Sprintf("**%s**", iss.GetTitle())
		}
		d.URL = e.GetComment().GetHTMLURL()

	case *github.PullRequestReviewCommentEvent:
		pr := e.GetPullRequest()
		action := e.GetAction()
		d.Title = fmt.Sprintf("💬 PR Comment %s", action)
		d.Action = action

		body := e.GetComment().GetBody()
		if action == "edited" && e.Changes != nil && e.Changes.Body != nil && e.Changes.Body.From != nil {
			body = GetDiffOnlyAdded(*e.Changes.Body.From, body)
		}

		commentBody := SafeText(strings.TrimSpace(body), 1500)
		if commentBody != "" {
			d.Text = fmt.Sprintf("**%s**\n\n**Content**\n%s", pr.GetTitle(), commentBody)
		} else {
			d.Text = fmt.Sprintf("**%s**", pr.GetTitle())
		}
		d.URL = e.GetComment().GetHTMLURL()

	case *github.PullRequestReviewEvent:
		pr := e.GetPullRequest()
		action := e.GetAction()
		d.Title = fmt.Sprintf("🧐 PR Review %s", action)
		d.Action = action

		body := e.GetReview().GetBody()
		// PullRequestReviewEvent 在 go-github 中目前没有 Changes 字段
		reviewBody := SafeText(strings.TrimSpace(body), 1500)
		if reviewBody != "" {
			d.Text = fmt.Sprintf("**%s**\n\n**Review**\n%s", pr.GetTitle(), reviewBody)
		} else {
			d.Text = fmt.Sprintf("**%s**", pr.GetTitle())
		}
		d.URL = e.GetReview().GetHTMLURL()

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
		durationPart := ""
		if conclusion != "" {
			start := wr.GetRunStartedAt().Time
			end := wr.GetUpdatedAt().Time
			if !start.IsZero() && !end.IsZero() {
				durationPart = fmt.Sprintf(" in %s", FormatDuration(end.Sub(start)))
			}
		}
		lines = append(lines, fmt.Sprintf("%s **%s** workflow run %s%s", icon, workflowName, stateVerb, durationPart))

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

	case *github.PublicEvent:
		d.Title = "🔓 Repository Made Public"
		d.Text = "This repository is now visible to everyone."

	case *github.RepositoryEvent:
		action := e.GetAction()
		switch action {
		case "publicized":
			// GitHub 同时会发送 public 事件，这里直接跳过以防重复
			d.Skip = true
		case "privatized":
			d.Title = "🔒 Repository Made Private"
		case "deleted":
			d.Title = "🗑️ Repository Deleted"
		case "renamed":
			d.Title = "📝 Repository Renamed"
			d.Text = fmt.Sprintf("Renamed to **%s**", e.GetRepo().GetFullName())
		default:
			// 其他 edited 事件（如修改描述、Logo 等）通常比较琐碎，默认跳过
			d.Skip = true
		}
		d.Action = action

	case *github.OrganizationEvent:
		d.Title = fmt.Sprintf("🏢 Org %s: %s", e.GetOrganization().GetLogin(), e.GetAction())
		d.Text = fmt.Sprintf("Action: **%s**\nMember: **%s**", e.GetAction(), e.GetMembership().GetUser().GetLogin())
		d.Action = e.GetAction()
		d.URL = e.GetOrganization().GetHTMLURL()

	case *github.MembershipEvent:
		d.Title = fmt.Sprintf("👥 Membership %s", e.GetAction())
		d.Text = fmt.Sprintf("Action: **%s**\nMember: **%s**\nScope: **%s**", e.GetAction(), e.GetMember().GetLogin(), e.GetScope())
		d.Action = e.GetAction()
	}
	return d
}

// BuildCard 构建现代化、高可读性的飞书卡片 (V2)
func BuildCard(ctx context.Context, repo, repoUrl, sender, senderUrl, avatarUrl string, detail EventDetail) *Card {
	card := NewCard()
	card.Header.Title = Text{Tag: "plain_text", Content: detail.Title}
	card.Header.Template = GetTemplate(detail.Title)


	// --- 1. 摘要信息 (仓库 / 分支 / [头像] 提交人) ---
	repoPart := ""
	if repo != "" {
		repoPart = fmt.Sprintf("📦 [%s](%s)", repo, repoUrl)
	}

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
			refPart = fmt.Sprintf("🏷️ **Tag** [%s](%s)%s", detail.RefName, link, shaPart)
		} else {
			refPart = fmt.Sprintf("🌿 **Branch** [%s](%s)%s", detail.RefName, link, shaPart)
		}
	}

	var metaParts []string
	if repoPart != "" {
		metaParts = append(metaParts, repoPart)
	}
	if refPart != "" {
		metaParts = append(metaParts, refPart)
	}
	metaText := strings.Join(metaParts, " / ")
	if metaText != "" {
		metaText += " / "
	}

	senderText := fmt.Sprintf("[%s](%s)", sender, senderUrl)

	var avatarElements []map[string]any
	// 如果有多个作者头像，优先显示它们
	avatarsToDisplay := detail.AuthorAvatars
	if len(avatarsToDisplay) == 0 && avatarUrl != "" {
		avatarsToDisplay = []string{avatarUrl}
	}

	// 最多显示 3 个头像，防止占用过宽
	maxAvatars := 3
	for i, url := range avatarsToDisplay {
		if i >= maxAvatars {
			break
		}
		key := GetImageKey(ctx, url)
		if key != "" {
			avatarElements = append(avatarElements, map[string]any{
				"tag":          "img",
				"img_key":      key,
				"custom_width": 24,
				"mode":         "crop_center",
				"alt": map[string]string{
					"tag":     "plain_text",
					"content": "avatar",
				},
			})
		}
	}

	if len(avatarElements) > 0 {
		columns := []map[string]any{
			{
				"tag":            "column",
				"width":          "auto",
				"vertical_align": "center",
				"elements": []map[string]any{
					{
						"tag":     "markdown",
						"content": metaText,
					},
				},
			},
		}

		// 为每个头像增加一个列
		for _, el := range avatarElements {
			columns = append(columns, map[string]any{
				"tag":            "column",
				"width":          "auto",
				"vertical_align": "center",
				"elements":       []map[string]any{el},
			})
		}

		columns = append(columns, map[string]any{
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
		})

		card.Body.Elements = append(card.Body.Elements, map[string]any{
			"tag":                "column_set",
			"flex_mode":          "none",
			"horizontal_spacing": "small",
			"columns":            columns,
		})
	} else {
		card.AddMarkdown(metaText + "👤 " + senderText)
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

// ProcessGithubMarkdown 转换 GitHub Markdown 为飞书卡片 Markdown，并提取折叠内容
func ProcessGithubMarkdown(s string) (text string, foldable string) {
	if s == "" {
		return "", ""
	}

	// 1. 预处理 Mermaid
	s = strings.ReplaceAll(s, "```mermaid", "```")

	// 2. 提取 <details> <summary> 内容作为 FoldableBody
	// 目前飞书 Markdown 不支持 details/summary，我们将其提取到 FoldableBody 中
	reDetails := regexp.MustCompile(`(?s)<details>\s*<summary>(.*?)</summary>(.*?)</details>`)
	var foldables []string
	
	// 提取并替换
	processed := reDetails.ReplaceAllStringFunc(s, func(m string) string {
		match := reDetails.FindStringSubmatch(m)
		if len(match) > 2 {
			title := strings.TrimSpace(match[1])
			// 移除 HTML 标签，只保留纯文本作为标题
			title = regexp.MustCompile(`<.*?>`).ReplaceAllString(title, "")
			content := strings.TrimSpace(match[2])
			foldables = append(foldables, fmt.Sprintf("**%s**\n%s", title, content))
		}
		return "" // 将其从主文档中移除，放入折叠面板
	})

	// 3. 简单的 Markdown 转换 (如处理一些 GitHub 特有的格式)
	processed = strings.TrimSpace(processed)
	
	// 4. 安全阶段 (截断长度，转义 < >)
	text = SafeText(processed, 2000)
	foldable = SafeText(strings.Join(foldables, "\n\n"), 3000)

	return text, foldable
}

// GetDiffOnlyAdded 生成仅包含新增内容的 Diff
func GetDiffOnlyAdded(old, new string) string {
	if old == "" {
		return new
	}

	oldLines := strings.Split(old, "\n")
	oldMap := make(map[string]bool)
	for _, l := range oldLines {
		oldMap[l] = true
	}

	newLines := strings.Split(new, "\n")
	var diff []string
	for _, l := range newLines {
		if !oldMap[l] {
			diff = append(diff, "+ "+l)
		}
	}

	if len(diff) == 0 {
		return ""
	}
	return strings.Join(diff, "\n")
}

var coAuthorRegex = regexp.MustCompile(`(?im)^Co-authored-by:\s*(.+?)\s*<(.+?)>`)

type AuthorInfo struct {
	Name  string
	Login string
}

// parseCoAuthors 解析提交信息中的共同作者
func parseCoAuthors(msg string) []AuthorInfo {
	matches := coAuthorRegex.FindAllStringSubmatch(msg, -1)
	var authors []AuthorInfo
	for _, m := range matches {
		if len(m) > 2 {
			name := strings.TrimSpace(m[1])
			email := strings.TrimSpace(m[2])
			login := ""
			// 尝试从邮箱提取用户名 (如果是 GitHub 自动生成的 noreply 邮箱)
			if strings.Contains(email, "@users.noreply.github.com") {
				parts := strings.Split(email, "@")
				if len(parts) > 0 {
					loginParts := strings.Split(parts[0], "+")
					login = loginParts[len(loginParts)-1]
				}
			}
			// 如果提取不到，且名字不含空格，尝试把名字当作 login
			if login == "" && !strings.Contains(name, " ") {
				login = name
			}
			authors = append(authors, AuthorInfo{Name: name, Login: login})
		}
	}
	return authors
}
