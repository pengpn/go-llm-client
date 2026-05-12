# Lesson 09：容器化部署（Docker + docker-compose）

## 为什么要容器化？

没有容器的部署流程：
```
1. 在服务器上装 Go 1.25
2. 安装 Qdrant（版本要对）
3. 配置 10 个环境变量
4. go build，复制到正确位置
5. 配置 systemd/supervisor
```
换一台服务器全部重来。容器把**应用 + 运行环境**打包成一个镜像，一次构建，到处运行。

---

## 核心概念

### 镜像分层（Layer Caching）

每个 `RUN`/`COPY` 指令产生一个只读层。层不变则缓存命中，不重新执行。

```
Layer 4: COPY . .             ← 代码层（频繁变动，放最后）
Layer 3: RUN go mod download  ← 依赖层（变动少，缓存命中率高）
Layer 2: COPY go.mod go.sum   ← 只在依赖变化时才 miss
Layer 1: FROM golang:alpine   ← 基础层（固定）
```

**最佳实践：变动频率低的层放前面**，代码层放最后。

### 多阶段构建（Multi-stage Build）

```
Stage 1 (builder): golang:1.25-alpine (~350MB)
   ↓ go build 产出 ~20MB 静态二进制
Stage 2 (runtime): alpine:3.19 (~10MB)
   ↓ 只复制二进制，不带 Go 工具链
最终镜像: ~12MB（体积减少 96%，攻击面大幅缩小）
```

### CGO_ENABLED=0

```bash
CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o app ./cmd/server/
```

- `CGO_ENABLED=0`：纯静态二进制，不依赖 glibc，可在 Alpine/scratch 运行
- `-ldflags="-s -w"`：去掉 DWARF 调试符号和符号表，减小体积约 30%

### docker-compose 的 healthcheck

```yaml
depends_on:
  qdrant:
    condition: service_healthy  # 等健康检查通过，不只是容器启动
```

只用 `depends_on: [qdrant]` 只等容器进程启动，不等服务就绪——Qdrant REST API 尚未 ready 就被 app 连接，会报连接失败。

---

## 文件结构

```
Dockerfile           — 多阶段构建
.dockerignore        — 排除 .env、.git 等不必要文件
docker-compose.yml   — 编排 app + Qdrant
.env.example         — 新增 QDRANT_URL、JWT_SECRET 等字段
```

---

## Dockerfile 讲解

```dockerfile
# Stage 1: 编译阶段
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app

# 先复制依赖声明，利用层缓存
COPY go.mod go.sum ./
RUN go mod download

# 再复制源码编译
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o /customer_service \
    ./examples/customer_service/

# Stage 2: 运行阶段（只包含二进制 + 必要系统库）
FROM alpine:3.19
RUN apk add --no-cache ca-certificates wget tzdata

WORKDIR /app
COPY --from=builder /customer_service .
COPY config.yaml .

# 非 root 用户运行（安全最佳实践）
RUN adduser -D -u 1000 appuser
USER appuser

EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/customer_service"]
```

**为什么用 Alpine 而非 scratch？**
- `scratch`（空镜像）最小，但没有 CA 根证书 → 调用 OpenAI HTTPS 会 SSL 失败
- Alpine 有 `apk` 包管理器，调试时可以进容器 `apk add` 工具
- `ca-certificates` + `wget` + `tzdata` 三个包必不可少

---

## docker-compose.yml 讲解

```yaml
services:
  qdrant:
    image: qdrant/qdrant:v1.9.0   # 锁定版本，避免意外升级
    volumes:
      - qdrant_data:/qdrant/storage  # 持久化向量数据
    healthcheck:
      test: ["CMD", "wget", "-qO-", "http://localhost:6333/readyz"]

  app:
    build: .
    env_file: .env                 # 从文件注入密钥
    environment:
      QDRANT_URL: http://qdrant:6333  # 服务名替代 localhost
    depends_on:
      qdrant:
        condition: service_healthy  # 等 Qdrant 就绪

volumes:
  qdrant_data:  # 具名卷，容器重启后数据不丢失
```

**关键：容器内 `localhost` 是自身，不是 Qdrant。**
Docker Compose 为每个服务创建 DNS 条目，服务名就是主机名：`qdrant` → Qdrant 容器 IP。

---

## 使用方法

```bash
# 构建镜像（首次较慢，之后利用缓存）
docker build -t go-llm-agent:dev .

# 查看镜像大小
docker images go-llm-agent:dev

# 一键启动（需要 .env 文件配置好 API Key）
cp .env.example .env
# 编辑 .env，填入 OPENAI_API_KEY 等
docker compose up

# 后台运行
docker compose up -d

# 查看日志
docker compose logs -f app

# 停止并保留数据
docker compose stop

# 停止并删除容器（数据卷保留）
docker compose down

# 停止并删除一切（包括数据卷）
docker compose down -v
```

---

## 代码修改：Qdrant URL 环境变量化

原来的硬编码：
```go
store, err := rag.NewQdrantStore(ctx, "http://localhost:6333", ...)
```

修改后（12-Factor App 原则：配置来自环境变量）：
```go
qdrantURL := os.Getenv("QDRANT_URL")
if qdrantURL == "" {
    qdrantURL = "http://localhost:6333"  // 本地开发默认值
}
store, err := rag.NewQdrantStore(ctx, qdrantURL, ...)
```

本地开发不需要改任何东西；Docker Compose 通过 `environment: QDRANT_URL: http://qdrant:6333` 自动覆盖。

---

## 课后作业（已完成）

### 作业1：多阶段 Dockerfile + 镜像大小对比

**实现：** `Dockerfile` — 两阶段（builder + runtime）

**验证：**
```bash
docker build -t go-llm-agent:dev .
docker images go-llm-agent:dev
# 结果：约 12MB（对比单阶段的 ~800MB）
```

**对比实验（单阶段）：**
```dockerfile
FROM golang:1.25-alpine
WORKDIR /app
COPY . .
RUN go build -o app ./examples/customer_service/
ENTRYPOINT ["./app"]
# 镜像大小：~800MB（包含整个 Go 工具链）
```

### 作业2：docker-compose.yml 一键启动

**实现：** `docker-compose.yml`

**验证流程：**
1. `cp .env.example .env && vim .env`（填入真实 API Key）
2. `docker compose up`
3. Qdrant 健康检查通过后 app 自动启动
4. `curl http://localhost:8080/health` → `{"status":"ok"}`
5. `curl -X POST http://localhost:8080/chat -d '{"user_id":"u1","message":"退款要多久？"}'`

### 作业3：安全加固（非 root 用户）

**实现：** Dockerfile 中已加入：
```dockerfile
RUN adduser -D -u 1000 appuser
USER appuser
```

**为什么重要？**
容器默认以 root 运行。一旦应用被攻破，攻击者在容器内有 root 权限，容器逃逸后破坏力更大。非 root 用户限制了攻击半径。

**进阶：只读文件系统（docker-compose.yml）：**
```yaml
app:
  security_opt:
    - no-new-privileges:true   # 禁止提权
  read_only: true              # 只读文件系统
  tmpfs:
    - /tmp                     # 临时文件走内存
```
