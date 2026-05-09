package rag

import (
	"context"
	"crypto/sha256"
	"fmt"
)

// Pipeline 串联 Indexing 全流程：切片 → 向量化 → 存储。
// 使用方只需调用 IndexText，内部自动完成所有步骤。
type Pipeline struct {
	chunker  Chunker
	embedder EmbedderInterface
	store    VectorStore
}

// NewPipeline 创建 RAG Indexing Pipeline。
func NewPipeline(chunker Chunker, embedder EmbedderInterface, store VectorStore) *Pipeline {
	return &Pipeline{
		chunker:  chunker,
		embedder: embedder,
		store:    store,
	}
}

// IndexText 对单篇文档执行完整 Indexing 流程：
// 1. 切片  2. 批量向量化  3. 写入向量库
//
// source 是文档来源标识（如文件名），写入 meta 便于后续溯源。
func (p *Pipeline) IndexText(ctx context.Context, text, source string) error {
	// Step 1: 切片
	chunks := p.chunker.Chunk(text, map[string]string{"source": source})
	if len(chunks) == 0 {
		return nil
	}

	// Step 2: 批量向量化（一次 API 调用处理所有 chunk）
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}

	vectors, err := p.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("批量向量化失败: %w", err)
	}
	if len(vectors) != len(chunks) {
		return fmt.Errorf("向量数量 %d 与 chunk 数量 %d 不匹配", len(vectors), len(chunks))
	}

	// Step 3: 组装 Document 并写入向量库
	docs := make([]Document, len(chunks))
	for i, chunk := range chunks {
		docs[i] = Document{
			// ID 用内容哈希：相同内容重复 Index 不产生重复条目（幂等）
			ID:      contentID(source, i, chunk.Content),
			Content: chunk.Content,
			Meta:    chunk.Meta,
			Vector:  vectors[i],
		}
	}

	if err := p.store.Index(ctx, docs); err != nil {
		return fmt.Errorf("写入向量库失败: %w", err)
	}
	return nil
}

// contentID 生成文档块的确定性 ID。
// 基于内容哈希：相同内容产生相同 ID，Qdrant upsert 会覆盖而非追加。
// 这是 RAG 系统实现幂等 Indexing 的标准做法。
func contentID(source string, index int, content string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", source, index, content)))
	// Qdrant 支持 UUID 或 uint64，这里用截断的 hex 字符串模拟 UUID 格式
	hex := fmt.Sprintf("%x", h)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hex[0:8], hex[8:12], hex[12:16], hex[16:20], hex[20:32])
}
