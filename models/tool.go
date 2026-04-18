package models

// ToolCall 表示 LLM 决定调用的一次工具调用。
// LLM 在一次响应中可以同时调用多个工具（并行工具调用）。
type ToolCall struct {
	ID       string       `json:"id"`   // 每次调用的唯一 ID，回传结果时需要带上
	Type     string       `json:"type"` // 固定为 "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall 是工具调用的具体内容。
// Arguments 是 JSON 字符串（不是对象），需要二次解析。
// OpenAI 设计成字符串而不是对象，是为了支持流式逐字输出参数。
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 字符串，如 `{"order_id":"ORDER-123"}`
}

// ToolDefinition 是注册给 LLM 的工具定义。
// LLM 根据这些定义决定何时调用哪个工具。
type ToolDefinition struct {
	Type     string              `json:"type"` // 固定为 "function"
	Function FunctionDefinition  `json:"function"`
}

// FunctionDefinition 描述一个工具的签名和用途。
// Description 非常关键——LLM 完全依靠它来判断是否应该调用这个工具。
type FunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  ToolParameters `json:"parameters"`
}

// ToolParameters 使用 JSON Schema 格式描述工具参数。
type ToolParameters struct {
	Type       string                    `json:"type"` // 固定为 "object"
	Properties map[string]ToolProperty   `json:"properties"`
	Required   []string                  `json:"required,omitempty"`
}

// ToolProperty 描述单个参数的类型和含义。
type ToolProperty struct {
	Type        string   `json:"type"`                  // string / number / boolean / array
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`        // 枚举值限制（可选）
}
