// Package agent 实现基于 Function Calling 的 ReAct Agent Loop。
package agent

import (
	"context"
	"fmt"

	"github.com/pengpn/go-llm-agent/models"
)

// NewTypedTool 是类型安全的工具创建函数。
// 工具函数直接接收解码并校验后的类型化参数 T，无需手动 json.Unmarshal。
//
// 示例：
//
//	type GetOrderReq struct {
//	    OrderID string `json:"order_id"`
//	}
//	func (r *GetOrderReq) Validate() error { ... }  // 可选
//
//	NewTypedTool("get_order", "查询订单", params,
//	    func(ctx context.Context, req GetOrderReq) (string, error) {
//	        // req.OrderID 直接可用，无样板代码
//	    })
func NewTypedTool[T any](name, description string, params models.ToolParameters, fn func(ctx context.Context, input T) (string, error)) *Tool {
	return NewTool(name, description, params, func(ctx context.Context, raw string) (string, error) {
		input, err := DecodeAndValidate[T](raw)
		if err != nil {
			return "", err
		}
		return fn(ctx, input)
	})
}

// ToolFunc 是工具函数的签名。
// input: LLM 传来的 JSON 字符串（需要自行解析）
// output: 执行结果（任意字符串，会作为 Observation 回传给 LLM）
type ToolFunc func(ctx context.Context, input string) (string, error)

// Tool 将工具的元数据（给 LLM 看）和执行逻辑（给 Go 程序用）绑定在一起。
type Tool struct {
	Definition models.ToolDefinition // LLM 看到的工具描述
	Execute    ToolFunc              // 实际执行逻辑
}

// Registry 是工具注册表，管理所有可用工具。
// Agent 从这里获取工具定义（发给 LLM）和执行函数（调用时使用）。
type Registry struct {
	tools map[string]*Tool
}

// NewRegistry 创建一个空的工具注册表。
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]*Tool),
	}
}

// Register 注册一个工具。name 必须与 Definition.Function.Name 一致。
// 重复注册同名工具会覆盖旧的。
func (r *Registry) Register(t *Tool) {
	r.tools[t.Definition.Function.Name] = t
}

// Definitions 返回所有工具的定义列表，用于发送给 LLM。
func (r *Registry) Definitions() []models.ToolDefinition {
	defs := make([]models.ToolDefinition, 0, len(r.tools))
	for _, t := range r.tools {
		defs = append(defs, t.Definition)
	}
	return defs
}

// Execute 执行指定名称的工具，返回结果字符串。
// 工具不存在时返回明确的错误描述（不 panic），LLM 可以据此调整策略。
func (r *Registry) Execute(ctx context.Context, name, input string) (string, error) {
	t, ok := r.tools[name]
	if !ok {
		return "", fmt.Errorf("工具 %q 不存在，可用工具: %v", name, r.Names())
	}
	return t.Execute(ctx, input)
}

// Names 返回所有已注册的工具名称列表。
func (r *Registry) Names() []string {
	names := make([]string, 0, len(r.tools))
	for name := range r.tools {
		names = append(names, name)
	}
	return names
}

// NewTool 是创建 Tool 的便捷函数，减少样板代码。
func NewTool(name, description string, params models.ToolParameters, fn ToolFunc) *Tool {
	return &Tool{
		Definition: models.ToolDefinition{
			Type: "function",
			Function: models.FunctionDefinition{
				Name:        name,
				Description: description,
				Parameters:  params,
			},
		},
		Execute: fn,
	}
}
