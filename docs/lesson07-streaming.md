# Lesson 07：流式响应（SSE Streaming）

## 为什么要学

当前 `/chat` 接口要等 LLM 生成完整答案后才返回，用户盯着空白等 5-10 秒。
改成流式响应，token 边生成边推送，用户立刻看到文字出现，体验质的提升。

---

## 学习目标

- 理解 SSE（Server-Sent Events）协议
- 掌握 Gin 的 `c.Stream()` 实现服务端推送
- 复用 Lesson 01 已有的 `client.ChatStream`
- 理解 Agent 流式输出的设计：工具调用阶段非流式，最终回答阶段流式

---

## 核心概念

### SSE 协议

SSE 是基于普通 HTTP 的单向推送协议，不需要 WebSocket，更简单。

| 特性 | SSE | WebSocket |
|------|-----|-----------|
| 方向 | 服务→客户端（单向） | 双向 |
| 协议 | HTTP | WS 升级 |
| 适用场景 | 流式文本、通知 | 实时双向通信 |

**响应头**：
```
Content-Type: text/event-stream
Cache-Control: no-cache
X-Accel-Buffering: no   ← 禁止 Nginx 缓冲，确保实时推送
```

**数据格式**（每个事件后必须有空行）：
```
event:token
data:退款

event:token
data:需要3-5

event:done
data:
```

### Gin 的 c.Stream()

```go
c.Stream(func(w io.Writer) bool {
    token, ok := <-tokenCh
    if !ok {
        c.SSEvent("done", "")
        return false // 结束流
    }
    c.SSEvent("token", token)
    return true // 继续推送下一个 token
})
```

`c.Stream` 持续调用回调直到 `return false`，**阻塞当前 goroutine**。

### 关键设计决策：分阶段流式

Agent Loop 无法全程流式，原因：
1. 工具调用阶段需要传递 `tool definitions`，`ChatStream` 不支持
2. 工具调用很快（<1s），用户等不到感受差异

**解决方案**：

```
工具调用阶段（ChatWithTools，同步）
    ↓ 执行工具（如 search_knowledge_base）
最终回答阶段（ChatStreamText，流式）← 这段推给用户看
```

### StreamingLLMClient 接口（可选扩展）

```go
// agent 包定义接口，不依赖 client 包（低耦合）
type StreamingLLMClient interface {
    ChatStreamText(ctx context.Context, messages []models.Message) (<-chan string, error)
}
```

通过类型断言检查：`streamClient, canStream := a.client.(StreamingLLMClient)`  
→ 不支持流式的 client 自动降级，老代码零改动。

---

## 关键实现

### client.ChatStreamText（新增）

```go
// 包装 ChatStream，返回纯文本 channel，丢弃 Err/Done 字段
func (c *Client) ChatStreamText(ctx context.Context, messages []models.Message) (<-chan string, error) {
    chunkCh, err := c.ChatStream(ctx, messages)
    // ... goroutine 过滤，只发 Content 字段
}
```

### agent.RunStream（新增）

```go
// 工具调用迭代：ChatWithTools（同步）
// 工具执行完毕后：ChatStreamText（流式），token → tokenCh
// RunStream 返回时 defer close(tokenCh)，通知 c.Stream 结束
func (a *Agent) RunStream(ctx context.Context, messages []models.Message,
    tokenCh chan<- string, opts ...RunOption) (string, []models.Message, error)
```

### handleChatStream（新增）

```go
// 1. 类型断言检查 ag 是否支持流式
streamAg, ok := s.ag.(StreamingAgentRunner)
if !ok {
    s.processChat(c, req)  // 降级：body 已解析，直接复用
    return
}

// 2. 后台 goroutine 运行 RunStream，向 tokenCh 写 token
tokenCh := make(chan string, 32)
go func() {
    defer close(done)
    finalAnswer, history, runErr = streamAg.RunStream(ctx, msgs, tokenCh)
}()

// 3. c.Stream 读 tokenCh 推 SSE
c.Stream(func(w io.Writer) bool {
    select {
    case token, open := <-tokenCh:
        if !open { c.SSEvent("done", ""); return false }
        c.SSEvent("token", token); return true
    case <-c.Request.Context().Done():
        return false  // 客户端断连，停止推送
    }
})
```

**为什么用后台 goroutine？**  
`c.Stream` 阻塞当前 goroutine，`RunStream` 也阻塞；两者需要并发执行，channel 作为桥梁。

**为什么 `select` 里要处理 `ctx.Done()`？**  
客户端断开连接 → `c.Request.Context()` 被取消 → `RunStream` 里的 LLM 请求也被取消（context 传播）→ 节省 token 费用。

---

## API 使用

### POST /chat/stream

```bash
curl -N -X POST http://localhost:8080/chat/stream \
  -H "Content-Type: application/json" \
  -d '{"user_id": "u001", "message": "退款要多久？"}'
```

`-N` 禁用 curl 的缓冲，实时看到输出：

```
event:token
data:退款

event:token
data:通常需要

event:token
data:3-5个工作日

event:done
data:
```

### 前端 EventSource

```javascript
const es = new EventSource('/chat/stream');
es.addEventListener('token', (e) => {
    document.getElementById('answer').textContent += e.data;
});
es.addEventListener('done', () => es.close());
```

---

## 构建的模块

```
client/stream.go      — 新增 ChatStreamText()，返回 <-chan string
agent/agent.go        — 新增 StreamingLLMClient 接口 + RunStream() + streamFinalAnswer()
server/server.go      — 新增 StreamingAgentRunner 接口 + /chat/stream 路由
server/handler.go     — 新增 handleChatStream()；提取 processChat() 共用逻辑
server/server_test.go — 新增 3 个流式测试（httptest.NewServer 支持 CloseNotify）
```

---

## 课后作业

### 作业1：实现 `POST /chat/stream`，返回 SSE 流 ✅（已完成）

核心：`RunStream` + `c.Stream()` + `tokenCh` 三角关系。

### 作业2：客户端断连时自动取消 LLM 请求 ✅（已完成）

`handleChatStream` 的 `c.Stream` 回调里监听 `c.Request.Context().Done()`：
- 客户端断连 → HTTP context 被取消
- `RunStream` 使用同一个 ctx → LLM 请求被取消
- 节省 token 费用，防止资源浪费

### 作业3：用 curl 测试流式接口

```bash
# 启动服务（需要 Qdrant + 有效 API Key）
go run ./examples/customer_service/

# 流式请求（-N 禁用缓冲，实时看到 token）
curl -N -X POST http://localhost:8080/chat/stream \
  -H "Content-Type: application/json" \
  -d '{"user_id": "u001", "message": "退款要多久？"}'
```

---

## 测试要点

SSE 测试需要用 `httptest.NewServer`（真实 TCP 连接），不能用 `httptest.NewRecorder`：

```go
// ❌ 失败：ResponseRecorder 不支持 http.CloseNotifier，c.Stream 会 panic
w := httptest.NewRecorder()

// ✅ 正确：真实 HTTP 服务，支持 CloseNotifier
ts := httptest.NewServer(srv)
resp, _ := http.Post(ts.URL+"/chat/stream", ...)
```

原因：Gin `c.Stream` 内部调用 `w.CloseNotify()` 检测客户端断连，`httptest.ResponseRecorder` 未实现此接口。
