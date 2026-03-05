package bot

import (
	"log"
	"strings"

	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	Feishu struct {
		Webhook string `koanf:"webhook"`
		Secret  string `koanf:"secret"`
	} `koanf:"feishu"`
	Github struct {
		WebhookKey string `koanf:"webhook_key"`
	} `koanf:"github"`
}

var C Config

// Load 从环境变量解析配置
func LoadConfig() {
	k := koanf.New(".")
	// 允许通过环境变量设置，如 FEISHU_WEBHOOK -> feishu.webhook
	err := k.Load(env.Provider("", ".", func(s string) string {
		return strings.Replace(strings.ToLower(s), "_", ".", -1)
	}), nil)
	if err != nil {
		log.Fatalf("无法加载配置: %v", err)
	}

	if err := k.Unmarshal("", &C); err != nil {
		log.Fatalf("解析配置失败: %v", err)
	}
}
