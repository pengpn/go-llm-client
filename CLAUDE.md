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

### 🔄 Lesson 05：RAG 知识库接入
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

### 🔄 Lesson 06：完整客服系统 + 生产部署
**已完成内容：**
- `server/server.go` — `Server` 结构体，Gin 路由注册，优雅关闭（signal.NotifyContext + http.Server.Shutdown，最多等 30 秒）
- `server/handler.go` — `handleChat`（对话）、`handleReload`（热更新知识库）、`handleHealth`（健康检查）
- `server/middleware.go` — 结构化日志中间件（log/slog JSON 格式，含 method/path/status/duration/user_id）
- `examples/customer_service/main.go` — 整合所有组件：RAG + Agent + Session Manager + HTTP 服务器

**核心设计思想：**
- 共享 vs 隔离：LLM Client / RAG Retriever 所有用户共享（无状态）；Session 按 user_id 隔离
- 优雅关闭：SIGINT/SIGTERM → 停止接受新请求 → 等进行中请求完成（30s 超时）→ 停止 Session TTL 清理
- 热更新知识库：`POST /reload` 重新索引 FAQ，Qdrant upsert 幂等，无需重启服务
- 结构化日志：slog JSON 格式，方便接入 ELK/Loki 等日志平台
- gin.Recovery() 捕获 panic，防止单个请求崩掉整个服务

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

## 仓库地址
https://github.com/pengpn/go-llm-agent
