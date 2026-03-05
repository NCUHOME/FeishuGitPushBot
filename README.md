# FeishuGitPushBot

一个轻量级、现代化的飞书 GitHub Webhook 通知机器人。它将 GitHub 事件转换为精细化、高可读性的飞书卡片消息，并支持自动化部署。

## 🌟 特性

- **现代化卡片布局**：采用三列布局展示仓库、分支/引用及触发者，信息高度聚合。
- **智能視覺反馈**：
  - 交替 Emoji (🔹/🔸) 展示提交记录。
  - 根据事件类型自动切换卡片配色（成功为绿，失败为红，推送为蓝）。
- **极简架构**：代码经过深度重构，结构扁平，易于维护。
- **安全与性能**：
  - 支持 GitHub Webhook 签名校验。
  - 使用 `cgr.dev/chainguard/static` 基础镜像，安全且体积极小（Docker 镜像约 10MB）。
- **全中文化**：所有通知及内置逻辑均已适配中文语境。

## 🛠️ 快速开始

### 1. 环境变量配置

程序通过环境变量加载配置：

| 环境变量 | 说明 | 示例 |
| :--- | :--- | :--- |
| `FEISHU_WEBHOOK` | 飞书机器人 Webhook 地址 | `https://open.feishu.cn/open-apis/bot/v2/hook/...` |
| `FEISHU_SECRET` | 飞书机器人安全校验密钥 | `your_feishu_secret` |
| `GITHUB_KEY` | GitHub Webhook Secret | `your_github_secret` |

### 2. 本地运行

```bash
# 1. 复制 .env 示例并配置 (如果有) 或直接设置环境变量
# 2. 获取依赖
go mod tidy
# 3. 运行项目
go run main.go
```

### 3. Docker 部署

使用我们优化过的多阶段构建 Dockerfile：

```bash
docker build -t feishu-git-push-bot .

docker run -d -p 8080:8080 \
  -e FEISHU_WEBHOOK="xxx" \
  -e FEISHU_SECRET="xxx" \
  -e GITHUB_KEY="xxx" \
  feishu-git-push-bot
```

## ⚓ Webhook 配置

在 GitHub 仓库或组织的 `Settings -> Webhooks` 中添加：

- **Payload URL**: `https://<你的域名>/github/webhook`
- **Content type**: `application/json`
- **Secret**: 设置为你的 `GITHUB_KEY`
- **Events**: 建议勾选 `Push`, `Pull Request`, `Issues`, `Workflow runs`, `Releases` 等。

## 📂 项目结构

```text
.
├── bot/                # 核心逻辑
│   ├── config.go       # 配置解析
│   ├── feishu.go       # 飞书 API 交互与卡片 DSL
│   ├── handler.go      # GitHub Webhook 解析与处理
│   └── router.go       # Gin 路由定义
├── main.go             # 入口文件
└── Dockerfile          # 安全精简的容器配置
```

## 📜 许可证

[MIT License](LICENSE)