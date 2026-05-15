# 课程笔记

## 课程列表

| 课程 | 主题 | 状态 |
|------|------|------|
| [Lesson 01](lesson01-llm-client.md) | 生产级 LLM Client | ✅ |
| [Lesson 02](lesson02-session.md) | 多轮对话管理 | ✅ |
| [Lesson 03](lesson03-agent-loop.md) | Agent Loop（ReAct 模式） | ✅ |
| [Lesson 04](lesson04-tool-integration.md) | 工具集成（Function Calling 深入） | ✅ |
| [Lesson 05](lesson05-rag.md) | RAG 知识库接入 | ✅ |
| [Lesson 06](lesson06-customer-service.md) | 完整客服系统 + 生产部署 | ✅ |
| [Lesson 07](lesson07-streaming.md) | 流式响应（SSE Streaming） | ✅ |
| [Lesson 08](lesson08-auth.md) | 身份认证（API Key / JWT） | ✅ |
| [Lesson 09](lesson09-docker.md) | 容器化部署（Docker + docker-compose） | ✅ |
| [Lesson 10](lesson10-human-in-the-loop.md) | Human-in-the-loop（人工转接） | ✅ |
| [Lesson 11](lesson11-evaluation.md) | 评估体系（AI Quality Evaluation） | ✅ |
| [Lesson 12](lesson12-multi-agent.md) | 多 Agent 协作 | ✅ |
| [Lesson 13](lesson13-structured-output.md) | Structured Output（结构化输出） | ✅ |
| Lesson 14 | Memory & Conversation Summary（长期记忆） | 📋 |
| Lesson 15 | Guardrails（安全护栏） | 📋 |
| Lesson 16 | Observability（可观测性） | 📋 |
| Lesson 17 | Caching & Cost Control（缓存与成本控制） | 📋 |
| Lesson 18 | Agentic RAG（自主检索增强） | 📋 |
| Lesson 19 | Planning Agent（规划型 Agent） | 📋 |
| Lesson 20 | Production Deployment（生产部署 + 毕业项目） | 📋 |

详细课程规划见 [curriculum.md](curriculum.md)。

## 核心能力依赖关系

```
第一阶段（基础）          第二阶段（加固）          第三阶段（进阶）
─────────────          ─────────────          ─────────────
01 LLM Client          07 SSE Streaming       14 Memory
  └→ 02 Session        08 JWT Auth            15 Guardrails
      └→ 03 Agent      09 Docker              16 Observability
          └→ 04 Tools   10 Human-in-loop       17 Cache
              └→ 05 RAG  11 Evaluation          18 Agentic RAG
                  └→ 06  12 Multi-Agent         19 Planning Agent
                         13 Structured Output   20 Production
```
