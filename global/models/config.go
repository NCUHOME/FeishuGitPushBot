package models

type Config struct {
	Feishu Feishu
	Github Github
}

type Feishu struct {
	Webhook string
	Secret  string
}

type Github struct {
	WebhookKey string
}
