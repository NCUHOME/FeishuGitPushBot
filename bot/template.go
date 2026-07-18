package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/go-github/v84/github"
	"github.com/kyokomi/emoji/v2"
)

type EventDetail struct {
	Title         string   `json:"title"`
	Text          string   `json:"text"`
	URL           string   `json:"url"`
	Ref           string   `json:"ref"`      // 原始 Ref markdown
	RefName       string   `json:"ref_name"` // 纯文本引用名 (如 main)
	RefURL        string   `json:"ref_url"`  // 引用的 URL
	ReplyToTitle  string   `json:"reply_to_title"`
	FoldableBody  string   `json:"foldable_body"`
	Skip          bool     `json:"skip"`
	SHA           string   `json:"sha"`
	FullSHA       string   `json:"full_sha"` // 完整 SHA，用于构建 commit 链接
	IsTag         bool     `json:"is_tag"`
	IsDeleted     bool     `json:"is_deleted"`       // 分支/标签删除事件
	AuthorAvatars []string `json:"author_avatars"`   // 提交者或协作者的头像 URL 列表
	AuthorLogins  []string `json:"author_logins"`    // 提交者或协作者的 login 列表（与 AuthorAvatars 顺序对应）
	Action        string   `json:"action"`           // 事件具体动作
	ExtraReply    string   `json:"extra_reply"`      // 需要另起一段话题回复的内容
	EventTime     string   `json:"event_time"`       // GitHub 事件原始发生时间 (RFC3339)
	RepoName      string   `json:"repo_name"`        // 仓库全名 (如 NCUHOME/repo)，用于合并展示
	RepoURL       string   `json:"repo_url"`         // 仓库 HTML URL，用于 BuildCard 构建链接
	Notice        string   `json:"notice,omitempty"` // 额外提示，展示在主体内容之后

	// CI 状态：内联到触发事件的卡片中
	CIStatuses []CIStatus `json:"ci_statuses,omitempty"`
	// 合并事件：记录时间范围和数量
	EventTimeEnd string `json:"event_time_end,omitempty"` // 合并事件的最后时间
	EventCount   int    `json:"event_count,omitempty"`    // 合并事件中的条目数（按 commit 计）
	CommitCount  int    `json:"commit_count,omitempty"`   // push 事件的 commit 总数（用于折叠）
}

// CIStatus 单条 CI/Workflow 运行状态
type CIStatus struct {
	WorkflowName string `json:"workflow_name"` // workflow 名称
	Status       string `json:"status"`        // completed / in_progress / queued / ...
	Conclusion   string `json:"conclusion"`    // success / failure / cancelled / ...
	RunID        int64  `json:"run_id"`        // GitHub workflow run ID，用于构建链接
	Ref          string `json:"ref"`           // 分支名
	Duration     string `json:"duration"`      // 格式化后的耗时
	UpdatedAt    string `json:"updated_at"`    // 最后更新时间 (RFC3339)
	// Job 专用字段
	JobName     string `json:"job_name,omitempty"`      // job 名称（用于显示）
	ParentRunID int64  `json:"parent_run_id,omitempty"` // 关联的 workflow run ID
}

const pushGroupSeparator = "\n---\n"

// ParseEvent 解析 GitHub 事件为极简明了的 Detail
func ParseEvent(event any, eventType string) EventDetail {
	d := EventDetail{
		Title: fmt.Sprintf("🔔 GitHub Event: %s", eventType),
		Skip:  false, // 默认不跳过任何事件
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
		repoUrl := ""
		if repo := e.GetRepo(); repo != nil {
			repoUrl = repo.GetHTMLURL()
		}

		if isTag {
			d.Title = fmt.Sprintf("🏷️ Tag: %s", refShort)
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
				login := ""
				if author := c.GetAuthor(); author != nil {
					login = author.GetLogin()
				}
				if login == "" {
					if committer := c.GetCommitter(); committer != nil {
						login = committer.GetLogin()
					}
				}
				if login != "" {
					authors[login] = true
					avatarMap[login] = fmt.Sprintf("https://avatars.githubusercontent.com/%s", login)
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

				msg := SafeText(c.GetMessage(), 5000)
				msg = ProcessCommitMessage(msg, repoUrl)

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
					login := ""
					name := ""
					if author := c.GetAuthor(); author != nil {
						login = author.GetLogin()
						name = author.GetName()
					}
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
			// 飞书卡片 markdown 中 \n 为软换行（可能被忽略），<br> 为硬换行（始终生效）
			// 用 <br> 分隔各条 commit，保证换行且不会有段落间距
			d.Text = strings.Join(lines, "<br>")
			d.CommitCount = len(e.Commits)
		} else if e.GetDeleted() {
			repoName := ""
			if repo := e.GetRepo(); repo != nil {
				repoName = repo.GetName()
			}
			if isTag {
				d.Title = fmt.Sprintf("🗑️ Tag Deleted: %s", repoName)
				d.IsDeleted = true
			} else {
				d.Title = fmt.Sprintf("🗑️ Branch Deleted: %s", repoName)
				d.IsDeleted = true
			}
			d.Text = refShort
		} else if e.GetCreated() {
			if isTag {
				d.Title = fmt.Sprintf("🏷️ New Tag: %s", refShort)
				d.Text = fmt.Sprintf("🏷️ %s", refShort)
			} else {
				d.Title = fmt.Sprintf("🆕 New Branch: %s", refShort)
				d.Text = ""
			}
		} else if isTag {
			// Tag 推送但没有 commits（可能是已有 tag 的更新）
			d.Title = fmt.Sprintf("🏷️ Tag: %s", refShort)
			d.Text = ""
		}
		if hc := e.GetHeadCommit(); hc != nil {
			d.URL = hc.GetURL()
			sha := hc.GetID()
			d.FullSHA = sha
			if len(sha) > 7 {
				d.SHA = sha[:7]
			} else {
				d.SHA = sha
			}
			if ts := hc.GetTimestamp(); !ts.IsZero() {
				d.EventTime = ts.Time.Format(time.RFC3339)
			}
		}
		d.Action = "push"

	case *github.PullRequestEvent:
		pr := e.GetPullRequest()
		if pr == nil {
			d.Skip = true
			return d
		}
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
			label := ""
			if l := e.GetLabel(); l != nil {
				label = l.GetName()
			}
			d.Text = fmt.Sprintf("%s\n\nLabel: `%s`", pr.GetTitle(), label)
		} else {
			text, foldable := ProcessGithubMarkdown(pr.GetBody())
			if text != "" {
				d.Text = fmt.Sprintf("%s\n%s", pr.GetTitle(), text)
			} else {
				d.Text = pr.GetTitle()
			}
			d.FoldableBody = foldable
		}

		refName := ""
		if head := pr.GetHead(); head != nil {
			refName = head.GetRef()
		}
		if base := pr.GetBase(); base != nil {
			if refName != "" {
				refName = refName + " ➔ " + base.GetRef()
			} else {
				refName = base.GetRef()
			}
		}
		d.RefName = refName
		if head := pr.GetHead(); head != nil {
			if headRepo := head.GetRepo(); headRepo != nil {
				d.RefURL = fmt.Sprintf("%s/tree/%s", headRepo.GetHTMLURL(), head.GetRef())
			}
		}
		d.URL = pr.GetHTMLURL()
		if ts := pr.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Format(time.RFC3339)
		}

	case *github.IssuesEvent:
		action := e.GetAction()
		iss := e.GetIssue()
		if iss == nil {
			d.Skip = true
			return d
		}
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
		body, foldable := ProcessGithubMarkdown(iss.GetBody())
		if body != "" {
			d.Text = fmt.Sprintf("**%s**\n%s", iss.GetTitle(), body)
		} else {
			d.Text = fmt.Sprintf("**%s**", iss.GetTitle())
		}
		d.FoldableBody = foldable
		d.URL = iss.GetHTMLURL()
		d.Action = action
		if ts := iss.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Format(time.RFC3339)
		}

	case *github.IssueCommentEvent:
		iss := e.GetIssue()
		comment := e.GetComment()
		if iss == nil || comment == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		d.Title = fmt.Sprintf("🌻 Comment %s", action)
		d.Action = action

		body := comment.GetBody()
		if action == "edited" && e.Changes != nil && e.Changes.Body != nil && e.Changes.Body.From != nil {
			body = GetDiffOnlyAdded(*e.Changes.Body.From, body)
		}

		commentBody, foldable := ProcessGithubMarkdown(body)
		d.FoldableBody = foldable
		if commentBody != "" {
			d.Text = fmt.Sprintf("**%s**\n%s", iss.GetTitle(), commentBody)
		} else {
			d.Text = fmt.Sprintf("**%s**", iss.GetTitle())
		}
		d.URL = e.GetComment().GetHTMLURL()
		if ts := comment.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Format(time.RFC3339)
		}

	case *github.PullRequestReviewCommentEvent:
		pr := e.GetPullRequest()
		comment := e.GetComment()
		if pr == nil || comment == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		d.Title = fmt.Sprintf("💬 PR Comment %s", action)
		d.Action = action

		body := comment.GetBody()
		if action == "edited" && e.Changes != nil && e.Changes.Body != nil && e.Changes.Body.From != nil {
			body = GetDiffOnlyAdded(*e.Changes.Body.From, body)
		}

		commentBody, foldable := ProcessGithubMarkdown(body)
		d.FoldableBody = foldable
		if commentBody != "" {
			d.Text = fmt.Sprintf("**%s**\n%s", pr.GetTitle(), commentBody)
		} else {
			d.Text = fmt.Sprintf("**%s**", pr.GetTitle())
		}
		d.URL = comment.GetHTMLURL()
		if ts := comment.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Format(time.RFC3339)
		}

	case *github.PullRequestReviewEvent:
		pr := e.GetPullRequest()
		review := e.GetReview()
		if pr == nil || review == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		d.Title = fmt.Sprintf("🧐 PR Review %s", action)
		d.Action = action

		body := review.GetBody()
		// PullRequestReviewEvent 在 go-github 中目前没有 Changes 字段
		reviewBody, foldable := ProcessGithubMarkdown(body)
		d.FoldableBody = foldable
		if reviewBody != "" {
			d.Text = fmt.Sprintf("**%s**\n%s", pr.GetTitle(), reviewBody)
		} else {
			d.Text = fmt.Sprintf("**%s**", pr.GetTitle())
		}
		d.URL = review.GetHTMLURL()
		if ts := review.GetSubmittedAt(); !ts.IsZero() {
			d.EventTime = ts.Format(time.RFC3339)
		}

	case *github.WorkflowRunEvent:
		wr := e.GetWorkflowRun()
		if wr == nil {
			d.Skip = true
			return d
		}
		status := wr.GetStatus()
		if status == "" {
			status = e.GetAction()
		}
		conclusion := wr.GetConclusion()
		workflowName := wr.GetName()
		ref := wr.GetHeadBranch()
		sha := wr.GetHeadSHA()
		shortSHA := sha
		if len(sha) > 7 {
			shortSHA = sha[:7]
		}

		icon, stateVerb := ciStateDisplay(status, conclusion)

		d.SHA = shortSHA
		d.FullSHA = sha
		repoUrl := ""
		if repo := e.GetRepo(); repo != nil {
			repoUrl = repo.GetHTMLURL()
		}
		if repoUrl != "" && ref != "" {
			d.RefURL = fmt.Sprintf("%s/tree/%s", repoUrl, ref)
		}
		titleWorkflowName := workflowName
		if attempt := wr.GetRunAttempt(); attempt > 1 {
			titleWorkflowName = fmt.Sprintf("%s (attempt #%d)", workflowName, attempt)
		}
		d.Title = fmt.Sprintf("%s Workflow %s: %s", icon, titleCase(stateVerb), titleWorkflowName)

		var lines []string
		durationPart := ""
		if conclusion != "" {
			startedAt := wr.GetRunStartedAt()
			updatedAt := wr.GetUpdatedAt()
			if !startedAt.IsZero() && !updatedAt.IsZero() {
				start := startedAt.Time
				end := updatedAt.Time
				if !start.IsZero() && !end.IsZero() {
					durationPart = fmt.Sprintf(" in %s", FormatDuration(end.Sub(start)))
				}
			}
		}
		attemptPart := ""
		if attempt := wr.GetRunAttempt(); attempt > 1 {
			attemptPart = fmt.Sprintf(" (attempt #%d)", attempt)
		}
		lines = append(lines, fmt.Sprintf("%s **%s** workflow run %s%s%s", icon, workflowName, stateVerb, durationPart, attemptPart))

		d.Text = strings.Join(lines, "\n")
		d.RefName = ref
		d.URL = wr.GetHTMLURL()
		if ts := wr.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Format(time.RFC3339)
		}

	case *github.WorkflowJobEvent:
		wj := e.GetWorkflowJob()
		if wj == nil {
			d.Skip = true
			return d
		}
		status := wj.GetStatus()
		conclusion := wj.GetConclusion()
		jobName := wj.GetName()
		shortSHA := wj.GetHeadSHA()
		if len(shortSHA) > 7 {
			shortSHA = shortSHA[:7]
		}
		d.SHA = shortSHA

		icon, stateVerb := ciStateDisplay(status, conclusion)

		d.Title = fmt.Sprintf("%s Job %s: %s", icon, titleCase(stateVerb), jobName)

		var lines []string
		// 如果有 workflow_name 则显示为 Workflow / Job 格式
		displayJobName := jobName
		if wj.GetWorkflowName() != "" {
			displayJobName = fmt.Sprintf("%s / %s", wj.GetWorkflowName(), jobName)
		}

		durationPart := ""
		if conclusion != "" {
			startedAt := wj.GetStartedAt()
			completedAt := wj.GetCompletedAt()
			if !startedAt.IsZero() && !completedAt.IsZero() {
				start := startedAt.Time
				end := completedAt.Time
				if !start.IsZero() && !end.IsZero() {
					durationPart = fmt.Sprintf(" in %s", FormatDuration(end.Sub(start)))
				}
			}
		}
		lines = append(lines, fmt.Sprintf("%s job **%s** %s%s", icon, displayJobName, stateVerb, durationPart))

		d.Text = strings.Join(lines, "\n")
		d.RefName = wj.GetHeadBranch()
		if repo := e.GetRepo(); repo != nil {
			repoUrl := repo.GetHTMLURL()
			if repoUrl != "" && wj.GetHeadBranch() != "" {
				d.RefURL = fmt.Sprintf("%s/tree/%s", repoUrl, wj.GetHeadBranch())
			}
		}
		d.URL = wj.GetHTMLURL()
		if ts := wj.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Format(time.RFC3339)
		}

	case *github.CheckRunEvent:
		cr := e.GetCheckRun()
		if cr == nil {
			d.Skip = true
			return d
		}
		status := cr.GetStatus()
		conclusion := cr.GetConclusion()
		name := cr.GetName()
		sha := cr.GetHeadSHA()
		shortSHA := sha
		if len(sha) > 7 {
			shortSHA = sha[:7]
		}
		d.SHA = shortSHA

		icon, stateVerb := ciStateDisplay(status, conclusion)

		d.Title = fmt.Sprintf("%s Check: %s", icon, name)
		d.Text = fmt.Sprintf("%s check **%s** %s", icon, name, stateVerb)
		d.URL = cr.GetHTMLURL()
		d.RefName = e.GetCheckRun().GetCheckSuite().GetHeadBranch()

	case *github.CheckSuiteEvent:
		cs := e.GetCheckSuite()
		if cs == nil {
			d.Skip = true
			return d
		}
		status := cs.GetStatus()
		conclusion := cs.GetConclusion()
		sha := cs.GetHeadSHA()
		shortSHA := sha
		if len(sha) > 7 {
			shortSHA = sha[:7]
		}
		d.SHA = shortSHA

		icon, stateVerb := ciStateDisplay(status, conclusion)

		d.Title = fmt.Sprintf("%s Check Suite %s", icon, titleCase(stateVerb))
		d.Text = fmt.Sprintf("%s check suite %s", icon, stateVerb)
		d.URL = "" // CheckSuite does not have a direct HTMLURL field in go-github
		d.RefName = cs.GetHeadBranch()
		if ts := cs.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Format(time.RFC3339)
		}

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
		if forkee == nil {
			d.Skip = true
			return d
		}
		d.Title = "🍴 Repository Forked"
		d.Text = fmt.Sprintf("Repository forked to [%s](%s)", forkee.GetFullName(), forkee.GetHTMLURL())
		if ts := forkee.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Format(time.RFC3339)
		}

	case *github.GollumEvent:
		d.Title = "📖 Wiki Updated"
		var pages []string
		for _, p := range e.Pages {
			pages = append(pages, fmt.Sprintf("• [%s](%s) (%s)", p.GetTitle(), p.GetHTMLURL(), p.GetAction()))
		}
		d.Text = strings.Join(pages, "\n")

	case *github.CreateEvent:
		// Branch/tag creations are already included in push events.
		d.Skip = true

	case *github.DeleteEvent:
		// Branch/tag deletions are already included in push events.
		d.Skip = true

	case *github.PublicEvent:
		d.Title = "🔓 Repository Made Public"
		d.Text = "This repository is now visible to everyone."

	case *github.RepositoryEvent:
		if repo := e.GetRepo(); repo != nil {
			if ts := repo.GetCreatedAt(); !ts.IsZero() {
				d.EventTime = ts.Format(time.RFC3339)
			}
		}
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
			if repo := e.GetRepo(); repo != nil {
				d.Text = fmt.Sprintf("Renamed to **%s**", repo.GetFullName())
			}
		default:
			// 其他 edited 事件（如修改描述、Logo 等）通常比较琐碎，默认跳过
			d.Skip = true
		}
		d.Action = action

	case *github.DeployKeyEvent:
		key := e.GetKey()
		repo := e.GetRepo()
		if key == nil || repo == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		d.Title = fmt.Sprintf("🔐 Deploy Key %s: %s", titleCase(action), key.GetTitle())
		d.Action = action
		lines := []string{
			fmt.Sprintf("Repository: **%s**", repo.GetFullName()),
			fmt.Sprintf("Key: **%s**", key.GetTitle()),
			fmt.Sprintf("Key ID: `%d`", key.GetID()),
		}
		if key.GetReadOnly() {
			lines = append(lines, "Access: **read-only**")
		} else {
			lines = append(lines, "Access: **read/write**")
		}
		if addedBy := key.GetAddedBy(); addedBy != "" {
			lines = append(lines, fmt.Sprintf("Added by: **%s**", addedBy))
		}
		if sender := e.GetSender(); sender != nil && sender.GetLogin() != "" {
			lines = append(lines, fmt.Sprintf("By: **%s**", sender.GetLogin()))
			d.AuthorLogins = []string{sender.GetLogin()}
			d.AuthorAvatars = []string{sender.GetAvatarURL()}
		}
		d.Text = strings.Join(lines, "\n")
		d.URL = repo.GetHTMLURL()
		if ts := key.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Format(time.RFC3339)
		}

	case *github.OrganizationEvent:
		org := e.GetOrganization()
		membership := e.GetMembership()
		if org == nil || membership == nil {
			d.Skip = true
			return d
		}
		member := membership.GetUser()
		if member == nil {
			d.Skip = true
			return d
		}
		login := member.GetLogin()
		if login == "****" || login == "" {
			// 如果是邀请，尝试从其他地方获取信息（如暂时显示为 "New Member"）
			if login == "" {
				login = "Someone"
			}
		}
		d.Title = fmt.Sprintf("🏢 Org %s: %s", org.GetLogin(), e.GetAction())
		text := fmt.Sprintf("Action: **%s**\nMember: **%s**", e.GetAction(), login)
		if sender := e.GetSender(); sender != nil && sender.GetLogin() != login {
			text += fmt.Sprintf("\nBy: **%s**", sender.GetLogin())
		}
		d.Text = text
		d.Action = e.GetAction()
		d.URL = org.GetHTMLURL()
		if login != "" && login != "****" {
			d.AuthorLogins = []string{login}
			d.AuthorAvatars = []string{member.GetAvatarURL()}
		}
		if sender := e.GetSender(); sender != nil && sender.GetLogin() != login {
			d.AuthorLogins = append(d.AuthorLogins, sender.GetLogin())
			d.AuthorAvatars = append(d.AuthorAvatars, sender.GetAvatarURL())
		}
		if ts := org.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Format(time.RFC3339)
		}

	case *github.OrgBlockEvent:
		blockedUser := e.GetBlockedUser()
		org := e.GetOrganization()
		if blockedUser == nil || org == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		d.Title = fmt.Sprintf("🚫 Org User %s: %s", titleCase(action), blockedUser.GetLogin())
		d.Action = action
		lines := []string{
			fmt.Sprintf("Organization: **%s**", org.GetLogin()),
			fmt.Sprintf("User: **%s**", blockedUser.GetLogin()),
			fmt.Sprintf("Action: **%s**", action),
		}
		if sender := e.GetSender(); sender != nil && sender.GetLogin() != "" {
			lines = append(lines, fmt.Sprintf("By: **%s**", sender.GetLogin()))
			d.AuthorLogins = []string{sender.GetLogin()}
			d.AuthorAvatars = []string{sender.GetAvatarURL()}
		}
		d.Text = strings.Join(lines, "\n")
		d.URL = blockedUser.GetHTMLURL()

	case *github.PersonalAccessTokenRequestEvent:
		req := e.GetPersonalAccessTokenRequest()
		if req == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		d.Title = fmt.Sprintf("🔑 Personal Access Token Request %s", titleCase(action))
		d.Action = action

		lines := []string{fmt.Sprintf("Action: **%s**", action)}
		if req.GetID() != 0 {
			lines = append(lines, fmt.Sprintf("Request ID: `%d`", req.GetID()))
		}
		if owner := req.GetOwner(); owner != nil && owner.GetLogin() != "" {
			lines = append(lines, fmt.Sprintf("Owner: **%s**", owner.GetLogin()))
			d.URL = owner.GetHTMLURL()
			d.AuthorLogins = append(d.AuthorLogins, owner.GetLogin())
			d.AuthorAvatars = append(d.AuthorAvatars, owner.GetAvatarURL())
		}
		if org := e.GetOrg(); org != nil && org.GetLogin() != "" {
			lines = append(lines, fmt.Sprintf("Organization: **%s**", org.GetLogin()))
			if d.URL == "" {
				d.URL = org.GetHTMLURL()
			}
		}
		if sender := e.GetSender(); sender != nil && sender.GetLogin() != "" {
			lines = append(lines, fmt.Sprintf("By: **%s**", sender.GetLogin()))
			d.AuthorLogins = append(d.AuthorLogins, sender.GetLogin())
			d.AuthorAvatars = append(d.AuthorAvatars, sender.GetAvatarURL())
		}
		if selection := req.GetRepositorySelection(); selection != "" {
			lines = append(lines, fmt.Sprintf("Repository selection: **%s**", selection))
		}
		if count := req.GetRepositoryCount(); count > 0 {
			lines = append(lines, fmt.Sprintf("Repository count: **%d**", count))
		}
		if expiresAt := req.GetTokenExpiresAt(); !expiresAt.IsZero() {
			lines = append(lines, fmt.Sprintf("Token expires: **%s**", expiresAt.Format(time.RFC3339)))
		}
		if req.GetTokenExpired() {
			lines = append(lines, "Token expired: **true**")
		}
		if permissionLines := formatPersonalAccessTokenPermissions(req.GetPermissionsResult()); len(permissionLines) > 0 {
			lines = append(lines, "Permissions:")
			lines = append(lines, permissionLines...)
		}
		d.Text = strings.Join(lines, "\n")
		if createdAt := req.GetCreatedAt(); !createdAt.IsZero() {
			d.EventTime = createdAt.Format(time.RFC3339)
		}

	case *github.TeamEvent:
		team := e.GetTeam()
		if team == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		if repo := e.GetRepo(); repo != nil {
			senderLogin, senderURL := teamEventSender(e.GetSender())
			d.Title = formatTeamAccessEventTitle(action, e.GetChanges())
			d.Text = formatTeamAccessOperationLine(action, team.GetName(), team.GetHTMLURL(), senderLogin, senderURL, formatTeamAccessChangeDetails(team, e.GetChanges()))
			d.URL = repoSettingsAccessURL(repo.GetHTMLURL())
			if sender := e.GetSender(); sender != nil {
				d.AuthorLogins = []string{sender.GetLogin()}
				d.AuthorAvatars = []string{sender.GetAvatarURL()}
			}
		} else {
			d.Title = fmt.Sprintf("👥 Team %s: %s", team.GetName(), action)
			d.Text = fmt.Sprintf("Action: **%s**\nTeam: **%s**", action, team.GetName())
			d.URL = team.GetHTMLURL()
		}
		d.Action = action

	case *github.MemberEvent:
		member := e.GetMember()
		if member == nil {
			d.Skip = true
			return d
		}
		d.Title = formatMemberEventTitle(e.GetAction(), e.GetChanges())
		senderLogin := ""
		senderURL := ""
		if sender := e.GetSender(); sender != nil && sender.GetLogin() != member.GetLogin() {
			senderLogin = sender.GetLogin()
			senderURL = sender.GetHTMLURL()
		}
		d.Text = formatMemberOperationLine(e.GetAction(), member.GetLogin(), member.GetHTMLURL(), senderLogin, senderURL, e.GetChanges())
		d.Action = e.GetAction()
		d.URL = member.GetHTMLURL()
		if repo := e.GetRepo(); repo != nil {
			if url := repoSettingsAccessURL(repo.GetHTMLURL()); url != "" {
				d.URL = url
			}
		}
		d.EventCount = 1
		if sender := e.GetSender(); sender != nil {
			d.AuthorLogins = []string{sender.GetLogin()}
			d.AuthorAvatars = []string{sender.GetAvatarURL()}
		}
		if ts := member.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Format(time.RFC3339)
		}

	case *github.ReleaseEvent:
		action := e.GetAction()
		release := e.GetRelease()
		repo := e.GetRepo()
		if release == nil {
			d.Skip = true
			return d
		}
		d.Action = action
		d.URL = release.GetHTMLURL()

		switch action {
		case "published":
			d.Title = fmt.Sprintf("🚀 Release Published: %s", release.GetName())
		case "unpublished":
			d.Title = fmt.Sprintf("🚫 Release Unpublished: %s", release.GetName())
		case "created":
			d.Title = fmt.Sprintf("📝 Release Created: %s", release.GetName())
		case "edited":
			d.Title = fmt.Sprintf("✏️ Release Edited: %s", release.GetName())
		case "deleted":
			d.Title = fmt.Sprintf("🗑️ Release Deleted: %s", release.GetName())
		case "prereleased":
			d.Title = fmt.Sprintf("🧪 Pre-release: %s", release.GetName())
		case "released":
			d.Title = fmt.Sprintf("🏆 Release: %s", release.GetName())
		default:
			d.Title = fmt.Sprintf("📦 Release %s: %s", action, release.GetName())
		}

		// 构建发布内容
		var lines []string
		if tag := release.GetTagName(); tag != "" {
			d.RefName = tag
			if repo != nil {
				d.RefURL = fmt.Sprintf("%s/releases/tag/%s", repo.GetHTMLURL(), tag)
			}
			lines = append(lines, fmt.Sprintf("**Tag:** `%s`", tag))
		}
		if author := release.GetAuthor(); author != nil {
			lines = append(lines, fmt.Sprintf("**Author:** [%s](https://github.com/%s)", author.GetLogin(), author.GetLogin()))
			d.AuthorLogins = []string{author.GetLogin()}
			d.AuthorAvatars = []string{author.GetAvatarURL()}
		}
		if body := release.GetBody(); body != "" {
			text, _ := ProcessGithubMarkdown(body)
			if text != "" {
				lines = append(lines, "---", "**Release Notes:**", text)
			}
		}
		d.Text = strings.Join(lines, "\n")
		if ts := release.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Format(time.RFC3339)
		}

	case *github.MembershipEvent:
		member := e.GetMember()
		if member == nil {
			d.Skip = true
			return d
		}
		d.Title = fmt.Sprintf("👥 Membership %s: %s", member.GetLogin(), e.GetAction())
		text := fmt.Sprintf("Action: **%s**\nMember: **%s**\nScope: **%s**", e.GetAction(), member.GetLogin(), e.GetScope())
		if sender := e.GetSender(); sender != nil && sender.GetLogin() != member.GetLogin() {
			text += fmt.Sprintf("\nBy: **%s**", sender.GetLogin())
		}
		d.Text = text
		d.Action = e.GetAction()
		d.AuthorLogins = []string{member.GetLogin()}
		d.AuthorAvatars = []string{member.GetAvatarURL()}
		if sender := e.GetSender(); sender != nil && sender.GetLogin() != member.GetLogin() {
			d.AuthorLogins = append(d.AuthorLogins, sender.GetLogin())
			d.AuthorAvatars = append(d.AuthorAvatars, sender.GetAvatarURL())
		}

	case *github.CommitCommentEvent:
		comment := e.GetComment()
		if comment == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		d.Title = fmt.Sprintf("💬 Commit Comment %s", action)
		d.Action = action
		body, foldable := ProcessGithubMarkdown(comment.GetBody())
		d.FoldableBody = foldable
		if body != "" {
			d.Text = fmt.Sprintf("**Commit comment**\n%s", body)
		} else {
			d.Text = "**Commit comment**"
		}
		d.URL = comment.GetHTMLURL()
		if ts := comment.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case *github.DeploymentEvent:
		deployment := e.GetDeployment()
		if deployment == nil {
			d.Skip = true
			return d
		}
		d.Title = "🚀 Deployment Created"
		env := deployment.GetEnvironment()
		ref := deployment.GetRef()
		desc := deployment.GetDescription()
		var lines []string
		if env != "" {
			lines = append(lines, fmt.Sprintf("**Environment:** `%s`", env))
		}
		if ref != "" {
			lines = append(lines, fmt.Sprintf("**Ref:** `%s`", ref))
		}
		if desc != "" {
			lines = append(lines, desc)
		}
		if task := deployment.GetTask(); task != "" {
			lines = append(lines, fmt.Sprintf("**Task:** `%s`", task))
		}
		d.Text = strings.Join(lines, "\n")
		d.URL = deployment.GetURL()
		if ts := deployment.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case *github.DeploymentStatusEvent:
		ds := e.GetDeploymentStatus()
		deployment := e.GetDeployment()
		if ds == nil || deployment == nil {
			d.Skip = true
			return d
		}
		state := ds.GetState()
		icon := "📊"
		switch state {
		case "success":
			icon = "✅"
		case "failure", "error":
			icon = "❌"
		case "pending":
			icon = "⏳"
		}
		d.Title = fmt.Sprintf("%s Deployment %s", icon, titleCase(state))
		d.Action = e.GetAction()
		var lines []string
		if env := deployment.GetEnvironment(); env != "" {
			lines = append(lines, fmt.Sprintf("**Environment:** `%s`", env))
		}
		if desc := ds.GetDescription(); desc != "" {
			lines = append(lines, desc)
		}
		d.Text = strings.Join(lines, "\n")
		d.URL = ds.GetTargetURL()
		if ts := ds.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case *github.DiscussionEvent:
		disc := e.GetDiscussion()
		if disc == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		switch action {
		case "created":
			d.Title = "💬 New Discussion"
		case "answered":
			d.Title = "✅ Discussion Answered"
		case "closed":
			d.Title = "💬 Discussion Closed"
		default:
			d.Title = fmt.Sprintf("💬 Discussion %s", action)
		}
		d.Action = action
		body, foldable := ProcessGithubMarkdown(disc.GetBody())
		d.FoldableBody = foldable
		if body != "" {
			d.Text = fmt.Sprintf("**%s**\n%s", disc.GetTitle(), body)
		} else {
			d.Text = fmt.Sprintf("**%s**", disc.GetTitle())
		}
		d.URL = disc.GetHTMLURL()
		if ts := disc.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case *github.DiscussionCommentEvent:
		disc := e.GetDiscussion()
		comment := e.GetComment()
		if disc == nil || comment == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		d.Title = fmt.Sprintf("💬 Discussion Comment %s", action)
		d.Action = action
		body, foldable := ProcessGithubMarkdown(comment.GetBody())
		d.FoldableBody = foldable
		if body != "" {
			d.Text = fmt.Sprintf("**%s**\n%s", disc.GetTitle(), body)
		} else {
			d.Text = fmt.Sprintf("**%s**", disc.GetTitle())
		}
		d.URL = comment.GetHTMLURL()
		if ts := comment.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case *github.LabelEvent:
		label := e.GetLabel()
		if label == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		d.Title = fmt.Sprintf("🏷️ Label %s: %s", action, label.GetName())
		d.Action = action
		text := fmt.Sprintf("**%s**", label.GetName())
		if desc := label.GetDescription(); desc != "" {
			text += fmt.Sprintf("\n%s", desc)
		}
		if color := label.GetColor(); color != "" {
			text += fmt.Sprintf("\n`#%s`", color)
		}
		d.Text = text

	case *github.MilestoneEvent:
		ms := e.GetMilestone()
		if ms == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		d.Title = fmt.Sprintf("🎯 Milestone %s: %s", action, ms.GetTitle())
		d.Action = action
		var lines []string
		if desc := ms.GetDescription(); desc != "" {
			lines = append(lines, desc)
		}
		if !ms.GetDueOn().IsZero() {
			lines = append(lines, fmt.Sprintf("**Due:** %s", ms.GetDueOn().Format("2006-01-02")))
		}
		lines = append(lines, fmt.Sprintf("Open: %d / Closed: %d", ms.GetOpenIssues(), ms.GetClosedIssues()))
		d.Text = strings.Join(lines, "\n")
		d.URL = ms.GetHTMLURL()
		if ts := ms.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case *github.PullRequestReviewThreadEvent:
		pr := e.GetPullRequest()
		if pr == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		switch action {
		case "resolved":
			d.Title = "✅ PR Review Thread Resolved"
		case "unresolved":
			d.Title = "🔓 PR Review Thread Unresolved"
		default:
			d.Title = fmt.Sprintf("🧵 PR Review Thread %s", action)
		}
		d.Action = action
		d.Text = fmt.Sprintf("**%s**", pr.GetTitle())
		d.URL = pr.GetHTMLURL()
		if ts := pr.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Format(time.RFC3339)
		}

	case *github.StatusEvent:
		state := e.GetState()
		context := e.GetContext()
		icon := "📊"
		switch state {
		case "success":
			icon = "✅"
		case "failure", "error":
			icon = "❌"
		case "pending":
			icon = "⏳"
		}
		d.Title = fmt.Sprintf("%s Status: %s", icon, context)
		desc := e.GetDescription()
		if desc != "" {
			d.Text = fmt.Sprintf("**%s** %s: %s", context, state, desc)
		} else {
			d.Text = fmt.Sprintf("**%s** %s", context, state)
		}
		d.URL = e.GetTargetURL()
		d.Action = state
		sha := e.GetSHA()
		if len(sha) > 7 {
			d.SHA = sha[:7]
		} else {
			d.SHA = sha
		}
		if ts := e.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case *github.BranchProtectionRuleEvent:
		rule := e.GetRule()
		if rule == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		d.Title = fmt.Sprintf("🛡️ Branch Protection Rule %s", action)
		d.Action = action
		d.Text = fmt.Sprintf("**%s**", rule.GetName())
		if ts := rule.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case *github.BranchProtectionConfigurationEvent:
		repo := e.GetRepo()
		if repo == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		d.Title = fmt.Sprintf("🛡️ Branch Protection Configuration %s", titleCase(action))
		d.Action = action
		lines := []string{fmt.Sprintf("Repository: **%s**", repo.GetFullName())}
		if sender := e.GetSender(); sender != nil && sender.GetLogin() != "" {
			lines = append(lines, fmt.Sprintf("By: **%s**", sender.GetLogin()))
			d.AuthorLogins = []string{sender.GetLogin()}
			d.AuthorAvatars = []string{sender.GetAvatarURL()}
		}
		d.Text = strings.Join(lines, "\n")
		d.URL = repo.GetHTMLURL()

	case *github.RepositoryRulesetEvent:
		ruleset := e.GetRepositoryRuleset()
		if ruleset == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		d.Title = fmt.Sprintf("📋 Repository Ruleset %s", action)
		d.Action = action
		d.Text = fmt.Sprintf("**Ruleset #%d**", ruleset.GetID())
		if ts := ruleset.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case *github.SecurityAndAnalysisEvent:
		repo := e.GetRepository()
		if repo == nil {
			d.Skip = true
			return d
		}
		d.Title = "🛡️ Security & Analysis Changed"
		d.Action = "updated"
		lines := []string{fmt.Sprintf("Repository: **%s**", repo.GetFullName())}
		if sender := e.GetSender(); sender != nil && sender.GetLogin() != "" {
			lines = append(lines, fmt.Sprintf("By: **%s**", sender.GetLogin()))
			d.AuthorLogins = []string{sender.GetLogin()}
			d.AuthorAvatars = []string{sender.GetAvatarURL()}
		}
		if previous := securityAndAnalysisFromChange(e.GetChanges()); previous != nil {
			lines = append(lines, securityAndAnalysisLines("Previous", previous)...)
		}
		if current := repo.GetSecurityAndAnalysis(); current != nil {
			lines = append(lines, securityAndAnalysisLines("Current", current)...)
		}
		d.Text = strings.Join(lines, "\n")
		d.URL = repo.GetHTMLURL()

	case *github.CodeScanningAlertEvent:
		alert := e.GetAlert()
		if alert == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		severity := alert.GetRuleSeverity()
		icon := "🔍"
		switch severity {
		case "error":
			icon = "🔴"
		case "warning":
			icon = "🟡"
		case "note":
			icon = "🔵"
		}
		d.Title = fmt.Sprintf("%s Code Scanning Alert #%d %s", icon, alert.GetNumber(), action)
		d.Action = action
		ruleDesc := alert.GetRuleDescription()
		if ruleDesc == "" {
			ruleDesc = alert.GetRuleID()
		}
		text := fmt.Sprintf("**%s** (severity: %s)", ruleDesc, severity)
		if ref := e.GetRef(); ref != "" {
			text += fmt.Sprintf("\n**Ref:** `%s`", ref)
		}
		d.Text = text
		d.URL = alert.GetHTMLURL()
		if ts := alert.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case *github.DependabotAlertEvent:
		alert := e.GetAlert()
		if alert == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		severity := ""
		if sv := alert.GetSecurityVulnerability(); sv != nil {
			severity = sv.GetSeverity()
		}
		icon := "🤖"
		switch severity {
		case "critical":
			icon = "🔴"
		case "high":
			icon = "🟠"
		case "medium":
			icon = "🟡"
		case "low":
			icon = "🟢"
		}
		d.Title = fmt.Sprintf("%s Dependabot Alert #%d %s", icon, alert.GetNumber(), action)
		d.Action = action
		dep := alert.GetDependency()
		pkgName := ""
		if dep != nil {
			pkgName = dep.GetPackage().GetName()
		}
		text := fmt.Sprintf("**%s** (severity: %s)", pkgName, severity)
		if advisory := alert.GetSecurityAdvisory(); advisory != nil {
			if summary := advisory.GetSummary(); summary != "" {
				text += fmt.Sprintf("\n%s", summary)
			}
		}
		d.Text = text
		d.URL = alert.GetHTMLURL()
		if ts := alert.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case *github.SecretScanningAlertEvent:
		alert := e.GetAlert()
		if alert == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		d.Title = fmt.Sprintf("🔐 Secret Scanning Alert #%d %s", alert.GetNumber(), action)
		d.Action = action
		secretType := alert.GetSecretTypeDisplayName()
		if secretType == "" {
			secretType = alert.GetSecretType()
		}
		text := fmt.Sprintf("**Secret type:** %s", secretType)
		if alert.GetPubliclyLeaked() {
			text += "\n⚠️ **Publicly leaked**"
		}
		d.Text = text
		d.URL = alert.GetHTMLURL()
		if ts := alert.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case *github.RepositoryVulnerabilityAlertEvent:
		alert := e.GetAlert()
		if alert == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		severity := alert.GetSeverity()
		icon := "⚠️"
		switch severity {
		case "critical":
			icon = "🔴"
		case "high":
			icon = "🟠"
		case "medium":
			icon = "🟡"
		case "low":
			icon = "🟢"
		}
		d.Title = fmt.Sprintf("%s Vulnerability Alert %s", icon, action)
		d.Action = action
		pkg := alert.GetAffectedPackageName()
		text := fmt.Sprintf("**%s** (severity: %s)", pkg, severity)
		if fixedIn := alert.GetFixedIn(); fixedIn != "" {
			text += fmt.Sprintf("\n**Fixed in:** %s", fixedIn)
		}
		d.Text = text
		if ref := alert.GetExternalReference(); ref != "" {
			d.URL = ref
		}
		if ts := alert.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case *github.SecurityAdvisoryEvent:
		adv := e.GetSecurityAdvisory()
		if adv == nil {
			d.Skip = true
			return d
		}
		action := e.GetAction()
		severity := adv.GetSeverity()
		icon := "🛡️"
		switch severity {
		case "critical":
			icon = "🔴"
		case "high":
			icon = "🟠"
		case "medium":
			icon = "🟡"
		case "low":
			icon = "🟢"
		}
		d.Title = fmt.Sprintf("%s Security Advisory %s", icon, action)
		d.Action = action
		text := fmt.Sprintf("**%s** (severity: %s)", adv.GetGHSAID(), severity)
		if summary := adv.GetSummary(); summary != "" {
			text += fmt.Sprintf("\n%s", summary)
		}
		if cve := adv.GetCVEID(); cve != "" {
			text += fmt.Sprintf("\n**CVE:** %s", cve)
		}
		d.Text = text
		d.URL = adv.GetHTMLURL()
		if ts := adv.GetPublishedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case *github.TeamAddEvent:
		team := e.GetTeam()
		if team == nil {
			d.Skip = true
			return d
		}
		d.Action = "added"
		senderLogin, senderURL := teamEventSender(e.GetSender())
		if repo := e.GetRepo(); repo != nil {
			d.Title = "👥 Team access added"
			d.Text = formatTeamAccessOperationLine("added", team.GetName(), team.GetHTMLURL(), senderLogin, senderURL, formatTeamAccessAddedDetails(team))
			d.URL = repoSettingsAccessURL(repo.GetHTMLURL())
		} else {
			d.Title = fmt.Sprintf("👥 Team Added: %s", team.GetName())
			d.Text = fmt.Sprintf("**Team:** [%s](%s)", team.GetName(), team.GetHTMLURL())
			d.URL = team.GetHTMLURL()
		}
		if sender := e.GetSender(); sender != nil {
			d.AuthorLogins = []string{sender.GetLogin()}
			d.AuthorAvatars = []string{sender.GetAvatarURL()}
		}

	case *github.PageBuildEvent:
		build := e.GetBuild()
		if build == nil {
			d.Skip = true
			return d
		}
		status := build.GetStatus()
		icon := "📄"
		stateVerb := status
		switch status {
		case "built":
			icon = "✅"
			stateVerb = "built"
		case "building":
			icon = "⏳"
			stateVerb = "building"
		case "errored":
			icon = "❌"
			stateVerb = "errored"
		}
		d.Title = fmt.Sprintf("%s Page Build %s", icon, titleCase(stateVerb))
		d.Action = status
		duration := time.Duration(build.GetDuration()) * time.Second
		d.Text = fmt.Sprintf("Pages build %s in %s", status, FormatDuration(duration))
		if ts := build.GetCreatedAt(); !ts.IsZero() {
			d.EventTime = ts.Time.Format(time.RFC3339)
		}

	case map[string]any:
		if eventType == "repository_advisory" {
			action := ext(e, "action")
			ghsaID := ext(e, "repository_advisory", "ghsa_id")
			summary := ext(e, "repository_advisory", "summary")
			severity := ext(e, "repository_advisory", "severity")
			repoName := ext(e, "repository", "full_name")
			d.Title = fmt.Sprintf("🛡️ Repository Advisory %s", titleCase(action))
			d.Action = action
			var lines []string
			if repoName != "" {
				lines = append(lines, fmt.Sprintf("Repository: **%s**", repoName))
			}
			if ghsaID != "" {
				lines = append(lines, fmt.Sprintf("GHSA: `%s`", ghsaID))
			}
			if severity != "" {
				lines = append(lines, fmt.Sprintf("Severity: **%s**", severity))
			}
			if summary != "" {
				lines = append(lines, fmt.Sprintf("Summary: **%s**", summary))
			}
			if sender := ext(e, "sender", "login"); sender != "" {
				lines = append(lines, fmt.Sprintf("By: **%s**", sender))
				d.AuthorLogins = []string{sender}
				if avatar := ext(e, "sender", "avatar_url"); avatar != "" {
					d.AuthorAvatars = []string{avatar}
				}
			}
			d.Text = strings.Join(lines, "\n")
			d.URL = ext(e, "repository_advisory", "html_url")
			if d.URL == "" {
				d.URL = ext(e, "repository", "html_url")
			}
		}
	}
	return d
}

func formatPersonalAccessTokenPermissions(perms *github.PersonalAccessTokenPermissions) []string {
	if perms == nil {
		return nil
	}
	var lines []string
	lines = append(lines, formatPermissionGroup("organization", perms.Org)...)
	lines = append(lines, formatPermissionGroup("repository", perms.Repo)...)
	lines = append(lines, formatPermissionGroup("other", perms.Other)...)
	return lines
}

func formatPermissionGroup(scope string, permissions map[string]string) []string {
	if len(permissions) == 0 {
		return nil
	}
	keys := make([]string, 0, len(permissions))
	for key := range permissions {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("- `%s.%s`: **%s**", scope, key, permissions[key]))
	}
	return lines
}

func formatMemberOperationLine(action, memberLogin, memberURL, senderLogin, senderURL string, changes *github.MemberChanges) string {
	if action == "" {
		action = "updated"
	}
	line := fmt.Sprintf("- **%s** member %s", action, boldMarkdownLink(memberLogin, memberURL))
	if senderLogin != "" && senderLogin != memberLogin {
		line += fmt.Sprintf(" by %s", boldMarkdownLink(senderLogin, senderURL))
	}
	if details := formatMemberChangeDetails(changes); len(details) > 0 {
		line += "; " + strings.Join(details, "; ")
	}
	return line
}

func boldMarkdownLink(text, url string) string {
	if text == "" {
		return ""
	}
	if url == "" {
		return fmt.Sprintf("**%s**", text)
	}
	return fmt.Sprintf("**[%s](%s)**", text, url)
}

func repoSettingsAccessURL(repoURL string) string {
	if repoURL == "" {
		return ""
	}
	return strings.TrimRight(repoURL, "/") + "/settings/access"
}

func teamEventSender(sender *github.User) (string, string) {
	if sender == nil {
		return "", ""
	}
	return sender.GetLogin(), sender.GetHTMLURL()
}

func formatTeamAccessEventTitle(action string, changes *github.TeamChange) string {
	if action == "" {
		action = "updated"
	}
	if action == "edited" && changes != nil && changes.GetRepository() != nil {
		return "👥 Team access edited"
	}
	return fmt.Sprintf("👥 Team access %s", action)
}

func formatTeamAccessOperationLine(action, teamName, teamURL, senderLogin, senderURL string, details []string) string {
	if action == "" {
		action = "updated"
	}
	line := fmt.Sprintf("- **%s** team %s", action, boldMarkdownLink(teamName, teamURL))
	if senderLogin != "" {
		line += fmt.Sprintf(" by %s", boldMarkdownLink(senderLogin, senderURL))
	}
	if len(details) > 0 {
		line += "; " + strings.Join(details, "; ")
	}
	return line
}

func formatTeamAccessAddedDetails(team *github.Team) []string {
	if permission := team.GetPermission(); permission != "" {
		return []string{formatMemberChangeValue("permission", "", permission)}
	}
	return nil
}

func formatTeamAccessChangeDetails(team *github.Team, changes *github.TeamChange) []string {
	if changes == nil || changes.GetRepository() == nil {
		return formatTeamAccessAddedDetails(team)
	}
	permissions := changes.GetRepository().GetPermissions()
	if permissions == nil || permissions.GetFrom() == nil {
		return formatTeamAccessAddedDetails(team)
	}
	if detail := formatMemberChangeValue("permission", teamPermissionFromFlags(permissions.GetFrom()), team.GetPermission()); detail != "" {
		return []string{detail}
	}
	return nil
}

func teamPermissionFromFlags(from *github.TeamPermissionsFrom) string {
	switch {
	case from.GetAdmin():
		return "admin"
	case from.GetPush():
		return "write"
	case from.GetPull():
		return "read"
	default:
		return ""
	}
}

func formatMemberEventTitle(action string, changes *github.MemberChanges) string {
	if action == "" {
		action = "updated"
	}
	if action == "edited" && changes != nil {
		hasPermission := changes.GetPermission() != nil
		hasRoleName := changes.GetRoleName() != nil
		switch {
		case hasPermission && hasRoleName:
			return "👤 Member access edited"
		case hasPermission:
			return "👤 Member permission edited"
		case hasRoleName:
			return "👤 Member role edited"
		}
	}
	return fmt.Sprintf("👤 Member %s", action)
}

func formatMemberChangeDetails(changes *github.MemberChanges) []string {
	if changes == nil {
		return nil
	}
	var details []string
	if permission := changes.GetPermission(); permission != nil {
		if detail := formatMemberChangeValue("permission", permission.GetFrom(), permission.GetTo()); detail != "" {
			details = append(details, detail)
		}
	}
	if roleName := changes.GetRoleName(); roleName != nil {
		if detail := formatMemberChangeValue("role_name", roleName.GetFrom(), roleName.GetTo()); detail != "" {
			details = append(details, detail)
		}
	}
	return details
}

func formatMemberChangeValue(name, from, to string) string {
	switch {
	case from != "" && to != "":
		return fmt.Sprintf("%s: `%s` -> `%s`", name, from, to)
	case from != "":
		return fmt.Sprintf("%s from `%s`", name, from)
	case to != "":
		return fmt.Sprintf("%s to `%s`", name, to)
	default:
		return ""
	}
}

func securityAndAnalysisFromChange(change *github.SecurityAndAnalysisChange) *github.SecurityAndAnalysis {
	if change == nil || change.GetFrom() == nil {
		return nil
	}
	return change.GetFrom().GetSecurityAndAnalysis()
}

func securityAndAnalysisLines(label string, analysis *github.SecurityAndAnalysis) []string {
	if analysis == nil {
		return nil
	}
	statuses := []struct {
		name   string
		status string
	}{
		{"Advanced security", analysis.GetAdvancedSecurity().GetStatus()},
		{"Secret scanning", analysis.GetSecretScanning().GetStatus()},
		{"Secret scanning push protection", analysis.GetSecretScanningPushProtection().GetStatus()},
		{"Secret scanning validity checks", analysis.GetSecretScanningValidityChecks().GetStatus()},
		{"Dependabot security updates", analysis.GetDependabotSecurityUpdates().GetStatus()},
	}

	var lines []string
	for _, status := range statuses {
		if status.status != "" {
			lines = append(lines, fmt.Sprintf("%s %s: **%s**", label, status.name, status.status))
		}
	}
	return lines
}

// splitCommits 将 push 事件的文本按 commit 条目拆分
// 每个 commit 以 🔸 或 🔹 开头，条目间用 <br> 分隔
func splitCommits(text string) []string {
	// 条目间用 <br> 分隔（飞书硬换行）
	parts := strings.Split(text, "<br>")
	var commits []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			commits = append(commits, part)
		}
	}
	return commits
}

func splitPushTextGroups(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "<br>---<br>", pushGroupSeparator)
	parts := strings.Split(text, pushGroupSeparator)
	var groups []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			groups = append(groups, part)
		}
	}
	return groups
}

// pushVisibleCommits 折叠前直接展示的 commit 条数。
const pushVisibleCommits = 5

const pushMinFoldedCommits = 5

func addPushMarkdown(card *Card, detail EventDetail) {
	groups := splitPushTextGroups(detail.Text)
	commitsByGroup := make([][]string, 0, len(groups))
	totalCommits := 0
	for _, group := range groups {
		commits := splitCommits(group)
		if len(commits) > 0 {
			commitsByGroup = append(commitsByGroup, commits)
			totalCommits += len(commits)
		}
	}
	if totalCommits == 0 {
		card.AddMarkdown(detail.Text)
		return
	}

	renderGroups := func(groupCommits [][]string) {
		rendered := 0
		for _, commits := range groupCommits {
			if len(commits) == 0 {
				continue
			}
			if rendered > 0 {
				card.AddDivider()
			}
			card.AddMarkdown(strings.Join(commits, "<br>"))
			rendered++
		}
	}

	if totalCommits >= pushVisibleCommits+pushMinFoldedCommits {
		visibleGroups := make([][]string, len(commitsByGroup))
		foldedGroups := make([]string, 0, len(commitsByGroup))
		visibleRemaining := pushVisibleCommits
		for i, commits := range commitsByGroup {
			visibleCount := min(visibleRemaining, len(commits))
			if visibleCount > 0 {
				visibleGroups[i] = commits[:visibleCount]
				visibleRemaining -= visibleCount
			}
			if visibleCount < len(commits) {
				foldedGroups = append(foldedGroups, strings.Join(commits[visibleCount:], "<br>"))
			}
		}
		renderGroups(visibleGroups)
		card.AddCollapsiblePanel(
			fmt.Sprintf("📝 展开查看其余 %d 条提交", totalCommits-pushVisibleCommits),
			strings.Join(foldedGroups, "<br>---<br>"),
		)
		return
	}

	renderGroups(commitsByGroup)
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
	if ContainsAny(title, "🗑️", "Deleted") {
		return string(cardColorGrey)
	}
	if ContainsAny(title, "🏷️", "Tag", "New Tag") {
		return string(cardColorPurple)
	}
	if ContainsAny(title, "🚀", "Release", "Pre-release") {
		return "turquoise"
	}
	if ContainsAny(title, "🆕", "New Branch", "New Commits", "commits") {
		return "wathet"
	}
	if ContainsAny(title, "🥕", "PullRequest", "PR") {
		return "indigo"
	}
	return string(cardColorBlue)
}

// extractPRNumber 从 commit message 中提取 PR 编号（匹配 "Merge pull request #N" 模式）
func extractPRNumber(msg string) string {
	matches := prMergeRegex.FindStringSubmatch(msg)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// parseRunIDFromURL 从 GitHub Actions URL 中解析 run ID
// URL 格式: https://github.com/owner/repo/actions/runs/123456
func parseRunIDFromURL(url string) int64 {
	if url == "" {
		return 0
	}
	// 查找 /runs/ 后的数字
	idx := strings.LastIndex(url, "/runs/")
	if idx < 0 {
		return 0
	}
	remaining := url[idx+6:]
	// 提取连续数字
	var numStr string
	for _, c := range remaining {
		if c >= '0' && c <= '9' {
			numStr += string(c)
		} else {
			break
		}
	}
	if numStr == "" {
		return 0
	}
	var id int64
	fmt.Sscanf(numStr, "%d", &id)
	return id
}

// getCIStatusesForParent 查询关联到指定父消息的所有 CI 事件状态
// 优先从 EventDetail.CIStatuses 读取结构化数据，回退到标题解析（兼容旧记录）
func getCIStatusesForParent(ctx context.Context, parentMsgID string) []CIStatus {
	if DB == nil || parentMsgID == "" {
		return nil
	}
	var records []MessageRecord
	if err := DB.NewSelect().Model(&records).
		Where("parent_msg_id = ?", parentMsgID).
		Where("event_type IN ('workflow_run', 'workflow_job', 'check_run', 'check_suite')").
		Order("id DESC"). // 最新的优先，去重时保留最新状态
		Scan(ctx); err != nil {
		return nil
	}
	var statuses []CIStatus
	seen := make(map[string]bool)
	for _, r := range records {
		var detail EventDetail
		_ = json.Unmarshal([]byte(r.Content), &detail)

		// 优先使用结构化的 CIStatuses 字段
		if len(detail.CIStatuses) > 0 {
			for _, cs := range detail.CIStatuses {
				key := cs.WorkflowName
				if cs.JobName != "" {
					key = cs.JobName
				}
				if !seen[key] {
					seen[key] = true
					if cs.UpdatedAt == "" {
						cs.UpdatedAt = r.UpdatedAt.Format(time.RFC3339)
					}
					statuses = append(statuses, cs)
				}
			}
			continue
		}

		// 回退：从 Title 解析（兼容旧记录）
		workflowName := ""
		titleParts := strings.SplitN(detail.Title, ": ", 2)
		if len(titleParts) > 1 {
			workflowName = titleParts[1]
		} else {
			workflowName = detail.Title
		}
		conclusion := ""
		status := "completed"
		if strings.Contains(detail.Title, "✅") {
			conclusion = "success"
		} else if strings.Contains(detail.Title, "❌") {
			conclusion = "failure"
		} else if strings.Contains(detail.Title, "⏳") {
			status = "in_progress"
		} else if strings.Contains(detail.Title, "⚙️") {
			status = "in_progress"
		}
		key := workflowName
		if !seen[key] {
			seen[key] = true
			duration := ""
			if idx := strings.Index(detail.Text, " in "); idx >= 0 {
				duration = strings.TrimSpace(detail.Text[idx+4:])
			}
			statuses = append(statuses, CIStatus{
				WorkflowName: workflowName,
				Status:       status,
				Conclusion:   conclusion,
				RunID:        parseRunIDFromURL(detail.URL),
				Duration:     duration,
				UpdatedAt:    r.UpdatedAt.Format(time.RFC3339),
			})
		}
	}
	return statuses
}

// renderCIStatuses 将 CI 状态列表渲染为飞书 markdown
func renderCIStatuses(statuses []CIStatus, repoURL string) string {
	if len(statuses) == 0 {
		return ""
	}

	// 分离 workflow 和 job 条目
	type workflowGroup struct {
		workflow CIStatus
		jobs     []CIStatus
	}
	var groups []workflowGroup
	jobsByRun := make(map[string][]CIStatus)

	for _, cs := range statuses {
		if strings.HasPrefix(cs.WorkflowName, "job:") {
			if cs.ParentRunID > 0 {
				key := fmt.Sprintf("%d", cs.ParentRunID)
				jobsByRun[key] = upsertRenderedCIStatus(jobsByRun[key], cs)
			}
		} else {
			// workflow 条目
			g := workflowGroup{workflow: cs}
			groups = append(groups, g)
		}
	}
	attachedJobRuns := make(map[string]bool)
	// 更新 groups 中的 jobs
	for i := range groups {
		key := fmt.Sprintf("%d", groups[i].workflow.RunID)
		if jobs, ok := jobsByRun[key]; ok {
			groups[i].jobs = jobs
			attachedJobRuns[key] = true
		}
	}

	var lines []string
	for _, g := range groups {
		lines = append(lines, renderSingleCIStatus(g.workflow, repoURL, false))
		for _, job := range g.jobs {
			lines = append(lines, renderSingleCIStatus(job, repoURL, true))
		}
	}
	var unattachedRunKeys []string
	for runKey := range jobsByRun {
		if !attachedJobRuns[runKey] {
			unattachedRunKeys = append(unattachedRunKeys, runKey)
		}
	}
	sort.Strings(unattachedRunKeys)
	for _, runKey := range unattachedRunKeys {
		for _, job := range jobsByRun[runKey] {
			lines = append(lines, renderSingleCIStatus(job, repoURL, false))
		}
	}
	return strings.Join(lines, "<br>")
}

func upsertRenderedCIStatus(statuses []CIStatus, status CIStatus) []CIStatus {
	key := status.WorkflowName
	for i, existing := range statuses {
		if existing.WorkflowName == key {
			statuses[i] = status
			return statuses
		}
	}
	return append(statuses, status)
}

func cleanCIStatusDisplayName(name string) string {
	for {
		trimmed := strings.TrimSpace(name)
		next := trimCIStatusIcon(trimmed)
		next = trimWorkflowTitlePrefix(next)
		if next == trimmed {
			return trimmed
		}
		name = next
	}
}

func trimCIStatusIcon(name string) string {
	for _, icon := range []string{"✅", "❌", "⏳", "⚙️", "🚫", "🕒", "⏭️", "➖", "⚠️"} {
		if strings.HasPrefix(name, icon) {
			return strings.TrimSpace(strings.TrimPrefix(name, icon))
		}
	}
	return name
}

func trimWorkflowTitlePrefix(name string) string {
	for _, prefix := range []string{
		"Workflow Succeeded: ",
		"Workflow Failed: ",
		"Workflow Running: ",
		"Workflow Started: ",
		"Workflow Requested: ",
		"Workflow Queued: ",
		"Workflow Waiting: ",
		"Workflow Completed: ",
		"Workflow Cancelled: ",
		"Workflow Canceled: ",
		"Workflow Timed_out: ",
		"Workflow Timed Out: ",
		"Workflow Neutral: ",
		"Workflow Skipped: ",
		"Workflow Action_required: ",
		"Workflow Action Required: ",
		"Workflow Startup Failure: ",
		"Workflow Startup_failure: ",
		"Workflow Stale: ",
	} {
		if strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
			return strings.TrimSpace(name[len(prefix):])
		}
	}
	if match := workflowTitlePrefixRe.FindString(name); match != "" {
		return strings.TrimSpace(name[len(match):])
	}
	return name
}

func ciStateDisplay(status, conclusion string) (string, string) {
	switch conclusion {
	case "success":
		return "✅", "succeeded"
	case "failure":
		return "❌", "failed"
	case "cancelled":
		return "🚫", "cancelled"
	case "timed_out":
		return "❌", "timed out"
	case "skipped":
		return "⏭️", "skipped"
	case "neutral":
		return "➖", "neutral"
	case "action_required":
		return "⚠️", "action required"
	}
	if conclusion != "" {
		return "⚠️", strings.ReplaceAll(conclusion, "_", " ")
	}
	switch status {
	case "requested":
		return "⚙️", "requested"
	case "queued":
		return "🕒", "queued"
	case "waiting":
		return "🕒", "waiting"
	case "pending":
		return "🕒", "pending"
	case "in_progress":
		return "⏳", "running"
	case "completed":
		return "✅", "completed"
	default:
		if status != "" {
			return "⚙️", status
		}
		return "⚙️", "started"
	}
}

// renderSingleCIStatus 渲染单条 CI 状态
func renderSingleCIStatus(cs CIStatus, repoURL string, isJob bool) string {
	icon, statusText := ciStateDisplay(cs.Status, cs.Conclusion)
	if cs.Conclusion == "success" {
		statusText = "passed"
	}
	durationPart := ""
	if cs.Duration != "" {
		durationPart = " (" + cs.Duration + ")"
	}
	runLink := ""
	if repoURL != "" && cs.RunID > 0 && !isJob {
		runLink = fmt.Sprintf(" ([logs](%s/actions/runs/%d))", repoURL, cs.RunID)
	}

	// 提取显示名称
	displayName := cs.WorkflowName
	if strings.HasPrefix(displayName, "job:") {
		displayName = cs.JobName // 使用存储的 job 名称
	}
	displayName = cleanCIStatusDisplayName(displayName)

	if isJob {
		return fmt.Sprintf("↳ %s %s **%s**%s", icon, displayName, statusText, durationPart)
	}
	return fmt.Sprintf("%s %s **%s**%s%s", icon, displayName, statusText, durationPart, runLink)
}

// ciFailed 检查 CI 状态列表中是否有失败的
func ciFailed(statuses []CIStatus) bool {
	for _, cs := range statuses {
		if ciConclusionNeedsAttention(cs.Conclusion) {
			return true
		}
	}
	return false
}

func ciConclusionNeedsAttention(conclusion string) bool {
	switch conclusion {
	case "failure", "cancelled", "timed_out", "startup_failure", "action_required":
		return true
	default:
		return false
	}
}

// makeCIActionButtons 为失败的 CI 事件生成操作按钮
func makeCIActionButtons(statuses []CIStatus, repoURL string) []ActionButton {
	if repoURL == "" {
		return nil
	}
	var btns []ActionButton
	seenRuns := make(map[int64]bool)
	for _, cs := range statuses {
		if ciConclusionNeedsAttention(cs.Conclusion) && cs.RunID > 0 && !seenRuns[cs.RunID] {
			seenRuns[cs.RunID] = true
			displayName := cs.WorkflowName
			if strings.HasPrefix(displayName, "job:") && cs.JobName != "" {
				displayName = cs.JobName
			}
			displayName = cleanCIStatusDisplayName(displayName)
			btns = append(btns, ActionButton{
				Text: fmt.Sprintf("View %s Logs", displayName),
				URL:  fmt.Sprintf("%s/actions/runs/%d", repoURL, cs.RunID),
				Type: "danger",
			})
		}
	}
	if len(btns) > 1 {
		btns = btns[:1]
	}
	if len(btns) > 0 {
		btns = append(btns, ActionButton{
			Text: "View All Workflows",
			URL:  repoURL + "/actions",
			Type: "default",
		})
	}
	return btns
}

// buildSenderText 构建发送者 markdown 文本（支持多作者）
func buildSenderText(sender, senderUrl string, authorLogins []string) string {
	if len(authorLogins) > 1 {
		var links []string
		for _, login := range authorLogins {
			links = append(links, fmt.Sprintf("[%s](https://github.com/%s)", login, login))
		}
		return strings.Join(links, "  ")
	}
	if len(authorLogins) == 1 {
		return fmt.Sprintf("[%s](https://github.com/%s)", authorLogins[0], authorLogins[0])
	}
	return fmt.Sprintf("[%s](%s)", sender, senderUrl)
}

// buildSummaryRow 构建摘要行 column_set：左列 [仓库+分支]，右列 [头像+用户名]
// 删除事件和正常事件共用此逻辑，避免重复代码
func buildSummaryRow(metaText, senderText string, resolvedAvatars []string) map[string]any {
	if len(resolvedAvatars) == 0 {
		// 无头像：纯文本一行
		line := metaText
		if line != "" {
			line += " / "
		}
		line += "👤 " + senderText
		return nil // 调用方应 fallback 到 AddMarkdown
	}

	// 有头像：column_set 左右布局
	// 右列用嵌套 column_set 让头像和用户名水平排列（飞书列内元素默认垂直堆叠）
	avatarCols := make([]any, 0, len(resolvedAvatars))
	for _, key := range resolvedAvatars {
		avatarCols = append(avatarCols, map[string]any{
			"tag":   "column",
			"width": "auto",
			"elements": []any{map[string]any{
				"tag":          "img",
				"img_key":      key,
				"custom_width": 20,
				"mode":         "crop_center",
				"alt":          map[string]string{"tag": "plain_text", "content": "avatar"},
			}},
		})
	}
	// 用户名列
	avatarCols = append(avatarCols, map[string]any{
		"tag":      "column",
		"width":    "auto",
		"elements": []any{map[string]any{"tag": "markdown", "content": senderText}},
	})

	return map[string]any{
		"tag":                "column_set",
		"flex_mode":          "flow",
		"horizontal_spacing": "small",
		"columns": []any{
			map[string]any{
				"tag": "column", "width": "weighted", "weight": 3,
				"vertical_align": "center",
				"elements":       []any{map[string]any{"tag": "markdown", "content": metaText}},
			},
			map[string]any{
				"tag":   "column",
				"width": "weighted", "weight": 2,
				"vertical_align": "center",
				"elements": []any{map[string]any{
					"tag":                "column_set",
					"flex_mode":          "flow",
					"horizontal_spacing": "small",
					"columns":            avatarCols,
				}},
			},
		},
	}
}

// BuildCard 构建符合飞书卡片 V2 规范的消息卡片
func BuildCard(ctx context.Context, repo, sender, senderUrl, avatarUrl string, detail EventDetail) *Card {
	card := NewCard()
	card.Header.Title = CardText{Tag: "plain_text", Content: detail.Title}
	card.Header.Template = GetTemplate(detail.Title)
	repoUrl := detail.RepoURL
	if repoUrl == "" && repo != "" {
		repoUrl = fmt.Sprintf("https://github.com/%s", repo)
	}

	// 删除事件：标题改为仓库名，避免冗余
	if detail.IsDeleted && repo != "" {
		card.Header.Title = CardText{Tag: "plain_text", Content: fmt.Sprintf("%s: %s", strings.SplitN(detail.Title, ":", 2)[0], repo)}
	}

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
		if detail.SHA != "" && repoUrl != "" {
			sha := detail.FullSHA
			if sha == "" {
				sha = detail.SHA
			}
			shaPart = fmt.Sprintf(" ([`%s`](%s/commit/%s))", detail.SHA, repoUrl, sha)
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
	senderText := buildSenderText(sender, senderUrl, detail.AuthorLogins)

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

	// 删除事件：简洁格式
	if detail.IsDeleted {
		tagIcon := "🌿"
		linkPrefix := "/tree/"
		if detail.IsTag {
			tagIcon = "🏷️"
			linkPrefix = "/releases/tag/"
		}

		// 摘要行：仓库名 / 头像 用户名
		repoLine := ""
		if repoUrl != "" {
			repoLine = fmt.Sprintf("[%s](%s)", repo, repoUrl)
		} else if repo != "" {
			repoLine = repo
		}
		if row := buildSummaryRow(repoLine, senderText, resolvedAvatars); row != nil {
			card.Body.Elements = append(card.Body.Elements, row)
		} else {
			card.AddMarkdown(repoLine + " / 👤 " + senderText)
		}

		// 分支/标签列表（用 extractRefs 去重，兼容裸文本和 markdown 格式）
		var refLines []string
		if detail.RefName != "" {
			if detail.RefURL != "" {
				refLines = append(refLines, fmt.Sprintf("%s [%s](%s)", tagIcon, detail.RefName, detail.RefURL))
			} else {
				refLines = append(refLines, fmt.Sprintf("%s %s", tagIcon, detail.RefName))
			}
		} else if detail.Text != "" {
			for _, name := range extractRefs(detail.Text) {
				if repoUrl != "" {
					refLines = append(refLines, fmt.Sprintf("%s [%s](%s%s)", tagIcon, name, repoUrl, linkPrefix+name))
				} else {
					refLines = append(refLines, fmt.Sprintf("%s %s", tagIcon, name))
				}
			}
		}
		if len(refLines) > 0 {
			card.AddMarkdown(strings.Join(refLines, "<br>"))
		}
		btnURL := repoUrl
		if detail.URL != "" {
			btnURL = detail.URL
		}
		if btnURL != "" {
			card.AddActions("flow", ActionButton{Text: "View Details", URL: btnURL, Type: "default"})
		}
	} else {

		// 摘要行：仓库+分支 / 头像+用户名
		if row := buildSummaryRow(metaText, senderText, resolvedAvatars); row != nil {
			card.Body.Elements = append(card.Body.Elements, row)
		} else {
			line := metaText
			if line != "" {
				line += " / "
			}
			line += "👤 " + senderText
			card.AddMarkdown(line)
		}

		// --- 2. 详情内容 ---
		// 有结构化 CI 数据时跳过 detail.Text（避免 workflow 状态重复显示）
		hasCI := len(detail.CIStatuses) > 0
		if detail.Text != "" && !hasCI {
			if detail.Action == "push" && !detail.IsDeleted {
				addPushMarkdown(card, detail)
			} else {
				card.AddMarkdown(detail.Text)
			}
		}

		// --- 2.5 CI 状态（内联到触发事件的卡片）---
		if ciText := renderCIStatuses(detail.CIStatuses, repoUrl); ciText != "" {
			card.AddMarkdown(ciText)
		}
		if detail.Notice != "" {
			card.AddMarkdown(detail.Notice)
		}

		// --- 3. 兼容旧记录中的附加内容，并应用统一折叠门槛 ---
		if detail.FoldableBody != "" {
			card.AddMarkdown(detail.FoldableBody)
		}

		// --- 4. 操作按钮（V2 规范：必须放在 action 容器内）---
		var btns []ActionButton
		if ciFailed(detail.CIStatuses) {
			// CI 失败时只显示 CI 专用按钮，不追加通用 View Details（URL 重复）
			btns = makeCIActionButtons(detail.CIStatuses, repoUrl)
		} else {
			// 非 CI 失败：Push / 删除 / 新建分支等事件不显示详情按钮
			skipBtn := strings.Contains(detail.Title, "commits") ||
				strings.Contains(detail.Title, "Deleted") ||
				strings.Contains(detail.Title, "Created")
			if detail.URL != "" && !skipBtn {
				btnType := "primary"
				if ContainsAny(detail.Title, "❌", "💥") {
					btnType = "danger"
				}
				btns = append(btns, ActionButton{Text: "View Details", URL: detail.URL, Type: btnType})
			}
		}
		if len(btns) > 0 {
			card.AddActions("flow", btns...)
		}
	} // end of non-delete rendering

	// --- 5. 事件发生时间（用飞书 <local_datetime> 自动适配用户时区）---
	// format_type 枚举: date_num(2026/6/3) date_short(6月3日) date(2026年6月3日)
	//   week/week_short(星期三/周三) time(14:05) time_sec(14:05:09) timezone(14:05:09 GMT+8)
	if detail.EventTime != "" {
		if t, err := time.Parse(time.RFC3339, detail.EventTime); err == nil {
			ms := t.UnixMilli()
			timeTag := fmt.Sprintf(`<local_datetime millisecond="%d" format_type="date"></local_datetime> <local_datetime millisecond="%d" format_type="time_sec"></local_datetime>`, ms, ms)
			// 合并事件显示时间范围
			if detail.EventTimeEnd != "" {
				if t2, err2 := time.Parse(time.RFC3339, detail.EventTimeEnd); err2 == nil {
					ms2 := t2.UnixMilli()
					timeTag += " ~ " + fmt.Sprintf(`<local_datetime millisecond="%d" format_type="time_sec"></local_datetime>`, ms2)
				}
			}
			card.AddMarkdown(fmt.Sprintf("🕐 %s", timeTag))
		}
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

// extractRefs 从删除事件的 detail.Text 中提取纯分支/标签名列表，
// 同时兼容裸文本（"feat/foo"）和 markdown 格式（"🌿 [feat/foo](url)"）。
func extractRefs(text string) []string {
	var names []string
	seen := make(map[string]bool)
	for _, line := range strings.Split(text, "\n") {
		name := extractRefName(line)
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// extractRefName 从一行文本中提取分支/标签名，兼容裸文本和 markdown 链接格式。
// "feat/foo" → "feat/foo"
// "🌿 [feat/foo](https://...)" → "feat/foo"
// "🌿 feat/foo" → "feat/foo"
func extractRefName(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	// 去掉开头的 emoji（🌿 🏷️ 等）
	for len(line) > 0 {
		r, size := utf8.DecodeRuneInString(line)
		if r > 0x7F { // 非 ASCII（emoji 或全角字符）
			line = strings.TrimSpace(line[size:])
		} else {
			break
		}
	}
	// markdown 链接格式：[name](url) — 提取 [] 内的内容
	if idx := strings.Index(line, "["); idx >= 0 {
		if end := strings.Index(line[idx:], "]"); end >= 0 {
			name := strings.TrimSpace(line[idx+1 : idx+end])
			if name != "" {
				return name
			}
		}
	}
	return strings.TrimSpace(line)
}

// escapeCodeHTML 保留行内和围栏代码块的反引号，并转义其中的 HTML 标签，
// 防止代码里的 <br> 等内容被飞书当作标签解析。
func escapeCodeHTML(s string) string {
	var buf strings.Builder
	i := 0
	n := len(s)

	for i < n {
		// 围栏代码块：```...```
		if i+2 < n && s[i] == '`' && s[i+1] == '`' && s[i+2] == '`' {
			end := strings.Index(s[i+3:], "```")
			if end >= 0 {
				buf.WriteString("```")
				code := s[i+3 : i+3+end]
				code = strings.ReplaceAll(code, "<", "＜")
				code = strings.ReplaceAll(code, ">", "＞")
				buf.WriteString(code)
				buf.WriteString("```")
				i = i + 3 + end + 3
				continue
			}
		}
		// 行内代码：`...`
		if s[i] == '`' {
			end := strings.Index(s[i+1:], "`")
			if end >= 0 {
				buf.WriteByte('`')
				code := s[i+1 : i+1+end]
				code = strings.ReplaceAll(code, "<", "＜")
				code = strings.ReplaceAll(code, ">", "＞")
				buf.WriteString(code)
				buf.WriteByte('`')
				i = i + 1 + end + 1
				continue
			}
		}
		buf.WriteByte(s[i])
		i++
	}
	return buf.String()
}

var conventionalRegex = regexp.MustCompile(`(?i)(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert|ref)(\([^)]+\))?(!?):`)
var shaRegex = regexp.MustCompile(`\b([0-9a-f]{7,40})\b`)
var issueRegex = regexp.MustCompile(`(?i)(?:^|[\s,.\-=(])#(\d+)\b`)
var prMergeRegex = regexp.MustCompile(`(?i)Merge pull request #(\d+)`)

// Linkify 将文本中的 SHA 哈希和 #123 转换为 GitHub 链接
func Linkify(text, repoUrl string) string {
	if repoUrl == "" {
		return text
	}

	// 1. 处理 SHA 哈希 (7-40位 16 进制)
	text = shaRegex.ReplaceAllStringFunc(text, func(sha string) string {
		// 启发式校验 SHA: 如果主要是数字且很短，则可能是版本号或其他 ID
		hasLetter := false
		for _, r := range sha {
			if (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F') {
				hasLetter = true
				break
			}
		}
		// 长度较短且不含字母（如 1234567）可能是地块价格或其他 ID，跳过
		if !hasLetter && len(sha) < 10 {
			return sha
		}

		displaySHA := sha
		if len(sha) > 7 {
			displaySHA = sha[:7]
		}
		return fmt.Sprintf("[%s](%s/commit/%s)", displaySHA, repoUrl, sha)
	})

	// 2. 处理 Issue/PR 引用 (#123)
	text = issueRegex.ReplaceAllStringFunc(text, func(match string) string {
		idx := strings.Index(match, "#")
		if idx == -1 {
			return match
		}
		prefix := match[:idx]
		number := match[idx+1:]
		return fmt.Sprintf("%s[#%s](%s/issues/%s)", prefix, number, repoUrl, number)
	})

	return text
}

// containsMarkdownList 检测字符串中是否包含 markdown 无序或有序列表语法
// 匹配行首（或 \n 后）有零或多个空格/制表符，后跟 - * + 或 数字. 及空格
var markdownListRe = regexp.MustCompile(`(?:^|\n)[ \t]*(?:[-*+]|\d+\.)[ \t]+`)

func containsMarkdownList(s string) bool {
	return markdownListRe.MatchString(s)
}

var markdownHeadingRe = regexp.MustCompile(`^[ \t]{0,3}#{1,6}[ \t]+(.+?)(?:[ \t]+#+[ \t]*)?$`)
var workflowTitlePrefixRe = regexp.MustCompile(`(?i)^Workflow (Succeeded|Failed|Running|Started|Requested|Queued|Waiting|Completed|Cancelled|Canceled|Timed[_ ]?Out|Neutral|Skipped|Action[_ ]?Required|Startup[_ ]?Failure|Stale):\s*`)

// ProcessCommitMessage 处理提交信息，转换 emoji、规范 Conventional Commit 前缀空格，并转换 SHA/Issue 为链接
func ProcessCommitMessage(msg string, repoUrl string) string {
	msg = strings.TrimSpace(msg)
	// 1. 转换 Emoji 短代码
	msg = emoji.Sprint(msg)

	// 2. 转换 SHA 和 #Issue
	if repoUrl != "" {
		msg = Linkify(msg, repoUrl)
	}

	// 3. 规范 Conventional Commit 前缀后的空格
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

		// 保持提交行普通字号，避免飞书将第一条 commit 渲染得过重。
		result.WriteString(msg[start:end])

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

// htmlToMarkdown 将 HTML 内容转换为飞书卡片 markdown 组件支持的纯 markdown 语法。
// 飞书卡片的 markdown 标签不支持原始 HTML，只支持标准 markdown 语法。
func htmlToMarkdown(s string) string {
	// Step 1: 内联标签转换（顺序重要：先处理内层标签）
	s = convertInlineTags(s)

	// Step 2: 表格转换（单元格内容是已经转换过的 markdown）
	s = convertHTMLTables(s)

	// Step 3: 块级元素转换
	s = convertBlockTags(s)

	// Step 4: 移除残留的 HTML 标签
	s = reStripHTML.ReplaceAllString(s, "")

	// Step 5: 清理多余空白
	s = reMultiNewline.ReplaceAllString(s, "\n\n")
	s = strings.TrimSpace(s)

	return s
}

var (
	reBr           = regexp.MustCompile(`(?is)<br\s*/?>`)
	reStrong       = regexp.MustCompile(`(?is)<(strong|b)\s*>(.*?)</(strong|b)>`)
	reEm           = regexp.MustCompile(`(?is)<(em|i)\s*>(.*?)</(em|i)>`)
	reCode         = regexp.MustCompile(`(?is)<code\s*>(.*?)</code>`)
	reDel          = regexp.MustCompile(`(?is)<(del|s|strike)\s*>(.*?)</(del|s|strike)>`)
	reA            = regexp.MustCompile(`(?is)<a\s+[^>]*href=["']([^"']*)["'][^>]*>(.*?)</a>`)
	reImgAltSrc    = regexp.MustCompile(`(?is)<img\s+[^>]*alt=["']([^"']*)["'][^>]*src=["']([^"']*)["'][^>]*/?>`)
	reImgSrcAlt    = regexp.MustCompile(`(?is)<img\s+[^>]*src=["']([^"']*)["'][^>]*alt=["']([^"']*)["'][^>]*/?>`)
	reImgSrcOnly   = regexp.MustCompile(`(?is)<img\s+[^>]*src=["']([^"']*)["'][^>]*/?>`)
	reTable        = regexp.MustCompile(`(?is)<table.*?>(.*?)</table>`)
	reTr           = regexp.MustCompile(`(?is)<tr.*?>(.*?)</tr>`)
	reTd           = regexp.MustCompile(`(?is)<t[dh].*?>(.*?)</t[dh]>`)
	reP            = regexp.MustCompile(`(?is)<p\b[^>]*>(.*?)</p>`)
	reHeading      = regexp.MustCompile(`(?is)<h[1-6]\b[^>]*>(.*?)</h[1-6]>`)
	reLi           = regexp.MustCompile(`(?is)<li\b[^>]*>(.*?)</li>`)
	reBq           = regexp.MustCompile(`(?is)<blockquote\b[^>]*>(.*?)</blockquote>`)
	reHr           = regexp.MustCompile(`(?is)<hr\s*/?>`)
	reStripHTML    = regexp.MustCompile(`(?s)<[^>]*>`)
	reMultiNewline = regexp.MustCompile(`\n{3,}`)
	reDetails      = regexp.MustCompile(`(?is)<details.*?>\s*<summary.*?>(.*?)</summary>(.*?)</details>`)
)

func convertInlineTags(s string) string {
	s = reBr.ReplaceAllString(s, "\n")
	s = reStrong.ReplaceAllString(s, "**$2**")
	s = reEm.ReplaceAllString(s, "*$2*")
	s = reCode.ReplaceAllString(s, "`$1`")
	s = reDel.ReplaceAllString(s, "~~$2~~")
	s = reA.ReplaceAllString(s, "[$2]($1)")
	s = reImgAltSrc.ReplaceAllString(s, "$1")
	s = reImgSrcAlt.ReplaceAllString(s, "$2")
	s = reImgSrcOnly.ReplaceAllString(s, "[image]($1)")
	return s
}

func convertHTMLTables(s string) string {
	return reTable.ReplaceAllStringFunc(s, func(match string) string {
		var rows [][]string
		for _, trMatch := range reTr.FindAllStringSubmatch(match, -1) {
			var cells []string
			for _, tdMatch := range reTd.FindAllStringSubmatch(trMatch[1], -1) {
				cells = append(cells, strings.TrimSpace(tdMatch[1]))
			}
			if len(cells) > 0 {
				rows = append(rows, cells)
			}
		}
		if len(rows) == 0 {
			return ""
		}

		maxCols := 0
		for _, row := range rows {
			if len(row) > maxCols {
				maxCols = len(row)
			}
		}

		// 单列表格 → 项目列表
		if maxCols == 1 {
			var items []string
			for _, row := range rows {
				items = append(items, "• "+row[0])
			}
			return strings.Join(items, "\n")
		}

		// 多列表格 → markdown 表格
		var lines []string
		for i, row := range rows {
			for len(row) < maxCols {
				row = append(row, "")
			}
			lines = append(lines, "| "+strings.Join(row, " | ")+" |")
			if i == 0 {
				seps := make([]string, maxCols)
				for j := range seps {
					seps[j] = "---"
				}
				lines = append(lines, "| "+strings.Join(seps, " | ")+" |")
			}
		}
		return strings.Join(lines, "\n")
	})
}

func convertBlockTags(s string) string {
	s = reP.ReplaceAllString(s, "$1\n\n")
	s = reHeading.ReplaceAllStringFunc(s, func(m string) string {
		match := reHeading.FindStringSubmatch(m)
		if len(match) > 1 {
			return "\n" + strings.TrimSpace(match[1]) + "\n"
		}
		return m
	})
	s = reLi.ReplaceAllString(s, "- $1\n")
	s = reBq.ReplaceAllString(s, "> $1")
	s = reHr.ReplaceAllString(s, "\n---\n")
	return s
}

// ProcessGithubMarkdown 转换 GitHub Markdown 为飞书卡片 Markdown。
// GitHub 自带的 details 也先展开为普通内容，最终统一按 5+5 行门槛折叠。
func ProcessGithubMarkdown(s string) (text string, foldable string) {
	if s == "" {
		return "", ""
	}

	// 1. 预处理 Markdown：外部文本中的字面 \n 还原为换行，标题降级为普通文本。
	s = normalizeLiteralNewlinesOutsideFences(s)
	s = normalizeMarkdownHeadings(s)
	s = strings.ReplaceAll(s, "```mermaid", "```")

	// 2. 将 <details> <summary> 还原为普通 Markdown，交由卡片统一决定是否折叠。
	processed := reDetails.ReplaceAllStringFunc(s, func(m string) string {
		match := reDetails.FindStringSubmatch(m)
		if len(match) > 2 {
			title := strings.TrimSpace(match[1])
			title = reStripHTML.ReplaceAllString(title, "")

			content := strings.TrimSpace(match[2])
			content = htmlToMarkdown(content)
			content = reMultiNewline.ReplaceAllString(content, "\n\n")

			return fmt.Sprintf("**%s**\n%s", title, strings.TrimSpace(content))
		}
		return ""
	})

	// 3. HTML 转 Markdown
	processed = htmlToMarkdown(processed)
	processed = strings.TrimSpace(processed)

	// 4. 安全截断
	text = SafeText(processed, 50000)
	return text, ""
}

type markdownLine struct {
	text string
	sep  string
}

func splitMarkdownLogicalLines(s string) []markdownLine {
	var lines []markdownLine
	for len(s) > 0 {
		idx, sep, sepLen := nextMarkdownLineBreak(s)
		if idx < 0 {
			lines = append(lines, markdownLine{text: s})
			break
		}
		lines = append(lines, markdownLine{text: s[:idx], sep: sep})
		s = s[idx+sepLen:]
	}
	return lines
}

func nextMarkdownLineBreak(s string) (idx int, sep string, sepLen int) {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\r':
			if i+1 < len(s) && s[i+1] == '\n' {
				return i, "\r\n", 2
			}
			return i, "\r", 1
		case '\n':
			return i, "\n", 1
		case '\\':
			if isEscapedBackslash(s, i) {
				continue
			}
			if i+3 < len(s) && s[i+1] == 'r' && s[i+2] == '\\' && s[i+3] == 'n' {
				return i, `\r\n`, 4
			}
			if i+1 < len(s) && s[i+1] == 'n' {
				return i, `\n`, 2
			}
		}
	}
	return -1, "", 0
}

func isEscapedBackslash(s string, idx int) bool {
	count := 0
	for i := idx - 1; i >= 0 && s[i] == '\\'; i-- {
		count++
	}
	return count%2 == 1
}

func isFenceLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

func normalizeLiteralNewlinesOutsideFences(s string) string {
	lines := splitMarkdownLogicalLines(s)
	var out strings.Builder
	inFence := false
	for _, line := range lines {
		wasInFence := inFence
		fence := isFenceLine(line.text)
		if fence && wasInFence {
			inFence = false
		}
		out.WriteString(line.text)
		if line.sep != "" {
			if inFence {
				out.WriteString(line.sep)
			} else {
				out.WriteString("\n")
			}
		}
		if fence && !wasInFence {
			inFence = true
		}
	}
	return out.String()
}

func normalizeMarkdownHeadings(s string) string {
	lines := splitMarkdownLogicalLines(s)
	var out strings.Builder
	inFence := false
	for _, line := range lines {
		wasInFence := inFence
		fence := isFenceLine(line.text)
		if fence && wasInFence {
			inFence = false
		}
		text := line.text
		if !inFence {
			if match := markdownHeadingRe.FindStringSubmatch(text); len(match) == 2 {
				text = strings.TrimSpace(match[1])
			}
		}
		out.WriteString(text)
		out.WriteString(line.sep)
		if fence && !wasInFence {
			inFence = true
		}
	}
	return out.String()
}

// GetDiffOnlyAdded 生成仅包含新增内容的 Diff
func GetDiffOnlyAdded(old, newStr string) string {
	if old == "" {
		return newStr
	}

	oldLines := strings.Split(old, "\n")
	oldMap := make(map[string]bool)
	for _, l := range oldLines {
		oldMap[l] = true
	}

	newLines := strings.Split(newStr, "\n")
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
				avatar = fmt.Sprintf("https://avatars.githubusercontent.com/%s", login)
			}

			authors = append(authors, AuthorInfo{Name: name, Login: login, Avatar: avatar})
		}
	}
	return authors
}
