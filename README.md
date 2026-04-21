# go-llm-agent

一个用 Go 从零实现的生产级 LLM / AI Agent 学习项目。

这个仓库不是"为了演示而演示"的玩具 Demo，而是按真实后端工程的要求，逐步搭建一个可扩展的对话助手 / 客服系统。

## 已完成课程

- **Lesson 01**：生产级 LLM Client
- **Lesson 02**：多轮对话管理 + 持久化 + tiktoken 精确计数
- **Lesson 03**：Agent Loop（ReAct / Function Calling）+ 并行工具调用 + Session 集成

## 为什么这样设计

这个项目的核心目标不是"先接 API 跑通"，而是建立一套后续能自然演进到 Agent、RAG、客服系统的底座。

- LLM Client 独立封装：重试、超时、并发控制、流式输出，未来所有 Agent 和业务模块都会复用
- Session 与 Client 解耦：调用模型和管理记忆是两个不同职责，拆开后更容易测试和扩展
- 配置分层加载：本地开发、测试环境、生产环境对配置来源的要求不同，分层能降低误配置风险
- LLMClient 接口：Agent 不直接依赖 `*client.Client`，方便测试注入 mock，也方便将来切换 Provider

## 当前能力

### Lesson 01：生产级 LLM Client

- OpenAI 兼容协议，通过切换 `BaseURL` 适配 OpenAI、智谱、通义、Ollama
- 指数退避自动重试，重点处理 `429` 和 `5xx`
- 信号量并发控制，避免瞬时请求把 Provider 打爆
- SSE 流式输出，支持边生成边消费
- Token 成本统计，便于后续做监控和预算控制
- 函数式选项模式，便于扩展配置而不破坏调用方

### Lesson 02：多轮对话管理

- 单 Session 维护历史消息，自动截断上下文
- 支持"按轮数"（ByTurns）和"按 Token 精确计数"（ByTokenCount + tiktoken）两种截断策略
- 模型不支持 tiktoken 时自动降级估算，不报错
- Session JSON 持久化：临时文件 + 原子重命名，保存全量历史（非截断后版本）
- 恢复时截断策略由调用方注入，数据与行为解耦
- 多用户 Session Manager，带 TTL 过期清理，并发安全

### Lesson 03：Agent Loop

- `agent.Agent` 实现完整 ReAct Loop：tool_calls → 执行工具 → 回传 Observation → 继续循环
- 工具**并发执行**：`sync.WaitGroup` + 预分配 slice，结果顺序与输入一致，ToolCallID 对应关系正确
- `LLMClient` 接口解耦 Agent 与具体实现，测试时注入 mock 无需网络
- `Registry` 统一管理工具注册、定义暴露和调用执行
- `RunWithTrace` 调试模式，记录每轮工具调用的输入输出
- `Session.AddAgentTurn` 原子写入 assistant(ToolCalls) + tool 消息，保证协议顺序
- `applyHistoryToSession` 将 Agent 产生的完整历史写回 Session，支持跨轮次上下文
- MaxIterations 防无限循环，工具失败作为 Observation 继续循环而不中断

## 项目结构

```text
.
├── agent/
│   ├── agent.go        # Agent Loop、LLMClient 接口、并发工具执行
│   ├── agent_test.go   # mock 测试：直接回答/工具调用/并行/失败/最大迭代
│   └── tool.go         # Tool 定义、ToolFunc 签名、Registry 注册表
├── client/
│   ├── client.go       # LLM 调用层：重试、Chat、ChatWithTools
│   ├── cost.go         # Token 成本统计
│   ├── options.go      # 函数式选项、Provider 预设、NewFromConfig
│   └── stream.go       # SSE 流式输出
├── config/
│   └── config.go       # 配置结构体，分层加载（env > .env > yaml > 默认值）
├── examples/
│   ├── chatbot/        # 命令行多轮对话 Demo
│   └── order_agent/    # 多轮对话订单查询 Agent（Session + Agent 联动）
├── models/
│   ├── message.go      # Message / Usage / Response / ChatRequest / ChatResponse
│   └── tool.go         # ToolCall / ToolDefinition / FunctionDefinition / ToolParameters
├── session/
│   ├── manager.go      # 多用户 Session Manager，TTL 清理
│   ├── persist.go      # JSON 序列化持久化，原子写入
│   ├── persist_test.go # 持久化测试
│   ├── session.go      # Session：消息历史、AddAgentTurn、截断
│   ├── session_test.go # AddAgentTurn 测试
│   ├── truncate.go     # TruncateStrategy 接口 + ByTurns / ByTokenCount / TokenCounter
│   └── truncate_test.go # 截断策略边界测试
├── .env.example        # 环境变量模板
├── config.yaml         # 非敏感默认配置
├── AGENTS.md           # 课程上下文与教学约定（同 CLAUDE.md）
└── CLAUDE.md           # 课程进度与设计说明
```

## 快速开始

### 1. 准备环境

要求：Go 1.23.1+，以及一个可用的 LLM API Key。

```bash
cp .env.example .env
# 编辑 .env，填入 OPENAI_API_KEY
```

### 2. 运行命令行客服 Demo

```bash
go run ./examples/chatbot/
```

支持命令：普通文本对话 / `clear` 清空历史 / `quit` 退出并打印 Token 成本。

### 3. 运行订单查询 Agent

```bash
go run ./examples/order_agent/
```

这是一个支持多轮对话的 Agent，工具调用历史存入 Session。支持命令：
- 普通文本：发给 Agent 处理，自动判断是否调用工具
- `history`：查看当前 Session 的消息列表（包含工具调用记录）
- `clear`：清空对话历史
- `quit`：退出

### 4. 运行测试

```bash
go test ./...
```

### 5. 切换 Provider

```bash
# 切换模型
LLM_MODEL=gpt-4o go run ./examples/chatbot/

# 切换到 Ollama 本地模型
LLM_PROVIDER=ollama go run ./examples/order_agent/

# 使用自定义配置文件
CONFIG_FILE=./my-config.yaml go run ./examples/chatbot/
```

## 支持的 Provider

| Provider | 设置 |
|----------|------|
| OpenAI | `LLM_PROVIDER=openai` + `OPENAI_API_KEY` |
| 智谱 GLM | `LLM_PROVIDER=zhipu` + `ZHIPU_API_KEY` |
| 阿里通义 | `LLM_PROVIDER=tongyi` + `TONGYI_API_KEY` |
| Ollama | `LLM_PROVIDER=ollama`（无需 Key） |

## 代码示例

### 最小 LLM 调用

```go
c := client.New(
    client.WithOpenAI(os.Getenv("OPENAI_API_KEY"), "gpt-4o-mini"),
)
resp, err := c.Chat(ctx, []models.Message{
    {Role: models.RoleSystem, Content: "你是 Go 助手"},
    {Role: models.RoleUser, Content: "解释指数退避重试"},
})
```

### Session 持久化

```go
strategy := session.NewByTokenCount(4000, "gpt-4o-mini")
sess := session.NewSession("user_001", "你是专业客服", strategy)
sess.AddUserMessage("帮我查询订单")

_ = sess.Save("./sessions/user_001.json")

// 恢复（截断策略由调用方注入）
loaded, _ := session.LoadSession("./sessions/user_001.json", strategy)
```

### 注册工具并运行 Agent

```go
registry := agent.NewRegistry()
registry.Register(agent.NewTool(
    "get_order_status",
    "查询订单状态和物流信息",
    models.ToolParameters{
        Type: "object",
        Properties: map[string]models.ToolProperty{
            "order_id": {Type: "string", Description: "订单号"},
        },
        Required: []string{"order_id"},
    },
    func(ctx context.Context, input string) (string, error) {
        return agent.BuildToolResult(map[string]any{"status": "已发货"})
    },
))

ag := agent.New(llmClient, registry)
answer, history, err := ag.Run(ctx, messages)
```

## Agent Loop 工作流程

```
用户消息
    ↓
ChatWithTools(messages + 工具定义)
    ↓
finish_reason = "tool_calls"?
    ├─ 是 → 并发执行工具 → 追加 tool 消息 → 继续循环
    └─ 否 → 返回最终答案
```

工具调用消息结构（OpenAI 协议）：

```
[assistant]  ToolCalls:[{id:"c1", name:"get_order_status", args:'{"order_id":"ORDER-001"}'}]
[tool]       Content:'{"status":"已发货"...}',  ToolCallID:"c1"
[assistant]  "您的订单已发货，运单号 SF123..."
```

## 下一步课程路线

| 课程 | 主题 | 状态 |
|------|------|------|
| Lesson 04 | 工具集成深入：参数校验、多工具协作、错误恢复 | 🔜 |
| Lesson 05 | RAG 知识库接入 | 待定 |
| Lesson 06 | 完整客服系统 + 生产部署 | 待定 |

## 代码规范

- 错误处理统一使用 `fmt.Errorf("操作失败: %w", err)` 包装原始错误
- 并发场景下共享状态必须显式加锁，优先使用 channel 传递数据
- 配置使用函数式选项模式 `WithXxx`
- 注释优先解释"为什么这样设计"，而不只是"这行代码在做什么"

## 适合谁

- 想用 Go 系统学习 AI Agent 的后端工程师
- 已经会调用 OpenAI API，但还没建立生产级抽象的人
- 想理解 Agent、RAG、Function Calling 底层实现，而不是只会用框架的人
