package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/pengpn/go-llm-agent/models"
	"github.com/pengpn/go-llm-agent/session"
	"github.com/pengpn/go-llm-agent/structured"
)

// ExtractedMemoryItem 从对话中提取的单条用户画像信息。
// Confidence 表示 LLM 对该信息的确信程度：
//   - 1.0：用户明确说了（"我住在北京朝阳区"）
//   - 0.7-0.9：强烈暗示（用户多次提到电子产品）
//   - <0.7：模糊推测，不写入
type ExtractedMemoryItem struct {
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
}

// ExtractedMemories 是 LLM 一次提取的画像列表。
// 实现 structured.Validator 接口，触发自动校验。
type ExtractedMemories struct {
	Items []ExtractedMemoryItem `json:"items"`
}

func (m *ExtractedMemories) Validate() error {
	for i, item := range m.Items {
		if item.Key == "" {
			return errorf("items[%d].key 不能为空", i)
		}
		if item.Value == "" {
			return errorf("items[%d].value 不能为空", i)
		}
		if item.Confidence < 0 || item.Confidence > 1 {
			return errorf("items[%d].confidence 必须在 0-1 之间，当前: %.2f", i, item.Confidence)
		}
	}
	return nil
}

// MemoryExtractor 从对话中自动提取用户画像。
// 复用 structured.Extractor[T] 泛型提取器，与 TicketExtractor 同模式。
//
// 工作流程：
//  1. 对话结束后，把完整消息历史发给 LLM
//  2. LLM 被 Schema 约束，返回 key-value-confidence 数组
//  3. 按 confidence 阈值过滤（默认 0.7）
//  4. 通过的条目写入 UserMemory（覆盖更新）
type MemoryExtractor struct {
	ext       *structured.Extractor[ExtractedMemories]
	threshold float64
}

const defaultConfidenceThreshold = 0.7

// MemoryExtractorOption 函数式选项
type MemoryExtractorOption func(*memoryExtractorConfig)

type memoryExtractorConfig struct {
	threshold  float64
	maxRetries int
}

// WithConfidenceThreshold 设置置信度阈值（默认 0.7）
func WithConfidenceThreshold(t float64) MemoryExtractorOption {
	return func(c *memoryExtractorConfig) { c.threshold = t }
}

// WithMemoryExtractorRetries 设置最大重试次数（默认 1，非关键路径不多试）
func WithMemoryExtractorRetries(n int) MemoryExtractorOption {
	return func(c *memoryExtractorConfig) { c.maxRetries = n }
}

// NewMemoryExtractor 创建画像自动提取器
func NewMemoryExtractor(client structured.LLMClient, opts ...MemoryExtractorOption) *MemoryExtractor {
	cfg := &memoryExtractorConfig{
		threshold:  defaultConfidenceThreshold,
		maxRetries: 1,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	schema := structured.Schema{
		Name:        "extract_user_profile",
		Description: "从客服对话中提取用户画像信息（偏好、地址、习惯等值得跨会话记住的信息）",
		Properties: map[string]models.ToolProperty{
			"items": {
				Type:        "array",
				Description: "提取到的画像条目列表。只提取用户明确提及或强烈暗示的信息，不要猜测。如果对话中没有值得记忆的信息，返回空数组。",
			},
		},
		Required: []string{"items"},
	}

	return &MemoryExtractor{
		ext: structured.NewExtractor[ExtractedMemories](client, schema,
			structured.WithMaxRetries(cfg.maxRetries)),
		threshold: cfg.threshold,
	}
}

// Extract 从对话中提取画像条目，按置信度过滤后返回。
// 提取失败返回 nil（不影响主流程）。
func (me *MemoryExtractor) Extract(ctx context.Context, msgs []models.Message) []ExtractedMemoryItem {
	memories, err := me.ext.Extract(ctx, msgs)
	if err != nil {
		slog.Warn("auto extract user memory failed", "err", err)
		return nil
	}

	filtered := make([]ExtractedMemoryItem, 0, len(memories.Items))
	for _, item := range memories.Items {
		if item.Confidence >= me.threshold {
			filtered = append(filtered, item)
		}
	}

	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

// ExtractAndSave 提取画像并保存到 MemoryStore。
// 返回写入的条目数（0 表示无新画像或提取失败）。
func (me *MemoryExtractor) ExtractAndSave(ctx context.Context, msgs []models.Message, userID string, store session.MemoryStore) int {
	items := me.Extract(ctx, msgs)
	if len(items) == 0 {
		return 0
	}

	memory, err := store.Load(userID)
	if err != nil {
		slog.Warn("load user memory for extraction failed", "user_id", userID, "err", err)
		return 0
	}

	for _, item := range items {
		memory.Set(item.Key, item.Value, 30*24*time.Hour) // 默认 30 天过期
	}

	if err := store.Save(memory); err != nil {
		slog.Warn("save extracted memory failed", "user_id", userID, "err", err)
		return 0
	}

	slog.Info("auto extracted user memory",
		"user_id", userID,
		"count", len(items),
	)
	return len(items)
}
