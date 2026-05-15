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

## 关键文件

| 文件 | 作用 |
|------|------|
| `agent/router.go` | Router Agent：意图分类 + 分发 + 并行执行 |
| `agent/router_test.go` | 19 个测试（解析/路由/fallback/置信度/并行） |
| `examples/multi_agent/main.go` | 四路由客服 Demo |

## 核心设计决策

| 决策 | 选择 | 为什么 |
|------|------|--------|
| Router 用 LLM vs 规则 | LLM | 语义理解，"我买的东西到哪了" → logistics |
| Sub-Agent 复用 vs 新建 | 工厂函数新建 | 避免跨请求状态污染 |
| 意图匹配大小写 | 不敏感 | LLM 输出不可控 |
| 置信度低时 | fallback 兜底 | 不报错，降级到 FAQ |
| RunOption 传递 | 透传给 Sub-Agent | 权限控制在子层生效 |

## 课后作业

- ✅ 作业2：Intent 增加 `Confidence` 字段 + `WithConfidenceThreshold`，低于阈值自动走 fallback
- ✅ 作业3：`RouteParallel` 并行分发 + `parseIntents` 多意图解析 + `deduplicateIntents` 去重 + `mergeAnswers` 合并

## 总结

多 Agent 的本质是**分治**：每个 Sub-Agent 只管自己领域的 2-3 个工具，比单 Agent 挂 20 个工具选错率低得多。
Router 是轻量的"调度器"——一次 LLM 调用判断意图，成本可忽略。
置信度机制是"不确定就交给通用 FAQ"，比硬匹配失败返回错误更友好。
