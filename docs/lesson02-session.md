# Lesson 02：多轮对话管理

## 学习目标

- 理解 LLM 无状态的本质，以及多轮对话"记忆"的实现方式
- 掌握 Session 管理的设计：单用户会话 + 多用户管理
- 理解两种截断策略（按轮次、按 Token 数）的适用场景
- 掌握 Session 持久化（JSON 原子写入）
- 理解并发安全：RWMutex 的读写分离

---

## 核心概念

### LLM 本身是无状态的

LLM 每次请求之间完全不记得你。多轮对话的"记忆"靠**客户端把历史消息全部带在每次请求里**实现：

```
第 1 轮请求：[system, user("你好")]
第 2 轮请求：[system, user("你好"), assistant("你好！"), user("我叫小明")]
第 3 轮请求：[system, user("你好"), assistant("你好！"), user("我叫小明"), assistant("你好小明！"), user("我叫什么？")]
```

这带来两个问题：
1. **上下文窗口有限**：历史太长超过模型的 context window 就报错
2. **多用户隔离**：客服系统有成千上万个用户，每个用户的对话历史要独立管理

### System Prompt 永远不截断

System Prompt 是 AI 的"人设"和"规则"，截断它会让 AI 忘记自己是谁、忘记行为规范。截断策略只针对历史轮次，System Prompt 始终保留。

---

## 关键设计决策

### 1. 截断策略接口

用接口抽象截断策略，方便扩展不同算法：

```go
type TruncationStrategy interface {
    Truncate(messages []models.Message) []models.Message
}
```

两种实现：
- **ByTurns**：按轮次截断，简单直接，适合 Token 数量差异不大的场景
- **ByTokenCount**：按 Token 精确计数，适合需要精确控制成本的场景

**为什么需要"降级估算"？**

不同模型的 Tokenizer 不同，`tiktoken` 不一定支持所有模型。当无法精确计算 Token 时，降级到按字符数估算（中文约 1.5 token/字，英文约 0.25 token/词），保证系统不因为 Tokenizer 缺失而崩溃。

### 2. double-check 防并发重复创建

Manager 创建 Session 时，可能多个 goroutine 同时发现 Session 不存在并同时创建：

```go
// 错误做法：check-then-act 有竞争
if _, ok := sessions[id]; !ok {
    sessions[id] = newSession() // 可能创建多次
}

// 正确做法：double-check
mu.Lock()
defer mu.Unlock()
if sess, ok := sessions[id]; ok {
    return sess // 加锁后再检查一次，已被其他 goroutine 创建
}
sess := newSession()
sessions[id] = sess
return sess
```

### 3. 保存全量历史（而非截断后的历史）

持久化保存**完整历史**（未截断），而非截断后的版本。原因：
- 截断策略可能随需求调整（如从 ByTurns 改为 ByTokenCount）
- 恢复会话后重新按当前策略计算，保证策略一致性
- 可以事后分析完整对话，不丢失用户行为数据

### 4. 原子写入防止数据损坏

```
写入流程：
1. 序列化为 JSON
2. 写入 .tmp 临时文件
3. os.Rename(.tmp → 目标文件)  ← 原子操作，失败不会损坏原文件
```

如果直接写目标文件，写到一半程序崩溃，会产生损坏的 JSON 文件。用临时文件 + Rename 保证要么完整写入，要么保留旧文件。

### 5. AddAgentTurn 原子写入工具调用

Agent 的一次工具调用会产生多条消息（assistant + 多个 tool result），必须原子写入：

```go
// 错误：分开写，中间可能被截断
session.AddMessage(assistantMsg)   // 含 tool_calls
session.AddMessage(toolResult1)    // 可能被截断丢失
session.AddMessage(toolResult2)

// 正确：原子写入一组消息
session.AddAgentTurn(assistantMsg, []toolResults)
```

---

## 构建的模块

```
session/
├── truncate.go    — 截断策略接口 + ByTurns / ByTokenCount（tiktoken + 降级估算）
├── session.go     — 单个会话：消息历史、自动截断、AddAgentTurn 原子写入
├── persist.go     — JSON 序列化持久化，原子写入（tmp + rename）
└── manager.go     — 多用户 Session 管理，TTL 过期清理，并发安全

examples/
└── chatbot/
    └── main.go    — 完整命令行客服 Demo，流式输出 + Session 联动
```

---

## 核心设计思想总结

| 设计点 | 实现方式 | 原因 |
|--------|----------|------|
| LLM 无状态 | 每次请求携带全部历史 | LLM 本身不记忆，记忆靠客户端维护 |
| System Prompt 保护 | 截断时跳过第一条消息 | System Prompt 是 AI 的"人设"，不能丢 |
| 截断策略解耦 | TruncationStrategy 接口 | 方便切换/扩展，不影响 Session 代码 |
| 并发安全 | RWMutex 读写分离 | 读多写少场景，读锁不互斥，性能更好 |
| 持久化幂等 | 保存全量历史 | 恢复后可用任意策略重新截断 |
| 原子写入 | tmp 文件 + os.Rename | 防止写入中途崩溃产生损坏文件 |
| double-check | 加锁后再次检查 | 防止并发重复创建 Session |

---

## 课后作业（已完成）

- ✅ **持久化**：`persist.go` 实现 `Save` / `Load`，原子写入 JSON
- ✅ **精确 Token 计数**：`ByTokenCount` 使用 tiktoken，不支持时降级到字符估算
- ✅ **AddAgentTurn**：原子写入 assistant + tool 消息组，支持 Lesson 03 的工具调用历史

---

## 运行方式

```bash
go run ./examples/chatbot/
# 输入 history  查看当前消息历史
# 输入 quit     退出并显示费用统计
```
