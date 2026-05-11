package rag

import (
	"context"
	"fmt"
	"strings"
)

// Retriever 封装"向量化问题 → 检索相关文档 → 组装上下文"全流程。
// 业务代码只需调用 Retrieve，不需要感知 Embedder 和 VectorStore 的存在。
type Retriever struct {
	embedder EmbedderInterface
	store    VectorStore
	topK     int
	minScore float32 // 相似度阈值，低于此值的结果被过滤；0 表示不过滤
}

// EmbedderInterface 是 Embedder 的接口别名，方便测试 mock。
type EmbedderInterface interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// RetrieverOption 函数式选项，和 EmbedderOption 风格一致。
type RetrieverOption func(*Retriever)

// WithMinScore 设置相似度过滤阈值（0~1）。
// 低于阈值的结果不会进入 Prompt，防止不相关内容干扰 LLM。
// 推荐值：0.5~0.7（Cosine 相似度）。不设置或设为 0 则不过滤。
func WithMinScore(score float32) RetrieverOption {
	return func(r *Retriever) { r.minScore = score }
}

// NewRetriever 创建检索器。topK 控制返回文档数量，通常 3~5 即可。
// topK 太大：Prompt 过长，LLM 容易被无关信息干扰。
// topK 太小：可能漏掉关键信息。
func NewRetriever(embedder EmbedderInterface, store VectorStore, topK int, opts ...RetrieverOption) *Retriever {
	if topK <= 0 {
		topK = 3
	}
	r := &Retriever{embedder: embedder, store: store, topK: topK}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Retrieve 检索与 query 最相关的文档，返回拼装好的上下文字符串。
func (r *Retriever) Retrieve(ctx context.Context, query string) (string, []SearchResult, error) {
	// Step 1: 将用户问题向量化
	vectors, err := r.embedder.Embed(ctx, []string{query})
	if err != nil {
		return "", nil, fmt.Errorf("问题向量化失败: %w", err)
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return "", nil, fmt.Errorf("embedding 返回空向量")
	}

	// Step 2: 在向量库中检索最相似的文档块
	hits, err := r.store.Search(ctx, vectors[0], r.topK)
	if err != nil {
		return "", nil, fmt.Errorf("向量检索失败: %w", err)
	}

	// Step 3: 相似度阈值过滤
	// 原因：Top-K 保证数量，但不保证质量；低分结果注入 Prompt 会干扰 LLM 判断
	if r.minScore > 0 {
		hits = filterByScore(hits, r.minScore)
	}

	// Step 4: 拼装上下文
	// 格式化为编号列表，方便 LLM 引用具体条目
	context := buildContext(hits)
	return context, hits, nil
}

// filterByScore 过滤掉相似度低于阈值的结果。
// 保留原始 slice 不变，返回新 slice（不可变原则）。
func filterByScore(hits []SearchResult, minScore float32) []SearchResult {
	result := make([]SearchResult, 0, len(hits))
	for _, h := range hits {
		if h.Score >= minScore {
			result = append(result, h)
		}
	}
	return result
}

// buildContext 将检索结果拼装成结构化上下文字符串。
// 编号格式让 LLM 能在回答中引用"根据第X条"，增加可解释性。
func buildContext(hits []SearchResult) string {
	if len(hits) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, hit := range hits {
		fmt.Fprintf(&sb, "[%d] %s\n\n", i+1, hit.Document.Content)
	}
	return strings.TrimSpace(sb.String())
}
