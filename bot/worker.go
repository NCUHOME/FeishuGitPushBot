package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"strings"
	"sync"

	"github.com/google/go-github/v84/github"
)

// StartWorker 启动消息队列处理工作者和图片刷新任务
func StartWorker() {
	if DB == nil {
		slog.Warn("Database not initialized, message worker will not start")
		return
	}

	// 1. 消息推送工作者
	go messageWorker()

	// 2. 图片异步刷新任务
	go imageRefreshWorker()
}

func messageWorker() {
	slog.Info("Message worker started")
	for {
		// 每次取一条待处理的消息：
		// 1. 状态为 pending
		// 2. 状态为 failed 且重试次数 < 5，且距离上次更新已过去一定时间 (简单指数退避)
		var event WebhookEvent
		err := DB.NewSelect().Model(&event).
			Where("status = ?", "pending").
			WhereOr("status = ? AND retry_count < 5 AND updated_at < ?", "failed", time.Now().Add(-1*time.Minute)).
			Order("id ASC").
			Limit(1).
			Scan(context.Background())

		if err != nil {
			// 如果没消息，歇会儿
			time.Sleep(2 * time.Second)
			continue
		}

		// 标记为处理中
		_, _ = DB.NewUpdate().Model(&event).Set("status = ?", "processing").WherePK().Exec(context.Background())

		err = processWebhookEvent(event)
		if err != nil {
			slog.Error("Failed to process Webhook event", "id", event.ID, "error", err)
			_, _ = DB.NewUpdate().Model(&event).
				Set("status = ?", "failed").
				Set("retry_count = retry_count + 1").
				Set("updated_at = ?", time.Now()).
				WherePK().Exec(context.Background())
		} else {
			// 检查事件是否已被 processWebhookEvent 重新排队（如 CI reschedule）
			var current WebhookEvent
			if err := DB.NewSelect().Model(&current).Where("id = ?", event.ID).Column("status").Scan(context.Background()); err == nil && current.Status == "pending" {
				// 已被重新排队，跳过 processed 标记
			} else {
				_, _ = DB.NewUpdate().Model(&event).
					Set("status = ?", "processed").
					Set("updated_at = ?", time.Now()).
					WherePK().Exec(context.Background())
			}
		}

		// 推送间隔，保证节奏
		time.Sleep(1 * time.Second)
	}
}

// getMergeWindow 返回配置的事件合并窗口时长
func getMergeWindow() time.Duration {
	return time.Duration(C.Events.MergeWindow) * time.Minute
}

// getThreadReplyWindow 返回配置的话题回复窗口时长
func getThreadReplyWindow() time.Duration {
	return time.Duration(C.Events.ThreadReplyWindow) * time.Minute
}

// mergeSearch 定义查找已有消息记录的搜索条件
// githubID / githubIDLike / githubIDLikes 为 OR 关系，eventType / withinWindow / recordType 为 AND 关系
type mergeSearch struct {
	githubID      string   // github_id 精确匹配
	githubIDLike  string   // github_id LIKE 模式匹配（单个）
	githubIDLikes []string // github_id LIKE 模式匹配（多个，OR 关系）
	eventType     string   // event_type 精确匹配（空值表示不筛选）
	recordType    string   // record_type 精确匹配（空值表示不筛选）
	withinWindow  bool     // 是否应用合并窗口时间过滤
}

// tryMergeWithExisting 尝试查找已有消息记录并合并/更新
// search: 搜索条件
// mergeFn: 合并策略，参数 (old, new *EventDetail)，可就地修改 new
// headSHA: 非空时更新记录的 head_sha 字段（用于 push 合并后保持 SHA 关联）
// 返回 (merged bool, err error)，merged=true 时调用方应立即返回
func tryMergeWithExisting(
	ctx context.Context,
	search mergeSearch,
	mergeFn func(old, new *EventDetail),
	headSHA string,
	detail *EventDetail,
	repo, repoUrl, sender, senderUrl, avatarUrl, logMsg string,
) (bool, error) {
	var record MessageRecord
	q := DB.NewSelect().Model(&record)

	// 构建 github_id 的 OR 条件
	var idClauses []string
	var idArgs []any
	if search.githubID != "" {
		idClauses = append(idClauses, "github_id = ?")
		idArgs = append(idArgs, search.githubID)
	}
	if search.githubIDLike != "" {
		idClauses = append(idClauses, "github_id LIKE ?")
		idArgs = append(idArgs, search.githubIDLike)
	}
	for _, like := range search.githubIDLikes {
		idClauses = append(idClauses, "github_id LIKE ?")
		idArgs = append(idArgs, like)
	}
	if len(idClauses) > 0 {
		q = q.Where(strings.Join(idClauses, " OR "), idArgs...)
	}
	if search.eventType != "" {
		q = q.Where("event_type = ?", search.eventType)
	}
	if search.recordType != "" {
		q = q.Where("record_type = ?", search.recordType)
	}
	if search.withinWindow {
		q = q.Where("updated_at > ?", time.Now().Add(-getMergeWindow()))
	}

	if err := q.Order("id DESC").Limit(1).Scan(ctx); err != nil {
		return false, nil // 未找到可合并的记录
	}

	// 合并内容
	var prevDetail EventDetail
	_ = json.Unmarshal([]byte(record.Content), &prevDetail)
	mergeFn(&prevDetail, detail)

	// 构建并更新卡片
	buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
	card := BuildCard(buildCtx, repo, sender, senderUrl, avatarUrl, *detail)
	buildCancel()

	if err := UpdateMessage(record.FeishuMessageID, card); err != nil {
		return false, fmt.Errorf("failed to update message: %w", err)
	}

	// 更新数据库记录
	detailJson, _ := json.Marshal(detail)
	updateQ := DB.NewUpdate().Model(&record).
		Set("content = ?", string(detailJson)).
		Set("card_string = ?", card.String()).
		Set("updated_at = ?", time.Now()).
		WherePK()
	if headSHA != "" {
		updateQ = updateQ.Set("head_sha = ?", headSHA)
	}
	_, _ = updateQ.Exec(ctx)

	slog.Info(logMsg, "github_id", record.GithubID)
	return true, nil
}

// tryDedup 尝试去重：如果合并窗口内已有相同 githubID + eventType 的记录，
// 静默更新内容并跳过发送新消息。返回 true 表示已去重（调用方应立即返回）。
func tryDedup(ctx context.Context, githubID, eventType string, detail *EventDetail) bool {
	var record MessageRecord
	if err := DB.NewSelect().Model(&record).
		Where("github_id = ?", githubID).
		Where("event_type = ?", eventType).
		Where("updated_at > ?", time.Now().Add(-getMergeWindow())).
		Order("id DESC").Limit(1).Scan(ctx); err != nil {
		return false
	}
	detailJson, _ := json.Marshal(detail)
	_, _ = DB.NewUpdate().Model(&record).
		Set("content = ?", string(detailJson)).
		Set("updated_at = ?", time.Now()).
		WherePK().Exec(ctx)
	slog.Info("Event deduplicated", "github_id", githubID, "event_type", eventType)
	return true
}

// extractCIStatus 从 CI 事件负载中提取 status 和 conclusion
func extractCIStatus(m map[string]any, eventType string) (status, conclusion string) {
	switch eventType {
	case "workflow_run":
		return ext(m, "workflow_run", "status"), ext(m, "workflow_run", "conclusion")
	case "workflow_job":
		return ext(m, "workflow_job", "status"), ext(m, "workflow_job", "conclusion")
	case "check_run":
		return ext(m, "check_run", "status"), ext(m, "check_run", "conclusion")
	case "check_suite":
		return ext(m, "check_suite", "status"), ext(m, "check_suite", "conclusion")
	}
	return "", ""
}

// sendTimeoutNotification 发送 Workflow 超时提醒回复
func sendTimeoutNotification(parentMsgID, title string, startedAt time.Time) {
	timeoutCard := NewCard()
	timeoutCard.Header.Title = CardText{Tag: "plain_text", Content: "⏰ Workflow 运行超时提醒"}
	timeoutCard.Header.Template = "orange"
	duration := time.Since(startedAt).Round(time.Minute)
	timeoutCard.AddMarkdown(fmt.Sprintf("**%s** 已经运行 **%s**，请检查是否卡住", title, duration))
	if _, err := ReplyToMessage(parentMsgID, timeoutCard); err != nil {
		slog.Error("Failed to send timeout notification", "error", err)
	}
}

// mergeRefs 合并两个删除事件的 Text，去重后返回纯分支/标签名列表（每行一个）。
// 兼容裸文本（"feat/foo"）和 markdown 格式（"🌿 [feat/foo](url)"）。
func mergeRefs(oldText, newText string) string {
	names := extractRefs(oldText)
	seen := make(map[string]bool)
	for _, n := range names {
		seen[n] = true
	}
	for _, n := range extractRefs(newText) {
		if !seen[n] {
			names = append(names, n)
		}
	}
	return strings.Join(names, "\n")
}

// findParentRecordBySHA 根据 commit SHA 查找同一仓库下 push/create 事件的消息记录
// 排除已删除的消息记录，避免 CI 事件错误关联到分支删除记录
func findParentRecordBySHA(ctx context.Context, repo, sha string) *MessageRecord {
	if sha == "" || repo == "" {
		return nil
	}
	var record MessageRecord
	if err := DB.NewSelect().Model(&record).
		Where("repo_name = ?", repo).
		Where("event_type IN ('push', 'create')").
		Where("head_sha = ?", sha).
		Where("record_type != 'deleted'").
		Order("id ASC").Limit(1).Scan(ctx); err == nil {
		return &record
	}
	return nil
}

// findRecentRepoTag 查找同一仓库最近的 tag 创建消息（用于 CI 事件关联，tag 的 head_sha 可能为空）
// 排除已删除的消息记录
func findRecentRepoTag(ctx context.Context, repo string) *MessageRecord {
	if repo == "" {
		return nil
	}
	var record MessageRecord
	if err := DB.NewSelect().Model(&record).
		Where("repo_name = ?", repo).
		Where("event_type = ?", "create").
		Where("record_type != 'deleted'").
		Where("updated_at > ?", time.Now().Add(-getMergeWindow())).
		Order("id DESC").Limit(1).Scan(ctx); err == nil {
		return &record
	}
	return nil
}

// findRecentRepoPush 查找同一仓库最近的推送消息（用于 tag/workflow 关联 commit）
// 排除已删除的消息记录
func findRecentRepoPush(ctx context.Context, repo string) string {
	if repo == "" {
		return ""
	}
	var record MessageRecord
	if err := DB.NewSelect().Model(&record).
		Where("repo_name = ?", repo).
		Where("event_type IN ('push', 'create')").
		Where("record_type != 'deleted'").
		Where("updated_at > ?", time.Now().Add(-getMergeWindow())).
		Order("id DESC").Limit(1).Scan(ctx); err == nil {
		return record.FeishuMessageID
	}
	return ""
}

// updateParentCardWithCI 查询父消息关联的所有 CI 状态，更新父消息卡片
func updateParentCardWithCI(ctx context.Context, parentMsgID string) {
	if parentMsgID == "" || DB == nil {
		return
	}
	var parentRecord MessageRecord
	if err := DB.NewSelect().Model(&parentRecord).
		Where("feishu_message_id = ?", parentMsgID).
		Where("event_type NOT IN ('workflow_run', 'workflow_job', 'check_run', 'check_suite')").
		Order("id ASC").Limit(1).Scan(ctx); err != nil {
		return
	}
	var parentDetail EventDetail
	_ = json.Unmarshal([]byte(parentRecord.Content), &parentDetail)
	// 兼容旧记录：回填 RepoURL
	if parentDetail.RepoURL == "" && parentRecord.RepoName != "" {
		parentDetail.RepoURL = fmt.Sprintf("https://github.com/%s", parentRecord.RepoName)
	}

	// 查询所有关联的 CI 记录
	statuses := getCIStatusesForParent(ctx, parentMsgID)
	parentDetail.CIStatuses = statuses

	buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
	card := BuildCard(buildCtx, parentRecord.RepoName, parentRecord.Sender, parentRecord.SenderURL, parentRecord.AvatarURL2, parentDetail)
	buildCancel()

	if err := UpdateMessage(parentMsgID, card); err != nil {
		slog.Error("Failed to update parent card with CI status", "parent", parentMsgID, "error", err)
		return
	}
	detailJson, _ := json.Marshal(parentDetail)
	_, _ = DB.NewUpdate().Model(&parentRecord).
		Set("content = ?", string(detailJson)).
		Set("card_string = ?", card.String()).
		Set("updated_at = ?", time.Now()).
		WherePK().Exec(ctx)
}

// detectPRMerge 检测 push 是否为 PR 合并，如果是则更新 PR 卡片而非创建新的 push 消息
// 返回 true 表示已处理（调用方应立即返回）
func detectPRMerge(ctx context.Context, event WebhookEvent, m map[string]any, detail *EventDetail,
	repo, repoUrl, sender, senderUrl, avatarUrl, sha string) bool {
	if event.EventType != "push" || detail.IsTag || detail.IsDeleted {
		return false
	}
	hc, _ := m["head_commit"].(map[string]any)
	if hc == nil {
		return false
	}
	msg, _ := hc["message"].(string)
	prNum := extractPRNumber(msg)
	if prNum == "" {
		return false
	}

	// 查找对应的 PR 消息记录
	prGithubID := fmt.Sprintf("pr:%s:%s", repo, prNum)
	var prRecord MessageRecord
	if err := DB.NewSelect().Model(&prRecord).
		Where("github_id = ?", prGithubID).
		Order("id DESC").Limit(1).Scan(ctx); err != nil {
		return false
	}

	// 合并 commit 信息到 PR 卡片
	var prDetail EventDetail
	_ = json.Unmarshal([]byte(prRecord.Content), &prDetail)
	if detail.Text != "" {
		if prDetail.Text != "" {
			prDetail.Text += "\n"
		}
		prDetail.Text += detail.Text
	}
	if detail.EventTime != "" {
		prDetail.EventTimeEnd = detail.EventTime
	}
	prDetail.RepoURL = repoUrl

	buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
	card := BuildCard(buildCtx, repo, sender, senderUrl, avatarUrl, prDetail)
	buildCancel()

	if err := UpdateMessage(prRecord.FeishuMessageID, card); err != nil {
		slog.Error("Failed to update PR card with merge commits", "pr", prGithubID, "error", err)
		return false
	}

	detailJson, _ := json.Marshal(prDetail)
	_, _ = DB.NewUpdate().Model(&prRecord).
		Set("content = ?", string(detailJson)).
		Set("card_string = ?", card.String()).
		Set("updated_at = ?", time.Now()).
		WherePK().Exec(ctx)

	slog.Info("PR merge detected, commits added to PR card", "pr", prGithubID)

	// 回填 head_sha 到 PR 记录
	if sha != "" {
		_, _ = DB.NewUpdate().Model(&prRecord).
			Set("head_sha = ?", sha).
			WherePK().Exec(ctx)
	}
	return true
}

func processWebhookEvent(event WebhookEvent) error {
	ctx := context.Background()
	var parentMsgID string // CI 事件关联的父消息 ID

	// 1. 解析 Payload
	payload := []byte(event.Payload)
	githubEvent, err := github.ParseWebHook(event.EventType, payload)
	if err != nil {
		return fmt.Errorf("failed to parse Webhook: %w", err)
	}

	// 跳过不需要通知的管理员/基础设施事件
	adminEvents := map[string]bool{
		"ping": true, "meta": true, "installation": true,
		"installation_repositories": true, "installation_target": true,
		"github_app_authorization": true, "marketplace_purchase": true,
		"registry_package": true, "content_reference": true,
		"personal_access_token_request": true, "custom_property": true,
		"custom_property_values": true, "repository_import": true,
		"security_and_analysis": true, "secret_scanning_alert_location": true,
		"deployment_protection_rule": true, "deployment_review": true,
		"user": true, "org_block": true, "sponsorship": true,
	}
	if adminEvents[event.EventType] {
		return nil
	}

	detail := ParseEvent(githubEvent, event.EventType)
	if detail.EventTime == "" && !event.CreatedAt.IsZero() {
		detail.EventTime = event.CreatedAt.Format(time.RFC3339)
	}
	if detail.Skip {
		return nil
	}

	// 2. 获取基本元数据
	var m map[string]any
	_ = json.Unmarshal(payload, &m)
	repo := ext(m, "repository", "full_name")
	repoUrl := ext(m, "repository", "html_url")
	sender := ext(m, "sender", "login")
	senderUrl := ext(m, "sender", "html_url")
	avatarUrl := ext(m, "sender", "avatar_url")
	detail.RepoName = repo    // 用于合并展示的仓库全名
	detail.RepoURL = repoUrl  // 用于 BuildCard 构建链接
	ref := ext(m, "ref")
	// Workflow 事件的 ref 在 head_branch 中
	if ref == "" {
		ref = ext(m, "workflow_run", "head_branch")
	}
	if ref == "" {
		ref = ext(m, "workflow_job", "head_branch")
	}

	// 检查是否为 Bot 用户
	isBotUser := false
	if C.Github.BotUsers != "" && sender != "" {
		// 简单的缓存或直接字符串包含检查即可，不需要每次都 Split
		if strings.Contains(","+C.Github.BotUsers+",", ","+sender+",") {
			isBotUser = true
		}
	}
	// Bot 用户只处理 PR 和 Comment 事件，其他一律跳过
	if isBotUser {
		isBotAllowed := event.EventType == "pull_request" ||
			event.EventType == "pull_request_review" ||
			event.EventType == "pull_request_review_comment" ||
			event.EventType == "issue_comment" ||
			event.EventType == "issues"
		if !isBotAllowed {
			slog.Info("Bot user event skipped", "sender", sender, "event", event.EventType)
			return nil
		}
	}

	// 2.1 提取 SHA (用于后续寻找父消息或更新原本的推送)
	sha := ext(m, "head_commit", "id")
	// push 事件：head_commit 可能缺失（如 force push、payload 裁剪），回退到 after 字段
	if sha == "" && event.EventType == "push" {
		if after := ext(m, "after"); after != "" && !strings.HasPrefix(after, "0000000000") {
			sha = after
		}
	}
	if sha == "" {
		sha = ext(m, "pull_request", "head", "sha")
	}
	if sha == "" {
		sha = ext(m, "workflow_run", "head_sha")
	}
	if sha == "" {
		sha = ext(m, "workflow_job", "head_sha")
	}
	if sha == "" {
		sha = ext(m, "check_run", "head_sha")
	}
	if sha == "" {
		sha = ext(m, "check_suite", "head_sha")
	}
	// create 事件（tag 创建）payload 不含 SHA，从已入库的同标签 push 事件获取
	if sha == "" && event.EventType == "create" && ext(m, "ref_type") == "tag" {
		tagName := ext(m, "ref")
		var pushRecord MessageRecord
		if err := DB.NewSelect().Model(&pushRecord).
			Where("repo_name = ?", repo).
			Where("event_type = ?", "push").
			Where("ref = ?", "refs/tags/"+tagName).
			Where("head_sha != ''").
			Order("id DESC").Limit(1).Scan(ctx); err == nil {
			sha = pushRecord.HeadSHA
		}
	}

	// 3. 构建追踪 ID
	var githubID string
	switch event.EventType {
	case "workflow_run":
		githubID = "wf:" + ext(m, "workflow_run", "id")
	case "workflow_job":
		// 每个 Job 使用独立 ID 追踪，避免覆盖 workflow_run 或其他 job 的记录
		githubID = "wfjob:" + ext(m, "workflow_job", "id")
	case "check_run":
		// 使用 check_suite.id 进行统一追踪
		githubID = "wf:" + ext(m, "check_run", "check_suite", "id")
	case "check_suite":
		githubID = "wf:" + ext(m, "check_suite", "id")
	case "push":
		githubID = fmt.Sprintf("push:%s:%s", repo, ref)
	case "create":
		// 创建事件：区分 tag 和 branch
		refType := ext(m, "ref_type")
		eventRef := ext(m, "ref")
		githubID = fmt.Sprintf("create:%s:%s:%s", repo, refType, eventRef)
	case "delete":
		// 删除事件：区分 tag 和 branch
		refType := ext(m, "ref_type")
		eventRef := ext(m, "ref")
		githubID = fmt.Sprintf("delete:%s:%s:%s", repo, refType, eventRef)
	case "release":
		// release 事件按 tag 区分，支持更新
		githubID = fmt.Sprintf("release:%s:%s", repo, ext(m, "release", "tag_name"))
	case "pull_request":
		githubID = fmt.Sprintf("pr:%s:%s", repo, ext(m, "pull_request", "number"))
	case "issues":
		githubID = fmt.Sprintf("issue:%s:%s", repo, ext(m, "issue", "number"))
	case "issue_comment":
		githubID = fmt.Sprintf("comment:%s:%s", repo, ext(m, "issue", "number"))
	case "pull_request_review_comment":
		githubID = fmt.Sprintf("prcomment:%s:%s", repo, ext(m, "pull_request", "number"))
	case "pull_request_review":
		githubID = fmt.Sprintf("prreview:%s:%s", repo, ext(m, "pull_request", "number"))
	case "commit_comment":
		githubID = fmt.Sprintf("commit_comment:%s:%s", repo, ext(m, "comment", "id"))
	case "deployment":
		githubID = fmt.Sprintf("deployment:%s:%s", repo, ext(m, "deployment", "id"))
	case "deployment_status":
		githubID = fmt.Sprintf("deployment:%s:%s", repo, ext(m, "deployment", "id"))
	case "discussion":
		githubID = fmt.Sprintf("discussion:%s:%s", repo, ext(m, "discussion", "number"))
	case "discussion_comment":
		githubID = fmt.Sprintf("discussion:%s:%s", repo, ext(m, "discussion", "number"))
	case "label":
		githubID = fmt.Sprintf("label:%s:%s", repo, ext(m, "label", "id"))
	case "milestone":
		githubID = fmt.Sprintf("milestone:%s:%s", repo, ext(m, "milestone", "number"))
	case "pull_request_review_thread":
		githubID = fmt.Sprintf("prthread:%s:%s", repo, ext(m, "pull_request", "number"))
	case "status":
		githubID = fmt.Sprintf("status:%s:%s:%s", repo, ext(m, "sha"), ext(m, "context"))
	case "branch_protection_rule":
		githubID = fmt.Sprintf("bprule:%s:%s", repo, ext(m, "rule", "id"))
	case "repository_ruleset":
		githubID = fmt.Sprintf("ruleset:%s:%s", repo, ext(m, "repository_ruleset", "id"))
	case "code_scanning_alert":
		githubID = fmt.Sprintf("codescan:%s:%s", repo, ext(m, "alert", "number"))
	case "dependabot_alert":
		githubID = fmt.Sprintf("dependabot:%s:%s", repo, ext(m, "alert", "number"))
	case "secret_scanning_alert":
		githubID = fmt.Sprintf("secretscan:%s:%s", repo, ext(m, "alert", "number"))
	case "repository_vulnerability_alert":
		githubID = fmt.Sprintf("vulnalert:%s:%s", repo, ext(m, "alert", "id"))
	case "security_advisory":
		githubID = fmt.Sprintf("advisory:%s:%s", repo, ext(m, "security_advisory", "ghsa_id"))
	case "team_add":
		githubID = fmt.Sprintf("team_add:%s:%s", repo, ext(m, "team", "id"))
	case "page_build":
		githubID = fmt.Sprintf("page_build:%s", repo)
	default:
		githubID = sha
		if githubID == "" {
			issueNum := ext(m, "issue", "number")
			if issueNum != "" {
				githubID = fmt.Sprintf("issue:%s:%s", repo, issueNum)
			}
		}
	}

	// 3.5 跳过非 "created"/"submitted" 的评论/Review 事件（避免 edited/deleted 通知噪音）
	if event.EventType == "issue_comment" && ext(m, "action") != "created" {
		return nil
	}
	if event.EventType == "pull_request_review_comment" && ext(m, "action") != "created" {
		return nil
	}
	if event.EventType == "pull_request_review" && ext(m, "action") != "submitted" {
		return nil
	}

	// 3.6 通用去重：合并窗口内同一 githubID + eventType 的事件只保留一次
	// 跳过已有专门合并逻辑的事件（push, create, delete, issue_comment/PR comment 的 created）
	needDedup := githubID != "" &&
		event.EventType != "push" &&
		event.EventType != "create" &&
		event.EventType != "delete" &&
		event.EventType != "issue_comment" &&
		event.EventType != "pull_request_review_comment"

	if needDedup {
		if tryDedup(ctx, githubID, event.EventType, &detail) {
			return nil
		}
	}

	// 4. 合并与更新逻辑
	// 4.1 CI/CD 事件 (Workflow, Check Run)：更新同一条消息，支持超时提醒
	isCIEvent := event.EventType == "workflow_run" ||
		event.EventType == "workflow_job" ||
		event.EventType == "check_run" ||
		event.EventType == "check_suite"

	if isCIEvent && githubID != "" {
		var record MessageRecord
		err := DB.NewSelect().Model(&record).
			Where("github_id = ?", githubID).
			Order("id DESC").
			Limit(1).Scan(ctx)

		if err == nil {
			status, conclusion := extractCIStatus(m, event.EventType)

			// 已完成则重置超时提醒标志
			if conclusion != "" && record.TimeoutNotified {
				_, _ = DB.NewUpdate().Model(&record).
					Set("timeout_notified = ?", false).
					WherePK().Exec(ctx)
			}

			// 运行中且超过 10 分钟未完成，发送超时提醒
			if conclusion == "" && status == "in_progress" &&
				!record.WorkflowStartedAt.IsZero() &&
				time.Since(record.WorkflowStartedAt) > 10*time.Minute &&
				!record.TimeoutNotified {
				sendTimeoutNotification(record.FeishuMessageID, detail.Title, record.WorkflowStartedAt)
				_, _ = DB.NewUpdate().Model(&record).
					Set("timeout_notified = ?", true).
					WherePK().Exec(ctx)
			}

			if record.ParentMsgID != "" {
				// CI 内联模式：更新 CI 记录，然后刷新父消息卡片
				detailJson, _ := json.Marshal(detail)
				_, _ = DB.NewUpdate().Model(&record).
					Set("content = ?", string(detailJson)).
					Set("updated_at = ?", time.Now()).
					WherePK().Exec(ctx)
				updateParentCardWithCI(ctx, record.ParentMsgID)
				slog.Info("CI status updated (inline)", "github_id", githubID, "parent", record.ParentMsgID)
				return nil
			}

			// 兼容旧模式：独立 CI 消息
			// workflow_job 事件：将 job 信息追加到 CIStatuses，而不是覆盖
			if event.EventType == "workflow_job" {
				var existingDetail EventDetail
				_ = json.Unmarshal([]byte(record.Content), &existingDetail)

				// 从 payload 提取 job 信息
				jobName := ext(m, "workflow_job", "name")
				jobStatus, jobConclusion := extractCIStatus(m, event.EventType)
				jobRunID, _ := strconv.ParseInt(ext(m, "workflow_job", "run_id"), 10, 64)
				jobDuration := ""
				if jobConclusion != "" {
					startedAtStr := ext(m, "workflow_job", "started_at")
					completedAtStr := ext(m, "workflow_job", "completed_at")
					if startedAtStr != "" && completedAtStr != "" {
						if t1, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
							if t2, err := time.Parse(time.RFC3339, completedAtStr); err == nil {
								jobDuration = FormatDuration(t2.Sub(t1))
							}
						}
					}
				}

				// 查找并更新或追加 job 状态
				jobKey := "job:" + ext(m, "workflow_job", "id")
				found := false
				for i, cs := range existingDetail.CIStatuses {
					if cs.WorkflowName == jobKey {
						existingDetail.CIStatuses[i] = CIStatus{
							WorkflowName: jobKey,
							JobName:      jobName,
							Status:       jobStatus,
							Conclusion:   jobConclusion,
							RunID:        jobRunID,
							ParentRunID:  jobRunID,
							Ref:          ref,
							Duration:     jobDuration,
							UpdatedAt:    detail.EventTime,
						}
						found = true
						break
					}
				}
				if !found {
					existingDetail.CIStatuses = append(existingDetail.CIStatuses, CIStatus{
						WorkflowName: jobKey,
						JobName:      jobName,
						Status:       jobStatus,
						Conclusion:   jobConclusion,
						RunID:        jobRunID,
						ParentRunID:  jobRunID,
						Ref:          ref,
						Duration:     jobDuration,
						UpdatedAt:    detail.EventTime,
					})
				}

				// 重建卡片
				buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
				card := BuildCard(buildCtx, repo, sender, senderUrl, avatarUrl, existingDetail)
				buildCancel()

				if err := UpdateMessage(record.FeishuMessageID, card); err != nil {
					return fmt.Errorf("failed to update message: %w", err)
				}

				updatedJson, _ := json.Marshal(existingDetail)
				_, _ = DB.NewUpdate().Model(&record).
					Set("content = ?", string(updatedJson)).
					Set("card_string = ?", card.String()).
					Set("updated_at = ?", time.Now()).
					WherePK().Exec(ctx)

				slog.Info("Workflow job appended", "github_id", githubID, "job", jobName)
				return nil
			}

			// workflow_run 等其他 CI 事件：直接更新
			buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
			card := BuildCard(buildCtx, repo, sender, senderUrl, avatarUrl, detail)
			buildCancel()

			if err := UpdateMessage(record.FeishuMessageID, card); err != nil {
				return fmt.Errorf("failed to update message: %w", err)
			}

			detailJson, _ := json.Marshal(detail)
			_, _ = DB.NewUpdate().Model(&record).
				Set("content = ?", string(detailJson)).
				Set("card_string = ?", card.String()).
				Set("updated_at = ?", time.Now()).
				WherePK().Exec(ctx)

			slog.Info("Workflow card updated", "github_id", githubID, "event_type", event.EventType)
			return nil
		}
	}

	// 4.2 Release 事件：更新同一个 release 的消息（编辑、正式发布等）
	if event.EventType == "release" && githubID != "" {
		merged, err := tryMergeWithExisting(ctx,
			mergeSearch{githubID: githubID, eventType: "release"},
			func(_, new *EventDetail) {}, // Release 直接替换，无需合并
			"", &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"Release card updated",
		)
		if merged {
			return err
		}
	}

	// 4.2a Deployment 事件：更新同一个 deployment 的消息（状态变更等）
	if event.EventType == "deployment" && githubID != "" {
		merged, err := tryMergeWithExisting(ctx,
			mergeSearch{githubID: githubID, eventType: "deployment"},
			func(_, new *EventDetail) {}, // Deployment 直接替换
			"", &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"Deployment card updated",
		)
		if merged {
			return err
		}
	}
	if event.EventType == "deployment_status" && githubID != "" {
		merged, err := tryMergeWithExisting(ctx,
			mergeSearch{githubID: githubID, withinWindow: true},
			func(_, new *EventDetail) {}, // 状态直接替换
			"", &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"Deployment status updated",
		)
		if merged {
			return err
		}
	}

	// 4.3 分支推送合并：同一分支在合并窗口内的连续推送合并为一条（排除分支删除）
	if event.EventType == "push" && githubID != "" && !detail.IsTag && !detail.IsDeleted && detail.Text != "" {
		detail.EventCount = len(strings.Split(detail.Text, "\n"))
		merged, err := tryMergeWithExisting(ctx,
			mergeSearch{githubID: githubID, withinWindow: true},
			func(old, new *EventDetail) {
				new.Text = old.Text + "\n---\n" + new.Text
				new.Title = "🍏 Branch Push"
				new.EventCount = len(strings.Split(new.Text, "\n"))
				new.CommitCount = old.CommitCount + new.CommitCount
				currentTime := new.EventTime
				if old.EventTime != "" {
					new.EventTime = old.EventTime // 保留最早时间
				}
				new.EventTimeEnd = currentTime // 最新事件时间作为结束时间
			},
			sha, &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"Push combined",
		)
		if merged {
			return err
		}
	}

	// 4.3a PR 合并去重：push 是 PR merge 时，更新 PR 卡片而非创建新 push 消息
	if detectPRMerge(ctx, event, m, &detail, repo, repoUrl, sender, senderUrl, avatarUrl, sha) {
		return nil
	}

	// 4.3b 分支删除合并：同一仓库在合并窗口内的分支删除合并为一条（同时匹配 push 和 delete 事件）
	if event.EventType == "push" && detail.IsDeleted && !detail.IsTag {
		merged, err := tryMergeWithExisting(ctx,
			mergeSearch{
				githubIDLikes: []string{
					fmt.Sprintf("push:%s:refs/heads/%%", repo),
					fmt.Sprintf("delete:%s:branch:%%", repo),
				},
				recordType:   "deleted",
				withinWindow: true,
			},
			func(old, new *EventDetail) {
				if old.Text != "" {
					new.Text = mergeRefs(old.Text, new.Text)
				}
				new.Title = fmt.Sprintf("🗑️ Branch Deleted: %s", repo)
				new.RefName = ""
				new.RefURL = ""
				currentTime := new.EventTime
				if old.EventTime != "" {
					new.EventTime = old.EventTime
				}
				new.EventTimeEnd = currentTime
			},
			"", &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"Branch deletions combined",
		)
		if merged {
			return err
		}
	}

	// 4.4 标签删除合并：同一仓库在合并窗口内的标签删除合并
	if event.EventType == "delete" && detail.IsTag {
		merged, err := tryMergeWithExisting(ctx,
			mergeSearch{githubIDLike: fmt.Sprintf("delete:%s:tag:%%", repo), withinWindow: true},
			func(old, new *EventDetail) {
				if old.Text != "" {
					new.Text = mergeRefs(old.Text, new.Text)
				}
				new.Title = fmt.Sprintf("🗑️ Tag Deleted: %s", repo)
				new.RefName = ""
				new.RefURL = ""
				currentTime := new.EventTime
				if old.EventTime != "" {
					new.EventTime = old.EventTime
				}
				new.EventTimeEnd = currentTime
			},
			"", &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"Tag deletions combined",
		)
		if merged {
			return err
		}
	}

	// 4.4a 分支删除合并：同一仓库在合并窗口内的分支删除合并（同时匹配 push 和 delete 事件）
	if event.EventType == "delete" && detail.IsDeleted && !detail.IsTag {
		merged, err := tryMergeWithExisting(ctx,
			mergeSearch{
				githubIDLikes: []string{
					fmt.Sprintf("delete:%s:branch:%%", repo),
					fmt.Sprintf("push:%s:refs/heads/%%", repo),
				},
				recordType:   "deleted",
				withinWindow: true,
			},
			func(old, new *EventDetail) {
				if old.Text != "" {
					new.Text = mergeRefs(old.Text, new.Text)
				}
				new.Title = fmt.Sprintf("🗑️ Branch Deleted: %s", repo)
				new.RefName = ""
				new.RefURL = ""
				currentTime := new.EventTime
				if old.EventTime != "" {
					new.EventTime = old.EventTime
				}
				new.EventTimeEnd = currentTime
			},
			"", &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"Branch deletions combined",
		)
		if merged {
			return err
		}
	}

	// 4.5 标签创建合并：同一仓库在合并窗口内的标签创建合并
	if event.EventType == "create" && detail.IsTag {
		merged, err := tryMergeWithExisting(ctx,
			mergeSearch{githubIDLike: fmt.Sprintf("create:%s:tag:%%", repo), withinWindow: true},
			func(old, new *EventDetail) {
				if old.Text != "" {
					new.Text = old.Text + "\n---\n" + new.Text
				}
				new.Title = fmt.Sprintf("🏷️ New Tag: %s", repo)
				new.RefName = ""
				new.RefURL = ""
				currentTime := new.EventTime
				if old.EventTime != "" {
					new.EventTime = old.EventTime
				}
				new.EventTimeEnd = currentTime
			},
			"", &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"Tag creations combined",
		)
		if merged {
			return err
		}
	}

	// 4.5a 评论合并：同一 Issue/PR 在合并窗口内的评论合并为一条
	if event.EventType == "issue_comment" && ext(m, "action") == "created" && githubID != "" {
		issueNum := ext(m, "issue", "number")
		merged, err := tryMergeWithExisting(ctx,
			mergeSearch{githubID: githubID, withinWindow: true},
			func(old, new *EventDetail) {
				if old.Text != "" {
					new.Text = old.Text + "\n---\n" + new.Text
				}
				new.Title = fmt.Sprintf("🌻 Comments on #%s", issueNum)
				new.Action = "created"
				currentTime := new.EventTime
				if old.EventTime != "" {
					new.EventTime = old.EventTime
				}
				new.EventTimeEnd = currentTime
				new.EventCount = old.EventCount + 1
			},
			"", &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"Comments merged",
		)
		if merged {
			return err
		}
	}
	if event.EventType == "pull_request_review_comment" && ext(m, "action") == "created" && githubID != "" {
		prNum := ext(m, "pull_request", "number")
		merged, err := tryMergeWithExisting(ctx,
			mergeSearch{githubID: githubID, withinWindow: true},
			func(old, new *EventDetail) {
				if old.Text != "" {
					new.Text = old.Text + "\n---\n" + new.Text
				}
				new.Title = fmt.Sprintf("💬 PR Comments on #%s", prNum)
				new.Action = "created"
				currentTime := new.EventTime
				if old.EventTime != "" {
					new.EventTime = old.EventTime
				}
				new.EventTimeEnd = currentTime
				new.EventCount = old.EventCount + 1
			},
			"", &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"PR comments merged",
		)
		if merged {
			return err
		}
	}

	// 4.6 Tag/Workflow 关联最近的 commit，以话题形式回复
	var parentID string
	if event.EventType == "create" && detail.IsTag {
		if sha != "" {
			if rec := findParentRecordBySHA(ctx, repo, sha); rec != nil {
				parentID = rec.FeishuMessageID
			}
		}
		if parentID == "" {
			parentID = findRecentRepoPush(ctx, repo)
		}
	}
	if isCIEvent && parentID == "" {
		// CI 事件未找到已有记录（否则已在 4.1 返回），通过 head_sha 精确关联
		if sha != "" {
			var record MessageRecord
			if err := DB.NewSelect().Model(&record).
				Where("repo_name = ?", repo).
				Where("event_type IN ('push', 'create')").
				Where("head_sha = ?", sha).
				Where("record_type != 'deleted'").
				OrderExpr("CASE event_type WHEN 'create' THEN 0 ELSE 1 END").
				Order("id ASC").
				Limit(1).Scan(ctx); err == nil {
				// 父消息过旧（workflow 重新运行），不内联，创建独立消息
				if time.Since(record.UpdatedAt) > getMergeWindow() {
					slog.Info("CI event: parent too old, creating standalone message", "sha", sha, "parent_id", record.FeishuMessageID, "parent_age", time.Since(record.UpdatedAt))
				} else {
					parentID = record.FeishuMessageID
					parentMsgID = record.FeishuMessageID
					slog.Info("CI event found parent by head_sha", "sha", sha, "parent_event", record.EventType, "parent_id", parentID)
				}
			} else {
				slog.Warn("CI event: no parent found by head_sha", "sha", sha, "repo", repo, "error", err)
			}
		}
		// SHA 匹配失败时，回退查找同仓库最近的 tag 创建消息（tag 的 head_sha 可能为空）
		if isCIEvent && parentID == "" {
			if rec := findRecentRepoTag(ctx, repo); rec != nil {
				parentID = rec.FeishuMessageID
				parentMsgID = rec.FeishuMessageID
				slog.Info("CI event found parent by recent tag fallback", "parent_event", rec.EventType, "parent_id", parentID)
			}
		}
		// 找不到父消息，检查关联的 push/create 事件是否还在队列中或等待重试（事件到达顺序不确定）
		if isCIEvent && parentID == "" && event.RescheduleCount < 5 {
			shouldReschedule := false
			if sha != "" {
				// 同 SHA 的 push 事件还在队列中
				var pendingPush WebhookEvent
				if err := DB.NewSelect().Model(&pendingPush).
					Where("id != ?", event.ID).
					Where("event_type = ?", "push").
					Where("status IN ('pending', 'processing') OR (status = 'failed' AND retry_count < 5)").
					Where("head_sha = ?", sha).
					Limit(1).Scan(ctx); err == nil {
					shouldReschedule = true
				}
			}
			// tag 触发的 workflow：create 事件可能还在队列中（head_sha 为空，无法用 SHA 关联）
			if !shouldReschedule && ref != "" {
				var pendingCreate WebhookEvent
				if err := DB.NewSelect().Model(&pendingCreate).
					Where("id != ?", event.ID).
					Where("event_type = ?", "create").
					Where("status IN ('pending', 'processing') OR (status = 'failed' AND retry_count < 5)").
					Where("ref = ?", ref).
					Limit(1).Scan(ctx); err == nil {
					shouldReschedule = true
				}
			}
			if shouldReschedule {
				slog.Info("Rescheduling CI event, waiting for parent event", "sha", sha, "ref", ref, "event_type", event.EventType, "reschedule", event.RescheduleCount+1)
				_, _ = DB.NewUpdate().Model(&event).
					Set("status = ?", "pending").
					Set("reschedule_count = reschedule_count + 1").
					Set("updated_at = ?", time.Now()).
					WherePK().Exec(ctx)
				return nil
			}
		}
	}

	// 4.7 CI 事件内联到父消息：保存 CI 记录并更新父消息卡片
	if isCIEvent && parentMsgID != "" {
		workflowStartedAt := time.Time{}
		status, conclusion := extractCIStatus(m, event.EventType)
		if status == "in_progress" && conclusion == "" {
			workflowStartedAt = time.Now()
		}
		// 构建初始 CIStatus 供 getCIStatusesForParent 直接读取
		ciStatus := CIStatus{
			WorkflowName: detail.Title,
			Status:       status,
			Conclusion:   conclusion,
			UpdatedAt:    detail.EventTime,
		}
		if event.EventType == "workflow_run" {
			ciStatus.RunID, _ = strconv.ParseInt(ext(m, "workflow_run", "id"), 10, 64)
		} else if event.EventType == "workflow_job" {
			ciStatus.JobName = ext(m, "workflow_job", "name")
			ciStatus.RunID, _ = strconv.ParseInt(ext(m, "workflow_job", "run_id"), 10, 64)
			ciStatus.ParentRunID = ciStatus.RunID
		}
		detail.CIStatuses = []CIStatus{ciStatus}
		detailJson, _ := json.Marshal(detail)

		if _, insertErr := DB.NewInsert().Model(&MessageRecord{
			GithubID:          githubID,
			FeishuMessageID:   parentMsgID, // 指向父消息，用于查询
			ChatID:            C.Feishu.ChatID,
			RepoName:          repo,
			EventType:         event.EventType,
			Content:           string(detailJson),
			EventID:           event.ID,
			WorkflowStartedAt: workflowStartedAt,
			HeadSHA:           sha,
			ParentMsgID:       parentMsgID,
		}).Exec(ctx); insertErr != nil {
			// GithubID 唯一约束处理幂等，其他错误记录日志
			if !strings.Contains(insertErr.Error(), "duplicate key") {
				slog.Error("Failed to insert CI record", "github_id", githubID, "error", insertErr)
			}
		}
		updateParentCardWithCI(ctx, parentMsgID)
		slog.Info("CI event inlined to parent", "github_id", githubID, "parent", parentMsgID)
		return nil
	}

	// 4.8 CI 事件无父消息时的处理：in_progress 静默丢弃，仅 completed/failed 创建独立消息
	if isCIEvent && parentMsgID == "" {
		status, _ := extractCIStatus(m, event.EventType)
		if status == "in_progress" || status == "queued" || status == "waiting" {
			slog.Info("CI event suppressed (no parent, in_progress)", "github_id", githubID, "status", status)
			return nil
		}
	}

	// 4.9 CI 事件合并：同一 commit 的多个 workflow 合并显示（仅在合并窗口内）
	if isCIEvent && parentMsgID == "" && sha != "" {
		var existingCIRecord MessageRecord
		if err := DB.NewSelect().Model(&existingCIRecord).
			Where("event_type IN ('workflow_run', 'workflow_job', 'check_run', 'check_suite')").
			Where("head_sha = ?", sha).
			Where("repo_name = ?", repo).
			Where("parent_msg_id = ''").
			Where("updated_at > ?", time.Now().Add(-getMergeWindow())).
			Order("id ASC").Limit(1).Scan(ctx); err == nil {
			// 找到同一 commit 的 CI 消息，合并 CI 状态
			var existingDetail EventDetail
			_ = json.Unmarshal([]byte(existingCIRecord.Content), &existingDetail)

			// 从 payload 提取 CI 信息
			ciStatus, ciConclusion := extractCIStatus(m, event.EventType)
			ciWorkflowName := detail.Title
			ciRunID := int64(0)
			ciDuration := ""
			if event.EventType == "workflow_run" {
				ciRunID, _ = strconv.ParseInt(ext(m, "workflow_run", "id"), 10, 64)
				// 尝试从 workflow_run 对象中提取时间计算耗时
				if wr, ok := m["workflow_run"].(map[string]any); ok {
					if startedAtStr, ok := wr["run_started_at"].(string); ok {
						if updatedAtStr, ok := wr["updated_at"].(string); ok {
							if t1, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
								if t2, err := time.Parse(time.RFC3339, updatedAtStr); err == nil {
									ciDuration = FormatDuration(t2.Sub(t1))
								}
							}
						}
					}
				}
			}

			// 检查是否已有同一 workflow 的状态，有则更新，无则追加
			found := false
			for i, cs := range existingDetail.CIStatuses {
				if cs.RunID == ciRunID && ciRunID > 0 {
					existingDetail.CIStatuses[i] = CIStatus{
						WorkflowName: ciWorkflowName,
						Status:       ciStatus,
						Conclusion:   ciConclusion,
						RunID:        ciRunID,
						Ref:          ref,
						Duration:     ciDuration,
						UpdatedAt:    detail.EventTime,
					}
					found = true
					break
				}
			}
			if !found {
				existingDetail.CIStatuses = append(existingDetail.CIStatuses, CIStatus{
					WorkflowName: ciWorkflowName,
					Status:       ciStatus,
					Conclusion:   ciConclusion,
					RunID:        ciRunID,
					Ref:          ref,
					Duration:     ciDuration,
					UpdatedAt:    detail.EventTime,
				})
			}

			// 重建卡片
			buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
			card := BuildCard(buildCtx, repo, existingCIRecord.Sender, existingCIRecord.SenderURL, existingCIRecord.AvatarURL2, existingDetail)
			buildCancel()

			if err := UpdateMessage(existingCIRecord.FeishuMessageID, card); err != nil {
				slog.Error("Failed to update merged CI card", "error", err)
			} else {
				updatedJson, _ := json.Marshal(existingDetail)
				_, _ = DB.NewUpdate().Model(&existingCIRecord).
					Set("content = ?", string(updatedJson)).
					Set("card_string = ?", card.String()).
					Set("updated_at = ?", time.Now()).
					WherePK().Exec(ctx)
				slog.Info("CI event merged with existing", "github_id", githubID, "existing_id", existingCIRecord.GithubID)
			}
			return nil
		}
	}

	// 5. 查找父级 ID (回复逻辑)
	// 改为：只要是 Issue/PR/Discussion 相关的非"创建"事件，都尝试寻找父消息进行话题回复
	// 注意：CI 事件不在这里处理，它们的父消息已在 4.6 通过 head_sha 精确关联
	// 注意：评论合并（4.5a）在本步骤之前执行，已合并的评论不会进入本逻辑
	isInteraction := event.EventType == "issue_comment" ||
		event.EventType == "pull_request_review_comment" ||
		event.EventType == "pull_request_review" ||
		event.EventType == "pull_request_review_thread" ||
		event.EventType == "pull_request" ||
		event.EventType == "issues" ||
		event.EventType == "discussion_comment" ||
		event.EventType == "commit_comment"

	action := ext(m, "action")
	if isInteraction && action != "opened" {
		// 通过 SHA 查找父消息，限制在话题回复窗口内
		if parentID == "" && sha != "" {
			var shaRecord MessageRecord
			if err := DB.NewSelect().Model(&shaRecord).
				Where("repo_name = ?", repo).
				Where("event_type IN ('push', 'create')").
				Where("head_sha = ?", sha).
				Where("record_type != 'deleted'").
				Where("updated_at > ?", time.Now().Add(-getThreadReplyWindow())).
				Order("id ASC").Limit(1).Scan(ctx); err == nil {
				parentID = shaRecord.FeishuMessageID
			}
		}
		// Discussion 评论：查找父 Discussion 消息
		if parentID == "" && event.EventType == "discussion_comment" {
			discNum := ext(m, "discussion", "number")
			if discNum != "" {
				var discRecord MessageRecord
				if err := DB.NewSelect().Model(&discRecord).
					Where("github_id = ?", fmt.Sprintf("discussion:%s:%s", repo, discNum)).
					Where("updated_at > ?", time.Now().Add(-getThreadReplyWindow())).
					Order("id ASC").Limit(1).Scan(ctx); err == nil {
					parentID = discRecord.FeishuMessageID
				}
			}
		}
		// Issue/PR 评论和 Review：查找父 Issue/PR 消息
		if parentID == "" {
			issueNum := ext(m, "issue", "number")
			if issueNum == "" {
				issueNum = ext(m, "pull_request", "number")
			}
			if issueNum != "" {
				var record MessageRecord
				searchID := fmt.Sprintf("%%:%s", issueNum)
				if strings.Contains(githubID, "pr:") || strings.Contains(githubID, "issue:") {
					searchID = fmt.Sprintf("%%:%s:%s", repo, issueNum)
				}
				if strings.Contains(githubID, "comment:") || strings.Contains(githubID, "prcomment:") || strings.Contains(githubID, "prreview:") || strings.Contains(githubID, "prthread:") {
					searchID = fmt.Sprintf("%%:%s:%s", repo, issueNum)
				}

				if err := DB.NewSelect().Model(&record).
					Where("github_id = ? OR github_id LIKE ?", fmt.Sprintf("pr:%s:%s", repo, issueNum), searchID).
					WhereOr("github_id = ?", fmt.Sprintf("issue:%s:%s", repo, issueNum)).
					Where("updated_at > ?", time.Now().Add(-getThreadReplyWindow())).
					Order("id ASC").Limit(1).Scan(ctx); err == nil {
					parentID = record.FeishuMessageID
				}
			}
		}
		// Commit Comment：通过 SHA 关联到 push 消息（如果还没找到）
		if parentID == "" && event.EventType == "commit_comment" && sha == "" {
			commitSHA := ext(m, "comment", "commit_id")
			if commitSHA != "" {
				var shaRecord MessageRecord
				if err := DB.NewSelect().Model(&shaRecord).
					Where("repo_name = ?", repo).
					Where("event_type = ?", "push").
					Where("head_sha = ?", commitSHA).
					Where("record_type != 'deleted'").
					Where("updated_at > ?", time.Now().Add(-getThreadReplyWindow())).
					Order("id ASC").Limit(1).Scan(ctx); err == nil {
					parentID = shaRecord.FeishuMessageID
				}
			}
		}
	}

	// Bot 用户的事件必须找到父消息才发送，否则跳过
	if isBotUser && parentID == "" {
		slog.Info("Bot user event skipped: no parent message found", "sender", sender, "event", event.EventType)
		return nil
	}

	// 6. 发送新消息
	// 检查所有需要显示的头像缓存状态，有任意一个未缓存就尝试立即同步上传（限时 5s）
	allAvatars := detail.AuthorAvatars
	if len(allAvatars) == 0 && avatarUrl != "" {
		allAvatars = []string{avatarUrl}
	}

	// 并行补齐缺失的头像缓存，避免串行等待导致超时
	var wg sync.WaitGroup
	uploadCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	for _, url := range allAvatars {
		if GetImageKey(ctx, url) == "" {
			wg.Add(1)
			go func(u string) {
				defer wg.Done()
				syncUploadImage(uploadCtx, u)
			}(url)
		}
	}
	wg.Wait()
	cancel()

	imageStatus := "done"
	for _, url := range allAvatars {
		var cache ImageCache
		if err := DB.NewSelect().Model(&cache).Where("url = ?", url).Scan(ctx); err == nil {
			if time.Since(cache.UpdatedAt) > 24*time.Hour {
				imageStatus = "pending"
			}
		} else {
			imageStatus = "pending"
		}
	}

	// 此时 BuildCard 调用 GetImageKey 是纯内存/DB查询，刚才同步上传成功的会立即显示
	card := BuildCard(ctx, repo, sender, senderUrl, avatarUrl, detail)

	var msgID string
	var sendErr error
	if parentID != "" {
		msgID, sendErr = ReplyToMessage(parentID, card)
	} else {
		msgID, sendErr = SendToChat("", card)
	}

	if sendErr != nil {
		return sendErr
	}

	// 6.5 发送长文本回复 (如果存在)
	if detail.ExtraReply != "" && msgID != "" {
		replyCard := NewCard()
		replyCard.AddMarkdown(detail.ExtraReply)
		_, _ = ReplyToMessage(msgID, replyCard)
	}
	if detail.FoldableBody != "" && msgID != "" {
		replyCard := NewCard()
		replyCard.AddMarkdown(detail.FoldableBody)
		_, _ = ReplyToMessage(msgID, replyCard)
	}

	// 7. 保存记录
	if githubID != "" && msgID != "" {
		detailJson, _ := json.Marshal(detail)

		// CI 事件：记录开始时间
		workflowStartedAt := time.Time{}
		if isCIEvent {
			status, conclusion := extractCIStatus(m, event.EventType)
			if status == "in_progress" && conclusion == "" {
				workflowStartedAt = time.Now()
			}
		}

		// push/create 事件：记录 head SHA 供后续 CI/tag 精确关联
		headSHA := ""
		if event.EventType == "push" || event.EventType == "create" {
			headSHA = sha
			slog.Info("Storing head_sha for event", "event_type", event.EventType, "head_sha", headSHA, "repo", repo)
		}

		// 删除事件标记 record_type，用于合并查询
		recordType := "normal"
		if detail.IsDeleted {
			recordType = "deleted"
		}

		_, insertErr := DB.NewInsert().Model(&MessageRecord{
			GithubID:          githubID,
			FeishuMessageID:   msgID,
			ChatID:            C.Feishu.ChatID,
			RepoName:          repo,
			Ref:               ref,
			EventType:         event.EventType,
			Content:           string(detailJson),
			CardString:        card.String(),
			ImageStatus:       imageStatus,
			AvatarURL:         avatarUrl,
			EventID:           event.ID,
			WorkflowStartedAt: workflowStartedAt,
			HeadSHA:           headSHA,
			RecordType:        recordType,
			ParentMsgID:       parentMsgID,
			Sender:            sender,
			SenderURL:         senderUrl,
			AvatarURL2:        avatarUrl,
		}).On("CONFLICT (github_id) DO UPDATE").
			Set("feishu_message_id = EXCLUDED.feishu_message_id").
			Set("content = EXCLUDED.content").
			Set("card_string = EXCLUDED.card_string").
			Set("event_id = EXCLUDED.event_id").
			Set("updated_at = NOW()").
			Set("head_sha = EXCLUDED.head_sha").
			Exec(ctx)
		if insertErr != nil {
			slog.Error("Failed to insert/update message record", "github_id", githubID, "error", insertErr)
		}

		// push 事件入库后，回填同标签 create 事件的 head_sha（create 可能先于 push 处理）
		if event.EventType == "push" && detail.IsTag && sha != "" {
			tagName := strings.TrimPrefix(ref, "refs/tags/")
			if tagName != ref {
				_, _ = DB.NewUpdate().Model((*MessageRecord)(nil)).
					Set("head_sha = ?", sha).
					Where("repo_name = ?", repo).
					Where("event_type = ?", "create").
					Where("ref = ?", tagName).
					Where("head_sha = ''").
					Exec(ctx)
			}
		}
	}

	return nil
}

func imageRefreshWorker() {
	slog.Info("Image refresh worker started")
	for {
		var records []MessageRecord
		err := DB.NewSelect().Model(&records).
			Where("image_status = ?", "pending").
			Order("id ASC").
			Limit(20).
			Scan(context.Background())

		if err != nil || len(records) == 0 {
			time.Sleep(5 * time.Second)
			continue
		}

		var wg sync.WaitGroup
		for _, record := range records {
			wg.Add(1)
			go func(r MessageRecord) {
				defer wg.Done()
				refreshOneImage(r)
			}(record)
			// 微小的启动间隔，避免瞬时并发压力过大
			time.Sleep(100 * time.Millisecond)
		}
		wg.Wait()
	}
}

// isKnownUnavailableAvatar 检查是否是已知无法下载的头像 URL（如 GitHub bot 账号）
func isKnownUnavailableAvatar(url string) bool {
	unavailable := []string{
		"github.com/Copilot.png",
	}
	for _, suffix := range unavailable {
		if strings.HasSuffix(url, suffix) {
			return true
		}
	}
	return false
}

func refreshOneImage(record MessageRecord) {
	// 1. 解析保存的卡片详情，获取所有需要刷新的头像 URL
	var detail EventDetail
	_ = json.Unmarshal([]byte(record.Content), &detail)

	allAvatars := detail.AuthorAvatars
	if len(allAvatars) == 0 && record.AvatarURL != "" {
		allAvatars = []string{record.AvatarURL}
	}
	if len(allAvatars) == 0 {
		_, _ = DB.NewUpdate().Model(&record).Set("image_status = ?", "done").WherePK().Exec(context.Background())
		return
	}

	// 2. 依次上传所有头像，每个最多重试 10 次，指数退避
	allUploaded := true
	for _, avatarURL := range allAvatars {
		// 跳过已知无法下载的头像（如 GitHub bot 账号）
		if isKnownUnavailableAvatar(avatarURL) {
			slog.Info("Image refresh: skipping known unavailable avatar", "url", avatarURL)
			continue
		}
		uploaded := false
		for attempt := 0; attempt < 10; attempt++ {
			if attempt > 0 {
				backoff := time.Duration(attempt*attempt) * 3 * time.Second
				slog.Info("Image refresh: retrying avatar upload",
					"url", avatarURL, "attempt", attempt+1, "backoff", backoff)
				time.Sleep(backoff)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			imgKey := syncUploadImage(ctx, avatarURL)
			cancel()
			if imgKey != "" {
				slog.Info("Image refresh: avatar uploaded", "url", avatarURL, "img_key", imgKey)
				uploaded = true
				break
			}
			slog.Warn("Image refresh: avatar upload failed", "url", avatarURL, "attempt", attempt+1)
		}
		if !uploaded {
			slog.Error("Image refresh: giving up on avatar after 10 attempts", "url", avatarURL)
			allUploaded = false
		}
	}

	if !allUploaded {
		// 部分头像上传失败，本轮不更新卡片，等下一轮重试
		return
	}

	// 3. 获取原始 Webhook 事件元数据
	var event WebhookEvent
	err := DB.NewSelect().Model(&event).Where("id = ?", record.EventID).Scan(context.Background())
	if err != nil {
		slog.Error("Image refresh: failed to find original event", "event_id", record.EventID, "error", err)
		return
	}

	var m map[string]any
	_ = json.Unmarshal([]byte(event.Payload), &m)
	repoUrl := ext(m, "repository", "html_url")
	sender := ext(m, "sender", "login")
	senderUrl := ext(m, "sender", "html_url")

	// 确保 detail.RepoURL 已填充（兼容旧记录）
	if detail.RepoURL == "" {
		detail.RepoURL = repoUrl
	}

	// 4. 重建卡片，此时所有头像的 img_key 均已缓存（哪怕是刚才刚同步成功的）
	buildCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	newCard := BuildCard(buildCtx, record.RepoName, sender, senderUrl, record.AvatarURL, detail)

	// 5. 核心优化：如果卡片内容（JSON）完全没变，则无需调用飞书 API 更新
	if newCard.String() == record.CardString {
		// 标记完成即可
		_, _ = DB.NewUpdate().Model(&record).Set("image_status = ?", "done").WherePK().Exec(context.Background())
		slog.Debug("Image refresh: card unchanged, skipping update", "message_id", record.FeishuMessageID)
		return
	}

	// 6. 更新飞书消息卡片
	if err = UpdateMessage(record.FeishuMessageID, newCard); err != nil {
		slog.Error("Image refresh: failed to update message card",
			"message_id", record.FeishuMessageID, "error", err)
		// 飞书消息超过 14 天不可更新，标记为完成避免无限重试
		if strings.Contains(err.Error(), "230031") || strings.Contains(err.Error(), "expired") {
			_, _ = DB.NewUpdate().Model(&record).Set("image_status = ?", "done").WherePK().Exec(context.Background())
		}
		return
	}

	// 7. 标记完成并记录新的卡片内容，防止重复刷新
	record.CardString = newCard.String()
	_, _ = DB.NewUpdate().Model(&record).
		Set("image_status = ?", "done").
		Set("card_string = ?", record.CardString).
		WherePK().Exec(context.Background())
	slog.Info("Image refresh successful, avatars updated",
		"message_id", record.FeishuMessageID, "avatar_count", len(allAvatars))
}
