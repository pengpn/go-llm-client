// Package models 定义与 LLM API 交互的核心数据结构。
// 遵循 OpenAI Chat Completion 协议——兼容大多数主流 Provider。
package models

// Role 表示对话中消息的发送方角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Message 是一条对话消息。
// ToolCallID 仅在 Role=tool 时使用，用于关联工具调用结果。
type Message struct {
	Role       Role   `json:"role"`
	Content    string `json:"content"`
	ToolCallID string `json:"tool_call_id,omitempty"`
}

// Usage 记录本次请求消耗的 Token 数量。
// 用于费用计算和配额监控。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Response 是 LLM 返回的完整响应。
// FinishReason 说明停止原因：stop / length / tool_calls / content_filter
type Response struct {
	Content      string  `json:"content"`
	FinishReason string  `json:"finish_reason"`
	Usage        Usage   `json:"usage"`
	Model        string  `json:"model"`
}

// chatRequest 是发送给 API 的请求体（内部使用）。
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// chatResponse 是 API 返回的原始 JSON 结构（内部使用）。
type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int     `json:"index"`
		Message Message `json:"message"`
		Delta   struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}
