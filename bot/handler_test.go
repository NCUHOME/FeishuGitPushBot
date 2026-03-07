package bot

import (
	"testing"

	"github.com/google/go-github/v84/github"
)

func TestParseEvent(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		payload   []byte
		wantTitle string
	}{
		{
			name:      "推送事件 - 分支",
			eventType: "push",
			payload: []byte(`{
				"ref": "refs/heads/main",
				"repository": {"html_url": "https://github.com/test/repo"},
				"commits": [
					{"id": "1234567890", "message": "Initial commit", "url": "http://commit", "author": {"name": "Alice"}}
				]
			}`),
			wantTitle: "🌿 分支推送",
		},
		{
			name:      "合并请求事件 - 已开启",
			eventType: "pull_request",
			payload: []byte(`{
				"action": "opened",
				"pull_request": {
					"title": "Fix bug",
					"state": "open",
					"html_url": "http://pr"
				}
			}`),
			wantTitle: "🔄 合并请求 已开启",
		},
		{
			name:      "问题评论",
			eventType: "issue_comment",
			payload: []byte(`{
				"action": "created",
				"issue": {"title": "Bug", "html_url": "http://issue"},
				"comment": {"body": "Me too", "html_url": "http://comment"}
			}`),
			wantTitle: "💬 问题 已评论",
		},
		{
			name:      "成员添加",
			eventType: "member",
			payload: []byte(`{
				"action": "added",
				"member": {"login": "Alice", "html_url": "http://alice"}
			}`),
			wantTitle: "👥 成员更新 已添加",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, _ := github.ParseWebHook(tt.eventType, tt.payload)
			got := parseEvent(event, tt.eventType, tt.payload)
			if got.Title != tt.wantTitle {
				t.Errorf("got %v, want %v", got.Title, tt.wantTitle)
			}
		})
	}
}

func TestGetTemplate(t *testing.T) {
	if getTemplate("💥 Workflow failed") != "red" {
		t.Error("failed template should be red")
	}
	if getTemplate("✅ Success") != "green" {
		t.Error("success template should be green")
	}
}
