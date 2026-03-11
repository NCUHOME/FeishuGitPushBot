package bot

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/pgdialect"
	"github.com/uptrace/bun/driver/pgdriver"
)

var DB *bun.DB

// MessageRecord 消息记录表，用于追踪 GitHub 事件与飞书消息的对应关系
type MessageRecord struct {
	bun.BaseModel `bun:"table:message_records,alias:mr"`

	ID              uint64    `bun:",pk,autoincrement"`
	GithubID        string    `bun:",unique,notnull"` // 可能是 workflow_run_id、分支引用或 commit SHA
	FeishuMessageID string    `bun:",notnull"`
	ChatID          string    `bun:",notnull"`
	RepoName        string    `bun:",notnull"`
	Ref             string    `bun:""`
	EventType       string    `bun:",notnull"`
	Content         string    `bun:"type:text"` // 存储卡片详情的 JSON
	CreatedAt       time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt       time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	DeletedAt       time.Time `bun:",soft_delete,nullzero"`

	// 关联原始事件 (新增)
	EventID         uint64    `bun:",notnull"`

	// 新增：图片状态，用于后台刷新
	ImageStatus     string    `bun:",default:'done'"` // done, pending
	AvatarURL       string    `bun:""`                // 原始头像 URL
}

// WebhookEvent 存储所有来自 GitHub 的原始请求，持久化保存
type WebhookEvent struct {
	bun.BaseModel `bun:"table:webhook_events,alias:we"`

	ID         uint64    `bun:",pk,autoincrement"`
	EventType  string    `bun:",notnull"`
	Payload    string    `bun:"type:text"`          // 原始 Webhook 负载
	Status     string    `bun:",default:'pending'"` // pending, processed, failed
	RetryCount int       `bun:",default:0"`
	CreatedAt  time.Time `bun:",nullzero,notnull,default:current_timestamp"`
	UpdatedAt  time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}

// ImageCache 图片缓存表，加速头像显示
type ImageCache struct {
	bun.BaseModel `bun:"table:image_caches,alias:ic"`

	URL       string    `bun:",pk"`
	ImgKey    string    `bun:",notnull"`
    Hash      string    `bun:",nullzero"` // 图片内容的哈希值 (MD5)
	UpdatedAt time.Time `bun:",nullzero,notnull,default:current_timestamp"`
}

// InitDB 初始化数据库连接并执行自动迁移
func InitDB() {
	if C.Database.URL == "" {
		log.Println("跳过数据库初始化: DATABASE_URL 未设置")
		return
	}

	sqldb := sql.OpenDB(pgdriver.NewConnector(pgdriver.WithDSN(C.Database.URL)))
	db := bun.NewDB(sqldb, pgdialect.New())

	// 自动迁移
	ctx := context.Background()
	_, err := db.NewCreateTable().Model((*MessageRecord)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		log.Printf("数据库迁移失败 (跳过数据库功能): %v", err)
		return
	}

	_, err = db.NewCreateTable().Model((*ImageCache)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		log.Printf("数据库图片缓存迁移失败 (跳过图片缓存): %v", err)
		return
	}

	_, err = db.NewCreateTable().Model((*WebhookEvent)(nil)).IfNotExists().Exec(ctx)
	if err != nil {
		log.Printf("数据库原始事件表迁移失败: %v", err)
		return
	}

	DB = db
	log.Println("数据库初始化成功")
}
