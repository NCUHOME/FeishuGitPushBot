package bot

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-github/v84/github"
)

func TestSendAllMessages(t *testing.T) {
	// 尝试加载配置
	LoadConfig()
	InitDB()

	if C.Feishu.Webhook == "" && C.Feishu.AppID == "" {
		t.Skip("跳过发送测试：未配置 FEISHU_WEBHOOK 或 FEISHU_APP_ID")
	}

	repo := "NCUHOME/FeishuGitPushBot"
	repoUrl := "https://github.com/NCUHOME/FeishuGitPushBot"
	sender := "Antigravity-AI"
	senderUrl := "https://github.com/Antigravity"
	avatarUrl := "https://avatars.githubusercontent.com/u/1?v=4" // GitHub 默认头像之一

	tests := []struct {
		name      string
		eventType string
		event     any
	}{
		{
			name:      "推送事件 (Multiple Authors)",
			eventType: "push",
			event: &github.PushEvent{
				Ref: github.Ptr("refs/heads/main"),
				Repo: &github.PushEventRepository{
					HTMLURL:  github.Ptr(repoUrl),
					FullName: github.Ptr(repo),
				},
				Commits: []*github.HeadCommit{
					{
						ID:      github.Ptr("1234567890abcdef"),
						Message: github.Ptr("feat: add multicore support :rocket:"),
						URL:     github.Ptr(repoUrl + "/commit/123456"),
						Author: &github.CommitAuthor{
							Name:  github.Ptr("Alice"),
							Login: github.Ptr("alice_dev"),
						},
					},
					{
						ID:      github.Ptr("abcdef1234567890"),
						Message: github.Ptr("fix: solve memory leak :bug:"),
						URL:     github.Ptr(repoUrl + "/commit/abcdef"),
						Author: &github.CommitAuthor{
							Name:  github.Ptr("Bob"),
							Login: github.Ptr("bob_fixer"),
						},
					},
				},
			},
		},
		{
			name:      "推送事件 (Push - New Commits)",
			eventType: "push",
			event: &github.PushEvent{
				Ref: github.Ptr("refs/heads/main"),
				Repo: &github.PushEventRepository{
					HTMLURL:  github.Ptr(repoUrl),
					FullName: github.Ptr(repo),
				},
				Commits: []*github.HeadCommit{
					{
						ID:      github.Ptr("1234567890abcdef"),
						Message: github.Ptr("feat: 增加更详细的测试用例"),
						URL:     github.Ptr(repoUrl + "/commit/123456"),
						Author: &github.CommitAuthor{
							Name:  github.Ptr("Andigravity-AI"),
							Login: github.Ptr("Antigravity"),
						},
					},
					{
						ID:      github.Ptr("abcdef1234567890"),
						Message: github.Ptr("fix: 优化卡片头像显示大小"),
						URL:     github.Ptr(repoUrl + "/commit/abcdef"),
						Author: &github.CommitAuthor{
							Name:  github.Ptr("Andigravity-AI"),
							Login: github.Ptr("Antigravity"),
						},
					},
				},
			},
		},
		{
			name:      "合并请求 (Pull Request - Opened)",
			eventType: "pull_request",
			event: &github.PullRequestEvent{
				Action: github.Ptr("opened"),
				PullRequest: &github.PullRequest{
					Title:   github.Ptr("🚀 升级飞书 SDK 版本"),
					Number:  github.Ptr(42),
					HTMLURL: github.Ptr(repoUrl + "/pull/42"),
					Body:    github.Ptr("本次 PR 将 resty 升级到 v3，并优化了重试逻辑。"),
					Head:    &github.PullRequestBranch{Ref: github.Ptr("feature/upgrade-sdk")},
					Base:    &github.PullRequestBranch{Ref: github.Ptr("main")},
				},
			},
		},
		{
			name:      "合并请求 (Pull Request - Merged)",
			eventType: "pull_request",
			event: &github.PullRequestEvent{
				Action: github.Ptr("closed"),
				PullRequest: &github.PullRequest{
					Title:   github.Ptr("🚀 升级飞书 SDK 版本"),
					Number:  github.Ptr(42),
					HTMLURL: github.Ptr(repoUrl + "/pull/42"),
					Merged:  github.Ptr(true),
					Head:    &github.PullRequestBranch{Ref: github.Ptr("feature/upgrade-sdk")},
					Base:    &github.PullRequestBranch{Ref: github.Ptr("main")},
				},
			},
		},
		{
			name:      "工作流 (Workflow Run - Success)",
			eventType: "workflow_run",
			event: &github.WorkflowRunEvent{
				Action: github.Ptr("completed"),
				WorkflowRun: &github.WorkflowRun{
					Name:       github.Ptr("Build and Test"),
					HTMLURL:    github.Ptr(repoUrl + "/actions/runs/1001"),
					Status:     github.Ptr("completed"),
					Conclusion: github.Ptr("success"),
					HeadBranch: github.Ptr("main"),
				},
			},
		},
		{
			name:      "工作流 (Workflow Run - Failed)",
			eventType: "workflow_run",
			event: &github.WorkflowRunEvent{
				Action: github.Ptr("completed"),
				WorkflowRun: &github.WorkflowRun{
					Name:       github.Ptr("Deploy to Production"),
					HTMLURL:    github.Ptr(repoUrl + "/actions/runs/1002"),
					Status:     github.Ptr("completed"),
					Conclusion: github.Ptr("failure"),
					HeadBranch: github.Ptr("release/v1"),
				},
			},
		},
		{
			name:      "工作流 (Workflow Run - In Progress)",
			eventType: "workflow_run",
			event: &github.WorkflowRunEvent{
				Action: github.Ptr("in_progress"),
				WorkflowRun: &github.WorkflowRun{
					Name:       github.Ptr("CI Checks"),
					HTMLURL:    github.Ptr(repoUrl + "/actions/runs/1003"),
					Status:     github.Ptr("in_progress"),
					HeadBranch: github.Ptr("feature/ui"),
				},
			},
		},
		{
			name:      "问题评论 (Issue Comment)",
			eventType: "issue_comment",
			event: &github.IssueCommentEvent{
				Action: github.Ptr("created"),
				Issue: &github.Issue{
					Title:   github.Ptr("建议增加暗黑模式支持"),
					Number:  github.Ptr(105),
					HTMLURL: github.Ptr(repoUrl + "/issues/105"),
				},
				Comment: &github.IssueComment{
					Body:    github.Ptr("这确实是个好主意，我们可以考虑使用 CSS 变量来实现。"),
					HTMLURL: github.Ptr(repoUrl + "/issues/105#issuecomment-999"),
				},
			},
		},
		{
			name:      "复刻事件 (Fork)",
			eventType: "fork",
			event: &github.ForkEvent{
				Forkee: &github.Repository{
					FullName: github.Ptr("other-user/FeishuGitPushBot"),
					HTMLURL:  github.Ptr("https://github.com/other-user/FeishuGitPushBot"),
				},
			},
		},
		{
			name:      "星标事件 (Star)",
			eventType: "star",
			event: &github.StarEvent{
				Action: github.Ptr("created"),
			},
		},
	}

	var firstMsgID string

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail := ParseEvent(tt.event, tt.eventType)
			card := BuildCard(context.Background(), repo, repoUrl, sender, senderUrl, avatarUrl, detail)

			msgID, err := SendToChat("", card)
			if err != nil {
				t.Errorf("发送 %s 失败: %v", tt.name, err)
			} else {
				fmt.Printf("✅ %s 发送成功, MessageID: %s\n", tt.name, msgID)
				if i == 0 {
					firstMsgID = msgID
				}
			}
		})
	}

	// 测试回复功能
	if firstMsgID != "" {
		t.Run("测试回复消息", func(t *testing.T) {
			replyDetail := EventDetail{
				Title: "💬 回复测试",
				Text:  "这也是一条自动测试生成的回复消息，用于验证主题模式（Topic Mode）。",
				URL:   repoUrl,
			}
			card := BuildCard(context.Background(), repo, repoUrl, sender, senderUrl, avatarUrl, replyDetail)
			replyID, err := ReplyToMessage(firstMsgID, card)
			if err != nil {
				t.Errorf("发送回复消息失败: %v", err)
			} else {
				fmt.Printf("✅ 回复消息发送成功, ReplyID: %s\n", replyID)
			}
		})
	}
}


