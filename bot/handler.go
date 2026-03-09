package bot

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/go-github/v84/github"
)

// GithubHandler 处理 GitHub Webhook 请求
func GithubHandler(c *gin.Context) {
	// 验证签名
	secret := strings.TrimSpace(C.Github.Key)
	payload, err := github.ValidatePayload(c.Request, []byte(secret))
	if err != nil {
		slog.Error("签名验证失败", "error", err)
		c.AbortWithStatusJSON(400, gin.H{"code": 1, "msg": "签名验证失败"})
		return
	}

	eventType := github.WebHookType(c.Request)
	event, err := github.ParseWebHook(eventType, payload)
	if err != nil {
		slog.Error("解析 Webhook 失败", "error", err)
		c.AbortWithStatusJSON(400, gin.H{"code": 2, "msg": err.Error()})
		return
	}

	detail := ParseEvent(event, eventType)
	if detail.Skip {
		c.JSON(200, gin.H{"code": 0, "msg": "ignored"})
		return
	}

	// 获取基本信息
	var m map[string]any
	_ = json.Unmarshal(payload, &m)
	repo := ext(m, "repository", "full_name")
	repoUrl := ext(m, "repository", "html_url")
	sender := ext(m, "sender", "login")
	senderUrl := ext(m, "sender", "html_url")
	avatarUrl := ext(m, "sender", "avatar_url")

	// 状态更新逻辑 (Workflows)
	if eventType == "workflow_run" || eventType == "workflow_job" {
		id := ext(m, "workflow_run", "id") // Run ID
		if id == "" {
			id = ext(m, "workflow_job", "run_id") // Link Job back to Run card
		}

		if DB != nil && id != "" {
			var record MessageRecord
			if err := DB.NewSelect().Model(&record).
				Where("github_id = ? AND (event_type = 'workflow_run' OR event_type = 'workflow_job')", id).
				Order("id DESC").
				Limit(1).
				Scan(c.Request.Context()); err == nil {
				// 更新原有卡片
				card := BuildCard(c.Request.Context(), repo, repoUrl, sender, senderUrl, avatarUrl, detail)
				if err := UpdateMessage(record.FeishuMessageID, card); err != nil {
					slog.Error("更新卡片失败", "error", err, "message_id", record.FeishuMessageID)
				} else {
					slog.Info("自动更新卡片成功", "github_id", id, "event", eventType)
				}
				c.JSON(200, gin.H{"code": 0, "msg": "updated"})
				return
			}
		}
	}

	// 合并推送逻辑
	if eventType == "push" && DB != nil {
		ref := ext(m, "ref")
		key := fmt.Sprintf("push:%s:%s", repo, ref)
		var record MessageRecord
		if err := DB.NewSelect().Model(&record).Where("github_id = ? AND updated_at > ?", key, time.Now().Add(-5*time.Minute)).Scan(c.Request.Context()); err == nil {
			// 合并到原有卡片
			var prevDetail EventDetail
			_ = json.Unmarshal([]byte(record.Content), &prevDetail)

			// 追加新提交
			detail.Text = prevDetail.Text + "\n" + detail.Text
			detail.Title = "🍏 分支推送 (合并已更新)"

			card := BuildCard(c.Request.Context(), repo, repoUrl, sender, senderUrl, avatarUrl, detail)
			if err := UpdateMessage(record.FeishuMessageID, card); err == nil {
				detailJson, _ := json.Marshal(detail)
				record.Content = string(detailJson)
				_, _ = DB.NewUpdate().Model(&record).Column("content").WherePK().Exec(c.Request.Context())
				c.JSON(200, gin.H{"code": 0, "msg": "merged"})
				return
			}
		}
	}

	// 构建飞书卡片
	card := BuildCard(c.Request.Context(), repo, repoUrl, sender, senderUrl, avatarUrl, detail)

	// 回复逻辑 (如评论回复到原推送)
	var parentID string
	if DB != nil && (eventType == "issue_comment" || eventType == "pull_request_review_comment" || eventType == "pull_request_review") {
		commitId := ext(m, "comment", "commit_id")
		if commitId == "" {
			commitId = ext(m, "pull_request", "head", "sha")
		}
		if commitId == "" {
			commitId = ext(m, "review", "commit_id")
		}
		if commitId != "" {
			var record MessageRecord
			if err := DB.NewSelect().Model(&record).Where("github_id LIKE ?", "%"+commitId+"%").Limit(1).Scan(c.Request.Context()); err == nil {
				parentID = record.FeishuMessageID
				var parentDetail EventDetail
				if err := json.Unmarshal([]byte(record.Content), &parentDetail); err == nil {
					detail.ReplyToTitle = parentDetail.Title
				}
			}
		}

		// 如果没有找到 commit，尝试通过 Issue/PR 编号查找
		if parentID == "" {
			issueNum := ext(m, "issue", "number")
			if issueNum == "" {
				issueNum = ext(m, "pull_request", "number")
			}
			if issueNum != "" {
				var record MessageRecord
				if err := DB.NewSelect().Model(&record).Where("github_id = ? OR github_id LIKE ?", issueNum, "%:"+issueNum).Limit(1).Scan(c.Request.Context()); err == nil {
					parentID = record.FeishuMessageID
					var parentDetail EventDetail
					if err := json.Unmarshal([]byte(record.Content), &parentDetail); err == nil {
						detail.ReplyToTitle = parentDetail.Title
					}
				}
			}
		}
	}

	var msgID string
	var sendErr error
	if parentID != "" {
		// 话题模式回复
		msgID, sendErr = ReplyToMessage(parentID, card)
	} else {
		msgID, sendErr = SendToChat("", card)
	}

	if sendErr != nil {
		slog.Error("发送飞书消息失败", "error", sendErr)
	} else if DB != nil && msgID != "" {
		// 保存记录
		var githubID string
		switch eventType {
		case "workflow_run":
			githubID = ext(m, "workflow_run", "id")
		case "workflow_job":
			githubID = ext(m, "workflow_job", "run_id") // Use Run ID as tracking ID
		case "push":
			githubID = fmt.Sprintf("push:%s:%s", repo, ext(m, "ref"))
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

		if githubID != "" {
			detailJson, _ := json.Marshal(detail)
			_, _ = DB.NewInsert().Model(&MessageRecord{
				GithubID:        githubID,
				FeishuMessageID: msgID,
				ChatID:          C.Feishu.ChatID,
				RepoName:        repo,
				Ref:             ext(m, "ref"),
				EventType:       eventType,
				Content:         string(detailJson),
				RawPayload:      string(payload),
			}).Exec(c.Request.Context())
		}
	}

	c.JSON(200, gin.H{"code": 0, "msg": "ok"})
}

func ext(m map[string]any, keys ...string) string {
	var cur any = m
	for _, k := range keys {
		cm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = cm[k]
	}
	switch v := cur.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%.0f", v)
	case int:
		return fmt.Sprintf("%d", v)
	}
	return ""
}
