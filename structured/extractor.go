package structured

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pengpn/go-llm-agent/models"
)

// LLMClient 结构化输出依赖的 LLM 接口（与 agent 包相同签名）
type LLMClient interface {
	ChatWithTools(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error)
}

// Validator 可选校验接口。提取目标类型实现此接口后，
// Extractor 会在 JSON 解码后自动调用 Validate()，失败则触发重试。
type Validator interface {
	Validate() error
}

// Schema 描述期望的输出结构（映射到 Function Calling 的 Parameters）
type Schema struct {
	Name        string                          // 输出函数名（如 "extract_ticket"）
	Description string                          // 告诉 LLM 这个函数做什么
	Properties  map[string]models.ToolProperty  // 各字段描述
	Required    []string                        // 必填字段
}

// Extractor 使用 Function Calling 从 LLM 提取结构化数据。
// T 是目标类型，必须是可 JSON 解码的结构体。
//
// 工作原理：
//  1. 把 Schema 包装为一个 ToolDefinition 发给 LLM
//  2. LLM 被迫按 Schema 填充参数（因为没有其他选择）
//  3. 从 ToolCall.Arguments 中 JSON 解码得到 T
//  4. 如果 T 实现了 Validator，调用 Validate() 校验
//  5. 校验失败 → 把错误信息作为 tool result 回传 → LLM 修正 → 重试
type Extractor[T any] struct {
	client     LLMClient
	schema     Schema
	maxRetries int
}

// ExtractorOption 函数式选项
type ExtractorOption func(e *extractorConfig)

type extractorConfig struct {
	maxRetries int
}

// WithMaxRetries 设置最大重试次数（默认 2）
func WithMaxRetries(n int) ExtractorOption {
	return func(c *extractorConfig) { c.maxRetries = n }
}

// NewExtractor 创建结构化输出提取器
func NewExtractor[T any](client LLMClient, schema Schema, opts ...ExtractorOption) *Extractor[T] {
	cfg := &extractorConfig{maxRetries: 2}
	for _, o := range opts {
		o(cfg)
	}
	return &Extractor[T]{
		client:     client,
		schema:     schema,
		maxRetries: cfg.maxRetries,
	}
}

// Extract 从消息中提取结构化数据
func (e *Extractor[T]) Extract(ctx context.Context, messages []models.Message) (T, error) {
	toolDef := models.ToolDefinition{
		Type: "function",
		Function: models.FunctionDefinition{
			Name:        e.schema.Name,
			Description: e.schema.Description,
			Parameters: models.ToolParameters{
				Type:       "object",
				Properties: e.schema.Properties,
				Required:   e.schema.Required,
			},
		},
	}

	history := make([]models.Message, len(messages))
	copy(history, messages)

	for attempt := range e.maxRetries + 1 {
		resp, err := e.client.ChatWithTools(ctx, history, []models.ToolDefinition{toolDef})
		if err != nil {
			var zero T
			return zero, fmt.Errorf("第 %d 次 LLM 请求失败: %w", attempt+1, err)
		}

		// LLM 应该返回 tool_calls，否则说明它没遵守指令
		if len(resp.ToolCalls) == 0 {
			var zero T
			return zero, fmt.Errorf("LLM 未返回结构化输出（finish_reason=%s，content=%q）", resp.FinishReason, resp.Content)
		}

		call := resp.ToolCalls[0]
		result, validationErr := e.decodeAndValidate(call.Function.Arguments)
		if validationErr == nil {
			return result, nil
		}

		// 校验失败 → 构造修正对话：告诉 LLM 哪里错了
		if attempt < e.maxRetries {
			history = append(history, models.Message{
				Role:      models.RoleAssistant,
				ToolCalls: resp.ToolCalls,
			})
			history = append(history, models.Message{
				Role:       models.RoleTool,
				Content:    fmt.Sprintf("输出校验失败，请修正：%v", validationErr),
				ToolCallID: call.ID,
			})
		}
	}

	var zero T
	return zero, fmt.Errorf("经过 %d 次重试仍无法获得有效输出", e.maxRetries+1)
}

// decodeAndValidate 解码 JSON 并校验
func (e *Extractor[T]) decodeAndValidate(arguments string) (T, error) {
	var result T
	if err := json.Unmarshal([]byte(arguments), &result); err != nil {
		return result, fmt.Errorf("JSON 解码失败: %w", err)
	}

	// 如果 T 实现了 Validator 接口，自动调用校验
	if v, ok := any(&result).(Validator); ok {
		if err := v.Validate(); err != nil {
			return result, fmt.Errorf("校验失败: %w", err)
		}
	}

	return result, nil
}
