# Lesson 05：RAG 知识库接入

## 核心概念

**RAG（Retrieval-Augmented Generation）= 检索 + 生成**

LLM 本身的知识是训练时固化的，无法感知业务数据。RAG 的本质是：**先检索相关文档，再把文档塞给 LLM 作为参考，让 LLM 基于事实回答**。

```
用户问题
   ↓
向量化（Embedding）
   ↓
向量数据库检索（相似度搜索）
   ↓
相关文档片段
   ↓
LLM 基于文档回答
```

---

## 两个关键角色（最重要的概念）

| 角色 | 做什么 | 本课使用 |
|------|--------|----------|
| **Embedding 模型** | 把文字翻译成向量（数字数组） | 千问 text-embedding-v3 |
| **向量数据库** | 存储向量、做相似度搜索 | Qdrant（本地 Docker）|

**Qdrant 只认数字，不认文字**——这是 RAG 系统必须有 Embedding 模型的根本原因。

语义相似的文字会被 Embedding 模型转换成数值上接近的向量，这就是语义搜索的本质：

```
"退款要多久？"    → [0.12, 0.87, ...]  ←→ 距离很近（同一概念）
"返款需要几天？"  → [0.11, 0.85, ...]  ↗
"苹果怎么削皮？"  → [0.78, -0.23, ...] ← 距离很远（不同概念）
```

---

## 构建的模块

```
rag/
├── chunker.go    切片：长文档 → 固定大小块（带重叠窗口，防语义截断）
├── embedder.go   向量化：文字 → 向量（QwenEmbedder，OpenAI 兼容协议）
├── store.go      存储：VectorStore 接口 + QdrantStore（REST API）
├── pipeline.go   索引流程：切片 → 批量向量化 → 写入（内容哈希 ID 保证幂等）
├── retriever.go  检索：问题向量化 → 相似度搜索 → 组装上下文
└── tool.go       Agent 工具：NewSearchKBTool，把 Retriever 包装成 Function Calling 工具
```

### Chunker（切片器）

为什么需要切片？LLM 有上下文长度限制，一篇长文档无法整体塞入。

为什么需要重叠（Overlap）？如果一句话恰好跨在两个块的边界，没有重叠时这句话会被截断，导致两个块都无法完整表达这个语义。重叠区域保证边界处的语义不丢失。

```go
// chunkSize=500, overlap=50（约 10% 重叠）
chunker := rag.NewFixedSizeChunker(500, 50)
```

### Pipeline（索引流程）

```go
// 建索引：切片 → 批量向量化 → 写入 Qdrant
pipeline.IndexText(ctx, "退款退货流程\n操作步骤...", "退款退货流程")
```

内容哈希 ID：相同内容重复 Index 不产生重复条目（幂等），可以放心反复运行。

### Retriever（检索器）

```go
// topK=3：返回最相似的 3 个文档块
// WithMinScore(0.5)：过滤相似度低于 0.5 的结果
retriever := rag.NewRetriever(embedder, store, 3, rag.WithMinScore(0.5))

context, hits, err := retriever.Retrieve(ctx, "退款多久到账")
// context → 拼装好的参考资料字符串，直接注入 Prompt
// hits    → 带 Score 的原始结果，可用于调试
```

---

## 三个作业的设计思想

### 作业1：相似度阈值过滤

**问题：** Top-K 保证数量，不保证质量。低相关度的结果注入 Prompt 会干扰 LLM 判断。

**解决：** `WithMinScore` 函数式选项，过滤掉 Score 低于阈值的结果。

```go
// 推荐值：Cosine 相似度 0.5~0.7
retriever := rag.NewRetriever(embedder, store, 3, rag.WithMinScore(0.6))
```

- 不传 `WithMinScore` 则默认不过滤，完全向后兼容
- `filterByScore` 返回新 slice，不修改原始数据（不可变原则）

### 作业2：RAG Tool 集成（架构升级）

**旧方案（每次强制检索）：**
```
用户输入 → 强制检索 → 注入上下文 → LLM 回答
```

**新方案（Agent Loop + Tool）：**
```
用户输入 → LLM 判断 → 决定是否调用 search_knowledge_base → LLM 回答
```

| 对比点 | 旧方案 | 新方案 |
|--------|--------|--------|
| 检索时机 | 每次用户输入强制检索 | LLM 自主决定 |
| "你好"这类问题 | 也浪费一次 Embedding 调用 | 直接回答 |
| Token 消耗 | 每条消息都注入 RAG 上下文 | 只在需要时才有检索结果 |
| 可扩展性 | 写死逻辑 | 可以继续添加其他工具 |

把 RAG 包装成 Agent Tool，是从"管道"思维升级到"代理"思维的关键一步。

```go
// rag/tool.go：一行代码注册知识库检索工具
registry.Register(rag.NewSearchKBTool(retriever))
```

### 作业3：httptest mock 测试

`httptest.NewServer` 的价值：让测试完全离线，不消耗真实 API 额度，还能精确控制各种异常响应。

**最核心的测试——乱序响应：**

```go
// API 不保证返回顺序，故意让 mock server 返回 index=1 在前
// 验证 Embed 按输入顺序还原：result[0] ↔ texts[0]，result[1] ↔ texts[1]
func TestQwenEmbedder_BatchOrder_ResponseOutOfOrder(t *testing.T) { ... }
```

这是代码里 `vectors[d.Index] = d.Embedding` 这行设计的价值所在——如果改成按响应顺序写入，这个 bug 在大多数情况下不会暴露，只有 API 真正返回乱序时才会出现。

**9 个测试覆盖点：**

| 测试 | 验证的代码分支 |
|------|---------------|
| EmptyInput / EmptySlice | 短路返回，不发 HTTP 请求 |
| SingleInput | 正常路径 |
| BatchOrder_ResponseOutOfOrder | 乱序响应按 index 还原顺序 |
| APIError | `result.Error != nil` 分支 |
| InvalidJSON | `json.Decode` 失败分支 |
| ServerDown | `httpClient.Do` 失败分支 |
| IndexOutOfRange | `d.Index >= len(vectors)` 越界分支 |
| SendsCorrectRequest | 请求内容和 Authorization header |

---

## 贯穿本课的设计原则

**1. 接口隔离**

`EmbedderInterface` + `VectorStore` 接口，测试可 mock，Provider 可替换：

```go
// 只需改 BaseURL，无需改任何业务代码
embedder := rag.NewQwenEmbedder(apiKey,
    rag.WithEmbedBaseURL("http://localhost:11434/v1"), // 切换到本地 Ollama
    rag.WithEmbedModel("nomic-embed-text"),
    rag.WithEmbedDimensions(768),
)
```

**2. 幂等 Indexing**

内容哈希 ID：`sha256(source + index + content)`，相同内容重复写入 Qdrant 会 upsert（覆盖）而非追加。可以放心每次启动都重建索引。

**3. 批量优化**

一次 API 调用处理所有 chunk，比逐条调用节省约 70% 延迟：

```go
// pipeline.go 内部：一次 Embed 调用处理整篇文档的所有 chunk
vectors, err := p.embedder.Embed(ctx, texts) // texts = 所有 chunk 的内容
```

**4. 不可变原则**

`filterByScore` 返回新 slice，不修改原始 hits：

```go
func filterByScore(hits []SearchResult, minScore float32) []SearchResult {
    result := make([]SearchResult, 0, len(hits)) // 新 slice
    for _, h := range hits {
        if h.Score >= minScore {
            result = append(result, h)
        }
    }
    return result
}
```

---

## 本地运行方式

```bash
# 1. 启动 Qdrant
docker run -d -p 6333:6333 --name qdrant qdrant/qdrant

# 2. 在 .env 中设置 API Key
DASHSCOPE_API_KEY=sk-xxxx

# 3. 运行 RAG Agent
go run ./examples/rag_agent/

# 4. 查看 Qdrant 中的数据
# 浏览器打开：http://localhost:6333/dashboard
# 或 curl：
curl -s -X POST http://localhost:6333/collections/order_faq/points/scroll \
  -H "Content-Type: application/json" \
  -d '{"limit": 10, "with_payload": true, "with_vector": false}'

# 5. 跑测试
go test ./rag/... -v
```

---

## 下一课预告

**Lesson 06：完整客服系统 + 生产部署**

把前五节课的所有组件整合：
- LLM Client（Lesson 01）
- 多轮会话管理（Lesson 02）
- Agent Loop / ReAct（Lesson 03）
- 权限控制 + 工具解码（Lesson 04）
- RAG 知识库（Lesson 05）

生产部署需要考虑的问题：并发处理、可观测性（日志/监控）、知识库热更新。
