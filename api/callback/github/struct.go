package githubCall

import "time"

type CommitInfo struct {
	ID        string    `json:"id" form:"id"`
	TreeID    string    `json:"tree_id" form:"tree_id"`
	Distinct  bool      `json:"distinct" form:"distinct"`
	Message   string    `json:"message" form:"message"`
	Timestamp time.Time `json:"timestamp" form:"timestamp"`
	Url       string    `json:"url" form:"url"`
	Author    struct {
		Name     string `json:"name" form:"name"`
		Email    string `json:"email" form:"email"`
		Username string `json:"username" form:"username"`
	} `json:"author" form:"author"`
	Committer struct {
		Name     string `json:"name" form:"name"`
		Email    string `json:"email" form:"email"`
		Username string `json:"username" form:"username"`
	} `json:"committer" form:"committer"`
	Added    []string `json:"added" form:"added"`
	Removed  []string `json:"removed" form:"removed"`
	Modified []string `json:"modified" form:"modified"`
}

type RepositoryInfo struct {
	ID       int    `json:"id" form:"id"`
	NodeID   string `json:"node_id" form:"node_id"`
	Name     string `json:"name" form:"name"`
	FullName string `json:"full_name" form:"full_name"`
	Private  bool   `json:"private" form:"private"`
	Owner    struct {
		Name              string `json:"name" form:"name"`
		Email             string `json:"email" form:"email"`
		Login             string `json:"login" form:"login"`
		Id                int    `json:"id" form:"id"`
		NodeId            string `json:"node_id" form:"node_id"`
		AvatarUrl         string `json:"avatar_url" form:"avatar_url"`
		GravatarId        string `json:"gravatar_id" form:"gravatar_id"`
		Url               string `json:"url" form:"url"`
		HtmlUrl           string `json:"html_url" form:"html_url"`
		FollowersUrl      string `json:"followers_url" form:"followers_url"`
		FollowingUrl      string `json:"following_url" form:"following_url"`
		GistsUrl          string `json:"gists_url" form:"gists_url"`
		StarredUrl        string `json:"starred_url" form:"starred_url"`
		SubscriptionsUrl  string `json:"subscriptions_url" form:"subscriptions_url"`
		OrganizationsUrl  string `json:"organizations_url" form:"organizations_url"`
		ReposUrl          string `json:"repos_url" form:"repos_url"`
		EventsUrl         string `json:"events_url" form:"events_url"`
		ReceivedEventsUrl string `json:"received_events_url" form:"received_events_url"`
		Type              string `json:"type" form:"type"`
		SiteAdmin         bool   `json:"site_admin" form:"site_admin"`
	} `json:"owner" form:"owner"`
	HtmlUrl          string      `json:"html_url" form:"html_url"`
	Description      interface{} `json:"description" form:"description"`
	Fork             bool        `json:"fork" form:"fork"`
	Url              string      `json:"url" form:"url"`
	ForksUrl         string      `json:"forks_url" form:"forks_url"`
	KeysUrl          string      `json:"keys_url" form:"keys_url"`
	CollaboratorsUrl string      `json:"collaborators_url" form:"collaborators_url"`
	TeamsUrl         string      `json:"teams_url" form:"teams_url"`
	HooksUrl         string      `json:"hooks_url" form:"hooks_url"`
	IssueEventsUrl   string      `json:"issue_events_url" form:"issue_events_url"`
	EventsUrl        string      `json:"events_url" form:"events_url"`
	AssigneesUrl     string      `json:"assignees_url" form:"assignees_url"`
	BranchesUrl      string      `json:"branches_url" form:"branches_url"`
	TagsUrl          string      `json:"tags_url" form:"tags_url"`
	BlobsUrl         string      `json:"blobs_url" form:"blobs_url"`
	GitTagsUrl       string      `json:"git_tags_url" form:"git_tags_url"`
	GitRefsUrl       string      `json:"git_refs_url" form:"git_refs_url"`
	TreesUrl         string      `json:"trees_url" form:"trees_url"`
	StatusesUrl      string      `json:"statuses_url" form:"statuses_url"`
	LanguagesUrl     string      `json:"languages_url" form:"languages_url"`
	StargazersUrl    string      `json:"stargazers_url" form:"stargazers_url"`
	ContributorsUrl  string      `json:"contributors_url" form:"contributors_url"`
	SubscribersUrl   string      `json:"subscribers_url" form:"subscribers_url"`
	SubscriptionUrl  string      `json:"subscription_url" form:"subscription_url"`
	CommitsUrl       string      `json:"commits_url" form:"commits_url"`
	GitCommitsUrl    string      `json:"git_commits_url" form:"git_commits_url"`
	CommentsUrl      string      `json:"comments_url" form:"comments_url"`
	IssueCommentUrl  string      `json:"issue_comment_url" form:"issue_comment_url"`
	ContentsUrl      string      `json:"contents_url" form:"contents_url"`
	CompareUrl       string      `json:"compare_url" form:"compare_url"`
	MergesUrl        string      `json:"merges_url" form:"merges_url"`
	ArchiveUrl       string      `json:"archive_url" form:"archive_url"`
	DownloadsUrl     string      `json:"downloads_url" form:"downloads_url"`
	IssuesUrl        string      `json:"issues_url" form:"issues_url"`
	PullsUrl         string      `json:"pulls_url" form:"pulls_url"`
	MilestonesUrl    string      `json:"milestones_url" form:"milestones_url"`
	NotificationsUrl string      `json:"notifications_url" form:"notifications_url"`
	LabelsUrl        string      `json:"labels_url" form:"labels_url"`
	ReleasesUrl      string      `json:"releases_url" form:"releases_url"`
	DeploymentsUrl   string      `json:"deployments_url" form:"deployments_url"`
	CreatedAt        interface{} `json:"created_at" form:"created_at"`
	UpdatedAt        time.Time   `json:"updated_at" form:"updated_at"`
	PushedAt         interface{} `json:"pushed_at" form:"pushed_at"`
	GitUrl           string      `json:"git_url" form:"git_url"`
	SshUrl           string      `json:"ssh_url" form:"ssh_url"`
	CloneUrl         string      `json:"clone_url" form:"clone_url"`
	SvnUrl           string      `json:"svn_url" form:"svn_url"`
	Homepage         interface{} `json:"homepage" form:"homepage"`
	Size             int         `json:"size" form:"size"`
	StargazersCount  int         `json:"stargazers_count" form:"stargazers_count"`
	WatchersCount    int         `json:"watchers_count" form:"watchers_count"`
	Language         string      `json:"language" form:"language"`
	HasIssues        bool        `json:"has_issues" form:"has_issues"`
	HasProjects      bool        `json:"has_projects" form:"has_projects"`
	HasDownloads     bool        `json:"has_downloads" form:"has_downloads"`
	HasWiki          bool        `json:"has_wiki" form:"has_wiki"`
	HasPages         bool        `json:"has_pages" form:"has_pages"`
	ForksCount       int         `json:"forks_count" form:"forks_count"`
	MirrorUrl        interface{} `json:"mirror_url" form:"mirror_url"`
	Archived         bool        `json:"archived" form:"archived"`
	Disabled         bool        `json:"disabled" form:"disabled"`
	OpenIssuesCount  int         `json:"open_issues_count" form:"open_issues_count"`
	License          interface{} `json:"license" form:"license"`
	Forks            int         `json:"forks" form:"forks"`
	OpenIssues       int         `json:"open_issues" form:"open_issues"`
	Watchers         int         `json:"watchers" form:"watchers"`
	DefaultBranch    string      `json:"default_branch" form:"default_branch"`
	Stargazers       int         `json:"stargazers" form:"stargazers"`
	MasterBranch     string      `json:"master_branch" form:"master_branch"`
}

type SenderInfo struct {
	ID                int    `json:"id" form:"id"`
	Login             string `json:"login" form:"login"`
	NodeId            string `json:"node_id" form:"node_id"`
	AvatarUrl         string `json:"avatar_url" form:"avatar_url"`
	GravatarId        string `json:"gravatar_id" form:"gravatar_id"`
	Url               string `json:"url" form:"url"`
	HtmlUrl           string `json:"html_url" form:"html_url"`
	FollowersUrl      string `json:"followers_url" form:"followers_url"`
	FollowingUrl      string `json:"following_url" form:"following_url"`
	GistsUrl          string `json:"gists_url" form:"gists_url"`
	StarredUrl        string `json:"starred_url" form:"starred_url"`
	SubscriptionsUrl  string `json:"subscriptions_url" form:"subscriptions_url"`
	OrganizationsUrl  string `json:"organizations_url" form:"organizations_url"`
	ReposUrl          string `json:"repos_url" form:"repos_url"`
	EventsUrl         string `json:"events_url" form:"events_url"`
	ReceivedEventsUrl string `json:"received_events_url" form:"received_events_url"`
	Type              string `json:"type" form:"type"`
	SiteAdmin         bool   `json:"site_admin" form:"site_admin"`
}
