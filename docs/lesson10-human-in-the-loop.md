# Lesson 10：Human-in-the-loop（人工转接）

## 为什么需要人工介入？

AI Agent 有三类能力边界：

| 类型 | 典型场景 | 后果（若不转接）|
|------|----------|----------------|
| **知识边界** | FAQ 里没有答案 | 模型幻觉，给出错误信息 |
| **权限边界** | 退款金额超过规定上限 | 越权操作 |
| **情感边界** | 用户激烈投诉、涉及法律 | 激化矛盾，品牌风险 |

---

## 设计原则：工具调用，而非硬编码规则

❌ **错误做法**（脆弱）：
```go
if strings.Contains(msg, "投诉") || strings.Contains(msg, "律师") {
    transferToHuman()
}
```

✅ **正确做法**（灵活）：
给 Agent 一个 `transfer_to_human` 工具，用 System Prompt 描述触发条件，让 LLM 自主判断。

LLM 可以理解语义，处理"我要找你们老板说话"（未出现"投诉"关键字）这类情况。

---

## 工单生命周期

```
用户问题无法解决
      ↓
Agent 调用 transfer_to_human(reason, summary)
      ↓                               ↑
  工具函数                       context 注入 userID
  从 context 读取 userID         (processChat 在调用 ag.Run 前注入)
      ↓
  store.Create(userID, reason, summary)
      ↓
  返回 "已生成工单 #TK-0001"
      ↓
Agent 把工单号告知用户
      ↓
人工客服 GET /tickets?status=pending  查看待处理工单
```

---

## 关键设计：userID 通过 context 传递

工具函数签名是 `func(ctx context.Context, input T) (string, error)`，不接受 userID 参数。

**解决方案：在调用 `ag.Run` 前把 userID 注入 context**

```go
// handler.go — processChat
ctx := ContextWithUserID(c.Request.Context(), userID)
answer, history, err := s.ag.Run(ctx, msgs)

// ticket.go — transfer_to_human 工具内部
func(ctx context.Context, input TransferInput) (string, error) {
    userID := userIDFromContext(ctx)  // 从 context 读取
    ticket := store.Create(userID, input.Reason, input.Summary)
    ...
}
```

`context.WithValue` 是 Go 传递请求作用域值的标准方式。使用私有类型 `userIDContextKey{}` 作为 key，避免与其他包的 key 冲突。

---

## 依赖共享（同一个 TicketStore）

```go
// main.go
ticketStore := server.NewTicketStore()

registry.Register(server.NewTransferTool(ticketStore))  // 工具写入
srv := server.New(..., server.WithTicketStore(ticketStore))  // 接口读取
```

工具和 HTTP 接口共享同一个 `TicketStore` 实例。工具 `Create` 的工单，`GET /tickets` 马上能看到。

---

## API 接口

### GET /tickets?status=pending

返回待处理工单列表。

**查询参数：**
- `status=pending`（默认）— 只返回待处理工单
- `status=all` — 返回所有工单

**响应：**
```json
{
  "tickets": [
    {
      "id": "TK-0001",
      "user_id": "user-alice",
      "reason": "知识库无答案",
      "summary": "用户询问企业采购折扣，知识库未覆盖",
      "status": "pending",
      "created_at": "2026-05-12T10:00:00Z"
    }
  ],
  "count": 1
}
```

---

## System Prompt 设计

```
遇到以下情况时，必须调用 transfer_to_human 转接人工：
- 知识库中确实没有答案
- 用户明确要求"转人工"或"联系客服"
- 涉及退款金额较大的争议
- 用户情绪激动或提到投诉、法律等词语
```

**Prompt 要点：**
1. 说清楚**什么情况触发**（正例）
2. 说清楚**参数怎么填**（`reason` 简短，`summary` 含背景）
3. 不要禁止太多，让 LLM 有判断空间

---

## 课后作业（已完成）

### 作业1：实现 transfer_to_human 工具 + 工单存储

**实现位置：** `server/ticket.go`

- `TicketStore`：`sync.Mutex` 保护的内存存储，工单 ID 格式 `TK-XXXX`
- `NewTransferTool(store)`：返回可注册到 Agent Registry 的工具
- `ContextWithUserID` / `userIDFromContext`：context 注入/读取

**测试覆盖：**
- ✅ `TestTicketStore_CreateAndList`：ID 递增、状态过滤
- ✅ `TestTransferTool_InjectsUserIDFromContext`：context 注入、工单 userID 正确

### 作业2：GET /tickets 人工查看接口

**实现位置：** `server/handler.go`（`handleListTickets`）、`server/server.go`（路由注册）

- `WithTicketStore(store)` — ServerOption，未配置时路由不注册（返回 404）
- `?status=pending` 默认只返回待处理工单

**测试覆盖：**
- ✅ `TestHandleListTickets_NoTicketStore_Returns404`
- ✅ `TestHandleListTickets_EmptyStore`
- ✅ `TestHandleListTickets_WithTickets`

### 作业3：Prompt 设计，知识库无结果时 Agent 自动触发转接

**实现位置：** `examples/customer_service/main.go`（System Prompt 更新）

触发条件（由 LLM 判断）：
- 知识库搜索无相关结果
- 用户明确要求人工
- 大额退款争议
- 情绪激动/法律威胁

**验证（需真实 LLM）：**
```bash
curl -X POST http://localhost:8080/chat \
  -d '{"user_id":"u1","message":"我要投诉，帮我找你们负责人"}'
# Agent 应调用 transfer_to_human 并返回工单号

curl http://localhost:8080/tickets
# 应看到刚创建的工单
```
