# Lesson 14：Memory & Conversation Summary（长期记忆）

## 学习目标

- 理解 LLM 无状态的本质限制，以及为什么需要外部记忆系统
- 实现对话摘要压缩：历史超长时 LLM 自动总结，Token 消耗大幅下降
- 实现用户画像记忆：跨会话记住用户偏好，提供个性化服务
- 理解记忆的写入时机、召回时机、过期淘汰策略

## 核心概念

### 为什么需要长期记忆？

LLM 是无状态的。每次请求都是独立的，它不会"记住"之前的对话。我们通过 Session 携带历史消息来模拟记忆，但有两个问题：

| 问题 | 影响 | 解决方案 |
|------|------|----------|
| 上下文窗口有限 | 旧对话被硬截断，关键信息丢失 | **对话摘要压缩** |
| 会话级隔离 | Session 过期后一切归零 | **用户画像记忆** |

### 对话摘要 vs 硬截断

硬截断（ByTurns）的问题：
```
轮次 1-10：用户报了订单号 ORDER-12345，说要退款
轮次 11-20：客服在处理退款流程
轮次 21+：超过 20 轮限制 → 轮次 1-10 被丢弃 → 客服"失忆"
```

摘要压缩的做法：
```
轮次 1-10 → LLM 压缩为一段摘要："用户 ORDER-12345 申请退款..."
摘要 + 轮次 11-20 → 继续对话 → 关键信息被保留
```

### 用户画像的三个关键问题

1. **何时写入？** 对话中识别到值得记住的信息时
2. **何时召回？** 新会话开始时注入 System Prompt
3. **何时过期？** 设置 TTL，避免过时信息误导

## 设计决策

### 1. Summarizer 为什么不是 TruncateStrategy？

`TruncateStrategy.Truncate()` 是同步方法，但摘要需要调用 LLM（异步 + 可能失败）。
另外，截断是可逆的（原始消息仍在 Session 中），但摘要是有损压缩——执行后无法恢复。

所以 `Summarizer` 是独立组件，需要显式调用 `CompressIfNeeded()`。

### 2. SummaryLLM 接口为什么不复用 agent.LLMClient？

```go
// session 包的最小接口（只需要 Chat）
type SummaryLLM interface {
    Chat(ctx context.Context, messages []models.Message) (*models.Response, error)
}

// agent 包的接口（需要 ChatWithTools）
type LLMClient interface {
    ChatWithTools(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error)
}
```

- 接口隔离原则：摘要只需要普通对话，不需要 Function Calling
- 依赖方向：session 包不应反向依赖 agent 包
- `*client.Client` 同时实现了两个接口（`Chat` 内部调用 `ChatWithTools(nil)`）

### 3. 用户画像为什么用 key-value 而不是自由文本？

| 方案 | 优点 | 缺点 |
|------|------|------|
| key-value | 精确更新、按需检索、精细过期 | 需要预定义 key |
| 自由文本 | 灵活 | 更新困难、检索模糊、容易冗余 |

生产系统通常用 key-value，因为客服场景的画像字段是可枚举的（地址、偏好、会员等级等）。

## 关键文件

| 文件 | 职责 |
|------|------|
| `session/summary.go` | 对话摘要压缩器（`Summarizer`） |
| `session/memory.go` | 用户画像（`UserMemory`）+ 文件存储（`FileMemoryStore`） |
| `session/summary_test.go` | 摘要压缩器 8 个测试 |
| `session/memory_test.go` | 用户画像 14 个测试 |
| `examples/memory_demo/main.go` | 完整 Demo |

## 核心代码讲解

### Summarizer 工作流

```
CompressIfNeeded(ctx, session)
    ├── 消息数 < 阈值 → return false（不压缩）
    ├── 分割消息：[需要总结的旧消息] + [保留的最近 N 条]
    ├── 旧消息 → formatConversation → LLM.Chat → 摘要文本
    └── 组装新消息：[system] + [摘要] + [最近 N 条] → 原子替换
```

### UserMemory 数据流

```
新会话 → FileMemoryStore.Load(userID) → UserMemory
           ↓
         FormatForPrompt() → 注入 System Prompt
           ↓
         对话中识别到新偏好 → Set(key, value, ttl)
           ↓
         FileMemoryStore.Save(memory) → 持久化到磁盘
           ↓
         下次会话 → Load → 画像自动恢复（回头客识别）
```

## 课后作业

### 作业 1（中等）：SummaryStrategy 截断策略

在 `session/summary.go` 中新增 `SummaryStrategy`，实现 `TruncateStrategy` 接口。

要求：
- 包装 `Summarizer`，在 `Truncate()` 中检查是否有已生成的摘要缓存
- 如果有缓存摘要 → 直接使用（避免重复调用 LLM）
- 如果没有缓存 → 退化为 `ByTurns` 截断（不能在同步方法中调用 LLM）
- 提供 `PrepareSummary(ctx, session)` 方法预生成摘要

### 作业 2（中等）：UserMemory 跨会话持久化

在客服系统（`examples/customer_service/main.go`）中集成 UserMemory。

要求：
- `GetOrCreate` 时自动加载用户画像
- 画像内容注入到 Session 的 System Prompt 中
- 提供 `PUT /memory/:user_id` 接口手动更新画像
- 提供 `GET /memory/:user_id` 接口查看画像

### 作业 3（挑战）：LLM 自动提取用户画像

实现 `MemoryExtractor`：每次对话后，用 LLM 从对话内容中自动提取值得记忆的信息。

要求：
- 复用 `structured.Extractor[T]` 泛型提取器
- 定义 `ExtractedMemories` 类型（key-value 数组 + 置信度）
- 低置信度的记忆不写入（阈值 0.7）
- 提取失败不影响正常对话流程
