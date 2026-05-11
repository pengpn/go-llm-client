# Lesson 03：Agent Loop（ReAct 模式）

## 学习目标

- 理解 Agent 和普通 LLM 对话的本质区别
- 掌握 ReAct 框架（Reasoning + Acting）的工作原理
- 理解 Function Calling 协议：LLM 决策，Go 程序执行
- 实现支持并发工具调用的 Agent Loop
- 掌握工具调用历史的持久化方案

---

## 核心概念

### 什么是 Agent？

前两节课，程序是"被动响应"的——用户说什么，LLM 回什么。Agent 是"主动行动"的：它能自己决定做什么、调用什么工具、看结果再决定下一步。

### ReAct 框架（Reasoning + Acting）

ReAct 是最主流的 Agent 架构，核心思想是让 LLM 交替输出"思考"和"行动"：

```
用户：查一下订单 ORDER-123 的状态

LLM → 决定调用工具：get_order_status(order_id="ORDER-123")
                         ↓
Go 程序执行工具 → Observation: {"status": "已发货", "carrier": "顺丰"}
                         ↓
LLM → 基于结果回答用户：您的订单已发货，承运商顺丰
```

这个"决策 → 执行 → 观察 → 再决策"的循环就是 **Agent Loop**。

### LLM 负责决策，Go 程序负责执行

LLM 只是文本预测机器，不能真的"执行"任何东西。Agent 的本质是：
- **LLM 决策**：判断该调哪个工具、传什么参数
- **Go 程序执行**：真正调用 API、查数据库、写文件
- **Observation 回传**：执行结果喂回给 LLM，让它继续判断

### Function Calling 协议

OpenAI Function Calling 是目前最主流的工具调用协议：

```
1. 把工具定义（JSON Schema）发给 LLM
2. LLM 返回要调用的工具名 + 参数（JSON）
3. 程序执行工具
4. 把结果作为 tool 角色的消息追加到历史
5. 重新请求 LLM，让它基于工具结果继续回答
6. 重复直到 LLM 给出最终答案（finish_reason = "stop"）
```

---

## 关键设计决策

### 1. 工具并发执行

LLM 可以在一次响应中要求调用多个工具（parallel tool calls）。这些工具之间通常没有依赖关系，应该并发执行：

```go
// 预分配 slice 保证结果顺序与输入一致
results := make([]models.Message, len(toolCalls))
var wg sync.WaitGroup

for i, call := range toolCalls {
    wg.Add(1)
    go func(idx int, c models.ToolCall) {
        defer wg.Done()
        output, _ := registry.Execute(ctx, c.Function.Name, c.Function.Arguments)
        results[idx] = models.Message{...} // 按 index 写入，不用加锁
    }(i, call)
}
wg.Wait()
```

**为什么预分配 + 按 index 写入，而不是 append？**

append 到共享 slice 有数据竞争。按 index 写入预分配的 slice，每个 goroutine 写不同位置，天然线程安全，且保证结果顺序与输入一致（LLM 依赖 ToolCallID 匹配结果）。

### 2. LLMClient 接口解耦

```go
type LLMClient interface {
    ChatWithTools(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error)
}
```

定义为接口的好处：
- **测试**：注入 mock LLMClient，无需真实 API
- **替换**：将来换 Provider 只需实现接口，Agent 代码不变

### 3. MaxIterations 防护

Agent Loop 可能陷入死循环（工具调用 → 工具调用 → 工具调用...）。设置最大迭代次数兜底：

```go
const MaxIterations = 10

for i := range MaxIterations {
    // ...
}
return "", history, fmt.Errorf("超过最大迭代次数 %d", MaxIterations)
```

### 4. 工具失败作为 Observation 继续

工具失败不终止 Agent，而是把错误信息作为 Observation 返回给 LLM，让 LLM 自己决定下一步（重试、换工具、或告知用户）：

```go
output, err := registry.Execute(ctx, name, args)
if err != nil {
    output = fmt.Sprintf("工具执行失败: %v", err) // 包装成 Observation
}
// 继续把 output 加入历史，让 LLM 看到
```

### 5. applyHistoryToSession：assistant + tool 原子存储

Agent Run 返回的 history 必须正确写入 Session：

```go
// assistant 消息（含 tool_calls）和紧随其后的 tool 消息必须原子写入
// 中间被截断会导致历史不合法（OpenAI 要求 tool 消息必须紧跟对应的 assistant 消息）
sess.AddAgentTurn(assistantMsg, toolResults)
```

---

## 构建的模块

```
agent/
├── tool.go        — Tool 定义、ToolFunc 签名、Registry 注册表、NewTool 便捷函数
└── agent.go       — Agent Loop 核心（Run / RunWithTrace），LLMClient 接口

models/
└── tool.go        — ToolCall / ToolDefinition / FunctionDefinition / ToolParameters

examples/
└── order_agent/
    └── main.go    — 多轮对话订单查询 Agent，Session 持久化工具调用历史
```

---

## Agent Loop 完整流程图

```
输入: messages（含 system prompt 和用户问题）
         ↓
    调用 LLM（携带工具定义）
         ↓
   finish_reason == "stop"? ──→ 返回最终答案
         ↓ No
   finish_reason == "tool_calls"?
         ↓ Yes
   并发执行所有工具调用
         ↓
   把工具结果追加到 history
         ↓
   是否超过 MaxIterations?  ──→ 返回错误
         ↓ No
   （循环）
```

---

## 核心设计思想总结

| 设计点 | 实现方式 | 原因 |
|--------|----------|------|
| 工具并发执行 | WaitGroup + 预分配 slice | 结果顺序与输入一致，天然线程安全 |
| LLM 接口解耦 | LLMClient interface | 便于测试 mock，便于替换 Provider |
| 防死循环 | MaxIterations = 10 | Agent Loop 有无限循环风险 |
| 工具失败处理 | 错误作为 Observation | LLM 自主决策，不强制终止 |
| 历史原子存储 | AddAgentTurn | 保证 assistant+tool 消息组完整性 |
| 调试支持 | RunWithTrace | 返回每步轨迹，方便定位问题 |

---

## 课后作业（已完成）

- ✅ **并行工具调用**：WaitGroup 并发执行，按 index 写入预分配 slice 保序
- ✅ **工具调用历史持久化**：AddAgentTurn 原子写入 + applyHistoryToSession + Session.Save
- ✅ **单元测试**：mock LLMClient，覆盖直接回答/工具调用/并行/失败/最大迭代/ctx 取消

---

## 运行方式

```bash
# 普通用户（只能查订单，不能取消）
go run ./examples/order_agent/

# 管理员（可以取消订单）
ROLE=admin go run ./examples/order_agent/

# 输入 history  查看消息历史
# 输入 clear    清空历史
# 输入 quit     退出
```
