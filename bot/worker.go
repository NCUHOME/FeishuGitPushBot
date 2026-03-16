package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/go-github/v84/github"
	"strings"
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

	// 3. 构建追踪 ID
	var githubID string
	switch event.EventType {
	case "workflow_run":
		githubID = ext(m, "workflow_run", "id")
	case "workflow_job":
		githubID = ext(m, "workflow_job", "run_id")
	case "push":
		githubID = fmt.Sprintf("push:%s:%s", repo, ref)
	case "pull_request":
		githubID = fmt.Sprintf("pr:%s:%s", repo, ext(m, "pull_request", "number"))
	case "issues":
		githubID = fmt.Sprintf("issue:%s:%s", repo, ext(m, "issue", "number"))
	default:
		githubID = ext(m, "head_commit", "id")
		if githubID == "" {
			githubID = ext(m, "pull_request", "head", "sha")
		}
		if githubID == "" {
			issueNum := ext(m, "issue", "number")
			if issueNum != "" {
				githubID = fmt.Sprintf("issue:%s:%s", repo, issueNum)
			}
		}
	}

	// 4. 合并与更新逻辑
	if (event.EventType == "workflow_run" || event.EventType == "workflow_job") && githubID != "" {
		var record MessageRecord
		if err := DB.NewSelect().Model(&record).
			Where("github_id = ? AND (event_type = 'workflow_run' OR event_type = 'workflow_job')", githubID).
			Order("id DESC").
			Limit(1).Scan(ctx); err == nil {
			
			buildCtx, buildCancel := context.WithTimeout(ctx, 5*time.Second)
			card := BuildCard(buildCtx, repo, repoUrl, sender, senderUrl, avatarUrl, detail)
			buildCancel()
			if err := UpdateMessage(record.FeishuMessageID, card); err == nil {
				slog.Info("Workflow card asynchronously updated", "github_id", githubID)
				return nil
			}
		}
	}

	if event.EventType == "push" && githubID != "" {
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
	// 改为：只要是 Issue/PR 相关的非“创建”事件，都尝试寻找父消息进行话题回复
	isIssueOrPR := event.EventType == "issue_comment" || 
		event.EventType == "pull_request_review_comment" || 
		event.EventType == "pull_request_review" || 
		event.EventType == "pull_request" || 
		event.EventType == "issues"

	action := ext(m, "action")
	if isIssueOrPR && action != "opened" {
		commitId := ext(m, "comment", "commit_id")
		if commitId == "" {
			commitId = ext(m, "pull_request", "head", "sha")
		}
		if commitId == "" {
			commitId = ext(m, "review", "commit_id")
		}
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

	// 6. 发送新消息
	// 获取头像缓存状态，决定是否需要后续异步刷新
	imageStatus := "done"
	if avatarUrl != "" {
		var cache ImageCache
		if err := DB.NewSelect().Model(&cache).Where("url = ?", avatarUrl).Scan(ctx); err == nil {
			// 如果缓存超过 24 小时，标记为待刷新，但在发送时先沿用旧缓存
			if time.Since(cache.UpdatedAt) > 24*time.Hour {
				imageStatus = "pending"
			}
		} else {
			// 完全没缓存，标记为待拉取
			imageStatus = "pending"
		}
	}

	// 此时 BuildCard 调用 GetImageKey 是纯内存/DB查询，不会阻塞
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

	// 7. 保存记录
	if githubID != "" && msgID != "" {
		detailJson, _ := json.Marshal(detail)
		_, _ = DB.NewInsert().Model(&MessageRecord{
			GithubID:        githubID,
			FeishuMessageID: msgID,
			ChatID:          C.Feishu.ChatID,
			RepoName:        repo,
			Ref:             ref,
			EventType:       event.EventType,
			Content:         string(detailJson),
			ImageStatus:     imageStatus,
			AvatarURL:       avatarUrl,
			EventID:         event.ID,
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
			Limit(10).
			Scan(context.Background())

		if err != nil || len(records) == 0 {
			time.Sleep(15 * time.Second)
			continue
		}

		for _, record := range records {
			refreshOneImage(record)
			time.Sleep(2 * time.Second)
		}
	}
}

func refreshOneImage(record MessageRecord) {
	if record.AvatarURL == "" {
		_, _ = DB.NewUpdate().Model(&record).Set("image_status = ?", "done").WherePK().Exec(context.Background())
		return
	}

	// 1. 同步尝试上传图片。syncUploadImage 内部会自动写入 ImageCache
	imgKey := syncUploadImage(context.Background(), record.AvatarURL)
	if imgKey == "" {
		// slog.Debug("图片刷新：上传仍未成功", "url", record.AvatarURL)
		return
	}

	// 2. 获取原始 Webhook 事件，用于重建卡片所需的元数据 (repoUrl, sender 等)
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

	// 3. 解析保存的卡片详情
	var detail EventDetail
	_ = json.Unmarshal([]byte(record.Content), &detail)

	// 4. 重建卡片。此时 BuildCard 内部的 GetImageKey 会击中刚刚生成的缓存
	// 设定一个 5s 超时以保证稳健
	buildCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	card := BuildCard(buildCtx, record.RepoName, repoUrl, sender, senderUrl, record.AvatarURL, detail)

	// 5. 调用飞书 API 更新原有消息卡片，带上头像
	err = UpdateMessage(record.FeishuMessageID, card)
	if err != nil {
		slog.Error("Image refresh: failed to update message card", "message_id", record.FeishuMessageID, "error", err)
		return
	}

	// 6. 成功，更新记录状态
	_, _ = DB.NewUpdate().Model(&record).Set("image_status = ?", "done").WherePK().Exec(context.Background())
	slog.Info("Image refresh successful, message card updated", "message_id", record.FeishuMessageID)
}
