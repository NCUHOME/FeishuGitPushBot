package bot

import (
	"log"
	"strings"

	"github.com/joho/godotenv"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	Feishu struct {
		AppID     string `koanf:"app_id"`
		AppSecret string `koanf:"app_secret"`
		Webhook   string `koanf:"webhook"`
		Secret    string `koanf:"secret"`
		ChatID    string `koanf:"chat_id"`
	} `koanf:"feishu"`
	Github struct {
		Key string `koanf:"github_key"`
	} `koanf:"github"`
	Database struct {
		URL string `koanf:"url"`
	} `koanf:"database"`
}

var C Config

// LoadConfig 从环境变量解析配置，优先从 .env 文件加载
func LoadConfig() {
	// 尝试加载 .env 文件 (支持本地运行和测试模式)
	_ = godotenv.Load()
	_ = godotenv.Load("../.env")

	k := koanf.New(".")

	err := k.Load(env.Provider("", ".", func(s string) string {
		s = strings.ToLower(s)
		// 统一处理下划线到点号的映射
		if strings.HasPrefix(s, "feishu_") {
			return "feishu." + strings.TrimPrefix(s, "feishu_")
		}
		if s == "github_key" {
			return "github.github_key"
		}
		if s == "database_url" {
			return "database.url"
		}
		return s
	}), nil)
	if err != nil {
		log.Fatalf("无法加载配置: %v", err)
	}

	if err := k.Unmarshal("", &C); err != nil {
		log.Fatalf("解析配置失败: %v", err)
	}

	// 打印关键配置状态
	if C.Feishu.Webhook == "" && (C.Feishu.AppID == "" || C.Feishu.AppSecret == "") {
		log.Println("警告: Webhook 和 AppID/AppSecret 均未设置，消息发送可能受限")
	}
	if C.Database.URL == "" {
		log.Println("警告: DATABASE_URL 未设置，将无法保存消息记录以便后续更新或回复")
	}
}
