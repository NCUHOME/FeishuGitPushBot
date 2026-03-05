# --- 阶段 1: 下载依赖 ---
FROM golang:alpine AS deps
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

# --- 阶段 2: 编译阶段 ---
FROM golang:alpine AS builder
WORKDIR /app

# 1. 安装 tzdata 并手动创建 timezone 文件
RUN apk add --no-cache tzdata && \
    echo "Asia/Shanghai" > /etc/timezone

COPY --from=deps /go/pkg /go/pkg
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -trimpath \
    -o /app/main .

# --- 阶段 3: 运行阶段 ---
FROM cgr.dev/chainguard/static:latest

# 设置时区环境变量 (Go 运行时会自动识别)
ENV TZ=Asia/Shanghai

WORKDIR /app

# 1. 复制二进制文件
COPY --from=builder /app/main /app/main

# 2. 复制时区数据 (只需这一个文件即可满足绝大多数需求)
COPY --from=builder /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
# 如果你的程序确实需要 /etc/timezone，现在它可以被找到了
COPY --from=builder /etc/timezone /etc/timezone

EXPOSE 8080

ENTRYPOINT ["/app/main"]