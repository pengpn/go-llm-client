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
| [Lesson 08](lesson08-auth.md) | 身份认证（API Key / JWT） | 📋 |
| [Lesson 09](lesson09-docker.md) | 容器化部署（Docker + docker-compose） | 📋 |
| [Lesson 10](lesson10-human-in-the-loop.md) | Human-in-the-loop（人工转接） | 📋 |
| [Lesson 11](lesson11-evaluation.md) | 评估体系（AI Quality Evaluation） | 📋 |
| [Lesson 12](lesson12-multi-agent.md) | 多 Agent 协作 | 📋 |

## 核心能力依赖关系

```
Lesson 01 LLM Client
    └── Lesson 02 Session 管理
            └── Lesson 03 Agent Loop
                    └── Lesson 04 工具集成
                            └── Lesson 05 RAG 知识库
                                    └── Lesson 06 完整系统
                                            ├── Lesson 07 流式响应
                                            ├── Lesson 08 身份认证
                                            ├── Lesson 09 容器化部署
                                            ├── Lesson 10 人工转接
                                            ├── Lesson 11 评估体系
                                            └── Lesson 12 多 Agent 协作
```
