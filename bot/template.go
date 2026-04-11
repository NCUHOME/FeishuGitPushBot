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
	AuthorLogins  []string `json:"author_logins"`  // 提交者或协作者的 login 列表（与 AuthorAvatars 顺序对应）
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
					if coAuthor.Avatar != "" {
						key := coAuthor.Login
						if key == "" {
							key = coAuthor.Name
						}
						authors[key] = true
						avatarMap[key] = coAuthor.Avatar
					}
				}
			}
			multiAuthor := len(authors) > 1

			// 收集所有头像和 login（保持顺序一致）
			for login, url := range avatarMap {
				d.AuthorAvatars = append(d.AuthorAvatars, url)
				d.AuthorLogins = append(d.AuthorLogins, login)
			}

			var lines []string
			for i, c := range e.Commits {
				emojiIcon := "🔸"
				if i%2 != 0 {
					emojiIcon = "🔹"
				}

				msg := SafeText(c.GetMessage(), 400)
				msg = ProcessCommitMessage(msg)

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
		if headRepo := pr.GetHead().GetRepo(); headRepo != nil {
			d.RefURL = fmt.Sprintf("%s/tree/%s", headRepo.GetHTMLURL(), pr.GetHead().GetRef())
		}
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
			d.Text = fmt.Sprintf("**%s**\n%s", iss.GetTitle(), commentBody)
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
			d.Text = fmt.Sprintf("**%s**\n%s", pr.GetTitle(), commentBody)
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
			d.Text = fmt.Sprintf("**%s**\n%s", pr.GetTitle(), reviewBody)
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
		d.Title = fmt.Sprintf("%s Workflow %s: %s", icon, titleCase(stateVerb), workflowName)

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

		d.Title = fmt.Sprintf("%s Job %s: %s", icon, titleCase(stateVerb), jobName)

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
		d.RefName = wj.GetHeadBranch()
		if repoUrl := e.GetRepo().GetHTMLURL(); repoUrl != "" && wj.GetHeadBranch() != "" {
			d.RefURL = fmt.Sprintf("%s/tree/%s", repoUrl, wj.GetHeadBranch())
		}
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

// titleCase 将字符串首字母大写（替代已废弃的 strings.Title）
func titleCase(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	if runes[0] >= 'a' && runes[0] <= 'z' {
		runes[0] -= 32
	}
	return string(runes)
}

// cardColor 枚举卡片标题颜色，避免依赖标题 emoji 做字符串匹配
type cardColor string

const (
	cardColorBlue   cardColor = "blue"
	cardColorGreen  cardColor = "green"
	cardColorRed    cardColor = "red"
	cardColorOrange cardColor = "orange"
	cardColorGrey   cardColor = "grey"
	cardColorPurple cardColor = "purple"
)

// GetTemplate 根据标题中的 emoji 或关键字返回对应的飞书卡片标题色
// 支持颜色: blue / green / red / orange / grey / purple / indigo / wathet / turquoise / yellow / lime / pink / carmine
func GetTemplate(title string) string {
	if ContainsAny(title, "❌", "💥", "💔", "failed", "Failure", "Failed") {
		return string(cardColorRed)
	}
	if ContainsAny(title, "✅", "succeeded", "Success", "Succeeded") {
		return string(cardColorGreen)
	}
	if ContainsAny(title, "⏳", "🏃", "running", "Started", "Running") {
		return string(cardColorOrange)
	}
	if ContainsAny(title, "🏷️", "Tag", "New Tag") {
		return string(cardColorPurple)
	}
	if ContainsAny(title, "🆕", "New Branch", "New Commits", "commits") {
		return "wathet"
	}
	if ContainsAny(title, "🥕", "PullRequest", "PR") {
		return "indigo"
	}
	if ContainsAny(title, "🗑️", "Deleted") {
		return string(cardColorGrey)
	}
	return string(cardColorBlue)
}

// BuildCard 构建符合飞书卡片 V2 规范的消息卡片
func BuildCard(ctx context.Context, repo, repoUrl, sender, senderUrl, avatarUrl string, detail EventDetail) *Card {
	card := NewCard()
	card.Header.Title = CardText{Tag: "plain_text", Content: detail.Title}
	card.Header.Template = GetTemplate(detail.Title)

	// --- 1. 摘要信息行：仓库 / 分支 / 提交人（含头像） ---
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
			shaPart = fmt.Sprintf(" ([`%s`](%s/commit/%s))", detail.SHA, repoUrl, detail.SHA)
		}
		if detail.IsTag {
			refPart = fmt.Sprintf("🏷️ [%s](%s)%s", detail.RefName, link, shaPart)
		} else {
			refPart = fmt.Sprintf("🌿 [%s](%s)%s", detail.RefName, link, shaPart)
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

	// 构建发送者文本
	senderText := fmt.Sprintf("[%s](%s)", sender, senderUrl)
	if len(detail.AuthorLogins) > 1 {
		var links []string
		for _, login := range detail.AuthorLogins {
			links = append(links, fmt.Sprintf("[%s](https://github.com/%s)", login, login))
		}
		senderText = strings.Join(links, "  ")
	} else if len(detail.AuthorLogins) == 1 {
		login := detail.AuthorLogins[0]
		senderText = fmt.Sprintf("[%s](https://github.com/%s)", login, login)
	}

	// 收集最多 3 个头像的 img_key（飞书列数有上限，超出会导致排版混乱）
	avatarsToDisplay := detail.AuthorAvatars
	if len(avatarsToDisplay) == 0 && avatarUrl != "" {
		avatarsToDisplay = []string{avatarUrl}
	}
	const maxAvatars = 3
	if len(avatarsToDisplay) > maxAvatars {
		avatarsToDisplay = avatarsToDisplay[:maxAvatars]
	}

	var resolvedAvatars []string // 已缓存的 img_key 列表
	for _, u := range avatarsToDisplay {
		if key := GetImageKey(ctx, u); key != "" {
			resolvedAvatars = append(resolvedAvatars, key)
		}
	}

	// 构建摘要行：用 column_set 排列 [meta文本] [头像...] [发送者]
	// 头像全部合并进一个列（inline 排列），避免列数过多
	if len(resolvedAvatars) > 0 {
		// 将所有头像以小图标方式拼成一段 markdown（飞书 lark_md 不支持 img，
		// 所以头像列仍用独立 img 元素，但合并到单个 column 的 elements 数组里）
		avatarEls := make([]any, 0, len(resolvedAvatars))
		for _, key := range resolvedAvatars {
			avatarEls = append(avatarEls, map[string]any{
				"tag":          "img",
				"img_key":      key,
				"custom_width": 20,
				"mode":         "crop_center",
				"alt": map[string]string{
					"tag":     "plain_text",
					"content": "avatar",
				},
			})
		}

		columns := []any{
			// 左列：仓库+分支
			map[string]any{
				"tag":            "column",
				"width":          "weighted",
				"weight":         3,
				"vertical_align": "center",
				"elements": []any{
					map[string]any{"tag": "markdown", "content": metaText},
				},
			},
			// 中列：头像（多个 img 叠在同一列）
			map[string]any{
				"tag":            "column",
				"width":          "auto",
				"vertical_align": "center",
				"elements":       avatarEls,
			},
			// 右列：发送者链接
			map[string]any{
				"tag":            "column",
				"width":          "weighted",
				"weight":         2,
				"vertical_align": "center",
				"elements": []any{
					map[string]any{"tag": "markdown", "content": senderText},
				},
			},
		}

		card.Body.Elements = append(card.Body.Elements, map[string]any{
			"tag":                "column_set",
			"flex_mode":          "none",
			"horizontal_spacing": "small",
			"columns":            columns,
		})
	} else {
		// 无头像缓存时退回到纯文本摘要行
		line := "👤 " + senderText
		if metaText != "" {
			line = metaText + " / " + line
		}
		card.AddMarkdown(line)
	}

	// --- 2. 详情内容 ---
	if detail.Text != "" {
		card.AddDivider()
		card.AddMarkdown(detail.Text)
	}

	// --- 3. 可折叠的附加内容（PR body 中的 <details> 块等）---
	if detail.FoldableBody != "" {
		card.AddCollapsiblePanel("📝 展开查看详情", detail.FoldableBody)
	}

	// --- 4. 操作按钮（V2 规范：必须放在 action 容器内）---
	// Push / 删除 / 新建分支等事件不显示详情按钮
	skipBtn := strings.Contains(detail.Title, "commits") ||
		strings.Contains(detail.Title, "Deleted") ||
		strings.Contains(detail.Title, "Created")
	if detail.URL != "" && !skipBtn {
		btnType := "primary"
		if ContainsAny(detail.Title, "❌", "💥") {
			btnType = "danger"
		}

		btns := []ActionButton{
			{Text: "View Details", URL: detail.URL, Type: btnType},
		}
		// 失败时额外提供分支快捷链接
		if btnType == "danger" && detail.RefURL != "" {
			btns = append(btns, ActionButton{Text: "View Branch", URL: detail.RefURL, Type: "default"})
		}
		card.AddActions("flow", btns...)
	}

	return card
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

	// 2. 高亮 Conventional Commit 并处理格式
	matches := conventionalRegex.FindAllStringIndex(msg, -1)
	if len(matches) == 0 {
		return msg
	}

	var result strings.Builder
	last := 0
	for _, match := range matches {
		start, end := match[0], match[1]

		// 写入上一个匹配到当前匹配之间的内容
		if start > last {
			part := msg[last:start]
			result.WriteString(part)
		}

		// 加粗匹配的前缀
		result.WriteString("**")
		result.WriteString(msg[start:end])
		result.WriteString("**")

		// 确保 prefix 后面有一个空格（解决 feat:xxxx 无法高亮的问题）
		if end < len(msg) && msg[end] != ' ' && msg[end] != '\n' && msg[end] != '\t' {
			result.WriteString(" ")
		}

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

	// 2. 更加鲁棒地提取 <details> <summary> 内容 (支持属性如 <details open>)
	reDetails := regexp.MustCompile(`(?is)<details.*?>\s*<summary.*?>(.*?)</summary>(.*?)</details>`)
	var foldables []string
	
	// 提取并替换
	processed := reDetails.ReplaceAllStringFunc(s, func(m string) string {
		match := reDetails.FindStringSubmatch(m)
		if len(match) > 2 {
			title := strings.TrimSpace(match[1])
			// 移除 HTML 标签，只保留纯文本作为标题
			title = regexp.MustCompile(`(?s)<.*?>`).ReplaceAllString(title, "")
			
			content := strings.TrimSpace(match[2])
			// 移除内容中的所有 HTML 标签 (如 table, tr, td, br 等)
			content = regexp.MustCompile(`(?s)<.*?>`).ReplaceAllString(content, "")
			// 压缩多余换行
			content = regexp.MustCompile(`\n{3,}`).ReplaceAllString(content, "\n\n")
			
			foldables = append(foldables, fmt.Sprintf("**%s**\n%s", title, strings.TrimSpace(content)))
		}
		return "" // 从主文档移除
	})

	// 3. 移除主体中可能残留的所有 HTML 标签
	processed = regexp.MustCompile(`(?s)<.*?>`).ReplaceAllString(processed, "")
	processed = strings.TrimSpace(processed)
	
	// 4. 安全阶段 (截断长度及最终清洗)
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

var coAuthorRegex = regexp.MustCompile(`(?im)^Co-authored-by:\s*(.+?)\s*[<＜](.+?)[>＞]`)

type AuthorInfo struct {
	Name   string
	Login  string
	Avatar string
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
			// 1. 尝试从 GitHub noreply 邮箱提取 login
			if strings.HasSuffix(email, "@users.noreply.github.com") {
				parts := strings.Split(email, "@")
				if len(parts) > 0 {
					loginParts := strings.Split(parts[0], "+")
					login = loginParts[len(loginParts)-1]
				}
			}
			// 2. 如果提取不到，且名字不含空格，尝试把名字当作 login
			if login == "" && !strings.Contains(name, " ") {
				login = name
			}

			// 3. 针对已知的 AI service 或 Bot 的猜测 (仅限 GitHub 官方路径成果)
			if login == "" {
				if strings.Contains(email, "@anthropic.com") {
					login = "Claude"
				} else if strings.Contains(email, "@openai.com") {
					login = "ChatGPT"
				} else if strings.Contains(email, "bot") || strings.Contains(name, "Bot") {
					login = "bot"
				}
			}

			// 4. 统一使用 GitHub 提供的头像
			avatar := ""
			if login != "" {
				avatar = fmt.Sprintf("https://github.com/%s.png", login)
			}

			authors = append(authors, AuthorInfo{Name: name, Login: login, Avatar: avatar})
		}
	}
	return authors
}
