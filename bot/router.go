package bot

import (
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

var (
	allowedCIDRs []*net.IPNet
	cidrOnce     sync.Once
)

// InitRouter 初始化 Gin 路由
func InitRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 基础中间件
	r.Use(cors.Default())

	// Webhook 路由（带 IP 白名单校验）
	r.POST("/github/webhook", ipWhitelistMiddleware(), GithubHandler)

	return r
}

// ipWhitelistMiddleware 返回一个 Gin 中间件，校验请求来源 IP 是否在白名单中。
// 若 GITHUB_WEBHOOK_IPS 未配置则放行所有请求。
func ipWhitelistMiddleware() gin.HandlerFunc {
	// 启动时解析一次 CIDR 列表
	cidrOnce.Do(func() {
		if C.Security.AllowedIPs == "" {
			return
		}
		for _, s := range strings.Split(C.Security.AllowedIPs, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			// 支持单个 IP（自动补 /32 或 /128）
			if !strings.Contains(s, "/") {
				if net.ParseIP(s).To4() != nil {
					s += "/32"
				} else {
					s += "/128"
				}
			}
			_, cidr, err := net.ParseCIDR(s)
			if err != nil {
				slog.Error("Invalid CIDR in GITHUB_WEBHOOK_IPS, skipping", "cidr", s, "error", err)
				continue
			}
			allowedCIDRs = append(allowedCIDRs, cidr)
		}
		if len(allowedCIDRs) == 0 {
			slog.Warn("GITHUB_WEBHOOK_IPS is set but no valid CIDRs parsed, all requests will be rejected")
		}
	})

	return func(c *gin.Context) {
		// 未配置白名单 → 放行
		if len(allowedCIDRs) == 0 && C.Security.AllowedIPs == "" {
			c.Next()
			return
		}

		clientIP := extractClientIP(c)
		if clientIP == "" {
			slog.Warn("Unable to determine client IP, rejecting request")
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "msg": "forbidden: unable to determine client IP"})
			return
		}

		ip := net.ParseIP(clientIP)
		if ip == nil {
			slog.Warn("Invalid client IP, rejecting", "ip", clientIP)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "msg": "forbidden"})
			return
		}

		for _, cidr := range allowedCIDRs {
			if cidr.Contains(ip) {
				c.Next()
				return
			}
		}

		slog.Warn("Webhook request rejected by IP whitelist", "ip", clientIP)
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "msg": "forbidden: ip not allowed"})
	}
}

// extractClientIP 从请求中提取客户端 IP。
// 优先级：X-Forwarded-For（取第一个）> X-Real-IP > RemoteAddr
func extractClientIP(c *gin.Context) string {
	// X-Forwarded-For 可能包含多个 IP，第一个是原始客户端
	if xff := c.GetHeader("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	if xri := c.GetHeader("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// RemoteAddr 格式为 "IP:port" 或 "IP"
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}
	return host
}
