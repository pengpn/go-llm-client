package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/pengpn/go-llm-agent/models"
)

// MaxIterations 是 Agent Loop 的最大循环次数。
// 防止 LLM 陷入无限工具调用循环（理论上不应发生，但工程上必须防御）。
const MaxIterations = 10

// LLMClient 是 Agent 依赖的 LLM 客户端接口。
// 定义为接口而不是直接依赖 *client.Client，有两个好处：
//  1. 测试时可以注入 mock，不需要真实网络请求
//  2. 将来替换底层实现（换 Provider、换协议）不影响 Agent 代码
//
// *client.Client 已实现此接口（有 ChatWithTools 方法），调用方无需修改。
type LLMClient interface {
	ChatWithTools(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error)
}

// Agent 是 ReAct 模式的 Agent，持有 LLM Client 和工具注册表。
type Agent struct {
	client   LLMClient
	registry *Registry
}

// New 创建一个新的 Agent。
// client 接受任何实现了 LLMClient 接口的对象（*client.Client 或测试 mock）。
func New(c LLMClient, registry *Registry) *Agent {
	return &Agent{
		client:   c,
		registry: registry,
	}
}

// Run 执行一次完整的 Agent Loop，直到 LLM 给出最终答案或达到最大迭代次数。
//
// Loop 流程：
//  1. 把用户消息 + 工具定义发给 LLM
//  2. LLM 返回 finish_reason="tool_calls" → 并发执行工具，把结果追加到消息，回到步骤 1
//  3. LLM 返回 finish_reason="stop" → 返回最终答案
func (a *Agent) Run(ctx context.Context, messages []models.Message) (string, []models.Message, error) {
	tools := a.registry.Definitions()
	history := make([]models.Message, len(messages))
	copy(history, messages)

	for i := range MaxIterations {
		resp, err := a.client.ChatWithTools(ctx, history, tools)
		if err != nil {
			return "", history, fmt.Errorf("第 %d 轮 LLM 请求失败: %w", i+1, err)
		}

		// 即使 Content 为空（只有 ToolCalls），也必须追加这条 assistant 消息。
		// OpenAI 协议要求 tool 消息前必须有对应的 assistant 消息。
		history = append(history, models.Message{
			Role:      models.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		switch resp.FinishReason {
		case "stop", "":
			return resp.Content, history, nil

		case "tool_calls":
			toolResults, err := a.executeToolCalls(ctx, resp.ToolCalls)
			if err != nil {
				return "", history, err
			}
			history = append(history, toolResults...)

		case "length":
			return resp.Content, history, fmt.Errorf("响应被截断（超出 max_tokens）")

		default:
			return "", history, fmt.Errorf("未知的 finish_reason: %q", resp.FinishReason)
		}
	}

	return "", history, fmt.Errorf("超过最大迭代次数 %d，可能存在工具调用循环", MaxIterations)
}

// executeToolCalls 并发执行 LLM 请求的所有工具调用。
//
// 为什么并发？LLM 一次可以请求多个工具（如同时查订单状态和物流信息），
// 顺序执行的延迟 = 各工具耗时之和，并发执行的延迟 ≈ 最慢那个工具的耗时。
//
// 结果顺序与输入顺序一致（按 index 写入预分配 slice），保证 ToolCallID 对应关系。
// 工具执行失败时，错误信息作为结果返回给 LLM，让它决定下一步（不终止整个 Loop）。
func (a *Agent) executeToolCalls(ctx context.Context, toolCalls []models.ToolCall) ([]models.Message, error) {
	results := make([]models.Message, len(toolCalls))
	var wg sync.WaitGroup

	for i, call := range toolCalls {
		wg.Add(1)
		go func(idx int, c models.ToolCall) {
			defer wg.Done()

			output, err := a.registry.Execute(ctx, c.Function.Name, c.Function.Arguments)
			if err != nil {
				output = fmt.Sprintf("工具执行失败: %v", err)
			}

			results[idx] = models.Message{
				Role:       models.RoleTool,
				Content:    output,
				ToolCallID: c.ID,
			}
		}(i, call)
	}

	wg.Wait()
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
			// 复用并发执行逻辑，但额外记录轨迹
			results := make([]models.Message, len(resp.ToolCalls)) // 预分配，按 index 写入
			records := make([]ToolCallRecord, len(resp.ToolCalls))
			var wg sync.WaitGroup

			for j, call := range resp.ToolCalls {
				wg.Add(1)
				go func(idx int, c models.ToolCall) {// 值传递，避免 goroutine 捕获循环变量
					defer wg.Done()
					rec := ToolCallRecord{Name: c.Function.Name, Input: c.Function.Arguments}

					output, err := a.registry.Execute(ctx, c.Function.Name, c.Function.Arguments)
					if err != nil {
						rec.Error = err.Error()
						output = fmt.Sprintf("工具执行失败: %v", err)
					} else {
						rec.Output = output
					}

					// 各 goroutine 写不同 index，无竞争
					records[idx] = rec
					results[idx] = models.Message{
						Role:       models.RoleTool,
						Content:    output,
						ToolCallID: c.ID,
					}
				}(j, call)
			}
			wg.Wait()

			step.ToolCalls = records
			history = append(history, results...)
		}

		trace = append(trace, step)
	}

	return "", trace, fmt.Errorf("超过最大迭代次数 %d", MaxIterations)
}

// BuildToolResult 将 Go 对象序列化为 JSON 字符串，作为工具结果返回。
func BuildToolResult(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("序列化工具结果失败: %w", err)
	}
	return string(b), nil
}
