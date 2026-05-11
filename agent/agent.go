package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// StreamingLLMClient 是可选的流式扩展接口。
// *client.Client 实现了此接口，通过类型断言检查是否支持流式输出。
// 为什么不直接合并到 LLMClient？
// → 保持向后兼容：老代码不需要实现流式方法；流式能力是"渐进增强"。
type StreamingLLMClient interface {
	ChatStreamText(ctx context.Context, messages []models.Message) (<-chan string, error)
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

// RunStream 与 Run 相同，但对最终回答使用流式输出。
//
// 工作流程：
//  1. 工具调用迭代：仍使用 ChatWithTools（同步），因为工具调用需要传递工具定义
//  2. 工具执行完毕后的最终回答：使用 ChatStreamText，逐 token 写入 tokenCh
//  3. RunStream 返回时关闭 tokenCh，通知读取方（SSE handler）流结束
//
// 为什么不对工具调用也使用流式？
// → 工具调用阶段 LLM 输出的是 JSON，速度快，无需流式
// → 流式响应不支持传递 tool definitions（无 function calling 能力）
//
// 降级：若 client 不支持流式（未实现 StreamingLLMClient），最终回答作为单个 token 推送。
func (a *Agent) RunStream(ctx context.Context, messages []models.Message, tokenCh chan<- string, opts ...RunOption) (string, []models.Message, error) {
	cfg := a.buildRunConfig(opts)

	tools := filterDefinitions(a.registry.Definitions(), cfg.gate, cfg.userID)
	history := make([]models.Message, len(messages))
	copy(history, messages)

	// 类型断言：检查 client 是否支持流式输出
	streamClient, canStream := a.client.(StreamingLLMClient)
	toolsExecuted := false // 是否已执行过工具调用

	defer close(tokenCh) // 保证 RunStream 返回时 tokenCh 被关闭

	for i := range MaxIterations {
		// 工具已执行 + 支持流式 → 流式生成最终回答
		if toolsExecuted && canStream {
			return a.streamFinalAnswer(ctx, history, tokenCh, streamClient)
		}

		// 普通请求（传递工具定义，LLM 自主决定是否调用工具）
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
			// 直接回答（无工具调用）：整体作为单个 token 推送
			select {
			case tokenCh <- resp.Content:
			case <-ctx.Done():
				return "", history, ctx.Err()
			}
			return resp.Content, history, nil

		case "tool_calls":
			results, err := a.executeToolCalls(ctx, resp.ToolCalls, cfg.strategy)
			if err != nil {
				return "", history, err
			}
			history = append(history, results...)
			toolsExecuted = true

		case "length":
			return resp.Content, history, fmt.Errorf("响应被截断（超出 max_tokens）")

		default:
			return "", history, fmt.Errorf("未知的 finish_reason: %q", resp.FinishReason)
		}
	}

	return "", history, fmt.Errorf("超过最大迭代次数 %d，可能存在工具调用循环", MaxIterations)
}

// streamFinalAnswer 调用 ChatStreamText，逐 token 写入 tokenCh，收集完整回答。
// 在 RunStream 确认工具调用已完成后才调用，此时不需要传递工具定义。
func (a *Agent) streamFinalAnswer(ctx context.Context, history []models.Message, tokenCh chan<- string, sc StreamingLLMClient) (string, []models.Message, error) {
	textCh, err := sc.ChatStreamText(ctx, history)
	if err != nil {
		return "", history, fmt.Errorf("流式生成最终回答失败: %w", err)
	}

	var sb strings.Builder
	for token := range textCh {
		select {
		case tokenCh <- token:
			sb.WriteString(token)
		case <-ctx.Done():
			return "", history, ctx.Err()
		}
	}

	answer := sb.String()
	newHistory := append(history, models.Message{
		Role:    models.RoleAssistant,
		Content: answer,
	})
	return answer, newHistory, nil
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
