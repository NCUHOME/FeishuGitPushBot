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
		Key string `koanf:"github_key"`
	} `koanf:"github"`
}

var C Config

// LoadConfig 从环境变量解析配置
func LoadConfig() {
	k := koanf.New(".")

	// 先尝试直接读取环境变量映射
	// 比如 FEISHU_WEBHOOK -> feishu.webhook
	// 比如 GITHUB_WEBHOOK_KEY -> github.webhook_key (需要特殊处理第一个下划线)
	err := k.Load(env.Provider("", ".", func(s string) string {
		s = strings.ToLower(s)
		if strings.HasPrefix(s, "feishu_") {
			return "feishu." + strings.TrimPrefix(s, "feishu_")
		}
		if s == "github_key" {
			return "github.github_key"
		}
		return s
	}), nil)
	if err != nil {
		log.Fatalf("无法加载配置: %v", err)
	}

	if err := k.Unmarshal("", &C); err != nil {
		log.Fatalf("解析配置失败: %v", err)
	}

	// 打印关键配置状态（不打印具体值）
	if C.Feishu.Webhook == "" {
		log.Println("警告: FEISHU_WEBHOOK 未设置")
	}
	if C.Github.Key == "" {
		log.Println("警告: GITHUB_KEY 未设置，Webhook 签名验证将失败")
	}
}
