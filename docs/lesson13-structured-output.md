# Lesson 13：Structured Output（结构化输出）

## 为什么要学

Agent 回答是自由文本——对人类友好，但对程序不友好。
从"ORDER-001 已发货，预计3天送达"中提取 `orderID`、`status`？正则太脆弱。
需要让 LLM 输出可编程的 JSON，直接 `json.Unmarshal` 得到结构体。

## 核心概念

### 三种结构化输出方案

| 方案 | 可靠性 | 原理 |
|------|--------|------|
| Prompt 约束 | ★★☆ | 在 Prompt 中写"只输出 JSON"，LLM 可能不遵守 |
| JSON Mode | ★★★ | API 参数强制 JSON，但不保证 Schema |
| **Function Calling** | ★★★★ | 定义"输出函数"，LLM 必须按 Schema 填参数 |

### Function Calling 作为结构化输出

```
之前的用法：                     本课的用法：
LLM → tool_calls → 程序执行    LLM → tool_calls → 直接拿参数当结果
      "get_order"                    "extract_ticket"
      执行查询逻辑                     Arguments 就是提取结果
      返回结果给 LLM                   不需要执行任何操作
```

本质是**"骗" LLM**：定义一个假的"输出函数"，LLM 以为自己在调工具，我们只取它填的参数。

### 自动重试机制

```
Extract(messages)
    ↓
LLM → tool_calls → Arguments: {...}
    ↓
json.Unmarshal + Validate()
    ↓ 校验失败
错误回传为 tool result → LLM 看到错误 → 修正输出 → 重试
```

校验失败不是直接报错，而是把错误信息作为 tool result 告诉 LLM，让它修正。
最多重试 N 次（默认 2），所有尝试都失败才返回错误。

## 关键文件

| 文件 | 作用 |
|------|------|
| `structured/extractor.go` | 泛型 `Extractor[T]`，Schema → ToolDef → LLM → 解码 → 校验 → 重试 |
| `structured/sentiment.go` | `SentimentReport` 类型 + `SentimentSchema()` + `Validate()` |
| `structured/extractor_test.go` | 8 个测试（Extractor 核心） |
| `structured/sentiment_test.go` | 4 个测试（Validate 边界 + Schema 完整性 + Extract 成功 + 重试） |
| `server/extract.go` | `TicketExtractor` + `ExtractedTicket` + `WithTicketExtractor` |
| `examples/structured_output/main.go` | 工单提取 + 情绪分析 Demo |
| `examples/structured_output/compare/main.go` | 温度对比实验（0.1 vs 0.8 稳定性） |

## 核心设计

### 泛型 Extractor[T]

```go
ext := NewExtractor[Ticket](client, schema)
ticket, err := ext.Extract(ctx, messages)
// ticket 是 Ticket 类型，编译期类型安全
```

### Validator 接口自动触发

```go
type Ticket struct { ... }

func (t *Ticket) Validate() error {
    if t.Priority < 1 || t.Priority > 5 {
        return fmt.Errorf("priority 必须 1-5")
    }
    return nil
}
// 实现了 Validate() → 自动调用
// 没实现 → 跳过，零侵入
```

### 不修改原始 messages

`Extract` 内部 `copy(history, messages)` 后操作，重试追加的消息不影响调用方。

## 课后作业

- ✅ 作业1（入门）：`examples/structured_output/compare/main.go` 温度对比实验——同一对话各跑 3 次，统计字段一致率
- ✅ 作业2（中等）：`structured/sentiment.go` + `SentimentSchema()` + 4 个新测试；Demo 增加情绪分析输出
- ✅ 作业3（挑战）：`server/extract.go` TicketExtractor + `WithTicketExtractor` ServerOption + `ChatResponse.Ticket`；3 个新测试（带提取/无提取/提取失败不影响主流程）

## 总结

结构化输出让 LLM 从"聊天机器人"变成"数据提取引擎"。
Function Calling 是最可靠的方案——Schema 约束 + 自动重试，比 Prompt 约束稳定得多。
`Extractor[T]` 的泛型设计让同一套机制可以提取任意类型：工单、情绪报告、对话摘要、实体抽取。
