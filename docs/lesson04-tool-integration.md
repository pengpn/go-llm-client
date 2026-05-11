# Lesson 04：工具集成（Function Calling 深入）

## 学习目标

- 掌握泛型解码层，消除工具函数中重复的 `json.Unmarshal` 样板代码
- 理解 `Validator` 接口的零侵入设计
- 掌握基于角色的工具权限控制（RBAC）
- 理解两种错误恢复策略：`ContinueOnError` vs `AbortOnError`
- 学会用函数式选项向 `Run` 注入运行时配置，保持向后兼容

---

## 核心概念

### Lesson 03 遗留的问题

Lesson 03 的每个工具函数都需要手动解析 JSON：

```go
// 每个工具都要写这段样板代码
func getOrderStatus(ctx context.Context, input string) (string, error) {
    var req struct {
        OrderID string `json:"order_id"`
    }
    if err := json.Unmarshal([]byte(input), &req); err != nil {
        return "", err
    }
    if req.OrderID == "" {
        return "", fmt.Errorf("order_id 不能为空")
    }
    // 真正的业务逻辑...
}
```

10 个工具就要写 10 遍 `json.Unmarshal` + 校验。本节课用泛型消除这些样板代码。

---

## 关键设计决策

### 1. 泛型解码层：`DecodeAndValidate[T]`

```go
// 工具函数变成这样：直接接收类型化的结构体，零 JSON 样板
func getOrderStatus(ctx context.Context, req OrderIDReq) (string, error) {
    if req.OrderID == "" { ... } // 校验已在 Validate() 里做了
    // 只写业务逻辑
}

// 注册时用 NewTypedTool
agent.NewTypedTool("get_order_status", "查询订单", params, getOrderStatus)
```

内部 `NewTypedTool` 自动完成：反序列化 → 校验 → 调用业务函数。

### 2. Validator 接口：实现即校验，零侵入

```go
type Validator interface {
    Validate() error
}
```

工具的参数结构体如果实现了 `Validate()` 方法，解码后自动触发校验；不实现则跳过。无需改动 `DecodeAndValidate` 的调用方。

```go
type CancelOrderReq struct {
    OrderID string `json:"order_id"`
    Reason  string `json:"reason"`
}

// 实现 Validator 接口，解码后自动触发
func (r *CancelOrderReq) Validate() error {
    if !orderIDRegex.MatchString(r.OrderID) {
        return fmt.Errorf("order_id 格式不正确: %q", r.OrderID)
    }
    if len([]rune(r.Reason)) > 100 {
        return fmt.Errorf("取消原因不能超过 100 个字符")
    }
    return nil
}
```

### 3. Gate 接口：从源头防止越权

```go
type Gate interface {
    Allowed(userID, toolName string) bool
}
```

发给 LLM 的工具定义列表**在 Gate 过滤后才发出**，LLM 根本看不到不允许调用的工具：

```
用户 USER-001 请求时：
  发给 LLM 的工具：[get_order, get_refund, list_orders]  ← cancel_order 被过滤
  LLM 无法调用 cancel_order（它根本不知道有这个工具）

用户 ADMIN-001 请求时：
  发给 LLM 的工具：[get_order, get_refund, list_orders, cancel_order]
```

这比"LLM 调用了再拦截"更安全——从源头消除越权可能。

**三种内置实现：**

```go
AllowAll{}          // 所有用户可调用所有工具（默认）
DenyAll{}           // 拒绝所有工具调用
NewRoleGate("user") // 基于角色的权限控制
    .DefineRole("user", "get_order", "get_refund")
    .DefineRole("admin", "get_order", "get_refund", "cancel_order")
    .AssignRole("USER-001", "user")
    .AssignRole("ADMIN-001", "admin")
```

### 4. ErrorStrategy：分离两种场景

**ContinueOnError（默认）**：工具失败 → 错误作为 Observation → LLM 自主决策

适合**独立工具**：查订单失败了，LLM 可以提示用户检查订单号，不需要终止整个 Agent。

**AbortOnError**：任意工具失败 → 立即终止 Agent Loop

适合**链式依赖**：取消订单前必须先查订单状态，查询失败了就不能继续取消（避免盲目操作）。

```go
// 普通查询用默认策略
ag.Run(ctx, msgs, agent.WithUser("USER-001"))

// 取消订单用 AbortOnError，依赖链不能中断
ag.Run(ctx, msgs,
    agent.WithUser("ADMIN-001"),
    agent.WithErrorStrategy(agent.AbortOnError),
)
```

### 5. RunOption 保持向后兼容

不修改 `Run` 的基础签名，通过可变参数注入运行时配置：

```go
// 旧代码无需修改
answer, history, err := ag.Run(ctx, messages)

// 新代码按需添加选项
answer, history, err := ag.Run(ctx, messages,
    agent.WithUser(userID),
    agent.WithGate(myGate),
    agent.WithErrorStrategy(agent.AbortOnError),
)
```

---

## 构建的模块

```
agent/
├── decode.go       — 泛型解码层：DecodeInput[T]、DecodeAndValidate[T]、Validator 接口
├── tool.go         — NewTypedTool[T]：类型安全工具创建（在 Lesson 03 基础上扩展）
├── permission.go   — Gate 接口 + AllowAll / DenyAll / RoleGate / UserGate
└── agent.go        — ErrorStrategy、RunOption（WithUser/WithGate/WithErrorStrategy）、AgentOption
```

---

## 核心设计思想总结

| 设计点 | 实现方式 | 原因 |
|--------|----------|------|
| 消除样板代码 | 泛型 `DecodeAndValidate[T]` | 工具函数只写业务逻辑，不写 JSON 解析 |
| 零侵入校验 | `Validator` 接口 | 实现即触发，不实现即跳过，无需修改框架代码 |
| 权限控制 | `Gate` 接口 + `filterDefinitions` | LLM 只看到被允许的工具，从源头防越权 |
| 错误策略 | `ContinueOnError` / `AbortOnError` | 独立工具 vs 链式依赖，两种场景各有最优解 |
| 向后兼容 | `RunOption` 函数式选项（可变参数） | 不破坏已有调用方 |

---

## 课后作业（已完成）

- ✅ **统一解码层**：`decode.go` + `NewTypedTool[T]`，order_agent 所有工具迁移完成
- ✅ **权限控制**：`RoleGate`，USER-001 不可访问 `cancel_order`，ADMIN-001 可访问
- ✅ **单元测试**：`decode_test.go`（7个）、`permission_test.go`（12个），全部通过
- ✅ **作业1（AbortOnError）**：3个测试覆盖单工具失败终止、多工具并发失败、ContinueOnError继续循环
- ✅ **作业2（复合Validator）**：`CancelOrderReq` 增加 order_id 格式校验（`^ORDER-\d+$`）和 reason 长度校验（≤100字符）；`validator_test.go` 8个测试含边界值
- ✅ **作业3（UserGate）**：`UserGate` 实现（按 userID 白名单，未配置用户拒绝）；7个测试含接口合规编译期验证

---

## 运行方式

```bash
# 普通用户（只能查询，不能取消）
go run ./examples/order_agent/

# 管理员（可以取消订单）
ROLE=admin go run ./examples/order_agent/
```

---

## 前四课能力图谱

完成 Lesson 01~04 后，已具备构建完整 Agent 的所有基础能力：

```
Lesson 01：LLM Client
    ↓ 提供 HTTP 调用、重试、并发控制、流式输出
Lesson 02：Session 管理
    ↓ 提供多轮对话记忆、截断、持久化
Lesson 03：Agent Loop
    ↓ 提供 ReAct 循环、工具注册与执行、并发工具调用
Lesson 04：工具集成
    ↓ 提供类型安全工具、权限控制、错误策略
         ↓
  具备构建生产级 Agent 的完整能力
```
