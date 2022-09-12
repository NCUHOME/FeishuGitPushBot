package githubCall

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
	Ref          string         `json:"ref" form:"ref"`
	RefType      string         `json:"ref_type" form:"ref_type"`
	MasterBranch string         `json:"master_branch" form:"master_branch"`
	Description  interface{}    `json:"description" form:"description"`
	PusherType   string         `json:"pusher_type" form:"pusher_type"`
	Repository   RepositoryInfo `json:"repository" form:"repository"`
	Organization struct {
		Login            string      `json:"login" form:"login"`
		Id               int         `json:"id" form:"id"`
		NodeId           string      `json:"node_id" form:"node_id"`
		Url              string      `json:"url" form:"url"`
		ReposUrl         string      `json:"repos_url" form:"repos_url"`
		EventsUrl        string      `json:"events_url" form:"events_url"`
		HooksUrl         string      `json:"hooks_url" form:"hooks_url"`
		IssuesUrl        string      `json:"issues_url" form:"issues_url"`
		MembersUrl       string      `json:"members_url" form:"members_url"`
		PublicMembersUrl string      `json:"public_members_url" form:"public_members_url"`
		AvatarUrl        string      `json:"avatar_url" form:"avatar_url"`
		Description      interface{} `json:"description" form:"description"`
	} `json:"organization" form:"organization"`
	Sender SenderInfo `json:"sender" form:"sender"`
}

type DeleteEvent struct {
	CreateEvent
}
