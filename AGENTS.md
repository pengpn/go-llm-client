# 教学上下文 — go-llm-agent

## 我是谁
Go 后端工程师，正在系统学习 AI Agent 开发，目标是构建对话助手/客服系统。
了解 RAG、Function Calling 等基础概念，Go 为主，可兼学 Python。

## 教学约定
- 你是我的老师，我是学生，**先讲原理再写代码**
- 每个设计决策说明"为什么这样做"
- 代码要生产可用，不要玩具 Demo
- 每节课结束给出课后作业

## 课程进度

### ✅ Lesson 01：生产级 LLM Client
**已完成内容：**
- `client/client.go` — 核心 Client，支持自动重试（指数退避）、并发控制（信号量）
- `client/stream.go` — SSE 流式输出，channel 传递
- `client/options.go` — 函数式选项模式，多 Provider 预设（OpenAI/智谱/通义/Ollama）
- `client/cost.go` — Token 费用计算器，Mutex 并发安全
- `models/message.go` — Message / Usage / Response 数据结构

**核心设计思想：**
- OpenAI 兼容协议：只换 BaseURL 切换任意 Provider
- 指数退避重试：1s/2s/4s，区分可重试错误（429/5xx）
- 信号量控制并发，防止触发 API Rate Limit

---

### ✅ Lesson 02：多轮对话管理
**已完成内容：**
- `session/session.go` — 单个会话，维护消息历史，自动截断
- `session/truncate.go` — 两种截断策略：按轮数、按 Token 数估算
- `session/manager.go` — 多用户 Session 管理，TTL 过期清理，并发安全
- `examples/chatbot/main.go` — 完整命令行客服 Demo

**核心设计思想：**
- LLM 本身无状态，记忆靠每次请求携带历史
- System Prompt 永远不截断
- Session 与 Client 解耦，一个 Client 服务多个 Session
- double-check 防并发重复创建

**遗留作业（未完成可在此继续）：**
- Session JSON 序列化持久化到文件
- 精确 Token 计算（tiktoken）

---

### 🔜 Lesson 03：Agent Loop（ReAct 模式）— 下一课
**计划内容：**
- ReAct 框架：Reasoning + Acting 循环
- Tool 定义与注册
- 用 Go 手写最小 Agent
- 实战：能查询订单状态的 Agent

---

### 待完成课程
| 课程 | 主题 |
|------|------|
| Lesson 04 | 工具集成（Function Calling 深入） |
| Lesson 05 | RAG 知识库接入 |
| Lesson 06 | 完整客服系统 + 生产部署 |

---

## 技术栈
- **语言**：Go（主）+ Python（原型验证）
- **LLM**：OpenAI API / 智谱 GLM / 通义千问 / 本地 Ollama
- **向量库**：待定（Qdrant / pgvector）
- **框架**：原生实现为主，参考 langchaingo

## 代码规范
- 错误处理：`fmt.Errorf("操作失败: %w", err)` wrap 原始错误
- 并发：共享状态必须加锁，优先用 channel 传递数据
- 配置：函数式选项模式（`WithXxx`）
- 注释：中文注释，说明"为什么"而不只是"是什么"

## 仓库地址
https://github.com/pengpn/go-llm-agent