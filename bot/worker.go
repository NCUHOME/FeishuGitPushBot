package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/go-github/v84/github"
)

// StartWorker 启动消息队列处理工作者和图片刷新任务
func StartWorker() {
	if DB == nil {
		slog.Warn("数据库未初始化，消息队列工作者将不启动")
		return
	}

	// 1. 消息推送工作者
	go messageWorker()

	// 2. 图片异步刷新任务
	go imageRefreshWorker()
}

func messageWorker() {
	slog.Info("消息队列工作者已启动")
	for {
		// 每次取一条待处理的消息
		var event WebhookEvent
		err := DB.NewSelect().Model(&event).
			Where("status = ?", "pending").
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
			slog.Error("处理 Webhook 事件失败", "id", event.ID, "error", err)
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
		return fmt.Errorf("解析 Webhook 失败: %w", err)
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
	default:
		githubID = ext(m, "head_commit", "id")
		if githubID == "" {
			githubID = ext(m, "pull_request", "head", "sha")
		}
		if githubID == "" {
			githubID = ext(m, "issue", "number")
		}
	}

	// 4. 合并与更新逻辑
	if (event.EventType == "workflow_run" || event.EventType == "workflow_job") && githubID != "" {
		var record MessageRecord
		if err := DB.NewSelect().Model(&record).
			Where("github_id = ? AND (event_type = 'workflow_run' OR event_type = 'workflow_job')", githubID).
			Order("id DESC").
			Limit(1).Scan(ctx); err == nil {
			
			card := BuildCard(ctx, repo, repoUrl, sender, senderUrl, avatarUrl, detail)
			if err := UpdateMessage(record.FeishuMessageID, card); err == nil {
				slog.Info("异步更新 Workflow 卡片成功", "github_id", githubID)
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
			detail.Title = "🍏 分支推送 (合并已更新)"

			card := BuildCard(ctx, repo, repoUrl, sender, senderUrl, avatarUrl, detail)
			if err := UpdateMessage(record.FeishuMessageID, card); err == nil {
				detailJson, _ := json.Marshal(detail)
				record.Content = string(detailJson)
				_, _ = DB.NewUpdate().Model(&record).Column("content").WherePK().Exec(ctx)
				slog.Info("异步合并 Push 成功", "github_id", githubID)
				return nil
			}
		}
	}

	// 5. 查找父级 ID (回复逻辑)
	var parentID string
	if event.EventType == "issue_comment" || event.EventType == "pull_request_review_comment" || event.EventType == "pull_request_review" {
		commitId := ext(m, "comment", "commit_id")
		if commitId == "" {
			commitId = ext(m, "pull_request", "head", "sha")
		}
		if commitId == "" {
			commitId = ext(m, "review", "commit_id")
		}
		if commitId != "" {
			var record MessageRecord
			if err := DB.NewSelect().Model(&record).Where("github_id LIKE ?", "%"+commitId+"%").Limit(1).Scan(ctx); err == nil {
				parentID = record.FeishuMessageID
				var parentDetail EventDetail
				if err := json.Unmarshal([]byte(record.Content), &parentDetail); err == nil {
					detail.ReplyToTitle = parentDetail.Title
				}
			}
		}
		if parentID == "" {
			issueNum := ext(m, "issue", "number")
			if issueNum == "" {
				issueNum = ext(m, "pull_request", "number")
			}
			if issueNum != "" {
				var record MessageRecord
				if err := DB.NewSelect().Model(&record).Where("github_id = ? OR github_id LIKE ?", issueNum, "%:"+issueNum).Limit(1).Scan(ctx); err == nil {
					parentID = record.FeishuMessageID
					var parentDetail EventDetail
					if err := json.Unmarshal([]byte(record.Content), &parentDetail); err == nil {
						detail.ReplyToTitle = parentDetail.Title
					}
				}
			}
		}
	}

	// 6. 发送新消息
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

	// 7. 保存记录
	if githubID != "" && msgID != "" {
		imageStatus := "done"
		if avatarUrl != "" && !strings.Contains(card.String(), "img_key") {
			imageStatus = "pending"
		}

		detailJson, _ := json.Marshal(detail)
		_, _ = DB.NewInsert().Model(&MessageRecord{
			GithubID:        githubID,
			FeishuMessageID: msgID,
			ChatID:          C.Feishu.ChatID,
			RepoName:        repo,
			Ref:             ref,
			EventType:       event.EventType,
			Content:         string(detailJson),
			RawPayload:      string(payload),
			ImageStatus:     imageStatus,
			AvatarURL:       avatarUrl,
			EventID:         event.ID,
		}).Exec(ctx)
	}

	return nil
}

func imageRefreshWorker() {
	slog.Info("图片刷新工作者已启动")
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

	imgKey := syncUploadImage(context.Background(), record.AvatarURL)
	if imgKey == "" {
		return
	}

	var detail EventDetail
	_ = json.Unmarshal([]byte(record.Content), &detail)

	card := BuildCard(context.Background(), record.RepoName, "", "", "", record.AvatarURL, detail)
	
	err := UpdateMessage(record.FeishuMessageID, card)
	if err != nil {
		slog.Error("图片刷新后更新消息失败", "message_id", record.FeishuMessageID, "error", err)
		return
	}

	_, _ = DB.NewUpdate().Model(&record).Set("image_status = ?", "done").WherePK().Exec(context.Background())
	slog.Info("图片异步刷新成功", "message_id", record.FeishuMessageID)
}
