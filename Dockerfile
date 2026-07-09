# 构建阶段
FROM golang:1.25-alpine AS builder

# 安装构建依赖
RUN apk add --no-cache gcc musl-dev git

# 设置工作目录
WORKDIR /app

# 复制依赖文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 构建
RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o star-meet-server .

# 运行阶段
FROM alpine:latest

# 安装运行时依赖
RUN apk add --no-cache ca-certificates tzdata

# 设置工作目录
WORKDIR /app

# 复制构建产物
COPY --from=builder /app/star-meet-server .
COPY --from=builder /app/config.example.yaml ./config.yaml
COPY --from=builder /app/admin ./admin
COPY --from=builder /app/static ./static

# 创建非 root 用户
RUN adduser -D -u 1000 appuser && \
    mkdir -p /app/logs && \
    chown -R appuser:appuser /app
USER appuser

# 暴露端口
EXPOSE 8080

# 启动命令
CMD ["./star-meet-server", "-host", "0.0.0.0"]
