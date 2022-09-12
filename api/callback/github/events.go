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
	Issue struct {
		Url           string `json:"url" form:"url"`
		RepositoryUrl string `json:"repository_url" form:"repositoryUrl"`
		LabelsUrl     string `json:"labels_url" form:"labelsUrl"`
		CommentsUrl   string `json:"comments_url" form:"commentsUrl"`
		EventsUrl     string `json:"events_url" form:"eventsUrl"`
		HtmlUrl       string `json:"html_url" form:"htmlUrl"`
		Id            int    `json:"id" form:"id"`
		NodeId        string `json:"node_id" form:"nodeId"`
		Number        int    `json:"number" form:"number"`
		Title         string `json:"title" form:"title"`
		User          struct {
			Login             string `json:"login" form:"login"`
			Id                int    `json:"id" form:"id"`
			NodeId            string `json:"node_id" form:"nodeId"`
			AvatarUrl         string `json:"avatar_url" form:"avatarUrl"`
			GravatarId        string `json:"gravatar_id" form:"gravatarId"`
			Url               string `json:"url" form:"url"`
			HtmlUrl           string `json:"html_url" form:"htmlUrl"`
			FollowersUrl      string `json:"followers_url" form:"followersUrl"`
			FollowingUrl      string `json:"following_url" form:"followingUrl"`
			GistsUrl          string `json:"gists_url" form:"gistsUrl"`
			StarredUrl        string `json:"starred_url" form:"starredUrl"`
			SubscriptionsUrl  string `json:"subscriptions_url" form:"subscriptionsUrl"`
			OrganizationsUrl  string `json:"organizations_url" form:"organizationsUrl"`
			ReposUrl          string `json:"repos_url" form:"reposUrl"`
			EventsUrl         string `json:"events_url" form:"eventsUrl"`
			ReceivedEventsUrl string `json:"received_events_url" form:"receivedEventsUrl"`
			Type              string `json:"type" form:"type"`
			SiteAdmin         bool   `json:"site_admin" form:"siteAdmin"`
		} `json:"user" form:"user"`
		Labels            []interface{} `json:"labels" form:"labels"`
		State             string        `json:"state" form:"state"`
		Locked            bool          `json:"locked" form:"locked"`
		Assignee          interface{}   `json:"assignee" form:"assignee"`
		Assignees         []interface{} `json:"assignees" form:"assignees"`
		Milestone         interface{}   `json:"milestone" form:"milestone"`
		Comments          int           `json:"comments" form:"comments"`
		CreatedAt         time.Time     `json:"created_at" form:"createdAt"`
		UpdatedAt         time.Time     `json:"updated_at" form:"updatedAt"`
		ClosedAt          interface{}   `json:"closed_at" form:"closedAt"`
		AuthorAssociation string        `json:"author_association" form:"authorAssociation"`
		ActiveLockReason  interface{}   `json:"active_lock_reason" form:"activeLockReason"`
		Body              string        `json:"body" form:"body"`
		Reactions         struct {
			Url        string `json:"url" form:"url"`
			TotalCount int    `json:"total_count" form:"totalCount"`
			Plus       int    `json:"+1" form:"+1"`
			Decrease   int    `json:"-1" form:"-1"`
			Laugh      int    `json:"laugh" form:"laugh"`
			Hooray     int    `json:"hooray" form:"hooray"`
			Confused   int    `json:"confused" form:"confused"`
			Heart      int    `json:"heart" form:"heart"`
			Rocket     int    `json:"rocket" form:"rocket"`
			Eyes       int    `json:"eyes" form:"eyes"`
		} `json:"reactions" form:"reactions"`
		TimelineUrl           string      `json:"timeline_url" form:"timelineUrl"`
		PerformedViaGithubApp interface{} `json:"performed_via_github_app" form:"performedViaGithubApp"`
		StateReason           interface{} `json:"state_reason" form:"stateReason"`
	} `json:"issue" form:"issue"`
	Repository   RepositoryInfo   `json:"repository" form:"repository"`
	Organization OrganizationInfo `json:"organization" form:"organization"`
	Sender       SenderInfo       `json:"sender" form:"sender"`
}
