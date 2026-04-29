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

程序通过环境变量加载配置，建议复制 `.env.example` 为 `.env` 进行配置：

| 环境变量 | 说明 | 示例 |
| :--- | :--- | :--- |
| `FEISHU_WEBHOOK` | 飞书自定义机器人 Webhook 地址 | `https://open.feishu.cn/open-apis/bot/v2/hook/...` |
| `FEISHU_SECRET` | 飞书机器人安全校验密钥 | `your_feishu_secret` |
| `GITHUB_KEY` | GitHub Webhook Secret | `your_github_secret` |
| `GITHUB_BOT_USERS` | (可选) 忽略推送的用户列表，逗号分隔 | `bot-user,silent-dev` |
| `FEISHU_APP_ID` | (可选) 飞书应用 App ID | `cli_xxx` |
| `FEISHU_APP_SECRET` | (可选) 飞书应用 App Secret | `xxx` |
| `FEISHU_CHAT_ID` | (可选) 指定接收消息的群组 ID | `oc_xxx` |
| `DATABASE_URL` | (可选) PostgreSQL 连接串，首次启动自动建表，升级自动补齐新字段 | `postgres://user:pass@host/db` |
| `EVENTS_MERGE_WINDOW` | (可选) 同类事件合并窗口（分钟），默认 10 | `10` |
| `GITHUB_WEBHOOK_IPS` | (可选) GitHub Webhook 来源 IP 白名单，CIDR 格式，逗号分隔 | `192.30.252.0/22,185.199.108.0/22` |

> 配置后，机器人将支持：
>
> 1. **消息合并**：同类事件在配置的时间窗口内（默认 10 分钟）自动合并为一条消息，避免频繁刷屏。
> 2. **状态更新**：GitHub Actions / Check Suite 的进度会实时更新在同一条消息中，而不是重复发送。
> 3. **关联回复**：评论（Issue/PR）将以话题模式回复到对应的推送消息下。
> 4. **IP 白名单**：可限制仅接受来自 GitHub 官方 IP 的 Webhook 请求，提升安全性。

### 2. 本地运行

```bash
# 1. 复制配置
cp .env.example .env
# 2. 获取依赖
go mod tidy
# 3. 运行项目
go run main.go
```

### 3. Docker 部署

```bash
docker build -t feishu-git-push-bot .

docker run -d -p 8080:8080 \
  --env-file .env \
  feishu-git-push-bot
```

## ⚓ Webhook 配置

在 GitHub 仓库或组织的 `Settings -> Webhooks` 中添加：

- **Payload URL**: `https://<你的域名>/github/webhook`
- **Content type**: `application/json`
- **Secret**: 设置为你的 `GITHUB_KEY`
- **Events**: 选择 `Let me select individual events`，建议勾选以下项以获得最佳体验：
  - **核心开发**: `Pushes`, `Pull requests`, `Issues`, `Releases`
  - **CI/CD 监控**: `Workflow runs`, `Workflow jobs` (必须开启以支持状态实时更新)
  - **互动交流**: `Issue comments`, `Pull request reviews`, `Pull request review comments`
  - **社交反馈**: `Stars`, `Forks`, `Watches` (可选)
  
> [!IMPORTANT]
> **注意**: 请勿勾选 `Branch or tag creation` 和 `Branch or tag deletion` 事件，这些信息已包含在 `Push` 事件中，重复勾选会导致冗余且无内容的通知。

## 📂 项目结构

```text
.
├── bot/                # 核心逻辑
│   ├── config.go       # 配置解析（环境变量 / .env）
│   ├── db.go           # 数据库持久化 (Bun ORM + PostgreSQL)
│   ├── feishu.go       # 飞书 API 交互、卡片 V2 构建与消息发送
│   ├── handler.go      # GitHub Webhook 入口与幂等性校验
│   ├── template.go     # 事件解析与消息模板渲染
│   ├── router.go       # Gin 路由定义与 IP 白名单中间件
│   └── worker.go       # 异步事件处理、消息合并与图片缓存
├── main.go             # 入口文件
├── .env.example        # 配置示例
└── Dockerfile          # 多阶段构建，Chainguard 静态镜像
```

## 🧪 测试

你可以使用内置的测试脚本模拟 GitHub Webhook 事件（需先配置飞书凭证）：

```bash
go test ./bot -v -run TestSendAllMessages
```

## ⚙️ 高级配置

### 事件合并窗口

通过 `EVENTS_MERGE_WINDOW` 环境变量配置同类事件的合并时间窗口（单位：分钟，默认 10）。在窗口内的连续同类事件会被合并为一条消息，减少通知噪音。

受影响的事件类型：
- **分支推送**：同一分支的连续提交合并显示
- **标签创建/删除**：同一仓库的批量标签操作合并显示
- **Release / CI 事件**：始终更新同一条消息（不受窗口限制）

### IP 白名单

通过 `GITHUB_WEBHOOK_IPS` 配置允许的来源 IP（CIDR 格式，逗号分隔），防止伪造请求。支持：
- CIDR 格式：`192.30.252.0/22`
- 单个 IP：`140.82.112.1`（自动补全为 `/32`）
- IPv6 地址

> 💡 GitHub 官方 IP 列表可从 `https://api.github.com/meta` 的 `hooks` 字段获取。

## 📜 许可证

[MIT License](LICENSE)
