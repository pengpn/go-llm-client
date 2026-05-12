# ── Stage 1: Builder ─────────────────────────────────────────────────────────
# 使用完整 Go 工具链编译；此镜像不会进入最终产物
FROM golang:1.25-alpine AS builder

# git 供某些 go mod 通过 VCS 下载；build-base 提供 gcc（CGO=0 其实用不到，但有些 CI 环境会用）
RUN apk add --no-cache git

WORKDIR /app

# 先复制依赖声明，利用 Docker 层缓存：
# 只有 go.mod/go.sum 变化时才重新 go mod download
COPY go.mod go.sum ./
RUN go mod download

# 复制源码（.dockerignore 排除了 .env、.git 等不需要的内容）
COPY . .

# CGO_ENABLED=0：纯静态二进制，不依赖 glibc，可在 Alpine 运行
# -ldflags="-s -w"：去掉 DWARF 调试符号和符号表，减小体积约 30%
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /customer_service \
    ./examples/customer_service/

# ── Stage 2: Runtime ─────────────────────────────────────────────────────────
# Alpine 约 10MB，包含 shell 和包管理器，方便调试
# 生产要求极致安全时可换成 scratch（连 shell 都没有）
FROM alpine:3.19

# ca-certificates：调用 OpenAI/通义等 HTTPS API 需要根证书
# wget：HEALTHCHECK 用
# tzdata：时区支持，日志时间戳正确
RUN apk add --no-cache ca-certificates wget tzdata

WORKDIR /app

# 从 builder 只复制二进制和配置文件，不携带 Go 工具链
COPY --from=builder /customer_service .
COPY config.yaml .

# 以非 root 用户运行：减少容器逃逸后的影响半径
# -D：不创建 home 目录，-u 1000：指定 UID
RUN adduser -D -u 1000 appuser
USER appuser

EXPOSE 8080

# 健康检查：start-period 给 Qdrant 索引预留时间，之后每 15s 检查 /health
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/customer_service"]
