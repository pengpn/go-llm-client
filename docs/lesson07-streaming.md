# Lesson 07：流式响应（SSE Streaming）

## 为什么要学

当前 `/chat` 接口要等 LLM 生成完整答案后才返回，用户盯着空白等 5-10 秒。
改成流式响应，token 边生成边推送，用户立刻看到文字出现，体验质的提升。

## 学习目标

- 理解 SSE（Server-Sent Events）协议
- 掌握 Gin 的 `c.Stream()` 实现服务端推送
- 复用 Lesson 01 已有的 `client.StreamChat`
- 前端如何用 `EventSource` 接收流

## 计划内容

### 原理
- SSE vs WebSocket：SSE 单向推送，HTTP 协议，更简单；WebSocket 双向，更复杂
- 响应头：`Content-Type: text/event-stream`，`Cache-Control: no-cache`
- 数据格式：`data: {token}\n\n`，`data: [DONE]\n\n` 表示结束

### 要实现的东西
```
POST /chat/stream   — 新增流式接口（原 /chat 保留）
```

```go
// Gin 流式推送
c.Stream(func(w io.Writer) bool {
    token, ok := <-tokenCh
    if !ok {
        c.SSEvent("done", "")
        return false // 结束流
    }
    c.SSEvent("token", token)
    return true // 继续
})
```

### 课后作业（预计）
- 作业1：实现 `POST /chat/stream`，返回 SSE 流
- 作业2：客户端断连时自动取消 LLM 请求（`c.Request.Context()` 取消传播）
- 作业3：用 curl 测试流式接口：`curl -N http://localhost:8080/chat/stream -d '{...}'`
