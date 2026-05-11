# Lesson 10：Human-in-the-loop（人工转接）

## 为什么要学

生产客服系统的铁律：AI 不确定时必须转人工。
纯 AI 兜底有风险，尤其是投诉、退款纠纷、法律问题等敏感场景。

## 学习目标

- 设计 `transfer_to_human` 工具，让 Agent 自主判断何时转接
- 实现工单系统：转接时生成工单，记录对话上下文
- 理解人机协作的工作流：AI 先处理 → 超出能力范围 → 人工接手
- 掌握 WebSocket 或轮询实现人工客服实时接入

## 计划内容

### 原理

AI 什么时候应该转人工？
- 用户明确要求（"我要找人工"）
- 知识库检索无结果且问题复杂
- 涉及退款金额争议、法律投诉等高风险场景
- 多轮对话后用户明显不满意

### 要实现的东西

```go
// transfer_to_human 工具
type TransferReq struct {
    Reason  string `json:"reason"`   // 转接原因
    Summary string `json:"summary"`  // 对话摘要（给人工客服看）
}

// 工具执行：生成工单 ID，返回给用户
// "您好，已为您生成工单 #TK-20240101-001，人工客服将在 5 分钟内联系您"
```

工单数据库（PostgreSQL）：
```sql
CREATE TABLE tickets (
    id          SERIAL PRIMARY KEY,
    user_id     TEXT NOT NULL,
    reason      TEXT,
    summary     TEXT,          -- AI 生成的对话摘要
    session_id  TEXT,          -- 关联的 Session，人工可查看历史
    status      TEXT DEFAULT 'pending',  -- pending/assigned/resolved
    created_at  TIMESTAMPTZ DEFAULT NOW()
);
```

### 课后作业（预计）
- 作业1：实现 `transfer_to_human` 工具 + 工单存储
- 作业2：`GET /tickets` 接口，人工客服查看待处理工单
- 作业3：Agent 在知识库无结果时自动触发转接（prompt 设计）
