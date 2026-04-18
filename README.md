# go-llm-agent

一个用 Go 从零实现的生产级 LLM / AI Agent 学习项目。

这个仓库不是“为了演示而演示”的玩具 Demo，而是按真实后端工程的要求，逐步搭建一个可扩展的对话助手 / 客服系统。课程当前已经完成：

- Lesson 01：生产级 LLM Client
- Lesson 02：多轮对话管理 + 课后作业增强
- Lesson 03：Agent Loop（ReAct）已完成

仓库地址当前是 `go-llm-client`，但 Go Module 名仍为 `github.com/pengpn/go-llm-agent`，这是为了保持代码内部导入路径一致。

## 为什么这样设计

这个项目的核心目标不是“先接 API 跑通”，而是建立一套后续能自然演进到 Agent、RAG、客服系统的底座。

- LLM Client 独立封装：因为重试、超时、并发控制、流式输出这些能力，未来所有 Agent 和业务模块都会复用
- Session 与 Client 解耦：因为“调用模型”和“管理记忆”是两个不同职责，拆开后更容易测试和扩展
- 配置分层加载：因为本地开发、测试环境、生产环境对配置来源的要求不同，分层能降低误配置风险
- 先做通用底座，再做 Agent：因为 ReAct、Function Calling、RAG 都建立在稳定的请求层和上下文管理层之上

## 当前能力

### Lesson 01：生产级 LLM Client

- OpenAI 兼容协议，可通过切换 `BaseURL` 适配 OpenAI、智谱、通义、Ollama
- 指数退避自动重试，重点处理 `429` 和 `5xx`
- 信号量并发控制，避免瞬时请求把 Provider 打爆
- SSE 流式输出，支持边生成边消费
- Token 成本统计，便于后续做监控和预算控制
- 函数式选项模式，便于扩展配置而不破坏调用方

### Lesson 02：多轮对话管理

- 单 Session 维护历史消息
- 自动截断上下文，支持“按轮数”和“按 Token 估算”两种策略
- 多用户 Session Manager，带 TTL 过期清理
- 并发安全，适合后续接入 Web 服务或客服系统
- 命令行聊天示例，可直接体验完整链路

### Lesson 02 课后作业：已完成增强

- Session 支持 JSON 持久化与恢复
- 保存全量历史，恢复后可切换不同截断策略重新计算
- 使用临时文件 + 原子重命名，避免写入中断导致文件损坏
- 引入 `TokenCounter` 接口，隔离精确计数实现与业务逻辑
- 支持 `TikTokenCounter` 精确计算，模型不支持时自动降级为估算
- 保留 `ByTokenEstimate` 兼容旧代码，推荐新代码使用 `NewByTokenCount`
- 为截断与持久化补充边界测试，覆盖空输入、预算边界、损坏文件、自动建目录等场景

### Lesson 03：Agent Loop（ReAct）

- 新增 `agent.Agent`，实现最小可用的 ReAct Loop
- 支持 `ChatWithTools` 请求，把工具定义一并发给模型
- 支持根据 `finish_reason="tool_calls"` 执行工具并继续循环
- 引入 `Registry` 统一管理工具注册、定义暴露和调用执行
- 增加 `models.ToolDefinition`、`ToolCall`、`ToolParameters` 等 Function Calling 结构
- 支持 `tool` 角色消息和 `tool_call_id` 回传，遵循 OpenAI 工具调用协议
- 提供 `RunWithTrace` 调试模式，方便观察每轮工具调用输入输出
- 提供订单查询 Agent 示例，演示查订单、查退款、列出用户订单

## 项目结构

```text
.
├── agent/              # ReAct Agent、工具注册表、工具执行辅助函数
├── client/             # LLM 调用层：重试、流式输出、Provider 选项、成本统计
├── config/             # 配置加载与校验
├── examples/chatbot/   # 命令行客服示例
├── examples/order_agent/ # 订单查询 Agent 示例
├── models/             # 通用数据结构：Message / Usage / Response / Tool Schema
├── session/            # 会话、上下文截断、持久化、多用户 Session 管理
├── .env.example        # 环境变量示例
├── config.yaml         # 非敏感默认配置
└── AGENTS.md           # 课程上下文与教学约定
```

## 快速开始

### 1. 准备环境

要求：

- Go 1.23.1+
- 一个可用的 LLM API Key

克隆后先准备环境变量：

```bash
cp .env.example .env
```

然后在 `.env` 中填入真实密钥，例如：

```bash
OPENAI_API_KEY=your-api-key
```

### 2. 查看默认配置

项目默认从以下来源按优先级加载配置：

1. 环境变量
2. `.env`
3. `config.yaml`
4. 代码内置默认值

这样设计的原因是：

- 敏感信息只放环境变量，避免误提交
- 非敏感默认值放配置文件，便于团队共享
- 代码保留兜底默认值，降低启动门槛

### 3. 运行命令行客服 Demo

```bash
go run ./examples/chatbot/
```

运行后你会得到一个简单的命令行客服：

- 输入普通文本，模型会结合历史对话回答
- 输入 `clear` 清空当前会话历史
- 输入 `quit` 退出，并打印本次 Token 成本统计

如果你想指定其他配置文件：

```bash
CONFIG_FILE=./config.yaml go run ./examples/chatbot/
```

### 4. 运行测试

```bash
go test ./...
```

这一步很重要，因为这个仓库不是只追求“能跑”，而是希望把 Agent 系统底层组件做成可验证、可回归、可迭代的工程资产。

### 5. 运行订单查询 Agent 示例

```bash
go run ./examples/order_agent/
```

这个示例会演示一个最小 ReAct Agent 的完整闭环：

- 模型先理解用户问题
- 判断是否要调用订单相关工具
- 工具返回结构化结果
- Agent 将结果作为 `tool` 消息回填给模型
- 模型再基于 Observation 生成最终答复

如果你想系统理解 Agent Loop，这个示例比普通聊天 Demo 更接近真实客服系统的工作方式。

## 支持的 Provider

项目目前按 OpenAI 兼容接口设计，支持以下常见 Provider：

- OpenAI
- 智谱 GLM
- 阿里通义千问
- Ollama（本地模型）

切换 Provider 的核心思路不是重写一套 Client，而是：

- 统一请求结构
- 通过 `provider`、`base_url`、`model` 切换目标模型服务

这也是后续做多模型路由、故障切换的基础。

## 一个最小使用示例

```go
package main

import (
	"context"
	"fmt"

	"github.com/pengpn/go-llm-agent/client"
	"github.com/pengpn/go-llm-agent/models"
)

func main() {
	c := client.New(
		client.WithOpenAI("your-api-key"),
		client.WithModel("gpt-4o-mini"),
	)

	resp, err := c.Chat(context.Background(), []models.Message{
		{Role: "system", Content: "你是一个专业的 Go 助手"},
		{Role: "user", Content: "请解释什么是指数退避重试"},
	})
	if err != nil {
		panic(err)
	}

fmt.Println(resp.Content)
}
```

## Session 持久化示例

Lesson 2 的作业完成后，Session 已经可以保存和恢复：

```go
strategy := session.NewByTokenCount(4000, "gpt-4o-mini")

sess := session.NewSession("user_001", "你是专业客服", strategy)
sess.AddUserMessage("帮我查询订单状态")

if err := sess.Save("./data/sessions/user_001.json"); err != nil {
	panic(err)
}

loaded, err := session.LoadSession("./data/sessions/user_001.json", strategy)
if err != nil {
	panic(err)
}

_ = loaded
```

这里有两个关键设计点：

- 文件里保存的是全量历史，而不是截断后的结果，因为存原始数据才能在未来切换策略时不丢信息
- 截断策略不写入文件，而由加载方注入，因为“数据”与“行为”解耦后更容易演进

## 精确 Token 计数

之前的按字符估算适合快速起步，但真实系统里，是否超上下文窗口、是否打满预算，最好基于更接近模型真实编码方式的计数。

因此现在的设计是：

- `TokenCounter` 作为抽象接口，负责隔离具体计数实现
- `TikTokenCounter` 用于支持的 OpenAI 模型精确计数
- `EstimateCounter` 作为兜底方案，保证在智谱、通义或未知模型上仍可工作
- `ByTokenEstimate` 保留向后兼容，避免旧调用方升级时全部重写

推荐新代码优先使用：

```go
strategy := session.NewByTokenCount(4000, "gpt-4o-mini")
```

这样做的原因不是“为了多一个抽象层”，而是为了把第三方依赖、模型兼容性和业务截断逻辑拆开，后续更容易测试、替换和扩展。

## Agent Loop 原理

Lesson 03 的核心不是“让模型会调函数”，而是建立一个最小可运行的 ReAct 闭环：

1. 用户提出问题
2. 模型判断是直接回答，还是先调用工具
3. 如果需要工具，模型返回 `tool_calls`
4. Go 程序执行工具，把结果封装成 `tool` 消息
5. 再把完整历史发给模型，让它基于工具结果继续推理
6. 当模型返回 `stop` 时，输出最终答案

这套设计的重要性在于：

- 普通聊天只能“说”，Agent 才能“查”和“做”
- 工具调用把模型能力从纯文本扩展到外部系统
- Loop 机制让模型可以根据 Observation 调整下一步动作
- 最大迭代次数限制可以防止模型陷入死循环

项目当前实现里，`agent.Agent` 做了三件关键事：

- 根据 `finish_reason` 判断是结束还是继续执行工具
- 维护完整消息历史，保证 assistant/tool 消息顺序符合协议要求
- 把工具执行错误也作为 Observation 返回给模型，让模型自行决定如何处理

## 最小 Tool 示例

```go
registry := agent.NewRegistry()

registry.Register(agent.NewTool(
	"get_order_status",
	"查询指定订单的状态和物流信息",
	models.ToolParameters{
		Type: "object",
		Properties: map[string]models.ToolProperty{
			"order_id": {
				Type:        "string",
				Description: "订单号，格式如 ORDER-001",
			},
		},
		Required: []string{"order_id"},
	},
	func(ctx context.Context, input string) (string, error) {
		return agent.BuildToolResult(map[string]any{
			"order_id": "ORDER-001",
			"status":   "已发货",
		})
	},
))
```

这里的设计重点是把两类信息分开：

- `Definition` 给模型看，决定“该不该调这个工具”
- `Execute` 给 Go 程序用，决定“这个工具实际怎么跑”

这样做的原因是，LLM 需要的是可理解的签名和描述，业务系统需要的是可执行的逻辑，它们本来就不该耦合成一个 JSON 配置文件。

## 订单查询 Agent 示例

`examples/order_agent` 演示了一个更贴近客服场景的最小 Agent：

- `get_order_status`：查询订单状态和物流
- `get_refund_status`：查询退款状态
- `list_user_orders`：列出用户所有订单

这个例子的教学价值在于，它不只是“能调用工具”，而是把工具调用放进了一个真实业务闭环里：

- 用户问具体订单，Agent 调 `get_order_status`
- 用户问退款，Agent 调 `get_refund_status`
- 用户没给订单号，Agent 可以先调 `list_user_orders`

这正是后面做客服系统、工单系统、内部助手时最常见的模式。

## 下一步课程路线

- Lesson 04：工具集成（Function Calling 深入）
- Lesson 05：RAG 知识库接入
- Lesson 06：完整客服系统 + 生产部署

## 已知待办

- 为 Agent 增加更完整的单元测试，覆盖多轮 tool_calls、未知 finish_reason、最大步数保护
- 设计更通用的 Tool 参数校验与输入解码层，减少工具样板代码
- 进入 Lesson 04，深入 Function Calling 的参数约束、错误恢复和多工具协作
- 为 `client` 模块补充更系统的重试、流式与并发测试
- 为 Session 持久化增加批量加载 / 自动恢复能力
- 评估是否为不同 Provider 增加更贴近真实编码的 Token 计数适配

## 代码规范

- 错误处理统一使用 `fmt.Errorf("操作失败: %w", err)` 包装原始错误
- 并发场景下共享状态必须显式加锁，优先使用 channel 传递数据
- 配置使用函数式选项模式 `WithXxx`
- 注释优先解释“为什么这样设计”，而不只是“这行代码在做什么”

## 适合谁

这个项目特别适合以下人群：

- 想用 Go 系统学习 AI Agent 的后端工程师
- 已经会调用 OpenAI API，但还没建立生产级抽象的人
- 想理解 Agent、RAG、Function Calling 底层实现，而不是只会用框架的人

## 课后作业

Lesson 3 完成后，建议你继续做这三个练习：

  1. 并行工具调用：当 LLM 在一次响应中请求多个工具时（len(ToolCalls) > 1），改为并发执行（用 goroutine + WaitGroup），而不是顺序执行
  2. 工具调用历史持久化：扩展 session.Session，让它能保存含 ToolCalls 的 assistant 消息（当前 AddAssistantMessage 只存文本）
  3. 测试：给 agent.Run 写单元测试，用 mock Client 模拟 LLM 的响应，验证工具调用→执行→回传的完整循环
