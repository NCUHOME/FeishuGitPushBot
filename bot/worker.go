package bot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

const workflowJobRescheduleDelay = 15 * time.Second

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
		event, err := claimNextWebhookEvent(context.Background())
		if err != nil {
			// 如果没消息，歇会儿
			time.Sleep(2 * time.Second)
			continue
		}

		err = processWebhookEvent(*event)
		if err != nil {
			slog.Error("Failed to process Webhook event", "id", event.ID, "error", err)
			_, _ = DB.NewUpdate().Model(event).
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
				_, _ = DB.NewUpdate().Model(event).
					Set("status = ?", "processed").
					Set("updated_at = ?", time.Now()).
					WherePK().Exec(context.Background())
			}
		}

		// 推送间隔，保证节奏
		time.Sleep(1 * time.Second)
	}
}

func claimNextWebhookEvent(ctx context.Context) (*WebhookEvent, error) {
	var event WebhookEvent
	err := DB.NewRaw(`
		UPDATE webhook_events
		SET status = 'processing', updated_at = NOW()
		WHERE id = (
			SELECT id
			FROM webhook_events
			WHERE (
					status = 'pending'
					AND updated_at <= NOW()
				)
				OR (
					status = 'failed'
					AND retry_count < 5
					AND updated_at < ?
				)
			ORDER BY id ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING *
	`, time.Now().Add(-1*time.Minute)).Scan(ctx, &event)
	if err != nil {
		return nil, err
	}
	return &event, nil
}

func nextWorkflowJobRetryAt(now time.Time) time.Time {
	return now.Add(workflowJobRescheduleDelay)
}

func webhookMergeLockKey(eventType string, detail EventDetail, repo string) string {
	if repo == "" || !detail.IsDeleted {
		return ""
	}
	if eventType == "push" {
		if detail.IsTag {
			return fmt.Sprintf("merge:tag-delete:%s", repo)
		}
		return fmt.Sprintf("merge:branch-delete:%s", repo)
	}
	return ""
}

// acquireEventLock 原子地抢占一个事件合并锁，避免两个实例并发处理同仓库的
// 删除事件。锁有 TTL，进程崩溃后会自动过期。
//
// 实现采用单条 INSERT ... ON CONFLICT DO UPDATE ... WHERE ... RETURNING，
// 在一次往返中完成"清掉过期锁 + 抢占"两件事：
//   - 键不存在 → 走 INSERT 分支，返回新行
//   - 键存在且 expires_at < NOW() → 走 DO UPDATE 分支，覆盖过期锁
//   - 键存在且未过期 → WHERE 过滤掉 DO UPDATE，RETURNING 为空 → 继续等待
//
// 这消除了之前"先 DELETE 过期 + 再 INSERT 竞争"两步操作之间的 TOCTOU 窗口。
func acquireEventLock(ctx context.Context, key string, ttl time.Duration) (func(), error) {
	if key == "" || DB == nil {
		return func() {}, nil
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		var got EventLock
		err := DB.NewRaw(`
			INSERT INTO event_locks (lock_key, expires_at)
			VALUES (?, ?)
			ON CONFLICT (lock_key) DO UPDATE
			SET expires_at = EXCLUDED.expires_at
			WHERE event_locks.expires_at < NOW()
			RETURNING lock_key
		`, key, time.Now().Add(ttl)).Scan(ctx, &got)

		if err == nil && got.LockKey == key {
			return func() {
				_, _ = DB.NewDelete().Model((*EventLock)(nil)).
					Where("lock_key = ?", key).
					Exec(context.Background())
			}, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for event lock %q", key)
		}
		time.Sleep(100 * time.Millisecond)
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
// eventID: 最新 Webhook 事件 ID，供后续图片刷新按最新负载重建卡片
// 返回 (merged bool, err error)，merged=true 时调用方应立即返回
func tryMergeWithExisting(
	ctx context.Context,
	search mergeSearch,
	mergeFn func(old, new *EventDetail),
	headSHA string,
	eventID uint64,
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
		q = q.Where("("+strings.Join(idClauses, " OR ")+")", idArgs...)
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
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil // 未找到可合并的记录
		}
		return true, fmt.Errorf("failed to find existing message: %w", err)
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
		if isMessageUpdateExpired(err) {
			return false, nil
		}
		return true, fmt.Errorf("failed to update message: %w", err)
	}

	// 更新数据库记录
	detailJson, _ := json.Marshal(detail)
	imageStatus := "done"
	allAvatars := detail.AuthorAvatars
	if len(allAvatars) == 0 && avatarUrl != "" {
		allAvatars = []string{avatarUrl}
	}
	for _, url := range allAvatars {
		if GetImageKey(ctx, url) == "" {
			imageStatus = "pending"
			break
		}
	}
	updateQ := DB.NewUpdate().Model(&record).
		Set("content = ?", string(detailJson)).
		Set("card_string = ?", card.String()).
		Set("image_status = ?", imageStatus).
		Set("avatar_url = ?", avatarUrl).
		Set("sender = ?", sender).
		Set("sender_url = ?", senderUrl).
		Set("avatar_url2 = ?", avatarUrl).
		Set("updated_at = ?", time.Now()).
		WherePK()
	if headSHA != "" {
		updateQ = updateQ.Set("head_sha = ?", headSHA)
	}
	if eventID != 0 {
		updateQ = updateQ.Set("event_id = ?", eventID)
	}
	if _, err := updateQ.Exec(ctx); err != nil {
		return true, fmt.Errorf("failed to persist updated message: %w", err)
	}

	slog.Info(logMsg, "github_id", record.GithubID)
	return true, nil
}

func shouldUpdateWithinMergeWindow(eventType string, isCIEvent bool) bool {
	return eventType != "push" &&
		eventType != "create" &&
		eventType != "delete" &&
		eventType != "issue_comment" &&
		eventType != "pull_request_review_comment" &&
		eventType != "member" &&
		!isCIEvent
}

func isMessageUpdateExpired(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "230031") || strings.Contains(message, "expired")
}

// extractCIStatus 从 CI 事件负载中提取 status 和 conclusion
func extractCIStatus(m map[string]any, eventType string) (status, conclusion string) {
	switch eventType {
	case "workflow_run":
		status = ext(m, "workflow_run", "status")
		if status == "" {
			status = ext(m, "action")
		}
		return status, ext(m, "workflow_run", "conclusion")
	case "workflow_job":
		return ext(m, "workflow_job", "status"), ext(m, "workflow_job", "conclusion")
	case "check_run":
		return ext(m, "check_run", "status"), ext(m, "check_run", "conclusion")
	case "check_suite":
		return ext(m, "check_suite", "status"), ext(m, "check_suite", "conclusion")
	}
	return "", ""
}

func isCIEventType(eventType string) bool {
	return eventType == "workflow_run" ||
		eventType == "workflow_job" ||
		eventType == "check_run" ||
		eventType == "check_suite"
}

func workflowRunAttempt(m map[string]any) int {
	attemptStr := ext(m, "workflow_run", "run_attempt")
	if attemptStr == "" {
		return 1
	}
	attempt, err := strconv.Atoi(attemptStr)
	if err != nil || attempt <= 0 {
		return 1
	}
	return attempt
}

func workflowRunBaseID(m map[string]any) string {
	runID := ext(m, "workflow_run", "id")
	if runID == "" {
		return ""
	}
	return "wf:" + runID
}

func workflowRunPRNumber(m map[string]any) string {
	prs, _ := m["pull_requests"].([]any)
	if len(prs) == 0 {
		return ""
	}
	if pr, ok := prs[0].(map[string]any); ok {
		switch num := pr["number"].(type) {
		case float64:
			return strconv.Itoa(int(num))
		case int:
			return strconv.Itoa(num)
		case int64:
			return strconv.FormatInt(num, 10)
		case string:
			return num
		}
	}
	return ""
}

func workflowRunAttemptID(m map[string]any) string {
	baseID := workflowRunBaseID(m)
	if baseID == "" {
		return ""
	}
	attempt := workflowRunAttempt(m)
	if attempt <= 1 {
		return baseID
	}
	return fmt.Sprintf("%s:attempt:%d", baseID, attempt)
}

func workflowJobAttempt(m map[string]any) int {
	attemptStr := ext(m, "workflow_job", "run_attempt")
	if attemptStr == "" {
		return 1
	}
	attempt, err := strconv.Atoi(attemptStr)
	if err != nil || attempt <= 0 {
		return 1
	}
	return attempt
}

func buildCardWithTimeout(ctx context.Context, repo, sender, senderUrl, avatarUrl string, detail EventDetail) (*Card, error) {
	buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
	defer buildCancel()
	card := BuildCard(buildCtx, repo, sender, senderUrl, avatarUrl, detail)
	if err := buildCtx.Err(); err != nil {
		return nil, fmt.Errorf("failed to build card: %w", err)
	}
	return card, nil
}

func workflowRunStartedAt(m map[string]any) time.Time {
	startedAtStr := ext(m, "workflow_run", "run_started_at")
	if startedAtStr == "" {
		return time.Time{}
	}
	startedAt, err := time.Parse(time.RFC3339, startedAtStr)
	if err != nil {
		return time.Time{}
	}
	return startedAt
}

func nextWorkflowStartedAt(current time.Time, reset bool, m map[string]any, status, conclusion string) time.Time {
	if status != "in_progress" || conclusion != "" {
		return current
	}
	if !reset && !current.IsZero() {
		return current
	}
	if startedAt := workflowRunStartedAt(m); !startedAt.IsZero() {
		return startedAt
	}
	return time.Now()
}

func applyWorkflowTriggeringActor(m map[string]any, sender, senderUrl, avatarUrl string) (string, string, string) {
	login := ext(m, "workflow_run", "triggering_actor", "login")
	if login == "" {
		return sender, senderUrl, avatarUrl
	}
	if htmlURL := ext(m, "workflow_run", "triggering_actor", "html_url"); htmlURL != "" {
		senderUrl = htmlURL
	}
	if avatar := ext(m, "workflow_run", "triggering_actor", "avatar_url"); avatar != "" {
		avatarUrl = avatar
	}
	return login, senderUrl, avatarUrl
}

func workflowRunDisplayName(m map[string]any, fallback string) string {
	name := ext(m, "workflow_run", "name")
	if name == "" {
		name = cleanCIStatusDisplayName(fallback)
	}
	if name == "" {
		name = "Workflow"
	}
	if attempt := workflowRunAttempt(m); attempt > 1 {
		name = fmt.Sprintf("%s (attempt #%d)", name, attempt)
	}
	return name
}

func setCIStatusForWorkflowRun(m map[string]any, detail *EventDetail, status, conclusion, ref string) {
	ciStatus := CIStatus{
		WorkflowName: workflowRunDisplayName(m, detail.Title),
		Status:       status,
		Conclusion:   conclusion,
		Ref:          ref,
		UpdatedAt:    detail.EventTime,
	}
	ciStatus.RunID, _ = strconv.ParseInt(ext(m, "workflow_run", "id"), 10, 64)
	if conclusion != "" {
		ciStatus.Duration = workflowRunDuration(m)
	}
	detail.CIStatuses = []CIStatus{ciStatus}
}

func workflowJobStatus(m map[string]any, detail EventDetail, ref string) CIStatus {
	status, conclusion := extractCIStatus(m, "workflow_job")
	runID, _ := strconv.ParseInt(ext(m, "workflow_job", "run_id"), 10, 64)
	duration := ""
	if conclusion != "" {
		startedAtStr := ext(m, "workflow_job", "started_at")
		completedAtStr := ext(m, "workflow_job", "completed_at")
		if startedAtStr != "" && completedAtStr != "" {
			if startedAt, err := time.Parse(time.RFC3339, startedAtStr); err == nil {
				if completedAt, err := time.Parse(time.RFC3339, completedAtStr); err == nil {
					duration = FormatDuration(completedAt.Sub(startedAt))
				}
			}
		}
	}
	jobID := ext(m, "workflow_job", "id")
	if jobID == "" {
		jobID = ext(m, "workflow_job", "name")
	}
	return CIStatus{
		WorkflowName: "job:" + jobID,
		JobName:      ext(m, "workflow_job", "name"),
		Status:       status,
		Conclusion:   conclusion,
		RunID:        runID,
		ParentRunID:  runID,
		Ref:          ref,
		Duration:     duration,
		UpdatedAt:    detail.EventTime,
	}
}

func upsertCIStatus(statuses []CIStatus, status CIStatus) []CIStatus {
	key := status.WorkflowName
	for i, existing := range statuses {
		if existing.WorkflowName == key {
			statuses[i] = status
			return statuses
		}
	}
	return append(statuses, status)
}

func findMessageRecordByGithubID(ctx context.Context, githubID string) *MessageRecord {
	if githubID == "" || DB == nil {
		return nil
	}
	var record MessageRecord
	if err := DB.NewSelect().Model(&record).
		Where("github_id = ?", githubID).
		Order("id DESC").
		Limit(1).Scan(ctx); err == nil {
		return &record
	}
	return nil
}

func findWorkflowRunRecordForJob(ctx context.Context, repo string, m map[string]any) *MessageRecord {
	if repo == "" || DB == nil {
		return nil
	}
	runID := ext(m, "workflow_job", "run_id")
	if runID == "" {
		return nil
	}
	baseID := "wf:" + runID
	var records []MessageRecord
	attemptPattern := escapeSQLLikePattern(baseID) + ":attempt:%"
	if err := DB.NewSelect().Model(&records).
		Where("repo_name = ?", repo).
		Where("event_type = ?", "workflow_run").
		Where("(github_id = ? OR github_id LIKE ? ESCAPE '\\')", baseID, attemptPattern).
		Order("updated_at DESC").
		Order("id DESC").
		Scan(ctx); err != nil {
		return nil
	}
	return selectWorkflowRunRecordForJob(records, baseID, workflowJobAttempt(m), time.Now())
}

func selectWorkflowRunRecordForJob(records []MessageRecord, baseID string, jobAttempt int, now time.Time) *MessageRecord {
	if baseID == "" {
		return nil
	}
	if jobAttempt <= 0 {
		jobAttempt = 1
	}
	exactID := baseID
	if jobAttempt > 1 {
		exactID = fmt.Sprintf("%s:attempt:%d", baseID, jobAttempt)
	}
	for i := range records {
		if records[i].GithubID == exactID {
			return &records[i]
		}
	}

	cutoff := now.Add(-getMergeWindow())
	var fallback *MessageRecord
	for i := range records {
		recordAttempt, ok := workflowRunRecordAttempt(records[i].GithubID, baseID)
		if !ok || recordAttempt > jobAttempt || !records[i].UpdatedAt.After(cutoff) {
			continue
		}
		if fallback == nil || records[i].UpdatedAt.After(fallback.UpdatedAt) ||
			(records[i].UpdatedAt.Equal(fallback.UpdatedAt) && records[i].ID > fallback.ID) {
			fallback = &records[i]
		}
	}
	return fallback
}

func workflowRunRecordAttempt(githubID, baseID string) (int, bool) {
	if githubID == baseID {
		return 1, true
	}
	prefix := baseID + ":attempt:"
	if !strings.HasPrefix(githubID, prefix) {
		return 0, false
	}
	attempt, err := strconv.Atoi(strings.TrimPrefix(githubID, prefix))
	if err != nil || attempt <= 0 {
		return 0, false
	}
	return attempt, true
}

func escapeSQLLikePattern(pattern string) string {
	pattern = strings.ReplaceAll(pattern, `\`, `\\`)
	pattern = strings.ReplaceAll(pattern, `%`, `\%`)
	pattern = strings.ReplaceAll(pattern, `_`, `\_`)
	return pattern
}

func findPreviousWorkflowRunRecord(ctx context.Context, repo, baseID, currentID string) *MessageRecord {
	if repo == "" || baseID == "" || DB == nil {
		return nil
	}
	attemptPattern := escapeSQLLikePattern(baseID) + ":attempt:%"
	var record MessageRecord
	if err := DB.NewSelect().Model(&record).
		Where("repo_name = ?", repo).
		Where("event_type = ?", "workflow_run").
		Where("github_id != ?", currentID).
		Where("(github_id = ? OR github_id LIKE ? ESCAPE '\\')", baseID, attemptPattern).
		Order("updated_at DESC").
		Order("id DESC").
		Limit(1).Scan(ctx); err == nil {
		return &record
	}
	return nil
}

func resetWorkflowTimeoutNotification(ctx context.Context, record *MessageRecord) {
	if record == nil || DB == nil {
		return
	}
	_, _ = DB.NewUpdate().Model(record).
		Set("timeout_notified = ?", false).
		WherePK().
		Where("timeout_notified = ?", true).
		Exec(ctx)
	record.TimeoutNotified = false
}

func claimWorkflowTimeoutNotification(ctx context.Context, record *MessageRecord) bool {
	if record == nil || DB == nil {
		return false
	}
	res, err := DB.NewUpdate().Model(record).
		Set("timeout_notified = ?", true).
		WherePK().
		Where("timeout_notified = ?", false).
		Exec(ctx)
	if err != nil {
		slog.Warn("Failed to claim workflow timeout notification", "github_id", record.GithubID, "error", err)
		return false
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false
	}
	if affected > 0 {
		record.TimeoutNotified = true
		return true
	}
	return false
}

func maybeSendWorkflowTimeoutNotification(ctx context.Context, record *MessageRecord, status, conclusion, title string) {
	if record == nil ||
		conclusion != "" ||
		status != "in_progress" ||
		record.WorkflowStartedAt.IsZero() ||
		time.Since(record.WorkflowStartedAt) <= 10*time.Minute ||
		record.TimeoutNotified {
		return
	}
	if !claimWorkflowTimeoutNotification(ctx, record) {
		return
	}
	if err := sendTimeoutNotification(record.FeishuMessageID, title, record.WorkflowStartedAt); err != nil {
		slog.Error("Failed to send timeout notification", "github_id", record.GithubID, "error", err)
	}
}

func updateWorkflowRunRecordWithJob(
	ctx context.Context,
	record *MessageRecord,
	event WebhookEvent,
	m map[string]any,
	jobDetail EventDetail,
	repo, sender, senderUrl, avatarUrl, ref, sha string,
) (EventDetail, string, error) {
	if record == nil {
		return EventDetail{}, "", nil
	}
	var existingDetail EventDetail
	_ = json.Unmarshal([]byte(record.Content), &existingDetail)
	if existingDetail.RepoURL == "" {
		if repoURL := ext(m, "repository", "html_url"); repoURL != "" {
			existingDetail.RepoURL = repoURL
		} else if record.RepoName != "" {
			existingDetail.RepoURL = fmt.Sprintf("https://github.com/%s", record.RepoName)
		}
	}
	if existingDetail.RepoName == "" {
		existingDetail.RepoName = repo
	}
	if existingDetail.EventTime == "" {
		existingDetail.EventTime = jobDetail.EventTime
	}

	jobStatus := workflowJobStatus(m, jobDetail, ref)
	existingDetail.CIStatuses = upsertCIStatus(existingDetail.CIStatuses, jobStatus)

	cardRepo := record.RepoName
	if cardRepo == "" {
		cardRepo = repo
	}
	cardSender := record.Sender
	if cardSender == "" {
		cardSender = sender
	}
	cardSenderURL := record.SenderURL
	if cardSenderURL == "" {
		cardSenderURL = senderUrl
	}
	cardAvatarURL := record.AvatarURL2
	if cardAvatarURL == "" {
		cardAvatarURL = avatarUrl
	}

	card, err := buildCardWithTimeout(ctx, cardRepo, cardSender, cardSenderURL, cardAvatarURL, existingDetail)
	if err != nil {
		return EventDetail{}, "", err
	}

	oldMsgID := record.FeishuMessageID
	msgID := record.FeishuMessageID
	parentMsgID := record.ParentMsgID
	if record.ParentMsgID != "" {
		msgID, err = SendToChat("", card)
		if err != nil {
			return EventDetail{}, "", fmt.Errorf("failed to send standalone workflow card: %w", err)
		}
		record.FeishuMessageID = msgID
		record.ParentMsgID = ""
	} else if err := UpdateMessage(record.FeishuMessageID, card); err != nil {
		return EventDetail{}, "", fmt.Errorf("failed to update workflow card with job: %w", err)
	}

	updatedJson, _ := json.Marshal(existingDetail)
	updateQ := DB.NewUpdate().Model(record).
		Set("content = ?", string(updatedJson)).
		Set("card_string = ?", card.String()).
		Set("event_id = ?", event.ID).
		Set("updated_at = ?", time.Now()).
		WherePK()
	if msgID != oldMsgID {
		updateQ = updateQ.Set("feishu_message_id = ?", msgID)
	}
	if parentMsgID != "" {
		updateQ = updateQ.Set("parent_msg_id = ?", "")
	}
	if sha != "" {
		updateQ = updateQ.Set("head_sha = ?", sha)
	}
	if _, err = updateQ.Exec(ctx); err != nil {
		return EventDetail{}, "", err
	}
	if parentMsgID != "" {
		clearParentCardCI(ctx, parentMsgID)
	}
	return existingDetail, card.String(), nil
}

func updateWorkflowRunRecord(
	ctx context.Context,
	record *MessageRecord,
	newGithubID string,
	event WebhookEvent,
	m map[string]any,
	detail EventDetail,
	repo, sender, senderUrl, avatarUrl, ref, sha string,
) error {
	if record == nil {
		return nil
	}
	status, conclusion := extractCIStatus(m, event.EventType)
	setCIStatusForWorkflowRun(m, &detail, status, conclusion, ref)

	if conclusion != "" {
		resetWorkflowTimeoutNotification(ctx, record)
	}

	switchingGithubID := newGithubID != "" && newGithubID != record.GithubID
	if !switchingGithubID {
		var previousDetail EventDetail
		if err := json.Unmarshal([]byte(record.Content), &previousDetail); err == nil {
			runID := int64(0)
			if len(detail.CIStatuses) > 0 {
				runID = detail.CIStatuses[0].RunID
			}
			for _, cs := range previousDetail.CIStatuses {
				if strings.HasPrefix(cs.WorkflowName, "job:") && (runID == 0 || cs.ParentRunID == runID) {
					detail.CIStatuses = upsertCIStatus(detail.CIStatuses, cs)
				}
			}
		}
	}
	if switchingGithubID {
		resetWorkflowTimeoutNotification(ctx, record)
	}
	workflowStartedAt := nextWorkflowStartedAt(record.WorkflowStartedAt, switchingGithubID, m, status, conclusion)
	timeoutRecord := *record
	timeoutRecord.WorkflowStartedAt = workflowStartedAt
	maybeSendWorkflowTimeoutNotification(ctx, &timeoutRecord, status, conclusion, detail.Title)
	detailJson, _ := json.Marshal(detail)

	updateQ := DB.NewUpdate().Model(record).
		Set("content = ?", string(detailJson)).
		Set("event_id = ?", event.ID).
		Set("workflow_started_at = ?", workflowStartedAt).
		Set("head_sha = ?", sha).
		Set("sender = ?", sender).
		Set("sender_url = ?", senderUrl).
		Set("avatar_url2 = ?", avatarUrl).
		Set("updated_at = ?", time.Now()).
		WherePK()
	if newGithubID != "" && newGithubID != record.GithubID {
		updateQ = updateQ.Set("github_id = ?", newGithubID)
	}

	if record.ParentMsgID != "" {
		card, err := buildCardWithTimeout(ctx, repo, sender, senderUrl, avatarUrl, detail)
		if err != nil {
			return err
		}
		msgID, err := SendToChat("", card)
		if err != nil {
			return fmt.Errorf("failed to send standalone workflow card: %w", err)
		}
		parentMsgID := record.ParentMsgID
		_, err = updateQ.
			Set("feishu_message_id = ?", msgID).
			Set("parent_msg_id = ?", "").
			Set("card_string = ?", card.String()).
			Exec(ctx)
		if err == nil {
			clearParentCardCI(ctx, parentMsgID)
		}
		return err
	}

	card, err := buildCardWithTimeout(ctx, repo, sender, senderUrl, avatarUrl, detail)
	if err != nil {
		return err
	}
	if err := UpdateMessage(record.FeishuMessageID, card); err != nil {
		return err
	}
	_, err = updateQ.Set("card_string = ?", card.String()).Exec(ctx)
	return err
}

func workflowRunRerunNotice(m map[string]any, sender string) string {
	attempt := workflowRunAttempt(m)
	if attempt <= 1 {
		return ""
	}
	actor := ext(m, "workflow_run", "triggering_actor", "login")
	if actor == "" {
		actor = sender
	}
	actorText := actor
	if actorURL := ext(m, "workflow_run", "triggering_actor", "html_url"); actorURL != "" && actor != "" {
		actorText = fmt.Sprintf("[%s](%s)", actor, actorURL)
	}
	attemptText := fmt.Sprintf("attempt #%d", attempt)
	if runURL := ext(m, "workflow_run", "html_url"); runURL != "" {
		attemptText = fmt.Sprintf("[%s](%s)", attemptText, runURL)
	}
	if actorText == "" {
		return fmt.Sprintf("🔁 This workflow was rerun as %s.", attemptText)
	}
	return fmt.Sprintf("🔁 This workflow was rerun as %s by %s.", attemptText, actorText)
}

func tryMarkPreviousWorkflowRunRerun(
	ctx context.Context,
	record *MessageRecord,
	m map[string]any,
	repo, repoUrl, sender, senderUrl, avatarUrl string,
) {
	if record == nil {
		return
	}
	notice := workflowRunRerunNotice(m, sender)
	if notice == "" {
		return
	}
	var previousDetail EventDetail
	if err := json.Unmarshal([]byte(record.Content), &previousDetail); err != nil {
		slog.Warn("Failed to parse previous workflow detail for rerun notice",
			"github_id", record.GithubID,
			"message_id", record.FeishuMessageID,
			"error", err)
		return
	}
	previousDetail.Notice = notice
	if previousDetail.RepoName == "" {
		previousDetail.RepoName = repo
	}
	if previousDetail.RepoURL == "" {
		previousDetail.RepoURL = repoUrl
	}

	recordSender := record.Sender
	if recordSender == "" {
		recordSender = sender
	}
	recordSenderURL := record.SenderURL
	if recordSenderURL == "" {
		recordSenderURL = senderUrl
	}
	recordAvatarURL := record.AvatarURL2
	if recordAvatarURL == "" {
		recordAvatarURL = record.AvatarURL
	}
	if recordAvatarURL == "" {
		recordAvatarURL = avatarUrl
	}

	card, err := buildCardWithTimeout(ctx, repo, recordSender, recordSenderURL, recordAvatarURL, previousDetail)
	if err != nil {
		slog.Warn("Failed to build previous workflow rerun notice card",
			"github_id", record.GithubID,
			"message_id", record.FeishuMessageID,
			"error", err)
		return
	}
	if err := UpdateMessage(record.FeishuMessageID, card); err != nil {
		slog.Warn("Failed to mark previous workflow message after rerun",
			"github_id", record.GithubID,
			"message_id", record.FeishuMessageID,
			"error", err)
		return
	}
	detailJson, _ := json.Marshal(previousDetail)
	_, _ = DB.NewUpdate().Model(record).
		Set("content = ?", string(detailJson)).
		Set("card_string = ?", card.String()).
		WherePK().
		Exec(ctx)
	slog.Info("Previous workflow message marked after rerun", "github_id", record.GithubID, "message_id", record.FeishuMessageID)
}

func workflowRunDuration(m map[string]any) string {
	startedAtStr := ext(m, "workflow_run", "run_started_at")
	updatedAtStr := ext(m, "workflow_run", "updated_at")
	if startedAtStr == "" || updatedAtStr == "" {
		return ""
	}
	startedAt, err := time.Parse(time.RFC3339, startedAtStr)
	if err != nil {
		return ""
	}
	updatedAt, err := time.Parse(time.RFC3339, updatedAtStr)
	if err != nil {
		return ""
	}
	return FormatDuration(updatedAt.Sub(startedAt))
}

// sendTimeoutNotification 发送 Workflow 超时提醒回复
func sendTimeoutNotification(parentMsgID, title string, startedAt time.Time) error {
	timeoutCard := NewCard()
	timeoutCard.Header.Title = CardText{Tag: "plain_text", Content: "⏰ Workflow 运行超时提醒"}
	timeoutCard.Header.Template = "orange"
	duration := time.Since(startedAt).Round(time.Minute)
	timeoutCard.AddMarkdown(fmt.Sprintf("**%s** 已经运行 **%s**，请检查是否卡住", title, duration))
	if _, err := ReplyToMessage(parentMsgID, timeoutCard); err != nil {
		return err
	}
	return nil
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

func memberTrackingID(payload map[string]any, repo string) string {
	memberID := ext(payload, "member", "id")
	if memberID == "" {
		memberID = ext(payload, "member", "login")
	}
	if repo == "" || memberID == "" {
		return ""
	}
	return fmt.Sprintf("member:%s:%s", repo, memberID)
}

func mergeMemberDetails(old, new *EventDetail) {
	new.Text = strings.Join(mergeMemberOperationLines(memberOperationLines(old.Text), memberOperationLines(new.Text)), "\n")
	new.Title = "👤 Member updates"
	new.Action = "updated"
	currentTime := new.EventTime
	if old.EventTime != "" {
		new.EventTime = old.EventTime
	}
	new.EventTimeEnd = currentTime

	new.EventCount = countMemberOperationLines(new.Text)
	new.AuthorLogins = mergeUniqueStrings(old.AuthorLogins, new.AuthorLogins)
	new.AuthorAvatars = mergeUniqueStrings(old.AuthorAvatars, new.AuthorAvatars)
}

func countMemberOperationLines(text string) int {
	return len(memberOperationLines(text))
}

func memberOperationLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			lines = append(lines, line)
		}
	}
	if len(lines) > 0 {
		return lines
	}
	if line := legacyMemberOperationLine(text); line != "" {
		return []string{line}
	}

	var parts []string
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			parts = append(parts, line)
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return []string{"- " + strings.Join(parts, "; ")}
}

func mergeMemberOperationLines(oldLines, newLines []string) []string {
	seen := make(map[string]bool)
	lines := make([]string, 0, len(oldLines)+len(newLines))
	for _, line := range append(oldLines, newLines...) {
		line = strings.TrimSpace(line)
		if line == "" || seen[line] {
			continue
		}
		seen[line] = true
		lines = append(lines, line)
	}
	return lines
}

func legacyMemberOperationLine(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	action := legacyMemberField(lines, "Action:")
	member := legacyMemberField(lines, "Member:")
	if action == "" || member == "" {
		return ""
	}
	line := fmt.Sprintf("- **%s** member **%s**", action, member)
	if sender := legacyMemberField(lines, "By:"); sender != "" && sender != member {
		line += fmt.Sprintf(" by **%s**", sender)
	}
	return line
}

func legacyMemberField(lines []string, prefix string) string {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), strings.ToLower(prefix)) {
			return trimMarkdownBold(strings.TrimSpace(line[len(prefix):]))
		}
	}
	return ""
}

func trimMarkdownBold(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "**") && strings.HasSuffix(s, "**") && len(s) >= 4 {
		s = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "**"), "**"))
	}
	return s
}

func mergeUniqueStrings(oldValues, newValues []string) []string {
	seen := make(map[string]bool)
	values := make([]string, 0, len(oldValues)+len(newValues))
	for _, value := range append(oldValues, newValues...) {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		values = append(values, value)
	}
	return values
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

func clearParentCardCI(ctx context.Context, parentMsgID string) {
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
	if len(parentDetail.CIStatuses) == 0 {
		return
	}
	parentDetail.CIStatuses = nil
	if parentDetail.RepoURL == "" && parentRecord.RepoName != "" {
		parentDetail.RepoURL = fmt.Sprintf("https://github.com/%s", parentRecord.RepoName)
	}

	buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
	card := BuildCard(buildCtx, parentRecord.RepoName, parentRecord.Sender, parentRecord.SenderURL, parentRecord.AvatarURL2, parentDetail)
	buildCancel()

	if err := UpdateMessage(parentMsgID, card); err != nil {
		slog.Error("Failed to clear CI status from parent card", "parent", parentMsgID, "error", err)
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
	var previousWorkflowRunRecord *MessageRecord

	// 1. 解析 Payload
	payload := []byte(event.Payload)
	githubEvent, err := parseWebhookPayload(event.EventType, payload)
	if err != nil {
		return fmt.Errorf("failed to parse Webhook: %w", err)
	}

	// 跳过不需要通知的管理员/基础设施事件
	adminEvents := map[string]bool{
		"ping": true, "meta": true, "installation": true,
		"installation_repositories": true, "installation_target": true,
		"github_app_authorization": true, "marketplace_purchase": true,
		"registry_package": true, "content_reference": true,
		"custom_property": true, "custom_property_values": true,
		"repository_import": true, "secret_scanning_alert_location": true,
		"deployment_protection_rule": true, "deployment_review": true,
		"user": true, "sponsorship": true,
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
	webhookSender := ext(m, "sender", "login")
	sender := webhookSender
	senderUrl := ext(m, "sender", "html_url")
	avatarUrl := ext(m, "sender", "avatar_url")
	detail.RepoName = repo   // 用于合并展示的仓库全名
	detail.RepoURL = repoUrl // 用于 BuildCard 构建链接
	ref := ext(m, "ref")
	// Workflow 事件的 ref 在 head_branch 中
	if ref == "" {
		ref = ext(m, "workflow_run", "head_branch")
	}
	if ref == "" {
		ref = ext(m, "workflow_job", "head_branch")
	}
	isCIEvent := isCIEventType(event.EventType)
	if event.EventType == "workflow_run" {
		sender, senderUrl, avatarUrl = applyWorkflowTriggeringActor(m, sender, senderUrl, avatarUrl)
	}

	// 检查是否为 Bot 用户
	isBotUser := false
	botSender := ""
	if matchedSender, ok := configuredBotEventActor(m, sender, webhookSender); ok {
		isBotUser = true
		botSender = matchedSender
	}
	// Bot 用户只处理 PR 和 Comment/Issue 互动事件，其他一律跳过
	if isBotUser {
		if !botUserInteractionEvent(event.EventType) {
			slog.Info("Bot user event skipped", "sender", botSender, "event", event.EventType)
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
		githubID = workflowRunAttemptID(m)
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
	case "branch_protection_configuration":
		githubID = fmt.Sprintf("bpconfig:%s:%s", repo, ext(m, "action"))
	case "repository_ruleset":
		githubID = fmt.Sprintf("ruleset:%s:%s", repo, ext(m, "repository_ruleset", "id"))
	case "repository_advisory":
		githubID = fmt.Sprintf("repository_advisory:%s:%s", repo, ext(m, "repository_advisory", "ghsa_id"))
	case "repository":
		githubID = fmt.Sprintf("repository:%s:%s", repo, ext(m, "action"))
	case "public":
		githubID = fmt.Sprintf("public:%s", repo)
	case "member":
		githubID = memberTrackingID(m, repo)
	case "membership":
		githubID = fmt.Sprintf("membership:%s:%s:%s:%s", ext(m, "organization", "login"), ext(m, "team", "id"), ext(m, "member", "id"), ext(m, "action"))
	case "team":
		githubID = fmt.Sprintf("team:%s:%s:%s", repo, ext(m, "team", "id"), ext(m, "action"))
	case "organization":
		githubID = fmt.Sprintf("organization:%s:%s:%s", ext(m, "organization", "login"), ext(m, "membership", "user", "id"), ext(m, "action"))
	case "org_block":
		githubID = fmt.Sprintf("org_block:%s:%s:%s", ext(m, "organization", "login"), ext(m, "blocked_user", "id"), ext(m, "action"))
	case "security_and_analysis":
		githubID = fmt.Sprintf("security_analysis:%s", repo)
	case "personal_access_token_request":
		githubID = fmt.Sprintf("pat_request:%s:%s", ext(m, "organization", "login"), ext(m, "personal_access_token_request", "id"))
	case "deploy_key":
		githubID = fmt.Sprintf("deploy_key:%s:%s:%s", repo, ext(m, "key", "id"), ext(m, "action"))
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

	if lockKey := webhookMergeLockKey(event.EventType, detail, repo); lockKey != "" {
		releaseLock, err := acquireEventLock(ctx, lockKey, 2*time.Minute)
		if err != nil {
			return err
		}
		defer releaseLock()
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

	// 3.6 合并窗口内同一 GitHub 对象直接更新原卡片。issues.opened 和
	// issues.edited 共用追踪 ID，因此编辑会 Patch 原来的 New Issue 消息。
	if githubID != "" && shouldUpdateWithinMergeWindow(event.EventType, isCIEvent) {
		updated, err := tryMergeWithExisting(ctx,
			mergeSearch{githubID: githubID, eventType: event.EventType, withinWindow: true},
			func(_, _ *EventDetail) {},
			"", event.ID, &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"Existing card updated within merge window",
		)
		if updated {
			return err
		}
	}

	// 4. 合并与更新逻辑
	// 4.1 CI/CD 事件 (Workflow, Check Run)：更新同一条消息，支持超时提醒
	if event.EventType == "workflow_run" && githubID != "" {
		status, conclusion := extractCIStatus(m, event.EventType)
		setCIStatusForWorkflowRun(m, &detail, status, conclusion, ref)

		if record := findMessageRecordByGithubID(ctx, githubID); record != nil {
			if err := updateWorkflowRunRecord(ctx, record, "", event, m, detail, repo, sender, senderUrl, avatarUrl, ref, sha); err != nil {
				return fmt.Errorf("failed to update workflow run message: %w", err)
			}
			slog.Info("Workflow run attempt updated", "github_id", githubID, "attempt", workflowRunAttempt(m))
			return nil
		}

		previousWorkflowRunRecord = findPreviousWorkflowRunRecord(ctx, repo, workflowRunBaseID(m), githubID)
		if previousWorkflowRunRecord != nil {
			if time.Since(previousWorkflowRunRecord.UpdatedAt) <= getMergeWindow() {
				if err := updateWorkflowRunRecord(ctx, previousWorkflowRunRecord, githubID, event, m, detail, repo, sender, senderUrl, avatarUrl, ref, sha); err != nil {
					return fmt.Errorf("failed to update workflow rerun within merge window: %w", err)
				}
				slog.Info("Workflow rerun updated within merge window", "old_github_id", previousWorkflowRunRecord.GithubID, "new_github_id", githubID, "attempt", workflowRunAttempt(m))
				return nil
			}
			slog.Info("Workflow rerun outside merge window, creating new message", "old_github_id", previousWorkflowRunRecord.GithubID, "new_github_id", githubID, "attempt", workflowRunAttempt(m))
		}
	}

	if event.EventType == "workflow_job" && githubID != "" {
		if workflowRecord := findWorkflowRunRecordForJob(ctx, repo, m); workflowRecord != nil {
			workflowDetail, workflowCardString, err := updateWorkflowRunRecordWithJob(ctx, workflowRecord, event, m, detail, repo, sender, senderUrl, avatarUrl, ref, sha)
			if err != nil {
				return err
			}

			workflowDetailJson, _ := json.Marshal(workflowDetail)
			_, insertErr := DB.NewInsert().Model(&MessageRecord{
				GithubID:        githubID,
				FeishuMessageID: workflowRecord.FeishuMessageID,
				ChatID:          C.Feishu.ChatID,
				RepoName:        repo,
				Ref:             ref,
				EventType:       event.EventType,
				Content:         string(workflowDetailJson),
				CardString:      workflowCardString,
				EventID:         event.ID,
				HeadSHA:         sha,
				Sender:          sender,
				SenderURL:       senderUrl,
				AvatarURL2:      avatarUrl,
			}).On("CONFLICT (github_id) DO UPDATE").
				Set("feishu_message_id = EXCLUDED.feishu_message_id").
				Set("content = EXCLUDED.content").
				Set("card_string = EXCLUDED.card_string").
				Set("event_id = EXCLUDED.event_id").
				Set("updated_at = NOW()").
				Set("head_sha = EXCLUDED.head_sha").
				Set("parent_msg_id = EXCLUDED.parent_msg_id").
				Exec(ctx)
			if insertErr != nil {
				slog.Error("Failed to upsert workflow job record", "github_id", githubID, "error", insertErr)
			}

			slog.Info("Workflow job updated workflow card", "github_id", githubID, "workflow", workflowRecord.GithubID)
			return nil
		}

		if event.RescheduleCount < 5 {
			retryAt := nextWorkflowJobRetryAt(time.Now())
			slog.Info("Rescheduling workflow job, waiting for workflow_run", "github_id", githubID, "run_id", ext(m, "workflow_job", "run_id"), "reschedule", event.RescheduleCount+1, "retry_at", retryAt)
			if _, err := DB.NewUpdate().Model(&event).
				Set("status = ?", "pending").
				Set("reschedule_count = reschedule_count + 1").
				Set("updated_at = ?", retryAt).
				WherePK().Exec(ctx); err != nil {
				return fmt.Errorf("failed to reschedule workflow job: %w", err)
			}
			return nil
		}

		detail.CIStatuses = []CIStatus{workflowJobStatus(m, detail, ref)}
	}

	if isCIEvent && githubID != "" {
		var record MessageRecord
		err := DB.NewSelect().Model(&record).
			Where("github_id = ?", githubID).
			Order("id DESC").
			Limit(1).Scan(ctx)

		if err == nil {
			status, conclusion := extractCIStatus(m, event.EventType)

			if conclusion != "" {
				resetWorkflowTimeoutNotification(ctx, &record)
			}
			maybeSendWorkflowTimeoutNotification(ctx, &record, status, conclusion, detail.Title)

			// 兼容旧模式：独立 CI 消息
			// workflow_job 事件：将 job 信息追加到 CIStatuses，而不是覆盖
			if event.EventType == "workflow_job" {
				var existingDetail EventDetail
				_ = json.Unmarshal([]byte(record.Content), &existingDetail)

				existingDetail.CIStatuses = upsertCIStatus(existingDetail.CIStatuses, workflowJobStatus(m, detail, ref))

				// 重建卡片
				buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
				card := BuildCard(buildCtx, repo, sender, senderUrl, avatarUrl, existingDetail)
				buildCancel()

				parentMsgID := record.ParentMsgID
				if parentMsgID != "" {
					msgID, sendErr := SendToChat("", card)
					if sendErr != nil {
						return fmt.Errorf("failed to send standalone workflow job card: %w", sendErr)
					}
					record.FeishuMessageID = msgID
					record.ParentMsgID = ""
				} else if err := UpdateMessage(record.FeishuMessageID, card); err != nil {
					return fmt.Errorf("failed to update message: %w", err)
				}

				updatedJson, _ := json.Marshal(existingDetail)
				updateQ := DB.NewUpdate().Model(&record).
					Set("content = ?", string(updatedJson)).
					Set("card_string = ?", card.String()).
					Set("updated_at = ?", time.Now()).
					WherePK()
				updateQ = updateQ.Set("parent_msg_id = ?", record.ParentMsgID)
				updateQ = updateQ.Set("feishu_message_id = ?", record.FeishuMessageID)
				if _, updateErr := updateQ.Exec(ctx); updateErr != nil {
					return updateErr
				}
				if parentMsgID != "" {
					clearParentCardCI(ctx, parentMsgID)
				}

				slog.Info("Workflow job appended", "github_id", githubID, "job", ext(m, "workflow_job", "name"))
				return nil
			}

			// workflow_run 等其他 CI 事件：直接更新
			buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
			card := BuildCard(buildCtx, repo, sender, senderUrl, avatarUrl, detail)
			buildCancel()

			parentMsgID := record.ParentMsgID
			if parentMsgID != "" {
				msgID, sendErr := SendToChat("", card)
				if sendErr != nil {
					return fmt.Errorf("failed to send standalone CI card: %w", sendErr)
				}
				record.FeishuMessageID = msgID
				record.ParentMsgID = ""
			} else if err := UpdateMessage(record.FeishuMessageID, card); err != nil {
				return fmt.Errorf("failed to update message: %w", err)
			}

			detailJson, _ := json.Marshal(detail)
			if _, updateErr := DB.NewUpdate().Model(&record).
				Set("content = ?", string(detailJson)).
				Set("card_string = ?", card.String()).
				Set("updated_at = ?", time.Now()).
				Set("feishu_message_id = ?", record.FeishuMessageID).
				Set("parent_msg_id = ?", record.ParentMsgID).
				WherePK().Exec(ctx); updateErr != nil {
				return updateErr
			}
			if parentMsgID != "" {
				clearParentCardCI(ctx, parentMsgID)
			}

			slog.Info("Workflow card updated", "github_id", githubID, "event_type", event.EventType)
			return nil
		}
	}

	// 4.2 Release 事件：更新同一个 release 的消息（编辑、正式发布等）
	if event.EventType == "release" && githubID != "" {
		merged, err := tryMergeWithExisting(ctx,
			mergeSearch{githubID: githubID, eventType: "release"},
			func(_, new *EventDetail) {}, // Release 直接替换，无需合并
			"", event.ID, &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
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
			"", event.ID, &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
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
			"", event.ID, &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"Deployment status updated",
		)
		if merged {
			return err
		}
	}

	// 4.2b Member 事件：同一仓库同一成员在合并窗口内的权限/成员操作合并为一条
	if event.EventType == "member" && githubID != "" {
		merged, err := tryMergeWithExisting(ctx,
			mergeSearch{
				githubID:     githubID,
				githubIDLike: githubID + ":%", // 兼容旧的 action 粒度记录：member:<repo>:<id>:<action>
				eventType:    "member",
				withinWindow: true,
			},
			mergeMemberDetails,
			"", event.ID, &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"Member updates merged",
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
			sha, event.ID, &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
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
			"", event.ID, &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"Branch deletions combined",
		)
		if merged {
			return err
		}
	}

	// 4.4 标签删除合并：同一仓库在合并窗口内的标签删除合并（支持 push 与历史 delete 事件）
	if (event.EventType == "push" || event.EventType == "delete") && detail.IsDeleted && detail.IsTag {
		merged, err := tryMergeWithExisting(ctx,
			mergeSearch{
				githubIDLikes: []string{
					fmt.Sprintf("push:%s:refs/tags/%%", repo),
					fmt.Sprintf("delete:%s:tag:%%", repo),
				},
				recordType:   "deleted",
				withinWindow: true,
			},
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
			"", event.ID, &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
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
			"", event.ID, &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
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
			"", event.ID, &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
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
			"", event.ID, &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
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
			"", event.ID, &detail, repo, repoUrl, sender, senderUrl, avatarUrl,
			"PR comments merged",
		)
		if merged {
			return err
		}
	}

	// 4.6 Tag 关联最近的 commit，以话题形式回复。CI/Workflow 独立成卡，不再混入 commit 卡片。
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

	// 4.7 Workflow 关联 PR：workflow_run 事件尝试通过 pull_requests 数组或 SHA 关联父 PR 消息
	if event.EventType == "workflow_run" && parentID == "" {
		if prNum := workflowRunPRNumber(m); prNum != "" {
			var record MessageRecord
			searchID := fmt.Sprintf("pr:%s:%s", repo, prNum)
			if err := DB.NewSelect().Model(&record).
				Where("github_id = ?", searchID).
				Where("updated_at > ?", time.Now().Add(-getThreadReplyWindow())).
				Order("id ASC").Limit(1).Scan(ctx); err == nil {
				parentID = record.FeishuMessageID
			}
		}
		if parentID == "" && sha != "" {
			if rec := findParentRecordBySHA(ctx, repo, sha); rec != nil {
				parentID = rec.FeishuMessageID
			}
		}
	}

	// 5. 查找父级 ID (回复逻辑)
	// 改为：只要是 Issue/PR/Discussion 相关的非"创建"事件，都尝试寻找父消息进行话题回复
	// 注意：CI/Workflow 事件不在这里处理，它们独立成卡，由 4.1 维护状态。
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
		slog.Info("Bot user event skipped: no parent message found", "sender", botSender, "event", event.EventType)
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
	// 7. 保存记录
	if githubID != "" && msgID != "" {
		detailJson, _ := json.Marshal(detail)

		// CI 事件：记录开始时间
		workflowStartedAt := time.Time{}
		if isCIEvent {
			status, conclusion := extractCIStatus(m, event.EventType)
			if status == "in_progress" && conclusion == "" {
				workflowStartedAt = nextWorkflowStartedAt(time.Time{}, true, m, status, conclusion)
			}
		}

		// push/create 事件：记录 head SHA 供后续 CI/tag 精确关联
		headSHA := ""
		if event.EventType == "push" || event.EventType == "create" {
			headSHA = sha
			slog.Info("Storing head_sha for event", "event_type", event.EventType, "head_sha", headSHA, "repo", repo)
		}
		if isCIEvent {
			headSHA = sha
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
		if event.EventType == "workflow_run" && previousWorkflowRunRecord != nil && previousWorkflowRunRecord.FeishuMessageID != msgID {
			tryMarkPreviousWorkflowRunRerun(ctx, previousWorkflowRunRecord, m, repo, repoUrl, sender, senderUrl, avatarUrl)
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
		if isMessageUpdateExpired(err) {
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
