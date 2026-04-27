package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/pengpn/go-llm-agent/models"
)

// MaxIterations 是 Agent Loop 的最大循环次数，防止工具调用死循环。
const MaxIterations = 10

// LLMClient 是 Agent 依赖的 LLM 客户端接口。
// 定义为接口便于测试注入 mock，也便于将来替换底层实现。
type LLMClient interface {
	ChatWithTools(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error)
}

// ErrorStrategy 定义工具执行失败时的处理策略。
type ErrorStrategy int

const (
	// ContinueOnError（默认）：工具失败的错误信息作为 Observation 返回给 LLM，
	// 由 LLM 决定是否调整策略。最灵活，适合大多数场景。
	ContinueOnError ErrorStrategy = iota

	// AbortOnError：任意工具失败时立即终止 Agent Loop 并返回错误。
	// 适用于链式依赖场景（下一个工具的输入依赖上一个工具的输出）。
	AbortOnError
)

// runConfig 保存单次 Run 调用的运行时配置（通过 RunOption 注入）。
type runConfig struct {
	userID   string
	gate     Gate
	strategy ErrorStrategy
}

// RunOption 用于在 Run/RunWithTrace 调用时注入运行时配置。
type RunOption func(*runConfig)

// WithUser 指定本次 Run 的用户 ID，用于权限控制。
func WithUser(userID string) RunOption {
	return func(c *runConfig) { c.userID = userID }
}

// WithGate 为本次 Run 注入权限控制策略（覆盖 Agent 的默认 Gate）。
func WithGate(gate Gate) RunOption {
	return func(c *runConfig) { c.gate = gate }
}

// WithErrorStrategy 设置工具失败时的处理策略。
func WithErrorStrategy(s ErrorStrategy) RunOption {
	return func(c *runConfig) { c.strategy = s }
}

// Agent 是 ReAct 模式的 Agent，持有 LLM Client 和工具注册表。
type Agent struct {
	client   LLMClient
	registry *Registry
	gate     Gate // 默认权限控制器，可被 RunOption 覆盖
}

// AgentOption 用于配置 Agent 实例。
type AgentOption func(*Agent)

// WithDefaultGate 设置 Agent 的默认权限控制器。
// 每次 Run 时若未通过 WithGate 覆盖，则使用此默认值。
func WithDefaultGate(gate Gate) AgentOption {
	return func(a *Agent) { a.gate = gate }
}

// New 创建一个新的 Agent。
// 默认使用 AllowAll（所有用户可调用所有工具），可通过 WithDefaultGate 覆盖。
func New(c LLMClient, registry *Registry, opts ...AgentOption) *Agent {
	a := &Agent{
		client:   c,
		registry: registry,
		gate:     AllowAll{},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Run 执行一次完整的 Agent Loop，直到 LLM 给出最终答案或达到最大迭代次数。
//
// 支持通过 RunOption 注入运行时配置：
//
//	answer, history, err := ag.Run(ctx, msgs, WithUser("u001"), WithErrorStrategy(AbortOnError))
func (a *Agent) Run(ctx context.Context, messages []models.Message, opts ...RunOption) (string, []models.Message, error) {
	cfg := a.buildRunConfig(opts)

	tools := filterDefinitions(a.registry.Definitions(), cfg.gate, cfg.userID)
	history := make([]models.Message, len(messages))
	copy(history, messages)

	for i := range MaxIterations {
		resp, err := a.client.ChatWithTools(ctx, history, tools)
		if err != nil {
			return "", history, fmt.Errorf("第 %d 轮 LLM 请求失败: %w", i+1, err)
		}

		history = append(history, models.Message{
			Role:      models.RoleAssistant,
			Content:   resp.Content,
			ToolCalls: resp.ToolCalls,
		})

		switch resp.FinishReason {
		case "stop", "":
			return resp.Content, history, nil

		case "tool_calls":
			toolResults, err := a.executeToolCalls(ctx, resp.ToolCalls, cfg.strategy)
			if err != nil {
				// AbortOnError 场景：工具失败，直接终止
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
// 结果顺序与输入顺序一致（按 index 写入预分配 slice），保证 ToolCallID 对应关系。
// strategy 决定工具失败时的行为：ContinueOnError 把错误信息包装成结果，
// AbortOnError 收集第一个失败并整体返回 error。
func (a *Agent) executeToolCalls(ctx context.Context, toolCalls []models.ToolCall, strategy ErrorStrategy) ([]models.Message, error) {
	results := make([]models.Message, len(toolCalls))
	errs := make([]error, len(toolCalls))
	var wg sync.WaitGroup

	for i, call := range toolCalls {
		wg.Add(1)
		go func(idx int, c models.ToolCall) {
			defer wg.Done()
			output, err := a.registry.Execute(ctx, c.Function.Name, c.Function.Arguments)
			if err != nil {
				errs[idx] = err
				if strategy == ContinueOnError {
					output = fmt.Sprintf("工具执行失败: %v", err)
				} else {
					return // AbortOnError：不写 results，由主协程检查 errs
				}
			}
			results[idx] = models.Message{
				Role:       models.RoleTool,
				Content:    output,
				ToolCallID: c.ID,
			}
		}(i, call)
	}

	wg.Wait()

	if strategy == AbortOnError {
		for i, err := range errs {
			if err != nil {
				return nil, fmt.Errorf("工具 %q 执行失败: %w", toolCalls[i].Function.Name, err)
			}
		}
	}

	return results, nil
}

// filterDefinitions 按照 Gate 和 userID 过滤工具定义列表。
// userID 为空时不过滤（向后兼容旧代码）。
func filterDefinitions(defs []models.ToolDefinition, gate Gate, userID string) []models.ToolDefinition {
	if userID == "" {
		return defs
	}
	filtered := make([]models.ToolDefinition, 0, len(defs))
	for _, d := range defs {
		if gate.Allowed(userID, d.Function.Name) {
			filtered = append(filtered, d)
		}
	}
	return filtered
}

func (a *Agent) buildRunConfig(opts []RunOption) *runConfig {
	cfg := &runConfig{
		gate:     a.gate,
		strategy: ContinueOnError,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// ---- RunWithTrace ----

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
func (a *Agent) RunWithTrace(ctx context.Context, messages []models.Message, opts ...RunOption) (string, []StepResult, error) {
	cfg := a.buildRunConfig(opts)

	tools := filterDefinitions(a.registry.Definitions(), cfg.gate, cfg.userID)
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
			results := make([]models.Message, len(resp.ToolCalls))
			records := make([]ToolCallRecord, len(resp.ToolCalls))
			var wg sync.WaitGroup

			for j, call := range resp.ToolCalls {
				wg.Add(1)
				go func(idx int, c models.ToolCall) {
					defer wg.Done()
					rec := ToolCallRecord{Name: c.Function.Name, Input: c.Function.Arguments}

					output, err := a.registry.Execute(ctx, c.Function.Name, c.Function.Arguments)
					if err != nil {
						rec.Error = err.Error()
						if cfg.strategy == ContinueOnError {
							output = fmt.Sprintf("工具执行失败: %v", err)
						} else {
							records[idx] = rec
							return
						}
					} else {
						rec.Output = output
					}

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
