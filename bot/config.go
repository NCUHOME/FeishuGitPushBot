package bot

import (
	"log"
	"strings"

	"github.com/knadh/koanf/parsers/dotenv"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// Config 结构体定义了所有配置项
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

// LoadConfig 从 .env 文件和环境变量加载配置
func LoadConfig() {
	k := koanf.New(".")

	// 1. 尝试从当前目录或上级目录加载 .env 文件
	_ = k.Load(file.Provider(".env"), dotenv.Parser())
	_ = k.Load(file.Provider("../.env"), dotenv.Parser())

	// 2. 加载环境变量，映射到配置结构体
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
		log.Fatalf("failed to load environment variables: %v", err)
	}

	// 将配置解析到全局变量 C
	if err := k.Unmarshal("", &C); err != nil {
		log.Fatalf("failed to unmarshal configuration: %v", err)
	}

	// 打印关键配置状态
	if C.Feishu.Webhook == "" && (C.Feishu.AppID == "" || C.Feishu.AppSecret == "") {
		log.Println("Warning: Both Webhook and AppID/AppSecret are not set, message sending might be limited")
	}
	if C.Database.URL == "" {
		log.Println("Warning: DATABASE_URL is not set, message records will not be saved for updates or replies")
	}
}
