package main

import (
	"log/slog"
	"os"

	"github.com/ncuhome/FeishuGitPushBot/bot"
)

func main() {
	// 初始化日志，生产环境推荐 JSON 格式
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, opts)))

	// 加载配置
	bot.LoadConfig()
	bot.InitDB()
	slog.Info("系统启动中...")

	// 启动路由
	r := bot.InitRouter()
	if err := r.Run(":8080"); err != nil {
		slog.Error("服务运行失败", "error", err)
		os.Exit(1)
	}
}
