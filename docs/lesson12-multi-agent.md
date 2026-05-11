# Lesson 12：多 Agent 协作

## 为什么要学

随着业务场景扩展，单个 Agent 挂载的工具越来越多，LLM 面对几十个工具时选择准确率会下降。
多 Agent 架构：路由 Agent 判断意图 → 派发给专门的子 Agent 处理，各司其职。

## 学习目标

- 理解多 Agent 架构的适用场景
- 实现 Router Agent（意图识别 + 分发）
- 实现 Sub-Agent 间的上下文传递
- 理解 Agent 间通信的两种模式：直接调用 vs 消息队列

## 计划内容

### 原理

**什么时候需要多 Agent？**
- 工具数量 > 15 个，单 Agent 工具选择准确率明显下降
- 不同业务域有明显边界（订单、物流、售后、账单）
- 需要并行处理多个子任务

**架构模式**：

```
用户问题
    ↓
Router Agent（意图识别）
    ├── "查订单" → Order Agent（get_order, cancel_order）
    ├── "看物流" → Logistics Agent（track_package, estimate_arrival）
    ├── "退款问题" → Refund Agent（apply_refund, check_refund）
    └── "其他" → FAQ Agent（search_knowledge_base）
```

### 两种实现方式

**方式1：直接调用**（简单，适合小规模）
```go
// Router Agent 的工具之一就是"调用子 Agent"
func callOrderAgent(ctx context.Context, req SubAgentReq) (string, error) {
    answer, _, err := orderAgent.Run(ctx, buildMessages(req))
    return answer, err
}
```

**方式2：消息队列**（解耦，适合高并发）
```
Router → 发消息到队列 → Sub-Agent 消费 → 结果回写
```

### 课后作业（预计）
- 作业1：实现 Router Agent，能识别"订单/物流/退款/其他"四类意图
- 作业2：实现 Order Sub-Agent 和 FAQ Sub-Agent
- 作业3：测试跨 Agent 的上下文传递（Router 把用户 ID 传给 Sub-Agent）
