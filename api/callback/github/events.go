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
	Action  string `json:"action" form:"action"`
	Changes struct {
		Body struct {
			From string `json:"from" form:"from"`
		} `json:"body" form:"body"`
		Title struct {
			From string `json:"from" form:"from"`
		} `json:"title" form:"title"`
	} `json:"changes" form:"changes"`
	Issue        IssueInfo        `json:"issue" form:"issue"`
	Repository   RepositoryInfo   `json:"repository" form:"repository"`
	Organization OrganizationInfo `json:"organization" form:"organization"`
	Sender       SenderInfo       `json:"sender" form:"sender"`
}

type IssueCommentEvent struct {
	Action  string    `json:"action"`
	Issue   IssueInfo `json:"issue"`
	Comment struct {
		Url      string `json:"url"`
		HtmlUrl  string `json:"html_url"`
		IssueUrl string `json:"issue_url"`
		Id       int    `json:"id"`
		NodeId   string `json:"node_id"`
		User     struct {
			Login             string `json:"login"`
			Id                int    `json:"id"`
			NodeId            string `json:"node_id"`
			AvatarUrl         string `json:"avatar_url"`
			GravatarId        string `json:"gravatar_id"`
			Url               string `json:"url"`
			HtmlUrl           string `json:"html_url"`
			FollowersUrl      string `json:"followers_url"`
			FollowingUrl      string `json:"following_url"`
			GistsUrl          string `json:"gists_url"`
			StarredUrl        string `json:"starred_url"`
			SubscriptionsUrl  string `json:"subscriptions_url"`
			OrganizationsUrl  string `json:"organizations_url"`
			ReposUrl          string `json:"repos_url"`
			EventsUrl         string `json:"events_url"`
			ReceivedEventsUrl string `json:"received_events_url"`
			Type              string `json:"type"`
			SiteAdmin         bool   `json:"site_admin"`
		} `json:"user"`
		CreatedAt         time.Time `json:"created_at"`
		UpdatedAt         time.Time `json:"updated_at"`
		AuthorAssociation string    `json:"author_association"`
		Body              string    `json:"body"`
		Reactions         struct {
			Url        string `json:"url"`
			TotalCount int    `json:"total_count"`
			Field3     int    `json:"+1"`
			Field4     int    `json:"-1"`
			Laugh      int    `json:"laugh"`
			Hooray     int    `json:"hooray"`
			Confused   int    `json:"confused"`
			Heart      int    `json:"heart"`
			Rocket     int    `json:"rocket"`
			Eyes       int    `json:"eyes"`
		} `json:"reactions"`
		PerformedViaGithubApp interface{} `json:"performed_via_github_app"`
	} `json:"comment"`
	Repository   RepositoryInfo   `json:"repository"`
	Organization OrganizationInfo `json:"organization"`
	Sender       SenderInfo       `json:"sender"`
}
