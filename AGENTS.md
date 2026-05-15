# 教学上下文 — go-llm-agent

## 我是谁
Go 后端工程师，正在系统学习 AI Agent 开发，目标是构建对话助手/客服系统。
了解 RAG、Function Calling 等基础概念，Go 为主，可兼学 Python。

## 教学约定
- 你是我的老师，我是学生，**先讲原理再写代码**
- 每个设计决策说明"为什么这样做"
- 代码要生产可用，不要玩具 Demo
- 每节课结束给出课后作业

## 课程进度

### ✅ Lesson 01：生产级 LLM Client
**已完成内容：**
- `client/client.go` — 核心 Client，支持自动重试（指数退避）、并发控制（信号量）
- `client/stream.go` — SSE 流式输出，channel 传递
- `client/options.go` — 函数式选项模式，多 Provider 预设（OpenAI/智谱/通义/Ollama）
- `client/cost.go` — Token 费用计算器，Mutex 并发安全
- `models/message.go` — Message / Usage / Response 数据结构

**核心设计思想：**
- OpenAI 兼容协议：只换 BaseURL 切换任意 Provider
- 指数退避重试：1s/2s/4s，区分可重试错误（429/5xx）
- 信号量控制并发，防止触发 API Rate Limit

---

### ✅ Lesson 02：多轮对话管理
**已完成内容：**
- `session/session.go` — 单个会话，维护消息历史，自动截断；新增 `AddAgentTurn` 支持工具调用轮次原子写入
- `session/truncate.go` — 截断策略接口 + ByTurns / ByTokenCount（tiktoken 精确计数 + 降级估算）
- `session/persist.go` — JSON 序列化持久化，原子写入，全量历史保存
- `session/manager.go` — 多用户 Session 管理，TTL 过期清理，并发安全
- `examples/chatbot/main.go` — 完整命令行客服 Demo

**核心设计思想：**
- LLM 本身无状态，记忆靠每次请求携带历史
- System Prompt 永远不截断
- Session 与 Client 解耦，一个 Client 服务多个 Session
- double-check 防并发重复创建
- 保存全量历史（未截断），恢复后可切换策略重新计算
- TokenCounter 接口隔离 tiktoken 与业务逻辑，模型不支持时自动降级

---

### ✅ Lesson 03：Agent Loop（ReAct 模式）
**已完成内容：**
- `agent/tool.go` — Tool 定义、ToolFunc 签名、Registry 注册表
- `agent/agent.go` — Agent Loop 核心（Run / RunWithTrace），LLMClient 接口
- `models/tool.go` — ToolCall / ToolDefinition / FunctionDefinition / ToolParameters
- `examples/order_agent/main.go` — 多轮对话订单查询 Agent，Session 持久化工具调用历史

**核心设计思想：**
- Function Calling 协议：LLM 决策，Go 程序执行，Observation 回传循环
- 工具并发执行：WaitGroup + 预分配 slice，结果顺序与输入一致
- LLMClient 接口解耦：便于测试注入 mock，也便于将来替换 Provider
- applyHistoryToSession：把 Agent Run 返回的 history 拆分写入 Session，assistant+tool 原子存储
- MaxIterations 防护 + 工具失败作为 Observation 继续循环，不终止 Agent

**课后作业（已完成）：**
- ✅ 并行工具调用：WaitGroup 并发执行，按 index 写入预分配 slice 保序
- ✅ 工具调用历史持久化：AddAgentTurn 原子写入 + applyHistoryToSession + Session.Save
- ✅ 单元测试：mock LLMClient，覆盖直接回答/工具调用/并行/失败/最大迭代/ctx 取消

---

### ✅ Lesson 04：工具集成（Function Calling 深入）
**计划内容：**
- 工具参数校验与统一解码层（减少每个工具重复写 json.Unmarshal）
- 多工具协作场景（一次回答需要串联多个工具）
- 错误恢复策略（工具失败后 LLM 重试 vs 降级回答）
- 工具权限控制（不同用户可调用的工具集不同）

**已完成内容：**
- `agent/decode.go` — 泛型解码层：`DecodeInput[T]`、`DecodeAndValidate[T]`、`Validator` 接口
- `agent/tool.go` — `NewTypedTool[T]`：类型安全工具创建，工具函数直接接收解码后的结构体
- `agent/permission.go` — `Gate` 接口 + `AllowAll`/`DenyAll`/`RoleGate`，基于角色的工具权限控制
- `agent/agent.go` — `ErrorStrategy`（ContinueOnError/AbortOnError）、`RunOption`（WithUser/WithGate/WithErrorStrategy）、`AgentOption`（WithDefaultGate）

**核心设计思想：**
- 泛型 `DecodeAndValidate[T]` 消除样板代码：工具函数零 JSON 样板，只写业务逻辑
- `Validator` 接口自动触发：实现即校验，不实现即跳过，零侵入
- `Gate` 接口 + `filterDefinitions`：LLM 只看到当前用户被允许的工具子集，从源头防止越权调用
- `ErrorStrategy` 分离两种场景：独立工具 ContinueOnError（LLM 自主决策），链式依赖 AbortOnError（失败即止损）
- `RunOption` 函数选项：不改变 `Run` 基础签名，完全向后兼容

**课后作业（已完成）：**
- ✅ 统一解码层：decode.go + NewTypedTool[T]，order_agent 所有工具迁移完成
- ✅ 权限控制：RoleGate，USER-001 不可访问 cancel_order，ADMIN-001 可访问
- ✅ 单元测试：decode_test.go（7个）、permission_test.go（12个），全部通过
- ✅ 作业1（AbortOnError）：3个测试覆盖单工具失败终止、多工具并发失败、ContinueOnError继续循环+验证Observation内容
- ✅ 作业2（复合Validator）：CancelOrderReq 增加 order_id 格式校验（`^ORDER-\d+$`）和 reason 长度校验（≤100字符）；validator_test.go 8个测试含边界值
- ✅ 作业3（UserGate）：UserGate 实现（按userID白名单，累加Allow，未配置用户拒绝）；7个测试含接口合规编译期验证和filterDefinitions集成测试

---

### ✅ Lesson 05：RAG 知识库接入
**已完成内容：**
- `rag/embedder.go` — `Embedder` 接口 + `QwenEmbedder`（text-embedding-v3，1024维，兼容 OpenAI /embeddings 协议）
- `rag/chunker.go` — `FixedSizeChunker`：固定大小 + 重叠窗口切片，Unicode 安全
- `rag/store.go` — `VectorStore` 接口 + `QdrantStore`（REST API，Cosine 距离，幂等 upsert）
- `rag/retriever.go` — 检索器：问题向量化 → 相似度搜索 → 组装上下文；`EmbedderInterface` 便于 mock 测试
- `rag/pipeline.go` — Indexing Pipeline：切片 → 批量向量化 → 写入；内容哈希 ID 保证幂等
- `examples/rag_agent/` — 完整订单客服 Demo，假数据 FAQ 8 条，Qdrant Docker 本地部署

**核心设计思想：**
- Qdrant 只存向量（数字），Embedding 模型负责文字→向量翻译，两者职责分离
- 批量 Embed：一次 API 调用处理所有 chunk，节省 ~70% 延迟
- 内容哈希 ID：相同内容重复 Index 不产生重复条目（幂等 Indexing）
- 接口隔离：`EmbedderInterface` + `VectorStore` 接口，测试可 mock，Provider 可替换

**课后作业（待完成）：**
- ✅ 作业1（中等）：相似度阈值过滤 — `WithMinScore` 函数式选项，`filterByScore` 过滤低分结果；retriever_test.go 8个测试全部通过
- ✅ 作业2（中等）：RAG Tool 集成 — `rag/tool.go` 的 `NewSearchKBTool` 将 Retriever 包装为 `search_knowledge_base` 工具；`rag_agent/main.go` 改用 Agent Loop，LLM 自主决定何时检索
- ✅ 作业3（挑战）：`QwenEmbedder` 单元测试 — `embedder_test.go` 9个测试；httptest.NewServer mock HTTP，覆盖空输入/乱序响应/API错误/JSON解析失败/server不可达/index越界/请求参数验证

### ✅ Lesson 06：完整客服系统 + 生产部署
**已完成内容：**
- `server/server.go` — `Server` 结构体（持有 `AgentRunner`/`KBIndexer` 接口），Gin 路由注册，优雅关闭，`ServerOption` 函数式选项
- `server/handler.go` — `handleChat`（对话+限流检查）、`handleReload`（热更新知识库）、`handleHealth`（健康检查）、`handleHistory`（对话历史）
- `server/middleware.go` — 结构化日志中间件（log/slog JSON 格式，含 method/path/status/duration/user_id）
- `server/ratelimit.go` — 滑动窗口 `RateLimiter`，基于 user_id 限流，`sync.Map` 并发安全
- `server/server_test.go` — 13 个单元测试，全部通过（mock Agent / mock Indexer）
- `session/session.go` — 新增 `History()` 方法，返回完整对话历史（不含 system prompt，不截断）
- `examples/customer_service/main.go` — 整合所有组件，`WithRateLimiter(10, time.Minute)` 启用限流

**核心设计思想：**
- 接口解耦：`AgentRunner`/`KBIndexer` 接口让 server 包可独立测试，无需真实 LLM 和 Qdrant
- 共享 vs 隔离：LLM Client / RAG Retriever 所有用户共享（无状态）；Session 按 user_id 隔离
- 优雅关闭：SIGINT/SIGTERM → 停止接受新请求 → 等进行中请求完成（30s 超时）→ 停止 Session TTL 清理
- 热更新知识库：`POST /reload` 重新索引 FAQ，Qdrant upsert 幂等，无需重启服务
- 结构化日志：slog JSON 格式，方便接入 ELK/Loki 等日志平台
- 滑动窗口限流：按 user_id 计数（非 IP），同一 NAT 下多用户互不影响

**课后作业（已完成）：**
- ✅ 作业1：引入 `AgentRunner`/`KBIndexer` 接口 + `ServeHTTP`；`server_test.go` 13个测试全部通过
- ✅ 作业2：`session.History()` + `GET /history/:user_id`；3个测试覆盖404/有消息/无system_prompt
- ✅ 作业3：滑动窗口 `RateLimiter` + `WithRateLimiter` ServerOption；2个单元测试 + 1个集成测试

---

### ✅ Lesson 07：流式响应（SSE Streaming）
**已完成内容：**
- `client/stream.go` — 新增 `ChatStreamText()`，包装 `ChatStream` 返回 `<-chan string`（agent 包不依赖 client 包）
- `agent/agent.go` — 新增 `StreamingLLMClient` 接口（可选扩展）+ `RunStream()` + `streamFinalAnswer()`
- `server/server.go` — 新增 `StreamingAgentRunner` 接口 + `POST /chat/stream` 路由
- `server/handler.go` — 新增 `handleChatStream()`；提取 `processChat()` 共用逻辑（解决 body 被消费后降级的问题）
- `server/server_test.go` — 新增 3 个流式测试（用 `httptest.NewServer` 支持 `CloseNotify`）

**核心设计思想：**
- 分阶段流式：工具调用用 `ChatWithTools`（同步快），工具完成后用 `ChatStreamText`（流式慢）
- `StreamingLLMClient` 可选接口：类型断言检查，不支持时降级为非流式，向后兼容
- 后台 goroutine + tokenCh + `c.Stream`：三角并发模型，`tokenCh` 桥接 Agent 和 SSE 推送
- 客户端断连传播：`c.Request.Context()` 取消 → `RunStream` 里的 LLM 请求自动取消 → 节省 token
- `processChat` 共用函数：解决 `/chat/stream` 降级时 body 已被消费的问题

**课后作业（已完成）：**
- ✅ 作业1：实现 `POST /chat/stream`，Gin `c.Stream()` 推送 SSE 事件；3个测试（SSE事件/降级/缺字段）
- ✅ 作业2：客户端断连 → `ctx.Done()` 检测 → `RunStream` context 取消 → LLM 请求终止
- ✅ 作业3：curl 验证：`curl -N http://localhost:8080/chat/stream -d '{...}'`

---

### ✅ Lesson 08：身份认证（API Key / JWT）
**已完成内容：**
- `server/auth.go` — `generateToken`/`parseToken`（HMAC-SHA256），`AuthRequired` Gin 中间件（no-op when nil）
- `server/handler.go` — `handleAuthToken`（API Key → JWT）、`handleRefreshToken`（刷新 Token）
- `server/handler.go` — `processChat`/`handleChatStream` 更新：JWT 优先读取 user_id，防止伪造
- `server/handler.go` — `handleHistory` 增加授权检查：JWT 启用时只能查看自己的历史
- `server/server.go` — 路由分组（公开：`/auth/token`、`/health`；受保护：其余所有路由）
- `examples/customer_service/main.go` — `WithJWTSecret`/`WithAPIKeys` 选项，从 env 读取配置

**核心设计思想：**
- `AuthRequired(nil)` 是 no-op：渐进增强，开发模式不需要改任何代码
- JWT 优先：`c.GetString("user_id")` > `req.UserID`，防止客户端伪造他人 user_id
- 算法混淆攻击防御：明确要求 `*jwt.SigningMethodHMAC`，拒绝 none/RSA
- API Key 不暴露错误原因（"无效 API Key" 统一返回），防止枚举攻击
- Token 过期后不能刷新，必须重新换取（安全边界清晰）

**课后作业（已完成）：**
- ✅ 作业1：`handleAuthToken`（API Key → JWT）；3个测试（成功/无效Key/缺字段）
- ✅ 作业2：`AuthRequired` 中间件 + `processChat` user_id JWT 优先；6个测试（no-op/有效token/无效token/无header/缺userid/History403）
- ✅ 作业3：`handleRefreshToken` + `/auth/refresh` 路由；2个测试（成功/无Token）

---

### ✅ Lesson 09：容器化部署（Docker + docker-compose）
**已完成内容：**
- `Dockerfile` — 多阶段构建（builder golang:1.25-alpine → runtime alpine:3.19）
- `.dockerignore` — 排除 .env、.git、data/、docs/ 等
- `docker-compose.yml` — 编排 app + Qdrant，`depends_on: condition: service_healthy`
- `examples/customer_service/main.go` — Qdrant URL 改为从 `QDRANT_URL` 环境变量读取

**核心设计思想：**
- 多阶段构建：镜像从 ~800MB 压到 12MB（96% 体积缩减）
- 层缓存：先 `COPY go.mod go.sum` + `go mod download`，代码变动不触发重新下载依赖
- `CGO_ENABLED=0`：纯静态二进制，不依赖 glibc
- `depends_on: condition: service_healthy`：等 Qdrant REST API 就绪后再启动 app
- 非 root 用户运行（`adduser appuser`）：减少攻击半径
- 12-Factor App：配置全部来自环境变量

**课后作业（已完成）：**
- ✅ 作业1：多阶段 Dockerfile，`docker build` 成功；镜像 12MB vs 单阶段 ~800MB
- ✅ 作业2：docker-compose.yml，`docker compose up` 一键启动（含 Qdrant healthcheck）
- ✅ 作业3：安全加固，非 root 用户运行

---

### ✅ Lesson 10：Human-in-the-loop（人工转接）
**已完成内容：**
- `server/ticket.go` — `TicketStore`（内存存储，线程安全）、`NewTransferTool`、`ContextWithUserID`
- `server/handler.go` — `handleListTickets`（`GET /tickets?status=pending`）；`processChat`/`handleChatStream` 注入 userID 到 context
- `server/server.go` — `WithTicketStore` ServerOption，路由按需注册
- `examples/customer_service/main.go` — 注册工具 + 更新 System Prompt 转接条件

**核心设计思想：**
- 工具调用而非硬编码规则：LLM 语义理解触发条件，而非关键词匹配
- `context.WithValue` 传递 userID：不改变工具函数签名，线程安全
- 同一 TicketStore 实例：工具写入、HTTP 接口读取，显式依赖注入
- `WithTicketStore(nil)` 时路由不注册（功能可选）

**课后作业（已完成）：**
- ✅ 作业1：`TicketStore` + `NewTransferTool`；2个测试（创建/列表/ID递增/context注入）
- ✅ 作业2：`GET /tickets?status=pending`；3个测试（404未配置/空列表/有工单）
- ✅ 作业3：System Prompt 更新，4类触发条件（知识库无答案/要求人工/争议/情绪激动）

---

### ✅ Lesson 11：评估体系（AI Quality Evaluation）
**已完成内容：**
- `eval/types.go` — `EvalCase`/`EvalResult`/`EvalReport`/`CategoryStat` 数据结构
- `eval/judge.go` — LLM Judge 评分器，四维度打分（准确/相关/完整/幻觉），Markdown 响应剥离
- `eval/runner.go` — 批量并发评估 Runner，信号量控制并发，按 Category 分组统计
- `eval/testdata.go` — 20 个测试用例，覆盖订单/退款/物流/商品/账户/边界场景
- `cmd/eval/main.go` — 评估入口，`-prompt v1/v2` A/B 对比，`-csv` CSV 导出，格式化报告输出
- `eval/*_test.go` — 13 个单元测试全部通过

**核心设计思想：**
- LLM-as-Judge：用裁判模型语义评分，替代精确字符串匹配
- 四维度分离：准确性/相关性/完整性/幻觉率独立打分，精准定位质量瓶颈
- 加权综合分：默认 [0.35, 0.25, 0.25, 0.15]，可自定义权重
- 并发执行：信号量限制并发数（默认 3），单用例超时保护（60s）
- Markdown 剥离：`parseJudgeResponse` 处理 LLM 输出的 ```json 包裹和前导文本
- A/B 对比：`-prompt v1/v2` 切换 System Prompt 版本，输出可比较的报告
- CSV 导出：UTF-8 BOM 确保 Excel 中文正确显示，含汇总行

**课后作业（已完成）：**
- ✅ 作业1：运行评估找出最低分 3 个用例（order-004/003/002），分析根因（缺工具 vs 幻觉）
- ✅ 作业3：`-csv` 参数导出 CSV（BOM 头 + 逐条数据 + 汇总行），`data/*.csv` 加入 .gitignore

---

### ✅ Lesson 12：多 Agent 协作
**已完成内容：**
- `agent/router.go` — Router Agent：意图识别（LLM JSON 分类）+ 分发到 Sub-Agent
- `agent/router_test.go` — 10 个测试（parseIntent 4 + buildRouterPrompt 1 + Router.Route 集成 5）
- `examples/multi_agent/main.go` — 完整多 Agent 客服 Demo（Order/Logistics/Refund/FAQ 四路由）

**核心设计思想：**
- 分治：每个 Sub-Agent 只拿自己领域的工具（2-3个），避免工具过多导致 LLM 选错
- `SubAgentFactory` 工厂函数：每次路由创建干净实例，避免跨请求状态污染
- Router 只做意图分类（轻量 LLM 调用），Sub-Agent 做实际工作（可能多轮工具调用）
- `parseIntent` 兼容 markdown 包裹和前导文本（与 eval/judge.go 同策略）
- `findFactory` 大小写不敏感匹配，容忍 LLM 输出格式差异
- `WithFallback` 兜底 Agent：无法匹配意图时降级到 FAQ（纯 LLM 回答）
- `RunOption` 透传：Router 把 `WithUser`/`WithGate` 等选项原样传给 Sub-Agent

**课后作业（已完成）：**
- ✅ 作业2：Intent 增加 `Confidence` 字段 + `WithConfidenceThreshold` 选项，低于阈值自动走 fallback；4个测试
- ✅ 作业3：`RouteParallel` 并行分发 + `classifyMulti` 多意图识别 + `parseIntents` 数组/单对象兼容解析 + `deduplicateIntents` 去重 + `mergeAnswers` 合并；9个新测试

---

### ✅ Lesson 13：Structured Output（结构化输出）
**已完成内容：**
- `structured/extractor.go` — `Extractor[T]` 泛型提取器：Schema → ToolDefinition → LLM → JSON 解码 → 校验 → 自动重试
- `structured/extractor_test.go` — 8 个单元测试全部通过
- `examples/structured_output/main.go` — Demo：从客服对话中提取工单（类别/优先级/情绪/摘要）

**核心设计思想：**
- Function Calling 作为结构化输出：把 Schema 包装为 ToolDefinition，LLM 被迫按 Schema 填参数
- 泛型 `Extractor[T]`：目标类型在编译期确定，JSON 解码后直接得到类型安全的结构体
- `Validator` 接口自动触发：实现即校验（枚举值、数值范围），不实现即跳过
- 校验失败自动重试：错误信息作为 tool result 回传 → LLM 看到哪里错了 → 修正输出
- 不修改原始 messages：copy 后操作，避免副作用

**课后作业（待完成）：**
- 作业1：运行 Demo，对比 temperature=0.1 和 temperature=0.8 的提取稳定性
- 作业2：新增一个 `SentimentReport` 类型（情绪分析报告），复用 `Extractor[T]`
- 作业3：将 `Extractor` 集成到客服系统——每次对话结束后自动提取工单

---

## 技术栈
- **语言**：Go（主）+ Python（原型验证）
- **LLM**：OpenAI API / 智谱 GLM / 通义千问 / 本地 Ollama
- **向量库**：待定（Qdrant / pgvector）
- **框架**：原生实现为主，参考 langchaingo

## 代码规范
- 错误处理：`fmt.Errorf("操作失败: %w", err)` wrap 原始错误
- 并发：共享状态必须加锁，优先用 channel 传递数据
- 配置：函数式选项模式（`WithXxx`）
- 注释：中文注释，说明"为什么"而不只是"是什么"

## 课后作业
- 作业1（入门）： 运行 Demo，对比 temperature=0.1 和 0.8 的提取稳定性差异
- 作业2（中等）： 新增一个 SentimentReport 类型（含 overall_sentiment、key_phrases []string、escalation_needed bool），复用 Extractor[T] 提取
- 作业3（挑战）： 将 Extractor 集成到客服 server——POST /chat 回答后自动提取工单字段，附在响应 JSON 中返回

## 仓库地址
https://github.com/pengpn/go-llm-agent
