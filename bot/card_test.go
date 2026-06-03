package bot

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSendDeleteCard(t *testing.T) {
	LoadConfig()
	InitDB()

	detail := EventDetail{
		Title:     "🗑️ Tag Deleted: v1.2",
		RefName:   "v1.2",
		RefURL:    "https://github.com/NCUHOME/K8sSetImageAction/tags",
		IsTag:     true,
		IsDeleted: true,
		EventTime: time.Now().Format(time.RFC3339),
		RepoName:  "NCUHOME/K8sSetImageAction",
		RepoURL:   "https://github.com/NCUHOME/K8sSetImageAction",
		URL:       "https://github.com/NCUHOME/K8sSetImageAction/tags",
	}

	ctx := context.Background()
	card := BuildCard(ctx, "NCUHOME/K8sSetImageAction", "HakimYu", "https://github.com/HakimYu", "", detail)

	msgID, err := SendToChat("", card)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	fmt.Println("sent message_id:", msgID)
}

func TestSendMergedDeleteCard(t *testing.T) {
	LoadConfig()
	InitDB()

	// 模拟合并后的分支删除：多个分支名在 Text 中
	detail := EventDetail{
		Title:         "🗑️ Branch Deleted: FeishuGitPushBot",
		IsDeleted:     true,
		Text:          "Plot\nfeature-abc\nfix-xyz",
		EventTime:     time.Now().Add(-2 * time.Minute).Format(time.RFC3339),
		EventTimeEnd:  time.Now().Format(time.RFC3339),
		RepoName:      "NCUHOME/FeishuGitPushBot",
		RepoURL:       "https://github.com/NCUHOME/FeishuGitPushBot",
		AuthorLogins:  []string{"hangone"},
	}

	ctx := context.Background()
	card := BuildCard(ctx, "NCUHOME/FeishuGitPushBot", "hangone", "https://github.com/hangone", "", detail)

	msgID, err := SendToChat("", card)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	fmt.Println("sent message_id:", msgID)
}

// TestSendPushCard 测试 push 事件卡片：多条 commit、换行、conventional commit 加粗
func TestSendPushCard(t *testing.T) {
	LoadConfig()
	InitDB()

	detail := EventDetail{
		Title:    "🍏 New commits",
		RefName:  "feat/ts-idiomatic",
		RefURL:   "https://github.com/NCUHOME/payfission/tree/feat/ts-idiomatic",
		SHA:      "0f6fbb7",
		FullSHA:  "0f6fbb7abc1234567890abcdef1234567890abc",
		RepoName: "NCUHOME/payfission",
		RepoURL:  "https://github.com/NCUHOME/payfission",
		URL:      "https://github.com/NCUHOME/payfission/commit/0f6fbb7abc1234567890abcdef1234567890abc",
		Text:     "🔸 **docs(domain):** 为值对象添加开发者须知注释 ([ae49a3a](https://github.com/NCUHOME/payfission/commit/ae49a3a123))<br>🔹 **docs:** 补充 TypeScript 惯用风格说明和开发者须知 ([70fd8f6](https://github.com/NCUHOME/payfission/commit/70fd8f6abc))<br>🔸 **docs:** 添加 TypeScript 惯用风格 Skill ([0f6fbb7](https://github.com/NCUHOME/payfission/commit/0f6fbb7abc))",
		AuthorLogins:  []string{"hesitling"},
		AuthorAvatars: []string{"https://avatars.githubusercontent.com/hesitling"},
		CommitCount:   3,
		Action:        "push",
		EventTime:     time.Now().Format(time.RFC3339),
	}

	ctx := context.Background()
	card := BuildCard(ctx, "NCUHOME/payfission", "hesitling", "https://github.com/hesitling", "", detail)

	msgID, err := SendToChat("", card)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	fmt.Println("sent message_id:", msgID)
}

// TestSendPushCardManyCommits 测试超过 3 条 commit 时的折叠逻辑
func TestSendPushCardManyCommits(t *testing.T) {
	LoadConfig()
	InitDB()

	commits := []string{
		"🔸 **feat:** 添加用户认证模块 ([abc1234](https://github.com/NCUHOME/test/commit/abc1234))",
		"🔹 **fix:** 修复登录页面样式问题 ([def5678](https://github.com/NCUHOME/test/commit/def5678))",
		"🔸 **docs:** 更新 README 文档 ([ghi9012](https://github.com/NCUHOME/test/commit/ghi9012))",
		"🔹 **refactor:** 重构数据库连接池 ([jkl3456](https://github.com/NCUHOME/test/commit/jkl3456))",
		"🔸 **test:** 添加单元测试 ([mno7890](https://github.com/NCUHOME/test/commit/mno7890))",
	}

	detail := EventDetail{
		Title:    "🍏 New commits",
		RefName:  "main",
		RefURL:   "https://github.com/NCUHOME/test/tree/main",
		SHA:      "mno7890",
		FullSHA:  "mno7890abcdef1234567890abcdef1234567890ab",
		RepoName: "NCUHOME/test",
		RepoURL:  "https://github.com/NCUHOME/test",
		URL:      "https://github.com/NCUHOME/test/commit/mno7890abcdef1234567890abcdef1234567890ab",
		Text:     joinCommits(commits),
		AuthorLogins:  []string{"testuser"},
		AuthorAvatars: []string{"https://avatars.githubusercontent.com/testuser"},
		CommitCount:   5,
		Action:        "push",
		EventTime:     time.Now().Format(time.RFC3339),
	}

	ctx := context.Background()
	card := BuildCard(ctx, "NCUHOME/test", "testuser", "https://github.com/testuser", "", detail)

	msgID, err := SendToChat("", card)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	fmt.Println("sent message_id:", msgID)
}

// TestSendPRCard 测试 Pull Request 卡片
func TestSendPRCard(t *testing.T) {
	LoadConfig()
	InitDB()

	detail := EventDetail{
		Title:         "🥕 New PullRequest",
		RefName:       "feat/new-feature ➔ main",
		RefURL:        "https://github.com/NCUHOME/test/tree/feat/new-feature",
		RepoName:      "NCUHOME/test",
		RepoURL:       "https://github.com/NCUHOME/test",
		URL:           "https://github.com/NCUHOME/test/pull/42",
		Text:          "**feat: 添加新功能**\n\n这是一个新功能的 PR 描述。\n\n## 改动内容\n\n- 添加了 xxx\n- 修改了 yyy",
		AuthorLogins:  []string{"testuser"},
		AuthorAvatars: []string{"https://avatars.githubusercontent.com/testuser"},
		Action:        "opened",
		EventTime:     time.Now().Format(time.RFC3339),
	}

	ctx := context.Background()
	card := BuildCard(ctx, "NCUHOME/test", "testuser", "https://github.com/testuser", "", detail)

	msgID, err := SendToChat("", card)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	fmt.Println("sent message_id:", msgID)
}

// TestSendWorkflowCard 测试 Workflow 状态卡片
func TestSendWorkflowCard(t *testing.T) {
	LoadConfig()
	InitDB()

	detail := EventDetail{
		Title:    "✅ Workflow succeeded: CI",
		RefName:  "main",
		RefURL:   "https://github.com/NCUHOME/test/tree/main",
		SHA:      "abc1234",
		RepoName: "NCUHOME/test",
		RepoURL:  "https://github.com/NCUHOME/test",
		URL:      "https://github.com/NCUHOME/test/actions/runs/12345",
		Text:     "✅ **CI** workflow run succeeded in 2 minutes 30 seconds",
		Action:   "workflow_run",
		EventTime: time.Now().Format(time.RFC3339),
	}

	ctx := context.Background()
	card := BuildCard(ctx, "NCUHOME/test", "github-actions[bot]", "https://github.com/github-actions[bot]", "", detail)

	msgID, err := SendToChat("", card)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	fmt.Println("sent message_id:", msgID)
}

// TestSendWorkflowFailedCard 测试 CI 失败卡片（带 job 状态和操作按钮）
func TestSendWorkflowFailedCard(t *testing.T) {
	LoadConfig()
	InitDB()

	detail := EventDetail{
		Title:    "❌ Workflow failed: CI",
		RefName:  "main",
		RefURL:   "https://github.com/NCUHOME/test/tree/main",
		SHA:      "abc1234",
		RepoName: "NCUHOME/test",
		RepoURL:  "https://github.com/NCUHOME/test",
		URL:      "https://github.com/NCUHOME/test/actions/runs/12345",
		Text:     "❌ **CI** workflow run failed in 1 minute 15 seconds",
		CIStatuses: []CIStatus{
			// workflow 级别
			{WorkflowName: "CI", Status: "completed", Conclusion: "failure", RunID: 12345, Duration: "1m 15s"},
			// job 级别（通过 ParentRunID 关联到 workflow）
			{WorkflowName: "job:build", JobName: "Build", Status: "completed", Conclusion: "success", RunID: 0, ParentRunID: 12345, Duration: "30s"},
			{WorkflowName: "job:test", JobName: "Test", Status: "completed", Conclusion: "failure", RunID: 0, ParentRunID: 12345, Duration: "45s"},
			{WorkflowName: "job:lint", JobName: "Lint", Status: "completed", Conclusion: "success", RunID: 0, ParentRunID: 12345, Duration: "10s"},
		},
		Action:    "workflow_run",
		EventTime: time.Now().Format(time.RFC3339),
	}

	ctx := context.Background()
	card := BuildCard(ctx, "NCUHOME/test", "github-actions[bot]", "https://github.com/github-actions[bot]", "", detail)

	msgID, err := SendToChat("", card)
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	fmt.Println("sent message_id:", msgID)
}

// joinCommits 用 <br> 连接 commit 条目（与 ParseEvent 中的逻辑一致）
func joinCommits(commits []string) string {
	result := ""
	for i, c := range commits {
		if i > 0 {
			result += "<br>"
		}
		result += c
	}
	return result
}

func TestOnConflictInsert(t *testing.T) {
	LoadConfig()
	InitDB()
	if DB == nil {
		t.Skip("DB not initialized")
	}

	ctx := context.Background()
	githubID := "test:conflict:" + fmt.Sprintf("%d", time.Now().UnixNano())

	// First insert
	_, err := DB.NewInsert().Model(&MessageRecord{
		GithubID:      githubID,
		FeishuMessageID: "msg_001",
		ChatID:        "test_chat",
		RepoName:      "test/repo",
		EventType:     "push",
		Content:       "{}",
		EventID:       99999,
		HeadSHA:       "sha_original",
	}).On("CONFLICT (github_id) DO UPDATE").
		Set("feishu_message_id = EXCLUDED.feishu_message_id").
		Set("head_sha = EXCLUDED.head_sha").
		Exec(ctx)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}
	fmt.Println("first insert OK")

	// Second insert (upsert)
	_, err = DB.NewInsert().Model(&MessageRecord{
		GithubID:      githubID,
		FeishuMessageID: "msg_002",
		ChatID:        "test_chat",
		RepoName:      "test/repo",
		EventType:     "push",
		Content:       `{"updated":true}`,
		EventID:       99998,
		HeadSHA:       "sha_updated",
	}).On("CONFLICT (github_id) DO UPDATE").
		Set("feishu_message_id = EXCLUDED.feishu_message_id").
		Set("head_sha = EXCLUDED.head_sha").
		Exec(ctx)
	if err != nil {
		t.Fatalf("upsert failed: %v", err)
	}
	fmt.Println("upsert OK")

	// Verify
	var rec MessageRecord
	err = DB.NewSelect().Model(&rec).Where("github_id = ?", githubID).Scan(ctx)
	if err != nil {
		t.Fatalf("select failed: %v", err)
	}
	fmt.Printf("feishu_message_id=%s, head_sha=%s\n", rec.FeishuMessageID, rec.HeadSHA)
	if rec.FeishuMessageID != "msg_002" {
		t.Errorf("expected msg_002, got %s", rec.FeishuMessageID)
	}
	if rec.HeadSHA != "sha_updated" {
		t.Errorf("expected sha_updated, got %s", rec.HeadSHA)
	}

	// Cleanup
	_, _ = DB.NewDelete().Model(&rec).Where("github_id = ?", githubID).Exec(ctx)
}

func TestExtractRefName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bare name", "feat/foo", "feat/foo"},
		{"bare name with spaces", "  feat/foo  ", "feat/foo"},
		{"markdown link", "🌿 [feat/foo](https://github.com/x/y/tree/feat/foo)", "feat/foo"},
		{"tag link", "🏷️ [v1.0](https://github.com/x/y/releases/tag/v1.0)", "v1.0"},
		{"emoji prefix only", "🌿 feat/bar", "feat/bar"},
		{"empty", "", ""},
		{"blank line", "   ", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRefName(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractRefs(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"bare names", "feat/a\nfeat/b\nfeat/a", []string{"feat/a", "feat/b"}},
		{"mixed format", "feat/a\n🌿 [feat/b](url)\nfeat/a", []string{"feat/a", "feat/b"}},
		{"markdown links", "🌿 [feat/a](url1)\n🌿 [feat/b](url2)", []string{"feat/a", "feat/b"}},
		{"empty", "", nil},
		{"blank lines", "\n\n\n", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRefs(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMergeRefs(t *testing.T) {
	tests := []struct {
		name   string
		old    string
		new    string
		expect string
	}{
		{"both bare", "feat/a", "feat/b", "feat/a\nfeat/b"},
		{"dedup bare", "feat/a\nfeat/b", "feat/b", "feat/a\nfeat/b"},
		{"mixed format", "feat/a", "🌿 [feat/a](url)", "feat/a"},
		{"old empty", "", "feat/a", "feat/a"},
		{"new empty", "feat/a", "", "feat/a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mergeRefs(tt.old, tt.new)
			assert.Equal(t, tt.expect, got)
		})
	}
}

func TestEscapeCodeHTML(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "plain text unchanged",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "br outside code preserved",
			input: "line1<br>line2",
			want:  "line1<br>line2",
		},
		{
			name:  "br inside inline code escaped",
			input: "use `<br>` for breaks",
			want:  "use `＜br＞` for breaks",
		},
		{
			name:  "br inside fenced code block escaped",
			input: "```\nline1\n<br>\nline3\n```",
			want:  "```\nline1\n＜br＞\nline3\n```",
		},
		{
			name:  "mixed: br outside and inside code",
			input: "line1<br>and `use <br>` here",
			want:  "line1<br>and `use ＜br＞` here",
		},
		{
			name:  "angle brackets inside code escaped",
			input: "`<div>` and `<script>`",
			want:  "`＜div＞` and `＜script＞`",
		},
		{
			name:  "multiple inline code spans",
			input: "`<a>` normal `<b>`",
			want:  "`＜a＞` normal `＜b＞`",
		},
		{
			name:  "unclosed backtick unchanged",
			input: "use `<br> here",
			want:  "use `<br> here",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeCodeHTML(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
