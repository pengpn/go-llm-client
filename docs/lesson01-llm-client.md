# Lesson 01：生产级 LLM Client

## 学习目标

- 理解为什么要封装 LLM Client，而不是直接调用 HTTP API
- 掌握指数退避重试的实现原理
- 掌握信号量并发控制
- 理解 SSE 流式输出协议
- 学会用函数式选项模式设计可扩展的配置接口

---

## 核心概念

### 为什么需要自己封装 LLM Client？

OpenAI、智谱、通义千问都提供 HTTP API，协议基本兼容。直接用 `http.Client` 调用有几个生产级问题：

1. **重试**：LLM 服务会返回 429（限流）和 5xx（服务错误），必须指数退避重试
2. **并发控制**：同时发出太多请求会触发 Rate Limit，需要信号量限流
3. **流式输出**：SSE（Server-Sent Events）协议逐 token 推送，用户体验更好
4. **多 Provider**：只改 `BaseURL` 就能切换，不重复造轮子

### OpenAI 兼容协议

几乎所有主流 LLM Provider 都兼容 OpenAI 的接口格式，只需要修改 `BaseURL` 就能切换：

```go
// 智谱 GLM
client.WithBaseURL("https://open.bigmodel.cn/api/paas/v4")

// 通义千问
client.WithBaseURL("https://dashscope.aliyuncs.com/compatible-mode/v1")

// 本地 Ollama
client.WithBaseURL("http://localhost:11434/v1")
```

---

## 关键设计决策

### 1. 指数退避重试

**为什么是指数退避而不是固定间隔？**

- 固定间隔重试：所有请求同时重试，可能加剧服务器压力
- 指数退避：每次等待时间翻倍，给服务器恢复时间，同时也减少无效请求

```
第 1 次失败 → 等 1s → 重试
第 2 次失败 → 等 2s → 重试
第 3 次失败 → 等 4s → 重试
第 4 次失败 → 返回错误
```

**只重试可重试的错误：**
- 429（Rate Limit）：可重试，等一等就好了
- 5xx（服务器错误）：可重试，服务可能在恢复
- 4xx（客户端错误，如 401 无权限）：不可重试，重试也没用

### 2. 信号量并发控制

用 channel 实现信号量，限制同时发出的请求数：

```go
// 创建容量为 N 的 channel，同时最多 N 个请求
semaphore := make(chan struct{}, maxConcurrency)

// 获取令牌（容量满时阻塞）
semaphore <- struct{}{}
defer func() { <-semaphore }() // 请求完成后释放
```

### 3. 函数式选项模式

比结构体配置更灵活，每个选项独立，不影响其他配置：

```go
// 构造 Client 时只传需要的选项
llm := client.New(
    client.WithBaseURL("https://api.openai.com/v1"),
    client.WithAPIKey(os.Getenv("OPENAI_API_KEY")),
    client.WithModel("gpt-4o"),
    client.WithMaxRetries(3),
    client.WithTimeout(30 * time.Second),
)
```

### 4. SSE 流式输出

LLM 生成的 token 是一个个产生的，SSE 协议让服务端边生成边发送：

```
data: {"choices":[{"delta":{"content":"你"}}]}
data: {"choices":[{"delta":{"content":"好"}}]}
data: {"choices":[{"delta":{"content":"！"}}]}
data: [DONE]
```

用 channel 传递 token，解耦生产者（HTTP 读取）和消费者（打印/处理）：

```go
tokenCh := client.StreamChat(ctx, messages)
for token := range tokenCh {
    fmt.Print(token) // 实时打印
}
```

---

## 构建的模块

```
models/
└── message.go     — Message / Usage / Response 数据结构（OpenAI 协议）

client/
├── options.go     — 函数式选项模式，多 Provider 预设（OpenAI/智谱/通义/Ollama）
├── client.go      — 核心 Client：Chat / StreamChat，指数退避重试 + 信号量并发控制
├── stream.go      — SSE 流式输出，channel 传递 token
└── cost.go        — Token 费用计算器，Mutex 并发安全
```

---

## 核心设计思想总结

| 设计点 | 实现方式 | 原因 |
|--------|----------|------|
| 多 Provider 支持 | OpenAI 兼容协议，只换 BaseURL | 避免为每个 Provider 写重复代码 |
| 指数退避重试 | 1s/2s/4s，最多 3 次 | 区分可重试错误（429/5xx），给服务恢复时间 |
| 并发限制 | channel 信号量 | 防止触发 API Rate Limit |
| 流式输出 | SSE 解析 + channel 传递 | 解耦读取和消费，提升用户体验 |
| 费用统计 | Mutex 保护的累加器 | 并发安全，方便监控 API 成本 |

---

## 课后作业（已完成）

Lesson 01 课程本身即为作业起点，为后续课程打基础。

---

## 运行方式

```bash
# 设置 API Key（支持任意 OpenAI 兼容 Provider）
export OPENAI_API_KEY=sk-xxx
# 或通义千问
export DASHSCOPE_API_KEY=sk-xxx

# 运行聊天机器人（Lesson 02 的 Demo）
go run ./examples/chatbot/
```
