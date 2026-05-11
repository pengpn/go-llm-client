# Lesson 09：容器化部署（Docker + docker-compose）

## 为什么要学

"在我电脑上能跑"不等于能部署。容器化解决环境一致性问题：
开发、测试、生产跑同一个镜像，一条命令启动完整环境（含 Qdrant）。

## 学习目标

- 写出生产可用的多阶段 Dockerfile（减小镜像体积）
- 用 docker-compose 编排服务 + Qdrant
- 理解健康检查、依赖顺序、环境变量注入
- 了解基本的云部署方式（Railway / Render / 阿里云 ECS）

## 计划内容

### 原理

**多阶段构建**（镜像从 ~800MB 压到 ~20MB）：
```dockerfile
# 阶段1：编译
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o customer_service ./examples/customer_service/

# 阶段2：只保留二进制
FROM alpine:latest
COPY --from=builder /app/customer_service .
COPY config.yaml .
CMD ["./customer_service"]
```

**docker-compose 编排**：
```yaml
services:
  qdrant:
    image: qdrant/qdrant
    ports: ["6333:6333"]
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:6333/health"]

  app:
    build: .
    ports: ["8080:8080"]
    depends_on:
      qdrant:
        condition: service_healthy  # 等 Qdrant 健康后再启动
    env_file: .env
```

### 要实现的东西
- `Dockerfile` — 多阶段构建
- `docker-compose.yml` — 编排 app + Qdrant
- `.dockerignore` — 排除 .env、data/ 等敏感/无用文件

### 课后作业（预计）
- 作业1：写 Dockerfile，`docker build` 成功
- 作业2：写 docker-compose.yml，`docker compose up` 一键启动
- 作业3：部署到一个云平台（Railway 免费额度够用）
