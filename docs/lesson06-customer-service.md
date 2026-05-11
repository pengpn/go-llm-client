# Lesson 06：完整客服系统 + 生产部署

## 学习目标

- 理解生产环境与 CLI Demo 的核心差距
- 掌握 Gin HTTP 服务器的核心模式（中间件、路由、Context）
- 实现优雅关闭：不丢失进行中的请求
- 理解多用户并发下的共享 vs 隔离设计
- 掌握热更新知识库（不重启服务）
- 学会用 slog 输出结构化日志

---

## 核心概念

### 生产环境 vs CLI Demo 的差距

| 问题 | CLI Demo | 生产要求 |
|------|----------|----------|
| 接入方式 | 命令行交互 | HTTP API，前端/APP 可调用 |
| 并发用户 | 单用户 | 成百上千用户同时请求 |
| 共享资源 | 无 | LLM Client、RAG 索引在所有用户间共享 |
| 可观测性 | fmt.Println | 结构化日志，知道每次请求耗时/错误 |
| 停机方式 | Ctrl+C 强杀 | 优雅关闭，处理完进行中的请求再退出 |
| 知识库更新 | 重启服务 | 热更新，不中断在线用户 |

### 整体架构

```
HTTP 请求
    ↓
Gin Router（Logger 中间件 → Recovery 中间件）
    ↓
Handler（每个请求一个 goroutine）
    ↓
Session Manager（按 user_id 隔离，TTL 2 小时自动清理）
    ↓
Agent（LLM Client + Registry，所有用户共享）
    └── search_knowledge_base → RAG Retriever（共享）
```

---

## 关键设计决策

### 1. 共享 vs 隔离

**共享（所有用户共用同一份）：**
- `client.Client` — 无状态，每次请求独立，天然并发安全
- `agent.Registry` — 注册后只读，无需加锁
- `rag.Retriever` / `rag.Pipeline` — 无状态，并发安全

**隔离（每个用户独立）：**
- `session.Session` — 每个 user_id 有自己的对话历史，内部 RWMutex 保护

```go
// Session Manager 自动按 user_id 隔离
sess := s.sessions.GetOrCreate(req.UserID)
```

### 2. Gin 中间件：洋葱模型

```
请求 → Logger(before) → Recovery(before) → Handler → Recovery(after) → Logger(after) → 响应
```

`c.Next()` 往里走，返回后往外走。Logger 在 Handler 执行完成后才能拿到状态码和耗时：

```go
func Logger() gin.HandlerFunc {
    return func(c *gin.Context) {
        start := time.Now()
        c.Next()                          // 执行 Handler
        slog.Info("request",
            "status",   c.Writer.Status(), // Handler 执行后才有
            "duration", time.Since(start),
        )
    }
}
```

`gin.Recovery()` 捕获 Handler 里的 panic，返回 500 而不是让整个进程崩掉。

### 3. 优雅关闭

直接 Ctrl+C 强杀进程，进行中的 LLM 请求（可能耗时数秒）会被强制中断，用户拿到空响应。

优雅关闭流程：

```
收到 SIGINT/SIGTERM
    ↓
停止接受新请求（端口关闭）
    ↓
等待进行中的请求处理完（最多 30 秒）
    ↓
停止 Session TTL 清理协程
    ↓
进程退出
```

```go
// signal.NotifyContext 监听系统信号
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer stop()
<-ctx.Done() // 阻塞，直到收到信号

// 给进行中请求最多 30 秒完成
shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
srv.Shutdown(shutdownCtx)
```

### 4. 热更新知识库

传统做法：更新 FAQ → 重启服务（所有用户对话中断）

热更新做法：`POST /reload` → 重新索引 FAQ → Qdrant upsert 覆盖旧数据 → 无需重启

之所以能安全热更新：
- Qdrant 内部处理并发读写
- 相同内容 → 相同 ID → upsert 覆盖（幂等），不产生脏数据
- 更新期间的检索请求读到旧数据或新数据都能正常工作

### 5. 结构化日志（slog）

`fmt.Println` 的问题：日志格式不统一，难以被 ELK/Grafana Loki 等平台解析。

`slog` JSON 格式：每条日志都是可解析的 JSON，方便告警和查询：

```json
{"time":"2024-01-01T10:00:00Z","level":"INFO","msg":"request","method":"POST","path":"/chat","status":200,"duration":"1.2s","user_id":"u001"}
```

---

## API 接口

### POST /chat — 对话

```bash
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{"user_id": "u001", "message": "退款要多久？"}'
```

```json
// 响应
{"user_id": "u001", "answer": "退款通常需要3-5个工作日..."}
```

### POST /reload — 热更新知识库

```bash
curl -X POST http://localhost:8080/reload
```

```json
{"status": "ok", "indexed": 6}
```

### GET /health — 健康检查

```bash
curl http://localhost:8080/health
```

```json
{"status": "ok", "uptime": "5m30s", "sessions": 12}
```

---

## 构建的模块

```
server/
├── server.go      — Server 结构体、路由注册、优雅关闭
├── handler.go     — handleChat / handleReload / handleHealth
└── middleware.go  — 结构化日志中间件（slog JSON）

examples/customer_service/
└── main.go        — 整合所有组件的启动入口
```

---

## 核心设计思想总结

| 设计点 | 实现方式 | 原因 |
|--------|----------|------|
| 并发安全 | 共享无状态资源，Session 用 RWMutex | 无状态天然并发安全，有状态才需要加锁 |
| 优雅关闭 | signal.NotifyContext + Shutdown(30s) | 不丢失进行中的 LLM 请求 |
| 热更新 | Qdrant upsert 幂等 + /reload 接口 | 知识库更新不中断用户对话 |
| 可观测性 | slog JSON 结构化日志 | 方便接入日志平台，支持告警查询 |
| Panic 防护 | gin.Recovery() | 单个请求 panic 不崩整个服务 |
| Session 隔离 | Manager.GetOrCreate(userID) | 多用户对话历史互不干扰 |

---

## 运行方式

```bash
# 1. 启动 Qdrant
docker start qdrant

# 2. 启动服务（确保 .env 中有 DASHSCOPE_API_KEY）
go run ./examples/customer_service/

# 3. 测试对话
curl -X POST http://localhost:8080/chat \
  -H "Content-Type: application/json" \
  -d '{"user_id": "u001", "message": "退款要多久？"}'

# 4. 热更新知识库
curl -X POST http://localhost:8080/reload

# 5. 健康检查
curl http://localhost:8080/health

# 6. 优雅停止（Ctrl+C，等待进行中请求完成）
```

---

## 课后作业

### 作业1（简单）：为 HTTP 接口编写单元测试

**核心思路**：测试 HTTP Handler 需要解耦依赖，否则会真正调用 LLM 和 Qdrant。  
引入 `AgentRunner` 和 `KBIndexer` 接口，Server 依赖接口而非具体类型（依赖反转）。

```go
// AgentRunner 接口：*agent.Agent 实现它，测试时注入 mockAgent
type AgentRunner interface {
    Run(ctx context.Context, msgs []models.Message, opts ...agent.RunOption) (string, []models.Message, error)
}

// KBIndexer 接口：*rag.Pipeline 实现它，测试时注入 mockIndexer
type KBIndexer interface {
    IndexText(ctx context.Context, text, source string) error
}

// Server 实现 http.Handler 接口，httptest 可以直接用
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    s.engine.ServeHTTP(w, r)
}
```

测试覆盖（`server/server_test.go`）：
- `/health`：返回 status/uptime/sessions 字段
- `/chat`：成功、缺少字段（400）、Agent 报错（500）
- `/reload`：成功、Indexer 报错（500）

### 作业2（中等）：实现 `GET /history/:user_id`

**原理**：Session 的 `Messages()` 是截断后的 LLM 输入；`History()` 是完整历史（不含 system prompt）。

```go
// session/session.go 新增
func (s *Session) History() []models.Message {
    // 返回所有非 system 消息，不截断
}

// handler：session 不存在返回 404，存在则返回消息列表
// 路由：GET /history/:user_id
```

测试覆盖：
- 用户不存在 → 404
- 先 `/chat` 再 `/history` → 返回 user + assistant 消息
- 历史中不含 system prompt

### 作业3（挑战）：添加基于 user_id 的频率限制

**原理**：滑动窗口算法——只统计最近 N 秒内的请求数，超过阈值返回 429。

```
滑动窗口（1分钟）：
时间轴：────────────────────────────►
       [t-60s]    [现在]
               ←窗口→
只统计窗口内的请求时间戳，超出 limit 拒绝请求。
```

实现要点（`server/ratelimit.go`）：
- `userBucket` 持有该用户的时间戳列表（独立加锁，互不影响）
- `allow()` 清理过期时间戳 + 检查数量（不可变：创建新 slice）
- `RateLimiter` 用 `sync.Map` 存储所有用户的 bucket
- 通过 `WithRateLimiter(10, time.Minute)` 挂载到 Server

```bash
# 效果验证：快速发3次请求（limit=2）
curl http://localhost:8080/chat -d '{"user_id":"u1","message":"hi"}'  # 200
curl http://localhost:8080/chat -d '{"user_id":"u1","message":"hi"}'  # 200
curl http://localhost:8080/chat -d '{"user_id":"u1","message":"hi"}'  # 429
```

**已完成：**
- ✅ 作业1：引入 `AgentRunner`/`KBIndexer` 接口 + `ServeHTTP`；`server_test.go` 13个测试全部通过
- ✅ 作业2：`session.History()` + `GET /history/:user_id`；3个测试覆盖404/有消息/无system_prompt
- ✅ 作业3：滑动窗口 `RateLimiter` + `WithRateLimiter` ServerOption；2个单元测试 + 1个集成测试

---

## 六课能力全图

```
Lesson 01  LLM Client（HTTP、重试、并发、流式）
    ↓
Lesson 02  Session 管理（历史、截断、持久化、多用户）
    ↓
Lesson 03  Agent Loop（ReAct、工具注册、并发执行）
    ↓
Lesson 04  工具集成（泛型解码、权限控制、错误策略）
    ↓
Lesson 05  RAG 知识库（Embedding、向量搜索、检索工具）
    ↓
Lesson 06  完整系统（HTTP 服务、优雅关闭、热更新、可观测性）
```
