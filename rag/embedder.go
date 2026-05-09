// Package rag 提供 RAG（检索增强生成）核心组件。
package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Embedder 将文本转换为向量。
// 接口设计：只暴露 Embed，具体 Provider（Qwen/OpenAI/本地模型）对上层透明。
type Embedder interface {
	// Embed 批量向量化，返回与输入等长的向量列表。
	// 批量比逐条调用节省 ~70% API 延迟。
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// QwenEmbedder 使用阿里通义 text-embedding-v3 模型。
// DashScope 完全兼容 OpenAI /embeddings 协议，切换成本极低。
type QwenEmbedder struct {
	apiKey     string
	model      string
	baseURL    string
	dimensions int
	httpClient *http.Client
}

// embeddingRequest 对应 OpenAI-compatible Embedding 请求体。
type embeddingRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"` // Qwen v3 支持自定义维度
}

// embeddingResponse 对应 OpenAI-compatible Embedding 响应体。
type embeddingResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error"`
}

// NewQwenEmbedder 创建 Qwen Embedder。
// 默认使用 text-embedding-v3，1024 维（精度和成本的最佳平衡点）。
func NewQwenEmbedder(apiKey string, opts ...EmbedderOption) *QwenEmbedder {
	e := &QwenEmbedder{
		apiKey:     apiKey,
		model:      "text-embedding-v3",
		baseURL:    "https://dashscope.aliyuncs.com/compatible-mode/v1",
		dimensions: 1024,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// EmbedderOption 函数式选项，保持和 client 包一致的风格。
type EmbedderOption func(*QwenEmbedder)

// WithEmbedModel 覆盖默认模型名。
func WithEmbedModel(model string) EmbedderOption {
	return func(e *QwenEmbedder) { e.model = model }
}

// WithEmbedDimensions 设置输出向量维度（Qwen v3 支持 512/768/1024）。
// 维度越低存储越小，维度越高语义越精确。
func WithEmbedDimensions(d int) EmbedderOption {
	return func(e *QwenEmbedder) { e.dimensions = d }
}

// WithEmbedBaseURL 切换 BaseURL，方便接入其他 OpenAI-compatible Provider。
func WithEmbedBaseURL(url string) EmbedderOption {
	return func(e *QwenEmbedder) { e.baseURL = url }
}

// Embed 批量将文本转换为向量。
// 实现保证：返回 slice 的顺序与 texts 输入顺序严格一致。
func (e *QwenEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	body, err := json.Marshal(embeddingRequest{
		Model:      e.model,
		Input:      texts,
		Dimensions: e.dimensions,
	})
	if err != nil {
		return nil, fmt.Errorf("序列化 embedding 请求失败: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		e.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("调用 embedding API 失败: %w", err)
	}
	defer resp.Body.Close()

	var result embeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("解析 embedding 响应失败: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("embedding API 错误 [%s]: %s", result.Error.Code, result.Error.Message)
	}

	// 按 index 排序写入，保证顺序与输入一致
	// 原因：API 不保证返回顺序，但调用方依赖 texts[i] ↔ vectors[i] 的对应关系
	vectors := make([][]float32, len(texts))
	for _, d := range result.Data {
		if d.Index >= len(vectors) {
			return nil, fmt.Errorf("响应 index %d 超出范围（输入长度 %d）", d.Index, len(texts))
		}
		vectors[d.Index] = d.Embedding
	}
	return vectors, nil
}
