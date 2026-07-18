package bot

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v84/github"
)

// helper functions to create pointers
func strPtr(s string) *string { return &s }
func int64Ptr(i int64) *int64 { return &i }
func boolPtr(b bool) *bool    { return &b }
func tsPtr(t time.Time) *github.Timestamp {
	ts := github.Timestamp{Time: t}
	return &ts
}

func TestParseEventNewTypes(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	tests := []struct {
		name      string
		event     interface{}
		eventType string
		wantTitle string
		wantSkip  bool
	}{
		// CommitCommentEvent
		{
			name: "commit_comment created",
			event: &github.CommitCommentEvent{
				Action: strPtr("created"),
				Comment: &github.RepositoryComment{
					ID:        int64Ptr(12345),
					Body:      strPtr("This is a commit comment"),
					HTMLURL:   strPtr("https://github.com/test/repo/commit/abc123#commitcomment-12345"),
					CreatedAt: &github.Timestamp{Time: now},
				},
				Repo: &github.Repository{
					FullName: strPtr("test/repo"),
					HTMLURL:  strPtr("https://github.com/test/repo"),
				},
				Sender: &github.User{
					Login:     strPtr("testuser"),
					HTMLURL:   strPtr("https://github.com/testuser"),
					AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
				},
			},
			eventType: "commit_comment",
			wantTitle: "💬 Commit Comment created",
		},
		// DeploymentEvent
		{
			name: "deployment created",
			event: &github.DeploymentEvent{
				Deployment: &github.Deployment{
					ID:          int64Ptr(100),
					Ref:         strPtr("main"),
					Environment: strPtr("production"),
					Description: strPtr("Deploy to production"),
					Task:        strPtr("deploy"),
					URL:         strPtr("https://api.github.com/repos/test/repo/deployments/100"),
					CreatedAt:   &github.Timestamp{Time: now},
				},
				Repo: &github.Repository{
					FullName: strPtr("test/repo"),
					HTMLURL:  strPtr("https://github.com/test/repo"),
				},
				Sender: &github.User{
					Login:     strPtr("testuser"),
					HTMLURL:   strPtr("https://github.com/testuser"),
					AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
				},
			},
			eventType: "deployment",
			wantTitle: "🚀 Deployment Created",
		},
		// DeploymentStatusEvent
		{
			name: "deployment_status success",
			event: &github.DeploymentStatusEvent{
				Action: strPtr("created"),
				Deployment: &github.Deployment{
					ID:          int64Ptr(100),
					Environment: strPtr("production"),
				},
				DeploymentStatus: &github.DeploymentStatus{
					ID:          int64Ptr(200),
					State:       strPtr("success"),
					Description: strPtr("Deployment succeeded"),
					TargetURL:   strPtr("https://example.com"),
					CreatedAt:   &github.Timestamp{Time: now},
				},
				Repo: &github.Repository{
					FullName: strPtr("test/repo"),
					HTMLURL:  strPtr("https://github.com/test/repo"),
				},
				Sender: &github.User{
					Login:     strPtr("testuser"),
					HTMLURL:   strPtr("https://github.com/testuser"),
					AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
				},
			},
			eventType: "deployment_status",
			wantTitle: "✅ Deployment Success",
		},
		// DiscussionEvent
		{
			name: "discussion created",
			event: &github.DiscussionEvent{
				Action: strPtr("created"),
				Discussion: &github.Discussion{
					ID:        int64Ptr(500),
					Number:    intPtr(42),
					Title:     strPtr("How to deploy?"),
					Body:      strPtr("I need help with deployment"),
					HTMLURL:   strPtr("https://github.com/test/repo/discussions/42"),
					CreatedAt: &github.Timestamp{Time: now},
				},
				Repo: &github.Repository{
					FullName: strPtr("test/repo"),
					HTMLURL:  strPtr("https://github.com/test/repo"),
				},
				Sender: &github.User{
					Login:     strPtr("testuser"),
					HTMLURL:   strPtr("https://github.com/testuser"),
					AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
				},
			},
			eventType: "discussion",
			wantTitle: "💬 New Discussion",
		},
		// DiscussionCommentEvent
		{
			name: "discussion_comment created",
			event: &github.DiscussionCommentEvent{
				Action: strPtr("created"),
				Discussion: &github.Discussion{
					Number: intPtr(42),
					Title:  strPtr("How to deploy?"),
				},
				Comment: &github.CommentDiscussion{
					ID:        int64Ptr(600),
					Body:      strPtr("Try using Docker"),
					HTMLURL:   strPtr("https://github.com/test/repo/discussions/42#discussioncomment-600"),
					CreatedAt: &github.Timestamp{Time: now},
				},
				Repo: &github.Repository{
					FullName: strPtr("test/repo"),
					HTMLURL:  strPtr("https://github.com/test/repo"),
				},
				Sender: &github.User{
					Login:     strPtr("testuser"),
					HTMLURL:   strPtr("https://github.com/testuser"),
					AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
				},
			},
			eventType: "discussion_comment",
			wantTitle: "💬 Discussion Comment created",
		},
		// LabelEvent
		{
			name: "label created",
			event: &github.LabelEvent{
				Action: strPtr("created"),
				Label: &github.Label{
					ID:          int64Ptr(700),
					Name:        strPtr("bug"),
					Description: strPtr("Something isn't working"),
					Color:       strPtr("d73a4a"),
				},
				Repo: &github.Repository{
					FullName: strPtr("test/repo"),
					HTMLURL:  strPtr("https://github.com/test/repo"),
				},
				Sender: &github.User{
					Login:     strPtr("testuser"),
					HTMLURL:   strPtr("https://github.com/testuser"),
					AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
				},
			},
			eventType: "label",
			wantTitle: "🏷️ Label created: bug",
		},
		// MilestoneEvent
		{
			name: "milestone created",
			event: &github.MilestoneEvent{
				Action: strPtr("created"),
				Milestone: &github.Milestone{
					ID:           int64Ptr(800),
					Number:       intPtr(1),
					Title:        strPtr("v1.0"),
					Description:  strPtr("First release"),
					State:        strPtr("open"),
					OpenIssues:   intPtr(5),
					ClosedIssues: intPtr(2),
					HTMLURL:      strPtr("https://github.com/test/repo/milestone/1"),
					CreatedAt:    &github.Timestamp{Time: now},
				},
				Repo: &github.Repository{
					FullName: strPtr("test/repo"),
					HTMLURL:  strPtr("https://github.com/test/repo"),
				},
				Sender: &github.User{
					Login:     strPtr("testuser"),
					HTMLURL:   strPtr("https://github.com/testuser"),
					AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
				},
			},
			eventType: "milestone",
			wantTitle: "🎯 Milestone created: v1.0",
		},
		// PullRequestReviewThreadEvent
		{
			name: "pull_request_review_thread resolved",
			event: &github.PullRequestReviewThreadEvent{
				Action: strPtr("resolved"),
				Thread: &github.PullRequestThread{
					ID: int64Ptr(900),
				},
				PullRequest: &github.PullRequest{
					Number: intPtr(10),
					Title:  strPtr("Fix bug"),
				},
				Repo: &github.Repository{
					FullName: strPtr("test/repo"),
					HTMLURL:  strPtr("https://github.com/test/repo"),
				},
				Sender: &github.User{
					Login:     strPtr("testuser"),
					HTMLURL:   strPtr("https://github.com/testuser"),
					AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
				},
			},
			eventType: "pull_request_review_thread",
			wantTitle: "✅ PR Review Thread Resolved",
		},
		// StatusEvent
		{
			name: "status success",
			event: &github.StatusEvent{
				SHA:         strPtr("abc1234567890"),
				State:       strPtr("success"),
				Context:     strPtr("ci/build"),
				Description: strPtr("Build succeeded"),
				TargetURL:   strPtr("https://ci.example.com/build/123"),
				CreatedAt:   &github.Timestamp{Time: now},
				Repo: &github.Repository{
					FullName: strPtr("test/repo"),
					HTMLURL:  strPtr("https://github.com/test/repo"),
				},
				Sender: &github.User{
					Login:     strPtr("testuser"),
					HTMLURL:   strPtr("https://github.com/testuser"),
					AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
				},
			},
			eventType: "status",
			wantTitle: "✅ Status: ci/build",
		},
		// BranchProtectionRuleEvent
		{
			name: "branch_protection_rule created",
			event: &github.BranchProtectionRuleEvent{
				Action: strPtr("created"),
				Rule: &github.BranchProtectionRule{
					ID:        int64Ptr(1000),
					Name:      strPtr("main"),
					CreatedAt: &github.Timestamp{Time: now},
				},
				Repo: &github.Repository{
					FullName: strPtr("test/repo"),
					HTMLURL:  strPtr("https://github.com/test/repo"),
				},
				Sender: &github.User{
					Login:     strPtr("testuser"),
					HTMLURL:   strPtr("https://github.com/testuser"),
					AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
				},
			},
			eventType: "branch_protection_rule",
			wantTitle: "🛡️ Branch Protection Rule created",
		},
		// CodeScanningAlertEvent
		{
			name: "code_scanning_alert created",
			event: &github.CodeScanningAlertEvent{
				Action: strPtr("created"),
				Alert: &github.Alert{
					Number:          intPtr(42),
					RuleID:          strPtr("js/unused-variable"),
					RuleDescription: strPtr("Unused variable"),
					RuleSeverity:    strPtr("warning"),
					HTMLURL:         strPtr("https://github.com/test/repo/security/code-scanning/42"),
					CreatedAt:       &github.Timestamp{Time: now},
				},
				Ref: strPtr("refs/heads/main"),
				Repo: &github.Repository{
					FullName: strPtr("test/repo"),
					HTMLURL:  strPtr("https://github.com/test/repo"),
				},
				Sender: &github.User{
					Login:     strPtr("testuser"),
					HTMLURL:   strPtr("https://github.com/testuser"),
					AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
				},
			},
			eventType: "code_scanning_alert",
			wantTitle: "🟡 Code Scanning Alert #42 created",
		},
		// DependabotAlertEvent
		{
			name: "dependabot_alert created",
			event: &github.DependabotAlertEvent{
				Action: strPtr("created"),
				Alert: &github.DependabotAlert{
					Number: intPtr(15),
					Dependency: &github.Dependency{
						Package: &github.VulnerabilityPackage{
							Name: strPtr("lodash"),
						},
					},
					SecurityVulnerability: &github.AdvisoryVulnerability{
						Severity: strPtr("high"),
					},
					HTMLURL:   strPtr("https://github.com/test/repo/security/dependabot/15"),
					CreatedAt: &github.Timestamp{Time: now},
				},
				Repo: &github.Repository{
					FullName: strPtr("test/repo"),
					HTMLURL:  strPtr("https://github.com/test/repo"),
				},
				Sender: &github.User{
					Login:     strPtr("testuser"),
					HTMLURL:   strPtr("https://github.com/testuser"),
					AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
				},
			},
			eventType: "dependabot_alert",
			wantTitle: "🟠 Dependabot Alert #15 created",
		},
		// SecretScanningAlertEvent
		{
			name: "secret_scanning_alert created",
			event: &github.SecretScanningAlertEvent{
				Action: strPtr("created"),
				Alert: &github.SecretScanningAlert{
					Number:                intPtr(8),
					SecretType:            strPtr("github_pat"),
					SecretTypeDisplayName: strPtr("GitHub Personal Access Token"),
					HTMLURL:               strPtr("https://github.com/test/repo/security/secret-scanning/8"),
					CreatedAt:             &github.Timestamp{Time: now},
				},
				Repo: &github.Repository{
					FullName: strPtr("test/repo"),
					HTMLURL:  strPtr("https://github.com/test/repo"),
				},
				Sender: &github.User{
					Login:     strPtr("testuser"),
					HTMLURL:   strPtr("https://github.com/testuser"),
					AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
				},
			},
			eventType: "secret_scanning_alert",
			wantTitle: "🔐 Secret Scanning Alert #8 created",
		},
		// TeamAddEvent
		{
			name: "team_add",
			event: &github.TeamAddEvent{
				Team: &github.Team{
					ID:         int64Ptr(200),
					Name:       strPtr("backend-team"),
					HTMLURL:    strPtr("https://github.com/orgs/test/teams/backend-team"),
					Permission: strPtr("write"),
				},
				Repo: &github.Repository{
					FullName: strPtr("test/repo"),
					HTMLURL:  strPtr("https://github.com/test/repo"),
				},
				Sender: &github.User{
					Login:     strPtr("testuser"),
					HTMLURL:   strPtr("https://github.com/testuser"),
					AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
				},
			},
			eventType: "team_add",
			wantTitle: "👥 Team access added",
		},
		// PingEvent (ParseEvent doesn't skip, but worker layer will skip)
		{
			name: "ping",
			event: &github.PingEvent{
				Zen:    strPtr("Keep it logically awesome."),
				HookID: int64Ptr(123456),
			},
			eventType: "ping",
			wantTitle: "🔔 GitHub Event: ping",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail := ParseEvent(tt.event, tt.eventType)

			if tt.wantSkip {
				if !detail.Skip {
					t.Errorf("expected Skip=true, got false")
				}
				return
			}

			if detail.Skip {
				t.Errorf("expected Skip=false, got true")
			}

			if detail.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", detail.Title, tt.wantTitle)
			}

			// Build card to verify it doesn't panic
			card := BuildCard(ctx, "test/repo", "testuser", "https://github.com/testuser", "", detail)
			if card == nil {
				t.Error("BuildCard returned nil")
			}

			// Verify card has content
			cardJSON := card.String()
			if len(cardJSON) < 10 {
				t.Errorf("Card JSON too short: %s", cardJSON)
			}

			fmt.Printf("✓ %s: Title=%q, Text=%q\n", tt.name, detail.Title, detail.Text[:min(50, len(detail.Text))])
		})
	}
}

func TestParseRefLifecycleUsesPushEvents(t *testing.T) {
	repo := &github.PushEventRepository{
		Name:    strPtr("repo"),
		HTMLURL: strPtr("https://github.com/test/repo"),
	}

	tests := []struct {
		name        string
		event       any
		eventType   string
		wantTitle   string
		wantText    string
		wantTag     bool
		wantDeleted bool
		wantSkip    bool
	}{
		{
			name: "tag created from push",
			event: &github.PushEvent{
				Ref:     strPtr("refs/tags/v1.2.3"),
				Created: boolPtr(true),
				Repo:    repo,
			},
			eventType: "push",
			wantTitle: "🏷️ New Tag: v1.2.3",
			wantText:  "🏷️ v1.2.3",
			wantTag:   true,
		},
		{
			name: "tag deleted from push",
			event: &github.PushEvent{
				Ref:     strPtr("refs/tags/v1.2.3"),
				Deleted: boolPtr(true),
				Repo:    repo,
			},
			eventType:   "push",
			wantTitle:   "🗑️ Tag Deleted: repo",
			wantText:    "v1.2.3",
			wantTag:     true,
			wantDeleted: true,
		},
		{
			name: "create webhook skipped",
			event: &github.CreateEvent{
				Ref:     strPtr("v1.2.3"),
				RefType: strPtr("tag"),
			},
			eventType: "create",
			wantSkip:  true,
		},
		{
			name: "delete webhook skipped",
			event: &github.DeleteEvent{
				Ref:     strPtr("feat/old"),
				RefType: strPtr("branch"),
			},
			eventType: "delete",
			wantSkip:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail := ParseEvent(tt.event, tt.eventType)
			if detail.Skip != tt.wantSkip {
				t.Fatalf("Skip = %v, want %v", detail.Skip, tt.wantSkip)
			}
			if tt.wantSkip {
				return
			}
			if detail.Title != tt.wantTitle {
				t.Fatalf("Title = %q, want %q", detail.Title, tt.wantTitle)
			}
			if detail.Text != tt.wantText {
				t.Fatalf("Text = %q, want %q", detail.Text, tt.wantText)
			}
			if detail.IsTag != tt.wantTag {
				t.Fatalf("IsTag = %v, want %v", detail.IsTag, tt.wantTag)
			}
			if detail.IsDeleted != tt.wantDeleted {
				t.Fatalf("IsDeleted = %v, want %v", detail.IsDeleted, tt.wantDeleted)
			}
		})
	}
}

func TestParseTeamAccessEvents(t *testing.T) {
	t.Run("team access added", func(t *testing.T) {
		detail := ParseEvent(&github.TeamAddEvent{
			Team: &github.Team{
				ID:         int64Ptr(200),
				Name:       strPtr("backend-team"),
				HTMLURL:    strPtr("https://github.com/orgs/NCUHOME/teams/backend-team"),
				Permission: strPtr("write"),
			},
			Repo: &github.Repository{
				FullName: strPtr("NCUHOME/FeishuGitPushBot"),
				HTMLURL:  strPtr("https://github.com/NCUHOME/FeishuGitPushBot"),
			},
			Sender: &github.User{
				Login:     strPtr("hangone"),
				HTMLURL:   strPtr("https://github.com/hangone"),
				AvatarURL: strPtr("https://avatars.githubusercontent.com/u/56105779?v=4"),
			},
		}, "team_add")

		if detail.Title != "👥 Team access added" {
			t.Fatalf("Title = %q", detail.Title)
		}
		for _, want := range []string{
			"- **added** team **[backend-team](https://github.com/orgs/NCUHOME/teams/backend-team)** by **[hangone](https://github.com/hangone)**",
			"permission to `write`",
		} {
			if !strings.Contains(detail.Text, want) {
				t.Fatalf("Text missing %q: %s", want, detail.Text)
			}
		}
		if detail.URL != "https://github.com/NCUHOME/FeishuGitPushBot/settings/access" {
			t.Fatalf("URL = %q", detail.URL)
		}
		if got := strings.Join(detail.AuthorLogins, ","); got != "hangone" {
			t.Fatalf("AuthorLogins = %q", got)
		}
	})

	t.Run("team access edited", func(t *testing.T) {
		detail := ParseEvent(&github.TeamEvent{
			Action: strPtr("edited"),
			Team: &github.Team{
				ID:         int64Ptr(200),
				Name:       strPtr("backend-team"),
				HTMLURL:    strPtr("https://github.com/orgs/NCUHOME/teams/backend-team"),
				Permission: strPtr("admin"),
			},
			Changes: &github.TeamChange{
				Repository: &github.TeamRepository{
					Permissions: &github.TeamPermissions{
						From: &github.TeamPermissionsFrom{
							Pull: boolPtr(true),
							Push: boolPtr(true),
						},
					},
				},
			},
			Repo: &github.Repository{
				FullName: strPtr("NCUHOME/FeishuGitPushBot"),
				HTMLURL:  strPtr("https://github.com/NCUHOME/FeishuGitPushBot"),
			},
			Sender: &github.User{
				Login:     strPtr("hangone"),
				HTMLURL:   strPtr("https://github.com/hangone"),
				AvatarURL: strPtr("https://avatars.githubusercontent.com/u/56105779?v=4"),
			},
		}, "team")

		if detail.Title != "👥 Team access edited" {
			t.Fatalf("Title = %q", detail.Title)
		}
		for _, want := range []string{
			"- **edited** team **[backend-team](https://github.com/orgs/NCUHOME/teams/backend-team)** by **[hangone](https://github.com/hangone)**",
			"permission: `write` -> `admin`",
		} {
			if !strings.Contains(detail.Text, want) {
				t.Fatalf("Text missing %q: %s", want, detail.Text)
			}
		}
		if detail.URL != "https://github.com/NCUHOME/FeishuGitPushBot/settings/access" {
			t.Fatalf("URL = %q", detail.URL)
		}
		if got := strings.Join(detail.AuthorLogins, ","); got != "hangone" {
			t.Fatalf("AuthorLogins = %q", got)
		}
	})
}

func TestParseMemberEventIncludesChangeDetails(t *testing.T) {
	detail := ParseEvent(&github.MemberEvent{
		Action: strPtr("edited"),
		Member: &github.User{
			ID:        int64Ptr(36563672),
			Login:     strPtr("Mmx233"),
			HTMLURL:   strPtr("https://github.com/Mmx233"),
			AvatarURL: strPtr("https://avatars.githubusercontent.com/u/36563672?v=4"),
		},
		Changes: &github.MemberChanges{
			Permission: &github.MemberChangesPermission{
				From: strPtr("write"),
				To:   strPtr("admin"),
			},
			RoleName: &github.MemberChangesRoleName{
				From: strPtr("write"),
				To:   strPtr("admin"),
			},
		},
		Sender: &github.User{
			Login:     strPtr("hangone"),
			HTMLURL:   strPtr("https://github.com/hangone"),
			AvatarURL: strPtr("https://avatars.githubusercontent.com/u/56105779?v=4"),
		},
		Repo: &github.Repository{
			FullName: strPtr("NCUHOME/FeishuGitPushBot"),
			HTMLURL:  strPtr("https://github.com/NCUHOME/FeishuGitPushBot"),
		},
	}, "member")

	if detail.Title != "👤 Member access edited" {
		t.Fatalf("Title = %q", detail.Title)
	}
	for _, want := range []string{
		"- **edited** member **[Mmx233](https://github.com/Mmx233)** by **[hangone](https://github.com/hangone)**",
		"permission: `write` -> `admin`",
		"role_name: `write` -> `admin`",
	} {
		if !strings.Contains(detail.Text, want) {
			t.Fatalf("Text missing %q: %s", want, detail.Text)
		}
	}
	if detail.EventCount != 1 {
		t.Fatalf("EventCount = %d, want 1", detail.EventCount)
	}
	if detail.URL != "https://github.com/NCUHOME/FeishuGitPushBot/settings/access" {
		t.Fatalf("URL = %q", detail.URL)
	}
	if got := strings.Join(detail.AuthorLogins, ","); got != "hangone" {
		t.Fatalf("AuthorLogins = %q", got)
	}

	fallbackDetail := ParseEvent(&github.MemberEvent{
		Action: strPtr("edited"),
		Member: &github.User{
			Login:   strPtr("Mmx233"),
			HTMLURL: strPtr("https://github.com/Mmx233"),
		},
		Sender: &github.User{
			Login:   strPtr("hangone"),
			HTMLURL: strPtr("https://github.com/hangone"),
		},
	}, "member")
	if fallbackDetail.URL != "https://github.com/Mmx233" {
		t.Fatalf("fallback URL = %q", fallbackDetail.URL)
	}
}

func TestMergeMemberDetails(t *testing.T) {
	oldDetail := &EventDetail{
		Title:         "👤 Member added",
		Text:          "- **added** member **[Mmx233](https://github.com/Mmx233)** by **[hangone](https://github.com/hangone)**",
		EventTime:     "2026-07-09T01:00:00Z",
		EventCount:    1,
		AuthorLogins:  []string{"hangone"},
		AuthorAvatars: []string{"avatar-hangone"},
	}
	newDetail := &EventDetail{
		Title:         "👤 Member permission edited",
		Text:          "- **edited** member **[Mmx233](https://github.com/Mmx233)** by **[hangone](https://github.com/hangone)**; permission: `write` -> `admin`",
		EventTime:     "2026-07-09T01:05:00Z",
		EventCount:    1,
		AuthorLogins:  []string{"hangone"},
		AuthorAvatars: []string{"avatar-hangone"},
	}

	mergeMemberDetails(oldDetail, newDetail)

	wantText := "- **added** member **[Mmx233](https://github.com/Mmx233)** by **[hangone](https://github.com/hangone)**\n- **edited** member **[Mmx233](https://github.com/Mmx233)** by **[hangone](https://github.com/hangone)**; permission: `write` -> `admin`"
	if newDetail.Text != wantText {
		t.Fatalf("Text = %q, want %q", newDetail.Text, wantText)
	}
	if newDetail.Title != "👤 Member updates" {
		t.Fatalf("Title = %q", newDetail.Title)
	}
	if newDetail.Action != "updated" {
		t.Fatalf("Action = %q", newDetail.Action)
	}
	if newDetail.EventTime != "2026-07-09T01:00:00Z" || newDetail.EventTimeEnd != "2026-07-09T01:05:00Z" {
		t.Fatalf("Event time range = %q -> %q", newDetail.EventTime, newDetail.EventTimeEnd)
	}
	if newDetail.EventCount != 2 {
		t.Fatalf("EventCount = %d, want 2", newDetail.EventCount)
	}
	if got := strings.Join(newDetail.AuthorLogins, ","); got != "hangone" {
		t.Fatalf("AuthorLogins = %q", got)
	}
}

func TestMergeMemberDetailsNormalizesLegacyAndDeduplicates(t *testing.T) {
	t.Run("legacy old format counts as one operation", func(t *testing.T) {
		oldDetail := &EventDetail{
			Title:     "👤 Member Mmx233: edited",
			Text:      "Action: **edited**\nMember: **Mmx233**\nBy: **hangone**",
			EventTime: "2026-07-09T01:00:00Z",
		}
		newDetail := &EventDetail{
			Title:      "👤 Member permission edited",
			Text:       "- **edited** member **[Mmx233](https://github.com/Mmx233)** by **[hangone](https://github.com/hangone)**; permission: `write` -> `admin`",
			EventTime:  "2026-07-09T01:05:00Z",
			EventCount: 1,
		}

		mergeMemberDetails(oldDetail, newDetail)

		if newDetail.EventCount != 2 {
			t.Fatalf("EventCount = %d, want 2", newDetail.EventCount)
		}
		if !strings.Contains(newDetail.Text, "- **edited** member **Mmx233** by **hangone**") {
			t.Fatalf("legacy member line not normalized: %s", newDetail.Text)
		}
	})

	t.Run("same operation line is idempotent", func(t *testing.T) {
		line := "- **edited** member **[Mmx233](https://github.com/Mmx233)** by **[hangone](https://github.com/hangone)**; permission: `write` -> `admin`"
		oldDetail := &EventDetail{Text: line, EventCount: 1}
		newDetail := &EventDetail{Text: line, EventCount: 1}

		mergeMemberDetails(oldDetail, newDetail)

		if newDetail.Text != line {
			t.Fatalf("Text = %q, want deduplicated line", newDetail.Text)
		}
		if newDetail.EventCount != 1 {
			t.Fatalf("EventCount = %d, want 1", newDetail.EventCount)
		}
	})
}

func TestMemberTrackingIDFallsBackToLogin(t *testing.T) {
	numericPayload := map[string]any{
		"member": map[string]any{
			"id":    float64(36563672),
			"login": "Mmx233",
		},
	}
	if got, want := memberTrackingID(numericPayload, "NCUHOME/FeishuGitPushBot"), "member:NCUHOME/FeishuGitPushBot:36563672"; got != want {
		t.Fatalf("memberTrackingID() = %q, want %q", got, want)
	}

	payload := map[string]any{
		"member": map[string]any{
			"login": "Mmx233",
		},
	}
	if got, want := memberTrackingID(payload, "NCUHOME/FeishuGitPushBot"), "member:NCUHOME/FeishuGitPushBot:Mmx233"; got != want {
		t.Fatalf("memberTrackingID() = %q, want %q", got, want)
	}
}

func TestWebhookMergeLockKey(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		detail    EventDetail
		repo      string
		want      string
	}{
		{
			name:      "branch delete push",
			eventType: "push",
			detail:    EventDetail{IsDeleted: true},
			repo:      "test/repo",
			want:      "merge:branch-delete:test/repo",
		},
		{
			name:      "tag delete push",
			eventType: "push",
			detail:    EventDetail{IsDeleted: true, IsTag: true},
			repo:      "test/repo",
			want:      "merge:tag-delete:test/repo",
		},
		{
			name:      "ordinary push",
			eventType: "push",
			detail:    EventDetail{},
			repo:      "test/repo",
		},
		{
			name:      "delete webhook skipped before lock",
			eventType: "delete",
			detail:    EventDetail{IsDeleted: true},
			repo:      "test/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := webhookMergeLockKey(tt.eventType, tt.detail, tt.repo); got != tt.want {
				t.Fatalf("webhookMergeLockKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseSecurityAuditEvents(t *testing.T) {
	now := time.Now()

	t.Run("personal access token request", func(t *testing.T) {
		detail := ParseEvent(&github.PersonalAccessTokenRequestEvent{
			Action: strPtr("created"),
			PersonalAccessTokenRequest: &github.PersonalAccessTokenRequest{
				ID:                  int64Ptr(42),
				Owner:               &github.User{Login: strPtr("token-owner"), HTMLURL: strPtr("https://github.com/token-owner")},
				RepositorySelection: strPtr("subset"),
				RepositoryCount:     int64Ptr(2),
				CreatedAt:           &github.Timestamp{Time: now},
				PermissionsResult:   &github.PersonalAccessTokenPermissions{Repo: map[string]string{"contents": "read", "metadata": "read"}},
				PermissionsAdded:    &github.PersonalAccessTokenPermissions{Repo: map[string]string{"contents": "read"}},
				PermissionsUpgraded: &github.PersonalAccessTokenPermissions{},
				TokenExpiresAt:      &github.Timestamp{Time: now.Add(24 * time.Hour)},
				TokenLastUsedAt:     &github.Timestamp{},
				TokenExpired:        boolPtr(false),
			},
			Org:    &github.Organization{Login: strPtr("test-org"), HTMLURL: strPtr("https://github.com/test-org")},
			Sender: &github.User{Login: strPtr("security-admin"), AvatarURL: strPtr("https://avatars.githubusercontent.com/u/1")},
		}, "personal_access_token_request")

		if detail.Skip {
			t.Fatal("expected PAT request event to be rendered")
		}
		if detail.Title != "🔑 Personal Access Token Request Created" {
			t.Fatalf("Title = %q", detail.Title)
		}
		for _, want := range []string{"Request ID: `42`", "Owner: **token-owner**", "Repository selection: **subset**", "`repository.contents`: **read**"} {
			if !strings.Contains(detail.Text, want) {
				t.Fatalf("Text missing %q: %s", want, detail.Text)
			}
		}
	})

	t.Run("security and analysis", func(t *testing.T) {
		detail := ParseEvent(&github.SecurityAndAnalysisEvent{
			Repository: &github.Repository{
				FullName: strPtr("test/repo"),
				HTMLURL:  strPtr("https://github.com/test/repo"),
				SecurityAndAnalysis: &github.SecurityAndAnalysis{
					SecretScanning:               &github.SecretScanning{Status: strPtr("enabled")},
					SecretScanningPushProtection: &github.SecretScanningPushProtection{Status: strPtr("enabled")},
				},
			},
			Changes: &github.SecurityAndAnalysisChange{
				From: &github.SecurityAndAnalysisChangeFrom{
					SecurityAndAnalysis: &github.SecurityAndAnalysis{
						SecretScanning: &github.SecretScanning{Status: strPtr("disabled")},
					},
				},
			},
			Sender: &github.User{Login: strPtr("security-admin"), AvatarURL: strPtr("https://avatars.githubusercontent.com/u/1")},
		}, "security_and_analysis")

		if detail.Skip {
			t.Fatal("expected security_and_analysis event to be rendered")
		}
		if detail.Title != "🛡️ Security & Analysis Changed" {
			t.Fatalf("Title = %q", detail.Title)
		}
		for _, want := range []string{"Repository: **test/repo**", "Previous Secret scanning: **disabled**", "Current Secret scanning: **enabled**"} {
			if !strings.Contains(detail.Text, want) {
				t.Fatalf("Text missing %q: %s", want, detail.Text)
			}
		}
	})

	t.Run("branch protection configuration", func(t *testing.T) {
		detail := ParseEvent(&github.BranchProtectionConfigurationEvent{
			Action: strPtr("enabled"),
			Repo: &github.Repository{
				FullName: strPtr("test/repo"),
				HTMLURL:  strPtr("https://github.com/test/repo"),
			},
			Sender: &github.User{Login: strPtr("security-admin"), AvatarURL: strPtr("https://avatars.githubusercontent.com/u/1")},
		}, "branch_protection_configuration")

		if detail.Skip {
			t.Fatal("expected branch_protection_configuration event to be rendered")
		}
		if detail.Title != "🛡️ Branch Protection Configuration Enabled" {
			t.Fatalf("Title = %q", detail.Title)
		}
		if !strings.Contains(detail.Text, "Repository: **test/repo**") {
			t.Fatalf("Text missing repository: %s", detail.Text)
		}
	})

	t.Run("org block", func(t *testing.T) {
		detail := ParseEvent(&github.OrgBlockEvent{
			Action:       strPtr("blocked"),
			Organization: &github.Organization{Login: strPtr("test-org")},
			BlockedUser:  &github.User{Login: strPtr("blocked-user"), HTMLURL: strPtr("https://github.com/blocked-user")},
			Sender:       &github.User{Login: strPtr("security-admin"), AvatarURL: strPtr("https://avatars.githubusercontent.com/u/1")},
		}, "org_block")

		if detail.Skip {
			t.Fatal("expected org_block event to be rendered")
		}
		if detail.Title != "🚫 Org User Blocked: blocked-user" {
			t.Fatalf("Title = %q", detail.Title)
		}
		if !strings.Contains(detail.Text, "Organization: **test-org**") {
			t.Fatalf("Text missing organization: %s", detail.Text)
		}
	})

	t.Run("repository advisory raw fallback", func(t *testing.T) {
		payload := []byte(`{
			"action": "published",
			"repository_advisory": {
				"ghsa_id": "GHSA-abcd-1234-5678",
				"summary": "Test advisory",
				"severity": "high",
				"html_url": "https://github.com/test/repo/security/advisories/GHSA-abcd-1234-5678"
			},
			"repository": {
				"full_name": "test/repo",
				"html_url": "https://github.com/test/repo"
			},
			"sender": {
				"login": "security-admin",
				"avatar_url": "https://avatars.githubusercontent.com/u/1"
			}
		}`)

		event, err := parseWebhookPayload("repository_advisory", payload)
		if err != nil {
			t.Fatalf("parseWebhookPayload() error = %v", err)
		}

		detail := ParseEvent(event, "repository_advisory")
		if detail.Skip {
			t.Fatal("expected repository_advisory event to be rendered")
		}
		if detail.Title != "🛡️ Repository Advisory Published" {
			t.Fatalf("Title = %q", detail.Title)
		}
		for _, want := range []string{"Repository: **test/repo**", "GHSA: `GHSA-abcd-1234-5678`", "Severity: **high**", "Summary: **Test advisory**", "By: **security-admin**"} {
			if !strings.Contains(detail.Text, want) {
				t.Fatalf("Text missing %q: %s", want, detail.Text)
			}
		}
		if detail.URL != "https://github.com/test/repo/security/advisories/GHSA-abcd-1234-5678" {
			t.Fatalf("URL = %q", detail.URL)
		}
	})

	t.Run("deploy key", func(t *testing.T) {
		detail := ParseEvent(&github.DeployKeyEvent{
			Action: strPtr("created"),
			Key: &github.Key{
				ID:        int64Ptr(1001),
				Title:     strPtr("production deploy"),
				ReadOnly:  boolPtr(false),
				AddedBy:   strPtr("platform-admin"),
				CreatedAt: &github.Timestamp{Time: now},
			},
			Repo: &github.Repository{
				FullName: strPtr("test/repo"),
				HTMLURL:  strPtr("https://github.com/test/repo"),
			},
			Sender: &github.User{Login: strPtr("security-admin"), AvatarURL: strPtr("https://avatars.githubusercontent.com/u/1")},
		}, "deploy_key")

		if detail.Skip {
			t.Fatal("expected deploy_key event to be rendered")
		}
		if detail.Title != "🔐 Deploy Key Created: production deploy" {
			t.Fatalf("Title = %q", detail.Title)
		}
		for _, want := range []string{"Repository: **test/repo**", "Key: **production deploy**", "Key ID: `1001`", "Access: **read/write**", "Added by: **platform-admin**", "By: **security-admin**"} {
			if !strings.Contains(detail.Text, want) {
				t.Fatalf("Text missing %q: %s", want, detail.Text)
			}
		}
		if detail.URL != "https://github.com/test/repo" {
			t.Fatalf("URL = %q", detail.URL)
		}
	})
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func intPtr(i int) *int {
	return &i
}

func TestParseWorkflowRunUsesActionWhenStatusMissing(t *testing.T) {
	detail := ParseEvent(&github.WorkflowRunEvent{
		Action: strPtr("requested"),
		WorkflowRun: &github.WorkflowRun{
			ID:         int64Ptr(12345),
			Name:       strPtr("CI"),
			HeadBranch: strPtr("main"),
			HeadSHA:    strPtr("abcdef123456"),
			HTMLURL:    strPtr("https://github.com/test/repo/actions/runs/12345"),
			CreatedAt:  tsPtr(time.Date(2026, 6, 9, 7, 0, 0, 0, time.UTC)),
		},
		Repo: &github.Repository{
			FullName: strPtr("test/repo"),
			HTMLURL:  strPtr("https://github.com/test/repo"),
		},
	}, "workflow_run")

	if detail.Title != "⚙️ Workflow Requested: CI" {
		t.Fatalf("Title = %q", detail.Title)
	}
	if strings.Contains(detail.Title, "Started") {
		t.Fatalf("Title should not fall back to started when action is present: %q", detail.Title)
	}
	if !strings.Contains(detail.Text, "requested") {
		t.Fatalf("Text = %q, want requested state", detail.Text)
	}
}

func TestParsePullRequestNormalizesLiteralNewlinesAndPlainTitle(t *testing.T) {
	detail := ParseEvent(&github.PullRequestEvent{
		Action: strPtr("opened"),
		PullRequest: &github.PullRequest{
			Title:   strPtr("feat: 通知功能基础实现"),
			Body:    strPtr(`Summary\n- 为帖子点赞和评论创建通知\n- 首页增加未读通知入口\n\n## Verification\n- npm run typecheck\n- npm run lint\n- npm run build`),
			HTMLURL: strPtr("https://github.com/NCUHOME/youth-pen/pull/1"),
			Head: &github.PullRequestBranch{
				Ref: strPtr("feat/notifications"),
				Repo: &github.Repository{
					HTMLURL: strPtr("https://github.com/NCUHOME/youth-pen"),
				},
			},
			Base: &github.PullRequestBranch{Ref: strPtr("main")},
			User: &github.User{
				Login:     strPtr("J621111"),
				HTMLURL:   strPtr("https://github.com/J621111"),
				AvatarURL: strPtr("https://avatars.githubusercontent.com/u/1"),
			},
			CreatedAt: tsPtr(time.Date(2026, 6, 9, 11, 30, 15, 0, time.UTC)),
		},
		Repo: &github.Repository{
			FullName: strPtr("NCUHOME/youth-pen"),
			HTMLURL:  strPtr("https://github.com/NCUHOME/youth-pen"),
		},
	}, "pull_request")

	if detail.Text == "" {
		t.Fatal("Text is empty")
	}
	if strings.Contains(detail.Text, `\n`) {
		t.Fatalf("Text still contains literal newline escapes: %q", detail.Text)
	}
	if strings.Contains(detail.Text, "**feat: 通知功能基础实现**") {
		t.Fatalf("PR title should not be wrapped in bold markdown: %q", detail.Text)
	}
	if !strings.Contains(detail.Text, "Summary\n- 为帖子点赞和评论创建通知") {
		t.Fatalf("Text did not normalize summary list: %q", detail.Text)
	}
	if strings.Contains(detail.Text, "## Verification") {
		t.Fatalf("Markdown heading should be downgraded for Feishu card body: %q", detail.Text)
	}
	if !strings.Contains(detail.Text, "Verification\n- npm run typecheck") {
		t.Fatalf("Text did not preserve markdown section/list: %q", detail.Text)
	}

	card := BuildCard(context.Background(), "NCUHOME/youth-pen", "J621111", "https://github.com/J621111", "", detail)
	var bodyContent string
	for _, element := range card.Body.Elements {
		el, ok := element.(map[string]any)
		if !ok || el["tag"] != "markdown" {
			continue
		}
		content, _ := el["content"].(string)
		if strings.Contains(content, "feat: 通知功能基础实现") {
			bodyContent = content
			break
		}
	}
	if bodyContent == "" {
		t.Fatalf("PR body markdown block not found in card: %#v", card.Body.Elements)
	}
	if strings.Contains(bodyContent, `\n`) {
		t.Fatalf("Card body still contains literal newline escapes: %q", bodyContent)
	}
	if strings.Contains(bodyContent, "## Verification") || strings.Contains(bodyContent, "**feat: 通知功能基础实现**") {
		t.Fatalf("Card body contains oversized markdown markers: %q", bodyContent)
	}
	if !strings.Contains(bodyContent, "Summary\n- 为帖子点赞和评论创建通知") {
		t.Fatalf("Card body did not preserve normalized newlines: %q", bodyContent)
	}
}

func TestProcessGithubMarkdownDoesNotNormalizeLiteralNewlinesInsideCodeFence(t *testing.T) {
	input := "Before\\n```js\\nconst s = \"hello\\\\nworld\";\\n```\\nAfter"
	text, _ := ProcessGithubMarkdown(input)

	if !strings.Contains(text, "Before\n```js") {
		t.Fatalf("Text did not normalize literal newlines outside fence: %q", text)
	}
	if !strings.Contains(text, `hello\\nworld`) {
		t.Fatalf("Code fence literal newline escape was modified: %q", text)
	}
	if strings.Contains(text, "##") {
		t.Fatalf("Unexpected markdown heading marker in text: %q", text)
	}
}

func TestProcessGithubMarkdownDoesNotNormalizeEscapedBackslashNewline(t *testing.T) {
	text, _ := ProcessGithubMarkdown(`Path C:\\new-folder\nNext`)

	if strings.Contains(text, "C:\\\n") {
		t.Fatalf("Escaped backslash newline should not split path text: %q", text)
	}
	if !strings.Contains(text, `C:\\new-folder`) {
		t.Fatalf("Escaped backslash path was modified: %q", text)
	}
}

func TestProcessGithubMarkdownDowngradesHeadingsWithoutTrimmingContentHash(t *testing.T) {
	text, _ := ProcessGithubMarkdown("## C#\\n## Verification ##")

	if strings.Contains(text, "##") {
		t.Fatalf("Heading markers should be removed: %q", text)
	}
	if !strings.Contains(text, "C#\nVerification") {
		t.Fatalf("Heading content was not preserved correctly: %q", text)
	}
}

func TestProcessGithubMarkdownDowngradesHTMLHeadings(t *testing.T) {
	text, _ := ProcessGithubMarkdown("<h2>Verification</h2><ul><li>npm run lint</li></ul>")

	if strings.Contains(text, "**Verification**") {
		t.Fatalf("HTML heading should not be converted to bold heading text: %q", text)
	}
	if !strings.Contains(text, "Verification\n- npm run lint") {
		t.Fatalf("HTML heading/list not preserved as compact text: %q", text)
	}
}

func TestProcessGithubMarkdownDowngradesHTMLHeadingsWithAttributes(t *testing.T) {
	text, _ := ProcessGithubMarkdown(`<h2 id="verification">Verification</h2><ul><li class="task">npm run lint</li></ul>`)

	if strings.Contains(text, "**Verification**") || strings.Contains(text, "<h2") {
		t.Fatalf("HTML heading should be downgraded to plain text: %q", text)
	}
	if !strings.Contains(text, "Verification\n- npm run lint") {
		t.Fatalf("HTML heading/list with attributes not preserved as compact text: %q", text)
	}
}

func TestProcessGithubMarkdownDetailsUseUnifiedCardFold(t *testing.T) {
	input := "before 1\nbefore 2\nbefore 3\nbefore 4\nbefore 5\n<details><summary>More</summary>inside 1\ninside 2\ninside 3\ninside 4\ninside 5</details>"
	text, foldable := ProcessGithubMarkdown(input)

	if foldable != "" {
		t.Fatalf("details content should not bypass the unified card fold: %q", foldable)
	}
	if !strings.Contains(text, "**More**\ninside 1") {
		t.Fatalf("details content was not restored to the main markdown: %q", text)
	}

	card := NewCard()
	card.AddMarkdown(text)
	if len(card.Body.Elements) != 2 {
		t.Fatalf("ten content lines should create one visible block and one fold: %#v", card.Body.Elements)
	}
	panel, ok := card.Body.Elements[1].(map[string]any)
	if !ok || panel["tag"] != "collapsible_panel" {
		t.Fatalf("expected collapsible panel, got %#v", card.Body.Elements[1])
	}
}

func TestIssueEditedBuildsLatestBodyForOriginalCardUpdate(t *testing.T) {
	body := strings.Join([]string{
		"section 1",
		"use `season_id`",
		"section 3",
		"section 4",
		"section 5",
		"section 6",
		"section 7",
		"section 8",
		"section 9",
	}, "\n")
	detail := ParseEvent(&github.IssuesEvent{
		Action: strPtr("edited"),
		Issue: &github.Issue{
			Number:    intPtr(36),
			Title:     strPtr("Season migration"),
			Body:      strPtr(body),
			HTMLURL:   strPtr("https://github.com/NCUHOME/Hr2026Be/issues/36"),
			CreatedAt: &github.Timestamp{Time: time.Now()},
		},
	}, "issues")

	if detail.Title != "🍄 Issue edited" || detail.Action != "edited" {
		t.Fatalf("unexpected edited Issue detail: %#v", detail)
	}
	if !strings.Contains(detail.Text, "section 9") {
		t.Fatalf("edited Issue should carry the complete latest body: %q", detail.Text)
	}

	card := BuildCard(context.Background(), "NCUHOME/Hr2026Be", "editor", "https://github.com/editor", "", detail)
	cardJSON := card.String()
	if !strings.Contains(cardJSON, `"tag":"collapsible_panel"`) {
		t.Fatalf("Issue title plus nine body lines should meet the 5+5 fold threshold: %s", cardJSON)
	}
	if !strings.Contains(cardJSON, "`season_id`") {
		t.Fatalf("inline code should preserve its backtick styling: %s", cardJSON)
	}
}

func TestWorkflowRunAttemptHelpers(t *testing.T) {
	payload := map[string]any{
		"workflow_run": map[string]any{
			"id":          float64(12345),
			"run_attempt": float64(2),
			"triggering_actor": map[string]any{
				"login":      "rerun-user",
				"html_url":   "https://github.com/rerun-user",
				"avatar_url": "https://avatars.githubusercontent.com/u/2",
			},
		},
	}

	if got := workflowRunBaseID(payload); got != "wf:12345" {
		t.Fatalf("workflowRunBaseID() = %q", got)
	}
	if got := workflowRunAttemptID(payload); got != "wf:12345:attempt:2" {
		t.Fatalf("workflowRunAttemptID() = %q", got)
	}

	sender, senderURL, avatarURL := applyWorkflowTriggeringActor(
		payload,
		"github-actions[bot]",
		"https://github.com/apps/github-actions",
		"https://avatars.githubusercontent.com/in/15368",
	)
	if sender != "rerun-user" || senderURL != "https://github.com/rerun-user" || avatarURL != "https://avatars.githubusercontent.com/u/2" {
		t.Fatalf("triggering actor not applied: sender=%q url=%q avatar=%q", sender, senderURL, avatarURL)
	}
}

func TestWorkflowRunAttemptIDDefaultsToBaseID(t *testing.T) {
	payload := map[string]any{
		"workflow_run": map[string]any{
			"id": float64(12345),
		},
	}

	if got := workflowRunAttemptID(payload); got != "wf:12345" {
		t.Fatalf("workflowRunAttemptID() = %q", got)
	}
}

func TestWorkflowRunAttemptIDSupportsIntegerPayloadValues(t *testing.T) {
	payload := map[string]any{
		"workflow_run": map[string]any{
			"id":          int64(12345),
			"run_attempt": int64(3),
		},
	}

	if got := workflowRunAttemptID(payload); got != "wf:12345:attempt:3" {
		t.Fatalf("workflowRunAttemptID() = %q", got)
	}
}

func TestWorkflowRunRerunNoticeUsesAttemptMetadata(t *testing.T) {
	payload := map[string]any{
		"workflow_run": map[string]any{
			"run_attempt": float64(2),
			"html_url":    "https://github.com/test/repo/actions/runs/123",
			"status":      "completed",
			"conclusion":  "success",
			"triggering_actor": map[string]any{
				"login":    "rerun-user",
				"html_url": "https://github.com/rerun-user",
			},
		},
	}

	got := workflowRunRerunNotice(payload, "github-actions[bot]")
	want := "🔁 This workflow was rerun as [attempt #2](https://github.com/test/repo/actions/runs/123) by [rerun-user](https://github.com/rerun-user)."
	if got != want {
		t.Fatalf("workflowRunRerunNotice() = %q", got)
	}
	if strings.Contains(got, "success") || strings.Contains(got, "completed") {
		t.Fatalf("rerun notice should not contain current attempt status: %q", got)
	}
}

func TestEscapeSQLLikePattern(t *testing.T) {
	got := escapeSQLLikePattern(`wf:12_%\34`)
	want := `wf:12\_\%\\34`
	if got != want {
		t.Fatalf("escapeSQLLikePattern() = %q", got)
	}
}

func TestSetCIStatusForWorkflowRunUsesWorkflowName(t *testing.T) {
	payload := map[string]any{
		"action": "requested",
		"workflow_run": map[string]any{
			"id":     float64(12345),
			"name":   "CI",
			"status": "requested",
		},
	}
	status, conclusion := extractCIStatus(payload, "workflow_run")
	detail := EventDetail{
		Title:     "⚙️ Workflow Requested: CI",
		EventTime: "2026-06-09T07:00:00Z",
	}

	setCIStatusForWorkflowRun(payload, &detail, status, conclusion, "main")

	if len(detail.CIStatuses) != 1 {
		t.Fatalf("CIStatuses len = %d", len(detail.CIStatuses))
	}
	got := detail.CIStatuses[0]
	if got.WorkflowName != "CI" {
		t.Fatalf("WorkflowName = %q", got.WorkflowName)
	}
	if got.Status != "requested" {
		t.Fatalf("Status = %q", got.Status)
	}
}

func TestSendNewEventTypeCards(t *testing.T) {
	LoadConfig()
	InitDB()

	if C.Feishu.AppID == "" || C.Feishu.ChatID == "" {
		t.Skip("Feishu credentials not configured")
	}

	ctx := context.Background()
	now := time.Now()

	// Test sending a Discussion card
	detail := ParseEvent(&github.DiscussionEvent{
		Action: strPtr("created"),
		Discussion: &github.Discussion{
			Number:    intPtr(1),
			Title:     strPtr("如何部署这个项目？"),
			Body:      strPtr("我想了解一下部署流程"),
			HTMLURL:   strPtr("https://github.com/test/repo/discussions/1"),
			CreatedAt: &github.Timestamp{Time: now},
		},
		Repo: &github.Repository{
			FullName: strPtr("test/repo"),
			HTMLURL:  strPtr("https://github.com/test/repo"),
		},
		Sender: &github.User{
			Login:     strPtr("testuser"),
			HTMLURL:   strPtr("https://github.com/testuser"),
			AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
		},
	}, "discussion")

	detail.RepoName = "test/repo"
	detail.RepoURL = "https://github.com/test/repo"

	card := BuildCard(ctx, "test/repo", "testuser", "https://github.com/testuser", "", detail)
	msgID, err := SendToChat("", card)
	if err != nil {
		t.Fatalf("send discussion card failed: %v", err)
	}
	fmt.Println("✓ Discussion card sent, message_id:", msgID)

	// Test sending a Deployment Status card
	detail2 := ParseEvent(&github.DeploymentStatusEvent{
		Action: strPtr("created"),
		Deployment: &github.Deployment{
			Environment: strPtr("production"),
		},
		DeploymentStatus: &github.DeploymentStatus{
			State:       strPtr("success"),
			Description: strPtr("部署成功"),
			TargetURL:   strPtr("https://example.com"),
			CreatedAt:   &github.Timestamp{Time: now},
		},
		Repo: &github.Repository{
			FullName: strPtr("test/repo"),
			HTMLURL:  strPtr("https://github.com/test/repo"),
		},
		Sender: &github.User{
			Login:     strPtr("testuser"),
			HTMLURL:   strPtr("https://github.com/testuser"),
			AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
		},
	}, "deployment_status")

	detail2.RepoName = "test/repo"
	detail2.RepoURL = "https://github.com/test/repo"

	card2 := BuildCard(ctx, "test/repo", "testuser", "https://github.com/testuser", "", detail2)
	msgID2, err := SendToChat("", card2)
	if err != nil {
		t.Fatalf("send deployment status card failed: %v", err)
	}
	fmt.Println("✓ Deployment Status card sent, message_id:", msgID2)

	// Test sending a Code Scanning Alert card
	detail3 := ParseEvent(&github.CodeScanningAlertEvent{
		Action: strPtr("created"),
		Alert: &github.Alert{
			Number:          intPtr(42),
			RuleDescription: strPtr("未使用的变量"),
			RuleSeverity:    strPtr("warning"),
			HTMLURL:         strPtr("https://github.com/test/repo/security/code-scanning/42"),
			CreatedAt:       &github.Timestamp{Time: now},
		},
		Ref: strPtr("refs/heads/main"),
		Repo: &github.Repository{
			FullName: strPtr("test/repo"),
			HTMLURL:  strPtr("https://github.com/test/repo"),
		},
		Sender: &github.User{
			Login:     strPtr("testuser"),
			HTMLURL:   strPtr("https://github.com/testuser"),
			AvatarURL: strPtr("https://avatars.githubusercontent.com/u/12345"),
		},
	}, "code_scanning_alert")

	detail3.RepoName = "test/repo"
	detail3.RepoURL = "https://github.com/test/repo"

	card3 := BuildCard(ctx, "test/repo", "testuser", "https://github.com/testuser", "", detail3)
	msgID3, err := SendToChat("", card3)
	if err != nil {
		t.Fatalf("send code scanning alert card failed: %v", err)
	}
	fmt.Println("✓ Code Scanning Alert card sent, message_id:", msgID3)
}

func TestWorkflowRunPRNumberExtractsNumber(t *testing.T) {
	m := map[string]any{
		"pull_requests": []any{
			map[string]any{"number": float64(26)},
		},
	}
	if got := workflowRunPRNumber(m); got != "26" {
		t.Fatalf("workflowRunPRNumber() = %q, want 26", got)
	}
}

func TestWorkflowRunPRNumberHandlesIntType(t *testing.T) {
	m := map[string]any{
		"pull_requests": []any{
			map[string]any{"number": int(42)},
		},
	}
	if got := workflowRunPRNumber(m); got != "42" {
		t.Fatalf("workflowRunPRNumber() = %q, want 42", got)
	}
}

func TestWorkflowRunPRNumberHandlesStringType(t *testing.T) {
	m := map[string]any{
		"pull_requests": []any{
			map[string]any{"number": "7"},
		},
	}
	if got := workflowRunPRNumber(m); got != "7" {
		t.Fatalf("workflowRunPRNumber() = %q, want 7", got)
	}
}

func TestWorkflowRunPRNumberReturnsEmptyForNilPayload(t *testing.T) {
	if got := workflowRunPRNumber(nil); got != "" {
		t.Fatalf("workflowRunPRNumber(nil) = %q, want empty", got)
	}
}

func TestWorkflowRunPRNumberReturnsEmptyForEmptyArray(t *testing.T) {
	m := map[string]any{"pull_requests": []any{}}
	if got := workflowRunPRNumber(m); got != "" {
		t.Fatalf("workflowRunPRNumber() = %q, want empty", got)
	}
}

func TestWorkflowRunPRNumberReturnsEmptyForMissingField(t *testing.T) {
	m := map[string]any{}
	if got := workflowRunPRNumber(m); got != "" {
		t.Fatalf("workflowRunPRNumber() = %q, want empty", got)
	}
}
