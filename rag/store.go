package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Document 是存入向量库的基本单元。
type Document struct {
	ID      string            // 唯一 ID（通常是 UUID 或业务 ID）
	Content string            // 原始文本，检索后直接拼入 Prompt
	Meta    map[string]string // 附加信息：来源、时间等
	Vector  []float32         // 向量（由 Embedder 填充）
}

// SearchResult 是向量检索的单条结果。
type SearchResult struct {
	Document Document
	Score    float32 // 相似度得分（0~1，越高越相似）
}

// VectorStore 向量存储接口。
// 只暴露 Index（写入）和 Search（检索）两个操作，足以支撑 RAG 全流程。
type VectorStore interface {
	// Index 批量写入文档（幂等，同 ID 会覆盖）。
	Index(ctx context.Context, docs []Document) error
	// Search 语义检索，返回 topK 个最相似结果。
	Search(ctx context.Context, vector []float32, topK int) ([]SearchResult, error)
}

// QdrantStore 使用 Qdrant REST API 实现向量存储。
// 选 REST 而非 gRPC SDK 的原因：无需新依赖，和项目现有风格一致。
type QdrantStore struct {
	baseURL    string
	collection string
	dimension  int
	httpClient *http.Client
}

// NewQdrantStore 创建 Qdrant 存储，并确保 collection 存在。
func NewQdrantStore(ctx context.Context, baseURL, collection string, dimension int) (*QdrantStore, error) {
	s := &QdrantStore{
		baseURL:    baseURL,
		collection: collection,
		dimension:  dimension,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
	if err := s.ensureCollection(ctx); err != nil {
		return nil, fmt.Errorf("初始化 Qdrant collection 失败: %w", err)
	}
	return s, nil
}

// ensureCollection 创建 collection（如已存在则跳过）。
// Cosine 距离适合文本语义检索；Dot（内积）适合已归一化的向量。
func (s *QdrantStore) ensureCollection(ctx context.Context) error {
	body, _ := json.Marshal(map[string]any{
		"vectors": map[string]any{
			"size":     s.dimension,
			"distance": "Cosine",
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		s.baseURL+"/collections/"+s.collection, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 200 = 创建成功，409 = 已存在，两者都正常
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusConflict {
		return fmt.Errorf("创建 collection 返回非预期状态码: %d", resp.StatusCode)
	}
	return nil
}

// qdrantPoint 对应 Qdrant REST API 的点结构。
type qdrantPoint struct {
	ID      string         `json:"id"`
	Vector  []float32      `json:"vector"`
	Payload map[string]any `json:"payload"`
}

// Index 批量写入文档到 Qdrant。
// Qdrant 的 upsert 是幂等的：同 ID 覆盖，不同 ID 追加。
func (s *QdrantStore) Index(ctx context.Context, docs []Document) error {
	if len(docs) == 0 {
		return nil
	}

	points := make([]qdrantPoint, len(docs))
	for i, doc := range docs {
		payload := map[string]any{
			"content": doc.Content,
		}
		for k, v := range doc.Meta {
			payload[k] = v
		}
		points[i] = qdrantPoint{
			ID:      doc.ID,
			Vector:  doc.Vector,
			Payload: payload,
		}
	}

	body, err := json.Marshal(map[string]any{"points": points})
	if err != nil {
		return fmt.Errorf("序列化 upsert 请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		s.baseURL+"/collections/"+s.collection+"/points", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("创建 upsert 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("写入 Qdrant 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Qdrant upsert 返回非预期状态码: %d", resp.StatusCode)
	}
	return nil
}

// qdrantSearchRequest 对应 Qdrant 搜索请求体。
type qdrantSearchRequest struct {
	Vector      []float32 `json:"vector"`
	Limit       int       `json:"limit"`
	WithPayload bool      `json:"with_payload"`
}

// qdrantSearchResponse 对应 Qdrant 搜索响应。
type qdrantSearchResponse struct {
	Result []struct {
		ID      string         `json:"id"`
		Score   float32        `json:"score"`
		Payload map[string]any `json:"payload"`
	} `json:"result"`
	Status string `json:"status"`
}

// Search 在向量库中检索最相似的 topK 个文档。
func (s *QdrantStore) Search(ctx context.Context, vector []float32, topK int) ([]SearchResult, error) {
	body, err := json.Marshal(qdrantSearchRequest{
		Vector:      vector,
		Limit:       topK,
		WithPayload: true,
	})
	if err != nil {
		return nil, fmt.Errorf("序列化搜索请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.baseURL+"/collections/"+s.collection+"/points/search", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建搜索请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 Qdrant 搜索失败: %w", err)
	}
	defer resp.Body.Close()

	var result qdrantSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析搜索响应失败: %w", err)
	}

	hits := make([]SearchResult, 0, len(result.Result))
	for _, r := range result.Result {
		content, _ := r.Payload["content"].(string)
		meta := make(map[string]string)
		for k, v := range r.Payload {
			if k != "content" {
				if sv, ok := v.(string); ok {
					meta[k] = sv
				}
			}
		}
		hits = append(hits, SearchResult{
			Document: Document{
				ID:      r.ID,
				Content: content,
				Meta:    meta,
			},
			Score: r.Score,
		})
	}
	return hits, nil
}
