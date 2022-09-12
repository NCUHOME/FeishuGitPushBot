package githubCall

import "time"

type PushEvent struct {
	Ref        string         `json:"ref" form:"ref"`
	Before     string         `json:"before" form:"before"`
	After      string         `json:"after" form:"after"`
	Created    bool           `json:"created" form:"created"`
	Deleted    bool           `json:"deleted" form:"deleted"`
	Forced     bool           `json:"forced" form:"forced"`
	BaseRef    string         `json:"base_ref" form:"base_ref"`
	Compare    string         `json:"compare" form:"compare"`
	Commits    []CommitInfo   `json:"commits" form:"commits"`
	HeadCommit CommitInfo     `json:"head_commit" form:"head_commit"`
	Repository RepositoryInfo `json:"repository" form:"repository"`
	Pusher     struct {
		Name  string `json:"name" form:"name"`
		Email string `json:"email" form:"email"`
	} `json:"pusher" form:"pusher"`
	Sender SenderInfo `json:"sender" form:"sender"`
}

type CreateEvent struct {
	Ref          string           `json:"ref" form:"ref"`
	RefType      string           `json:"ref_type" form:"ref_type"`
	MasterBranch string           `json:"master_branch" form:"master_branch"`
	Description  interface{}      `json:"description" form:"description"`
	PusherType   string           `json:"pusher_type" form:"pusher_type"`
	Repository   RepositoryInfo   `json:"repository" form:"repository"`
	Organization OrganizationInfo `json:"organization" form:"organization"`
	Sender       SenderInfo       `json:"sender" form:"sender"`
}

type DeleteEvent struct {
	CreateEvent
}

type IssueEvent struct {
	Action       string           `json:"action" form:"action"`
	Changes      ChangeInfo       `json:"changes" form:"changes"`
	Issue        IssueInfo        `json:"issue" form:"issue"`
	Repository   RepositoryInfo   `json:"repository" form:"repository"`
	Organization OrganizationInfo `json:"organization" form:"organization"`
	Sender       SenderInfo       `json:"sender" form:"sender"`
}

type IssueCommentEvent struct {
	Action  string    `json:"action" form:"action"`
	Issue   IssueInfo `json:"issue" form:"issue"`
	Comment struct {
		Url                   string        `json:"url" form:"url"`
		HtmlUrl               string        `json:"html_url" form:"html_url"`
		IssueUrl              string        `json:"issue_url" form:"issue_url"`
		Id                    int           `json:"id" form:"id"`
		NodeId                string        `json:"node_id" form:"node_id"`
		User                  UserInfo      `json:"user" form:"user"`
		CreatedAt             time.Time     `json:"created_at" form:"created_at"`
		UpdatedAt             time.Time     `json:"updated_at" form:"updated_at"`
		AuthorAssociation     string        `json:"author_association" form:"author_association"`
		Body                  string        `json:"body" form:"body"`
		Reactions             ReactionsInfo `json:"reactions" form:"reactions"`
		PerformedViaGithubApp interface{}   `json:"performed_via_github_app" form:"performed_via_github_app"`
	} `json:"comment" form:"comment"`
	Repository   RepositoryInfo   `json:"repository" form:"repository"`
	Organization OrganizationInfo `json:"organization" form:"organization"`
	Sender       SenderInfo       `json:"sender" form:"sender"`
}

type PullRequestEvent struct {
	Action      string     `json:"action" form:"action"`
	Number      int        `json:"number" form:"number"`
	Changes     ChangeInfo `json:"changes" form:"changes"`
	PullRequest struct {
		Url                string        `json:"url" form:"url"`
		Id                 int           `json:"id" form:"id"`
		NodeId             string        `json:"node_id" form:"node_id"`
		HtmlUrl            string        `json:"html_url" form:"html_url"`
		DiffUrl            string        `json:"diff_url" form:"diff_url"`
		PatchUrl           string        `json:"patch_url" form:"patch_url"`
		IssueUrl           string        `json:"issue_url" form:"issue_url"`
		Number             int           `json:"number" form:"number"`
		State              string        `json:"state" form:"state"`
		Locked             bool          `json:"locked" form:"locked"`
		Title              string        `json:"title" form:"title"`
		User               UserInfo      `json:"user" form:"user"`
		Body               string        `json:"body" form:"body"`
		CreatedAt          time.Time     `json:"created_at" form:"created_at"`
		UpdatedAt          time.Time     `json:"updated_at" form:"updated_at"`
		ClosedAt           interface{}   `json:"closed_at" form:"closed_at"`
		MergedAt           interface{}   `json:"merged_at" form:"merged_at"`
		MergeCommitSha     interface{}   `json:"merge_commit_sha" form:"merge_commit_sha"`
		Assignee           interface{}   `json:"assignee" form:"assignee"`
		Assignees          []interface{} `json:"assignees" form:"assignees"`
		RequestedReviewers []interface{} `json:"requested_reviewers" form:"requested_reviewers"`
		RequestedTeams     []interface{} `json:"requested_teams" form:"requested_teams"`
		Labels             []interface{} `json:"labels" form:"labels"`
		Milestone          interface{}   `json:"milestone" form:"milestone"`
		Draft              bool          `json:"draft" form:"draft"`
		CommitsUrl         string        `json:"commits_url" form:"commits_url"`
		ReviewCommentsUrl  string        `json:"review_comments_url" form:"review_comments_url"`
		ReviewCommentUrl   string        `json:"review_comment_url" form:"review_comment_url"`
		CommentsUrl        string        `json:"comments_url" form:"comments_url"`
		StatusesUrl        string        `json:"statuses_url" form:"statuses_url"`
		Head               struct {
			Label string         `json:"label" form:"label"`
			Ref   string         `json:"ref" form:"ref"`
			Sha   string         `json:"sha" form:"sha"`
			User  UserInfo       `json:"user" form:"user"`
			Repo  RepositoryInfo `json:"repo" form:"repo"`
		} `json:"head" form:"head"`
		Base struct {
			Label string         `json:"label" form:"label"`
			Ref   string         `json:"ref" form:"ref"`
			Sha   string         `json:"sha" form:"sha"`
			User  UserInfo       `json:"user" form:"user"`
			Repo  RepositoryInfo `json:"repo" form:"repo"`
		} `json:"base" form:"base"`
		Links struct {
			Self struct {
				Href string `json:"href" form:"href"`
			} `json:"self" form:"self"`
			Html struct {
				Href string `json:"href" form:"href"`
			} `json:"html" form:"html"`
			Issue struct {
				Href string `json:"href" form:"href"`
			} `json:"issue" form:"issue"`
			Comments struct {
				Href string `json:"href" form:"href"`
			} `json:"comments" form:"comments"`
			ReviewComments struct {
				Href string `json:"href" form:"href"`
			} `json:"review_comments" form:"review_comments"`
			ReviewComment struct {
				Href string `json:"href" form:"href"`
			} `json:"review_comment" form:"review_comment"`
			Commits struct {
				Href string `json:"href" form:"href"`
			} `json:"commits" form:"commits"`
			Statuses struct {
				Href string `json:"href" form:"href"`
			} `json:"statuses" form:"statuses"`
		} `json:"_links" form:"links"`
		AuthorAssociation   string      `json:"author_association" form:"author_association"`
		AutoMerge           interface{} `json:"auto_merge" form:"auto_merge"`
		ActiveLockReason    interface{} `json:"active_lock_reason" form:"active_lock_reason"`
		Merged              bool        `json:"merged" form:"merged"`
		Mergeable           interface{} `json:"mergeable" form:"mergeable"`
		Rebaseable          interface{} `json:"rebaseable" form:"rebaseable"`
		MergeableState      string      `json:"mergeable_state" form:"mergeable_state"`
		MergedBy            interface{} `json:"merged_by" form:"merged_by"`
		Comments            int         `json:"comments" form:"comments"`
		ReviewComments      int         `json:"review_comments" form:"review_comments"`
		MaintainerCanModify bool        `json:"maintainer_can_modify" form:"maintainer_can_modify"`
		Commits             int         `json:"commits" form:"commits"`
		Additions           int         `json:"additions" form:"additions"`
		Deletions           int         `json:"deletions" form:"deletions"`
		ChangedFiles        int         `json:"changed_files" form:"changed_files"`
	} `json:"pull_request" form:"pull_request"`
	Repository   RepositoryInfo   `json:"repository" form:"repository"`
	Organization OrganizationInfo `json:"organization" form:"organization"`
	Sender       SenderInfo       `json:"sender" form:"sender"`
}
