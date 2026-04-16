# go-llm-agent

一个用 Go 从零实现的生产级 LLM / AI Agent 学习项目。

这个仓库不是“为了演示而演示”的玩具 Demo，而是按真实后端工程的要求，逐步搭建一个可扩展的对话助手 / 客服系统。课程当前已经完成：

- Lesson 01：生产级 LLM Client
- Lesson 02：多轮对话管理
- Lesson 03：Agent Loop（ReAct）准备中

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

## 项目结构

```text
.
├── client/             # LLM 调用层：重试、流式输出、Provider 选项、成本统计
├── config/             # 配置加载与校验
├── examples/chatbot/   # 命令行客服示例
├── models/             # 通用数据结构：Message / Usage / Response
├── session/            # 会话、上下文截断、多用户 Session 管理
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

## 下一步课程路线

- Lesson 03：Agent Loop（ReAct 模式）
- Lesson 04：工具集成（Function Calling 深入）
- Lesson 05：RAG 知识库接入
- Lesson 06：完整客服系统 + 生产部署

其中 Lesson 03 会重点解决一个关键问题：

为什么“普通聊天”还不是 Agent？

因为真正的 Agent 不只是生成文本，而是能在“思考 -> 选择动作 -> 调用工具 -> 观察结果 -> 继续推理”的循环中完成任务。前两课做的 Client 和 Session，本质上就是在为这个循环打地基。

## 已知待办

- Session JSON 序列化持久化到文件
- 更精确的 Token 计算（如接入 tiktoken）
- 补充单元测试与集成测试
- 进入 Lesson 03，落地最小 ReAct Agent

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

如果你正在跟着这个仓库学习，建议先完成这三个练习：

1. 给 `session` 模块增加 JSON 持久化能力，并思考如何处理 TTL 恢复
2. 为 `client` 补充单元测试，重点覆盖重试和并发控制
3. 设计一个最小 Tool 接口，为下一节 ReAct Agent 做准备
