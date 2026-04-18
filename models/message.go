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
//
// Function Calling 场景下的消息流转：
//  1. assistant 消息：Content="" , ToolCalls=[{id, name, args}]  ← LLM 决定调用工具
//  2. tool 消息：    Content=结果, ToolCallID=对应的 id          ← 程序执行结果回传
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`  // LLM 请求调用的工具列表
	ToolCallID string     `json:"tool_call_id,omitempty"` // tool 角色回传结果时使用
}

// Usage 记录本次请求消耗的 Token 数量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Response 是 LLM 返回的完整响应。
// FinishReason：
//   - "stop"       — 正常结束
//   - "tool_calls" — LLM 要调用工具，需要执行后继续循环
//   - "length"     — 超出 max_tokens
type Response struct {
	Content      string     `json:"content"`
	FinishReason string     `json:"finish_reason"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	Usage        Usage      `json:"usage"`
	Model        string     `json:"model"`
}

// ChatRequest 是发送给 API 的请求体。
type ChatRequest struct {
	Model       string           `json:"model"`
	Messages    []Message        `json:"messages"`
	Tools       []ToolDefinition `json:"tools,omitempty"`
	Temperature float64          `json:"temperature,omitempty"`
	MaxTokens   int              `json:"max_tokens,omitempty"`
	Stream      bool             `json:"stream,omitempty"`
}

// ChatResponse 是 API 返回的原始 JSON 结构。
type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int     `json:"index"`
		Message struct {
			Role      Role       `json:"role"`
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}
