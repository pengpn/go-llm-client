package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pengpn/go-llm-agent/client"
	"github.com/pengpn/go-llm-agent/models"
)

// MaxIterations 是 Agent Loop 的最大循环次数。
// 防止 LLM 陷入无限工具调用循环（理论上不应发生，但工程上必须防御）。
const MaxIterations = 10

// Agent 是 ReAct 模式的 Agent，持有 LLM Client 和工具注册表。
type Agent struct {
	client   *client.Client
	registry *Registry
}

// New 创建一个新的 Agent。
func New(c *client.Client, registry *Registry) *Agent {
	return &Agent{
		client:   c,
		registry: registry,
	}
}

// Run 执行一次完整的 Agent Loop，直到 LLM 给出最终答案或达到最大迭代次数。
//
// Loop 流程：
//  1. 把用户消息 + 工具定义发给 LLM
//  2. LLM 返回 finish_reason="tool_calls" → 执行工具，把结果追加到消息，回到步骤 1
//  3. LLM 返回 finish_reason="stop" → 返回最终答案
//
// messages 是完整的对话历史（含 system prompt），Run 会追加新消息并返回。
func (a *Agent) Run(ctx context.Context, messages []models.Message) (string, []models.Message, error) {
	tools := a.registry.Definitions()
	history := make([]models.Message, len(messages))
	copy(history, messages)

	for i := range MaxIterations {
		resp, err := a.client.ChatWithTools(ctx, history, tools)
		if err != nil {
			return "", history, fmt.Errorf("第 %d 轮 LLM 请求失败: %w", i+1, err)
		}

		// 将 LLM 的本次响应追加到历史
		// 注意：即使 Content 为空（只有 ToolCalls），也必须追加这条 assistant 消息
		// 原因：OpenAI 协议要求 tool 消息前必须有对应的 assistant 消息
		assistantMsg := models.Message{
			Role:      models.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		}
		history = append(history, assistantMsg)

		switch resp.FinishReason {
		case "stop", "": // 正常结束，返回最终答案
			return resp.Content, history, nil

		case "tool_calls": // LLM 要调用工具
			toolResults, err := a.executeToolCalls(ctx, resp.ToolCalls)
			if err != nil {
				return "", history, err
			}
			history = append(history, toolResults...)
			// 继续下一轮循环

		case "length":
			return resp.Content, history, fmt.Errorf("响应被截断（超出 max_tokens），当前内容: %s", resp.Content)

		default:
			return "", history, fmt.Errorf("未知的 finish_reason: %q", resp.FinishReason)
		}
	}

	return "", history, fmt.Errorf("超过最大迭代次数 %d，可能存在工具调用循环", MaxIterations)
}

// executeToolCalls 并发执行 LLM 请求的所有工具调用，收集结果。
// OpenAI 允许 LLM 一次请求多个工具（提高效率），我们顺序执行以保持简单。
// 如果某个工具执行失败，错误信息本身作为结果返回给 LLM，让它决定如何处理。
func (a *Agent) executeToolCalls(ctx context.Context, toolCalls []models.ToolCall) ([]models.Message, error) {
	results := make([]models.Message, 0, len(toolCalls))

	for _, call := range toolCalls {
		result, err := a.registry.Execute(ctx, call.Function.Name, call.Function.Arguments)
		if err != nil {
			// 工具执行失败：把错误信息作为结果返回给 LLM
			// 这样 LLM 可以决定重试、换工具、或告知用户
			result = fmt.Sprintf("工具执行失败: %v", err)
		}

		// tool 消息必须携带 ToolCallID，LLM 用它来对应是哪次调用的结果
		results = append(results, models.Message{
			Role:       models.RoleTool,
			Content:    result,
			ToolCallID: call.ID,
		})
	}

	return results, nil
}

// StepResult 是单步执行的详细信息，用于调试和日志。
type StepResult struct {
	Iteration    int
	ToolCalls    []ToolCallRecord
	FinalAnswer  string
	FinishReason string
}

// ToolCallRecord 记录一次工具调用的输入和输出。
type ToolCallRecord struct {
	Name   string
	Input  string
	Output string
	Error  string
}

// RunWithTrace 与 Run 功能相同，但额外返回每步的执行轨迹，方便调试。
func (a *Agent) RunWithTrace(ctx context.Context, messages []models.Message) (string, []StepResult, error) {
	tools := a.registry.Definitions()
	history := make([]models.Message, len(messages))
	copy(history, messages)

	var trace []StepResult

	for i := range MaxIterations {
		resp, err := a.client.ChatWithTools(ctx, history, tools)
		if err != nil {
			return "", trace, fmt.Errorf("第 %d 轮请求失败: %w", i+1, err)
		}

		step := StepResult{Iteration: i + 1, FinishReason: resp.FinishReason}

		history = append(history, models.Message{
			Role:      models.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		if resp.FinishReason == "stop" || resp.FinishReason == "" {
			step.FinalAnswer = resp.Content
			trace = append(trace, step)
			return resp.Content, trace, nil
		}

		if resp.FinishReason == "tool_calls" {
			for _, call := range resp.ToolCalls {
				record := ToolCallRecord{Name: call.Function.Name, Input: call.Function.Arguments}

				output, err := a.registry.Execute(ctx, call.Function.Name, call.Function.Arguments)
				if err != nil {
					record.Error = err.Error()
					output = fmt.Sprintf("工具执行失败: %v", err)
				} else {
					record.Output = output
				}

				step.ToolCalls = append(step.ToolCalls, record)
				history = append(history, models.Message{
					Role:       models.RoleTool,
					Content:    output,
					ToolCallID: call.ID,
				})
			}
		}

		trace = append(trace, step)
	}

	return "", trace, fmt.Errorf("超过最大迭代次数 %d", MaxIterations)
}

// BuildToolResult 将 Go 对象序列化为 JSON 字符串，作为工具结果返回。
// 工具函数用这个辅助函数构造标准格式的输出。
func BuildToolResult(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("序列化工具结果失败: %w", err)
	}
	return string(b), nil
}
