# 课程规划

## 概览

本课程分三个阶段，共 20 节课，目标是从零构建一个生产级 AI Agent 客服系统。

| 阶段 | 课程 | 目标 |
|------|------|------|
| 基础构建 | Lesson 01-06 | 从 LLM Client 到完整客服系统 |
| 工程加固 | Lesson 07-13 | 流式响应、认证、部署、评估、多Agent、结构化输出 |
| 进阶深化 | Lesson 14-20 | 记忆、安全、可观测、缓存、自主RAG、规划Agent、生产部署 |

## 第一阶段：基础构建（已完成）

| 课程 | 主题 | 核心能力 |
|------|------|----------|
| ✅ Lesson 01 | 生产级 LLM Client | 重试、并发控制、多Provider |
| ✅ Lesson 02 | 多轮对话管理 | Session、截断策略、持久化 |
| ✅ Lesson 03 | Agent Loop（ReAct） | 工具调用循环、并发执行 |
| ✅ Lesson 04 | 工具集成深入 | 泛型解码、权限控制、错误策略 |
| ✅ Lesson 05 | RAG 知识库 | Embedding、向量检索、Indexing Pipeline |
| ✅ Lesson 06 | 完整客服系统 | HTTP Server、限流、优雅关闭 |

## 第二阶段：工程加固（已完成）

| 课程 | 主题 | 核心能力 |
|------|------|----------|
| ✅ Lesson 07 | 流式响应（SSE） | 分阶段流式、客户端断连传播 |
| ✅ Lesson 08 | 身份认证 | API Key → JWT、算法混淆防御 |
| ✅ Lesson 09 | 容器化部署 | 多阶段Docker、docker-compose编排 |
| ✅ Lesson 10 | 人工转接 | 工具触发转接、context传递userID |
| ✅ Lesson 11 | 评估体系 | LLM-as-Judge、四维度打分、A/B对比 |
| ✅ Lesson 12 | 多Agent协作 | Router分发、并行子任务、置信度降级 |
| ✅ Lesson 13 | 结构化输出 | Function Calling提取、Schema校验、自动重试 |

## 第三阶段：进阶深化

### Lesson 14：Memory & Conversation Summary（长期记忆）
**难度：** ★★☆☆☆

**目标：**
- 对话摘要压缩：历史超长时，LLM 自动总结前文，压缩后继续对话
- 用户画像记忆：跨会话记住用户偏好（如常用地址、购买习惯）
- 记忆存储与检索：何时写入、何时召回、过期淘汰

**实战：** 回头客识别——新会话自动加载上次对话摘要和用户画像

**计划文件：**
```
session/summary.go       — 对话摘要压缩器（LLM 自动总结）
session/memory.go        — 用户画像记忆（跨会话持久化）
session/memory_test.go   — 单元测试
```

**预计作业：**
- 作业1：实现 `SummaryStrategy`，当消息数超过阈值时自动压缩历史
- 作业2：实现 `UserMemory`（key-value 画像存储），跨会话持久化
- 作业3：在客服系统中集成——新会话自动注入上次摘要

---

### Lesson 15：Guardrails（安全护栏）
**难度：** ★★★☆☆

**目标：**
- 输入审查：检测 Prompt 注入攻击、越狱尝试
- 输出审查：过滤敏感信息（手机号、身份证号、内部数据）
- 话题边界：防止 LLM 回答超出业务范围的问题

**实战：** 防止 LLM 泄露 System Prompt、数据库连接信息或用户隐私

**计划文件：**
```
guardrail/
├── input.go       — 输入审查器（注入检测、敏感词过滤）
├── output.go      — 输出审查器（PII脱敏、话题边界）
├── chain.go       — 审查链（多个审查器串联）
└── *_test.go      — 单元测试
```

**预计作业：**
- 作业1：实现输入审查——检测 "忽略之前的指令" 类注入模式
- 作业2：实现输出审查——正则脱敏手机号/邮箱，拦截 System Prompt 泄露
- 作业3：集成到 Server 中间件——所有 /chat 请求自动过审查链

---

### Lesson 16：Observability（可观测性）
**难度：** ★★★☆☆

**目标：**
- Trace：每次请求的完整链路（Router → SubAgent → Tool → LLM）
- Metrics：Token 消耗、延迟分布、工具调用成功率
- OpenTelemetry 集成：标准化遥测数据输出

**实战：** 给 Agent 加全链路追踪，输出到 Jaeger 可视化

**计划文件：**
```
observe/
├── trace.go       — Span 追踪（LLM调用、工具执行、路由分发）
├── metrics.go     — 指标收集器（Token、延迟、成功率）
├── middleware.go   — Agent/Server 中间件
└── *_test.go      — 单元测试
```

**预计作业：**
- 作业1：实现 `Tracer` 接口，记录 Agent 每步的输入/输出/耗时
- 作业2：实现 `MetricsCollector`，统计 Token 消耗和延迟 P50/P99
- 作业3：OpenTelemetry 集成，导出到 Jaeger

---

### Lesson 17：Caching & Cost Control（缓存与成本控制）
**难度：** ★★★☆☆

**目标：**
- 语义缓存：相似问题复用已有回答（embedding 相似度匹配）
- Token 预算控制：单请求/单用户/全局三层限额
- 成本监控：实时统计各模型/用户的费用

**实战：** 加入语义缓存，FAQ 类问题命中率 > 60%，LLM 调用量减半

**计划文件：**
```
cache/
├── semantic.go    — 语义缓存（embedding + 相似度阈值）
├── budget.go      — Token 预算控制（三层限额）
└── *_test.go      — 单元测试
```

**预计作业：**
- 作业1：实现语义缓存——问题 embedding → 相似度搜索 → 命中则直接返回
- 作业2：实现 Token 预算——单请求 4096、单用户日限 100K、全局日限 1M
- 作业3：缓存命中率监控——统计命中/未命中次数，输出报告

---

### Lesson 18：Agentic RAG（自主检索增强）
**难度：** ★★★★☆

**目标：**
- 查询改写：LLM 先改写用户问题再检索（提升召回率）
- 多步检索：第一次不够 → LLM 决定补充检索
- Self-RAG：LLM 自评检索结果质量，决定是否重新检索

**实战：** 升级 RAG Pipeline，对比改写前后召回率

**计划文件：**
```
rag/
├── rewriter.go    — 查询改写器（LLM 重写用户问题）
├── self_rag.go    — Self-RAG 循环（检索 → 评估 → 决策）
└── *_test.go      — 单元测试
```

**预计作业：**
- 作业1：实现查询改写——用 LLM 将口语化问题改写为检索友好的关键词组合
- 作业2：实现多步检索——检索结果不足时 LLM 生成补充查询
- 作业3：对比评估——在 eval 体系中跑改写前后的准确率差异

---

### Lesson 19：Planning Agent（规划型 Agent）
**难度：** ★★★★☆

**目标：**
- Plan-and-Execute：先生成计划（子任务列表），再逐步执行
- 动态重规划：某步失败时 LLM 调整后续计划
- 与 ReAct 对比：ReAct 是边想边做，Plan-and-Execute 是先想后做

**实战：** 复杂售后处理（检查订单 → 核实物流 → 计算退款 → 生成方案）

**计划文件：**
```
agent/
├── planner.go     — Plan-and-Execute Agent
├── plan.go        — Plan 数据结构（步骤列表、状态追踪）
└── planner_test.go — 单元测试
```

**预计作业：**
- 作业1：实现 `Planner`——LLM 生成子任务列表，逐步调用工具执行
- 作业2：实现动态重规划——某步失败时 LLM 输出修正后的计划
- 作业3：对比 ReAct vs Plan-and-Execute 在复杂任务上的完成率

---

### Lesson 20：Production Deployment（生产部署 + 毕业项目）
**难度：** ★★★★★

**目标：**
- 配置中心：System Prompt / 模型 / 参数热更新（不重启服务）
- A/B 测试框架：不同 Prompt 版本灰度切流
- 降级策略：LLM 不可用时的兜底方案
- 整合所有 Lesson 为可交付的生产系统

**实战：** 毕业项目——完整的智能客服系统，包含所有已学模块

**计划文件：**
```
deploy/
├── config_center.go   — 配置热更新
├── ab_test.go         — A/B 测试分流
├── fallback.go        — LLM 降级策略
└── *_test.go          — 单元测试
```

**预计作业：**
- 作业1：配置热更新——`PUT /config` 动态修改 System Prompt 和模型
- 作业2：A/B 测试——按 user_id hash 分流，记录各版本指标
- 作业3：毕业项目——整合全部模块，输出架构文档和部署手册

---

## 能力依赖关系

```
第一阶段（基础）          第二阶段（加固）          第三阶段（进阶）
─────────────          ─────────────          ─────────────
01 LLM Client          07 SSE Streaming       14 Memory ← 02 Session
  └→ 02 Session        08 JWT Auth            15 Guardrails
      └→ 03 Agent      09 Docker              16 Observability
          └→ 04 Tools   10 Human-in-loop       17 Cache ← 05 RAG
              └→ 05 RAG  11 Evaluation          18 Agentic RAG ← 05 RAG
                  └→ 06  12 Multi-Agent ← 03    19 Planning ← 03 Agent
                         13 Structured ← 04     20 Production ← ALL
```

## 学完能做什么

完成全部 20 节课后，你将拥有：
1. **一个生产级 AI 客服系统**——可直接部署上线
2. **Go AI Agent 开发的完整技能树**——从 LLM 调用到分布式部署
3. **可复用的组件库**——client、agent、rag、structured、cache、observe 各包独立可用
4. **面试/简历加分项**——能讲清楚每个设计决策的 why
