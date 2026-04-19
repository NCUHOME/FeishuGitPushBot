package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/go-github/v84/github"
	"strings"
	"sync"
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
			// 处理成功，标记已处理
			_, _ = DB.NewUpdate().Model(&event).
				Set("status = ?", "processed").
				Set("updated_at = ?", time.Now()).
				WherePK().Exec(context.Background())
		}

		// 推送间隔，保证节奏
		time.Sleep(1 * time.Second)
	}
}

func processWebhookEvent(event WebhookEvent) error {
	ctx := context.Background()

	// 1. 解析 Payload
	payload := []byte(event.Payload)
	githubEvent, err := github.ParseWebHook(event.EventType, payload)
	if err != nil {
		return fmt.Errorf("failed to parse Webhook: %w", err)
	}

	detail := ParseEvent(githubEvent, event.EventType)
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
		for _, u := range strings.Split(C.Github.BotUsers, ",") {
			if strings.TrimSpace(u) == sender {
				isBotUser = true
				break
			}
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
	if sha == "" {
		sha = ext(m, "pull_request", "head", "sha")
	}
	if sha == "" {
		sha = ext(m, "workflow_run", "head_sha")
	}
	if sha == "" {
		sha = ext(m, "workflow_job", "head_sha")
	}
	shortSHA := sha
	if len(sha) > 7 {
		shortSHA = sha[:7]
	}

	// 3. 构建追踪 ID
	var githubID string
	switch event.EventType {
	case "workflow_run":
		githubID = "wr:" + ext(m, "workflow_run", "id")
	case "workflow_job":
		// 使用 job 自己的 id，而不是 run_id，这样每个 job 都有独立的通知
		githubID = "wj:" + ext(m, "workflow_job", "id")
	case "push":
		githubID = fmt.Sprintf("push:%s:%s", repo, ref)
	case "create":
		// 创建事件：区分 tag 和 branch
		refType := ext(m, "ref_type")
		ref := ext(m, "ref")
		githubID = fmt.Sprintf("create:%s:%s:%s", repo, refType, ref)
	case "delete":
		// 删除事件：区分 tag 和 branch
		refType := ext(m, "ref_type")
		ref := ext(m, "ref")
		githubID = fmt.Sprintf("delete:%s:%s:%s", repo, refType, ref)
	case "release":
		// release 事件按 tag 区分，支持更新
		githubID = fmt.Sprintf("release:%s:%s", repo, ext(m, "release", "tag_name"))
	case "pull_request":
		githubID = fmt.Sprintf("pr:%s:%s", repo, ext(m, "pull_request", "number"))
	case "issues":
		githubID = fmt.Sprintf("issue:%s:%s", repo, ext(m, "issue", "number"))
	default:
		githubID = sha
		if githubID == "" {
			issueNum := ext(m, "issue", "number")
			if issueNum != "" {
				githubID = fmt.Sprintf("issue:%s:%s", repo, issueNum)
			}
		}
	}

	// 4. 合并与更新逻辑
	// 4.1 Workflow 事件：更新同一条 workflow 的消息，支持超时提醒
	if (event.EventType == "workflow_run" || event.EventType == "workflow_job") && githubID != "" {
		var record MessageRecord
		rawID := strings.TrimPrefix(strings.TrimPrefix(githubID, "wr:"), "wj:")
		err := DB.NewSelect().Model(&record).
			Where("github_id = ? OR github_id = ?", githubID, rawID).
			Order("id DESC").
			Limit(1).Scan(ctx)

		// 如果没找到对应的 workflow 记录，尝试寻找对应的 push 记录进行“合并更新”
		isFirstWorkflowEvent := false
		if err != nil && shortSHA != "" {
			err = DB.NewSelect().Model(&record).
				Where("repo_name = ? AND event_type = 'push' AND content LIKE ?", repo, "%"+shortSHA+"%").
				Order("id DESC").Limit(1).Scan(ctx)
			if err == nil {
				isFirstWorkflowEvent = true
				slog.Info("Matching push record found for workflow", "sha", shortSHA, "msg_id", record.FeishuMessageID)
			}
		}

		if err == nil {
			// 获取当前状态
			status := ""
			conclusion := ""
			if event.EventType == "workflow_run" {
				status = ext(m, "workflow_run", "status")
				conclusion = ext(m, "workflow_run", "conclusion")
			} else {
				status = ext(m, "workflow_job", "status")
				conclusion = ext(m, "workflow_job", "conclusion")
			}

			// 检查是否需要超时提醒（运行中且超过10分钟）
			needTimeoutNotify := false
			if conclusion == "" && status == "in_progress" && !record.WorkflowStartedAt.IsZero() {
				if time.Since(record.WorkflowStartedAt) > 10*time.Minute && !record.TimeoutNotified {
					needTimeoutNotify = true
				}
			}

			// 如果已完成，重置超时提醒标志
			if conclusion != "" && record.TimeoutNotified {
				_, _ = DB.NewUpdate().Model(&record).
					Set("timeout_notified = ?", false).
					WherePK().Exec(ctx)
			}

			// 如果需要超时提醒，发送回复提醒
			if needTimeoutNotify {
				timeoutCard := NewCard()
				timeoutCard.Header.Title = CardText{Tag: "plain_text", Content: "⏰ Workflow 运行超时提醒"}
				timeoutCard.Header.Template = "orange"
				duration := time.Since(record.WorkflowStartedAt).Round(time.Minute)
				timeoutCard.AddMarkdown(fmt.Sprintf("**%s** 已经运行 **%s**，请检查是否卡住", detail.Title, duration))
				if _, err := ReplyToMessage(record.FeishuMessageID, timeoutCard); err == nil {
					// 标记已发送超时提醒
					_, _ = DB.NewUpdate().Model(&record).
						Set("timeout_notified = ?", true).
						WherePK().Exec(ctx)
					slog.Info("Workflow timeout notification sent", "github_id", githubID, "duration", duration)
				}
				// 继续更新原消息状态
			}

			// 正常更新原消息
			buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
			var card *Card
			if isFirstWorkflowEvent {
				// 如果是合并到 push 消息，我们需要保持 push 的内容，但更新标题
				var pushDetail EventDetail
				_ = json.Unmarshal([]byte(record.Content), &pushDetail)
				// 替换标题为 Workflow 状态 + 原标题
				pushDetail.Title = detail.Title
				card = BuildCard(buildCtx, repo, repoUrl, sender, senderUrl, avatarUrl, pushDetail)
			} else {
				card = BuildCard(buildCtx, repo, repoUrl, sender, senderUrl, avatarUrl, detail)
			}
			buildCancel()

			if err := UpdateMessage(record.FeishuMessageID, card); err == nil {
				// 更新数据库中的内容，防止后续操作（如刷新图片）使用旧数据
				detailJson, _ := json.Marshal(detail)
				if isFirstWorkflowEvent {
					// 如果是第一次合并，我们需要为这个 workflow_run 创建一个新记录，指向同一个消息 ID
					// 这样后续的 in_progress/completed 就能直接找到这条记录进行更新
					_, _ = DB.NewInsert().Model(&MessageRecord{
						GithubID:          githubID,
						FeishuMessageID:   record.FeishuMessageID,
						ChatID:            record.ChatID,
						RepoName:          repo,
						Ref:               ref,
						EventType:         event.EventType,
						Content:           string(detailJson),
						CardString:        card.String(),
						EventID:           event.ID,
						WorkflowStartedAt: time.Now(),
					}).Exec(ctx)
				} else {
					_, _ = DB.NewUpdate().Model(&record).
						Set("content = ?", string(detailJson)).
						Set("card_string = ?", card.String()).
						Set("updated_at = ?", time.Now()).
						WherePK().Exec(ctx)
				}

				slog.Info("Workflow card updated", "github_id", githubID, "event_type", event.EventType)
				return nil
			} else {
				// 如果更新失败，记录错误并返回，不要继续往下走去发新消息
				return fmt.Errorf("failed to update message: %w", err)
			}
		}
	}

	// 4.2 Release 事件：更新同一个 release 的消息（编辑、正式发布等）
	if event.EventType == "release" && githubID != "" {
		var record MessageRecord
		if err := DB.NewSelect().Model(&record).
			Where("github_id = ? AND event_type = 'release'", githubID).
			Order("id DESC").
			Limit(1).Scan(ctx); err == nil {

			buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
			card := BuildCard(buildCtx, repo, repoUrl, sender, senderUrl, avatarUrl, detail)
			buildCancel()
			if err := UpdateMessage(record.FeishuMessageID, card); err == nil {
				detailJson, _ := json.Marshal(detail)
				record.Content = string(detailJson)
				record.CardString = card.String()
				_, _ = DB.NewUpdate().Model(&record).
					Column("content", "card_string").
					WherePK().Exec(ctx)
				slog.Info("Release card asynchronously updated", "github_id", githubID)
				return nil
			}
		}
	}

	// 只合并分支推送（非 tag 推送），且需要有实际内容
	if event.EventType == "push" && githubID != "" && !detail.IsTag && detail.Text != "" {
		var record MessageRecord
		if err := DB.NewSelect().Model(&record).
			Where("github_id = ? AND updated_at > ?", githubID, time.Now().Add(-5*time.Minute)).
			Scan(ctx); err == nil {

			var prevDetail EventDetail
			_ = json.Unmarshal([]byte(record.Content), &prevDetail)
			detail.Text = prevDetail.Text + "\n" + detail.Text
			detail.Title = "🍏 Branch Push (Merged)"

			buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
			card := BuildCard(buildCtx, repo, repoUrl, sender, senderUrl, avatarUrl, detail)
			buildCancel()
			if err := UpdateMessage(record.FeishuMessageID, card); err == nil {
				detailJson, _ := json.Marshal(detail)
				record.Content = string(detailJson)
				_, _ = DB.NewUpdate().Model(&record).Column("content").WherePK().Exec(ctx)
				slog.Info("Push merged asynchronously", "github_id", githubID)
				return nil
			}
		}
	}

	// 5. 查找父级 ID (回复逻辑)
	var parentID string
	// 改为：只要是 Issue/PR/Workflow 相关的非“创建”事件，都尝试寻找父消息进行话题回复
	isInteraction := event.EventType == "issue_comment" ||
		event.EventType == "pull_request_review_comment" ||
		event.EventType == "pull_request_review" ||
		event.EventType == "pull_request" ||
		event.EventType == "issues" ||
		event.EventType == "workflow_run" ||
		event.EventType == "workflow_job"

	action := ext(m, "action")
	if isInteraction && action != "opened" {
		commitId := sha // 使用已经提取好的 sha
		if commitId != "" {
			var record MessageRecord
			// 始终按 ID 升序取第一条 (Root message)
			if err := DB.NewSelect().Model(&record).Where("github_id LIKE ?", "%"+commitId+"%").Order("id ASC").Limit(1).Scan(ctx); err == nil {
				parentID = record.FeishuMessageID
			}
		}
		if parentID == "" {
			issueNum := ext(m, "issue", "number")
			if issueNum == "" {
				issueNum = ext(m, "pull_request", "number")
			}
			if issueNum != "" {
				var record MessageRecord
				searchID := fmt.Sprintf("%%:%s", issueNum)
				if strings.Contains(githubID, "pr:") || strings.Contains(githubID, "issue:") {
					// 如果我们已经有了带 repo 的 ID 前缀，直接搜索完整匹配或相似匹配
					searchID = fmt.Sprintf("%%:%s:%s", repo, issueNum)
				}

				if err := DB.NewSelect().Model(&record).
					Where("github_id = ? OR github_id LIKE ?", fmt.Sprintf("pr:%s:%s", repo, issueNum), searchID).
					WhereOr("github_id = ?", fmt.Sprintf("issue:%s:%s", repo, issueNum)).
					Order("id ASC").Limit(1).Scan(ctx); err == nil {
					parentID = record.FeishuMessageID
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
	card := BuildCard(ctx, repo, repoUrl, sender, senderUrl, avatarUrl, detail)

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

		// Workflow 事件：记录开始时间
		workflowStartedAt := time.Time{}
		if event.EventType == "workflow_run" || event.EventType == "workflow_job" {
			status := ""
			conclusion := ""
			if event.EventType == "workflow_run" {
				status = ext(m, "workflow_run", "status")
				conclusion = ext(m, "workflow_run", "conclusion")
			} else {
				status = ext(m, "workflow_job", "status")
				conclusion = ext(m, "workflow_job", "conclusion")
			}
			// 只有在运行中且无结论时才记录开始时间
			if status == "in_progress" && conclusion == "" {
				workflowStartedAt = time.Now()
			}
		}

		_, _ = DB.NewInsert().Model(&MessageRecord{
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
		}).Exec(ctx)
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

	// 4. 重建卡片，此时所有头像的 img_key 均已缓存（哪怕是刚才刚同步成功的）
	buildCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	newCard := BuildCard(buildCtx, record.RepoName, repoUrl, sender, senderUrl, record.AvatarURL, detail)

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
