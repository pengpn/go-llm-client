package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pengpn/go-llm-agent/models"
)

// ---- Mock LLM Client ----

// mockClient 预存一组响应，按顺序返回，用于测试中替代真实网络请求。
type mockClient struct {
	mu        sync.Mutex
	responses []*models.Response
	errors    []error // 与 responses 对应，非 nil 时返回 error
	idx       int
	calls     []callRecord // 记录所有调用，用于断言
}

type callRecord struct {
	messages []models.Message
	tools    []models.ToolDefinition
}

func (m *mockClient) ChatWithTools(_ context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, callRecord{
		messages: append([]models.Message{}, messages...),
		tools:    tools,
	})

	if m.idx >= len(m.responses) {
		return nil, fmt.Errorf("mock: 没有更多预设响应（已消耗 %d 个）", m.idx)
	}

	resp := m.responses[m.idx]
	err := m.errors[m.idx]
	m.idx++
	return resp, err
}

func (m *mockClient) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// newMock 创建一个按顺序返回预设响应的 mockClient。
func newMock(responses ...*models.Response) *mockClient {
	errs := make([]error, len(responses))
	return &mockClient{responses: responses, errors: errs}
}

// newMockWithErrors 创建带错误的 mock（response 和 error 一一对应）。
func newMockWithErrors(pairs ...any) *mockClient {
	m := &mockClient{}
	for i := 0; i < len(pairs)-1; i += 2 {
		resp, _ := pairs[i].(*models.Response)
		err, _ := pairs[i+1].(error)
		m.responses = append(m.responses, resp)
		m.errors = append(m.errors, err)
	}
	return m
}

// ---- 测试工具注册表 ----

// callCounter 记录工具被调用的次数，用于验证并发执行。
type callCounter struct {
	count atomic.Int32
}

func (c *callCounter) inc() { c.count.Add(1) }
func (c *callCounter) get() int { return int(c.count.Load()) }

// buildTestRegistry 创建一个包含模拟工具的注册表。
func buildTestRegistry(tools ...*Tool) *Registry {
	r := NewRegistry()
	for _, t := range tools {
		r.Register(t)
	}
	return r
}

func echoTool(name string) *Tool {
	return NewTool(name, "echo tool", models.ToolParameters{Type: "object"}, func(_ context.Context, input string) (string, error) {
		return "echo:" + input, nil
	})
}

func errorTool(name string) *Tool {
	return NewTool(name, "always fails", models.ToolParameters{Type: "object"}, func(_ context.Context, _ string) (string, error) {
		return "", fmt.Errorf("工具故意失败")
	})
}

// toolCallMsg 构造 LLM 请求调用工具的 assistant 消息。
func toolCallMsg(calls ...models.ToolCall) *models.Response {
	return &models.Response{
		FinishReason: "tool_calls",
		ToolCalls:    calls,
	}
}

func toolCall(id, name, args string) models.ToolCall {
	return models.ToolCall{
		ID:   id,
		Type: "function",
		Function: models.FunctionCall{Name: name, Arguments: args},
	}
}

func finalMsg(content string) *models.Response {
	return &models.Response{
		Content:      content,
		FinishReason: "stop",
	}
}

// makeDefs 构造工具定义列表，用于权限测试。
func makeDefs(names ...string) []models.ToolDefinition {
	defs := make([]models.ToolDefinition, len(names))
	for i, n := range names {
		defs[i] = models.ToolDefinition{
			Type: "function",
			Function: models.FunctionDefinition{Name: n},
		}
	}
	return defs
}

// ====================================================================
// Run 测试
// ====================================================================

func TestRun_DirectAnswer(t *testing.T) {
	// LLM 直接返回答案，不调用任何工具
	mock := newMock(finalMsg("你好，有什么可以帮您？"))
	ag := New(mock, buildTestRegistry())

	answer, history, err := ag.Run(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "你好"},
	})

	if err != nil {
		t.Fatalf("Run 返回错误: %v", err)
	}
	if answer != "你好，有什么可以帮您？" {
		t.Errorf("answer = %q", answer)
	}
	if mock.callCount() != 1 {
		t.Errorf("应只调用 LLM 1 次，实际 %d 次", mock.callCount())
	}
	// history: 原始 user + assistant
	if len(history) != 2 {
		t.Errorf("history 长度应为 2，got %d", len(history))
	}
}

func TestRun_SingleToolCall(t *testing.T) {
	// LLM 调用一次工具，然后给出答案
	mock := newMock(
		toolCallMsg(toolCall("call_1", "echo", `{"q":"hello"}`)),
		finalMsg("工具返回了 echo:{\"q\":\"hello\"}"),
	)
	registry := buildTestRegistry(echoTool("echo"))
	ag := New(mock, registry)

	answer, history, err := ag.Run(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "测试"},
	})

	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	if answer == "" {
		t.Error("answer 不应为空")
	}
	// LLM 调用了 2 次（第一次返回 tool_calls，第二次返回答案）
	if mock.callCount() != 2 {
		t.Errorf("应调用 LLM 2 次，实际 %d 次", mock.callCount())
	}
	// history: user + assistant(tool_calls) + tool + assistant(final)
	if len(history) != 4 {
		t.Errorf("history 长度应为 4，got %d: %v", len(history), history)
	}
	// 验证 tool 消息的 ToolCallID 正确对应
	if history[2].Role != models.RoleTool || history[2].ToolCallID != "call_1" {
		t.Errorf("第 3 条消息应为 tool 且 ToolCallID=call_1，got: %+v", history[2])
	}
}

func TestRun_ToolFailure_ContinuesLoop(t *testing.T) {
	// 工具执行失败时，错误信息应作为结果返回给 LLM，不终止 Loop
	mock := newMock(
		toolCallMsg(toolCall("call_fail", "bad_tool", "{}")),
		finalMsg("工具执行失败，我来告诉你原因"),
	)
	registry := buildTestRegistry(errorTool("bad_tool"))
	ag := New(mock, registry)

	answer, _, err := ag.Run(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "调用一个会失败的工具"},
	})

	if err != nil {
		t.Fatalf("工具失败不应导致 Run 返回 error，got: %v", err)
	}
	if answer == "" {
		t.Error("即使工具失败，最终仍应有答案")
	}

	// 验证第二次 LLM 调用收到了工具的错误信息
	secondCall := mock.calls[1]
	toolMsg := secondCall.messages[len(secondCall.messages)-1]
	if toolMsg.Role != models.RoleTool {
		t.Errorf("第二次调用的最后一条消息应为 tool，got %s", toolMsg.Role)
	}
	if toolMsg.Content == "" {
		t.Error("工具失败结果不应为空")
	}
}

func TestRun_MaxIterations(t *testing.T) {
	// LLM 一直返回 tool_calls，应在 MaxIterations 次后终止
	responses := make([]*models.Response, MaxIterations+1)
	for i := range responses {
		responses[i] = toolCallMsg(toolCall(fmt.Sprintf("call_%d", i), "echo", "{}"))
	}
	mock := newMock(responses...)
	ag := New(mock, buildTestRegistry(echoTool("echo")))

	_, _, err := ag.Run(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "无限循环测试"},
	})

	if err == nil {
		t.Error("应返回 MaxIterations 错误")
	}
	if mock.callCount() != MaxIterations {
		t.Errorf("应恰好调用 %d 次，实际 %d 次", MaxIterations, mock.callCount())
	}
}

func TestRun_ContextCancellation(t *testing.T) {
	// ctx 取消时，LLM 调用应返回错误
	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	// 第一次正常返回 tool_calls，cancel 后第二次失败
	mock := &mockClient{}
	mock.responses = []*models.Response{
		toolCallMsg(toolCall("c1", "slow", "{}")),
		nil, // 第二次会因为 ctx 取消而失败
	}
	mock.errors = []error{nil, context.Canceled}

	slowTool := NewTool("slow", "slow tool", models.ToolParameters{Type: "object"},
		func(ctx context.Context, _ string) (string, error) {
			callCount++
			cancel() // 工具执行时取消 ctx
			return "done", nil
		})

	ag := New(mock, buildTestRegistry(slowTool))
	_, _, err := ag.Run(ctx, []models.Message{
		{Role: models.RoleUser, Content: "test"},
	})

	if err == nil {
		t.Error("ctx 取消后应返回错误")
	}
}

func TestRun_LLMError(t *testing.T) {
	// LLM 请求本身失败时，应返回错误
	mock := newMockWithErrors(
		(*models.Response)(nil), fmt.Errorf("网络超时"),
	)
	ag := New(mock, buildTestRegistry())

	_, _, err := ag.Run(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "test"},
	})

	if err == nil {
		t.Error("LLM 请求失败应返回 error")
	}
}

func TestRun_EmptyFinishReason(t *testing.T) {
	// finish_reason="" 时（部分 Provider 的行为），应等同于 "stop"
	mock := newMock(&models.Response{Content: "答案", FinishReason: ""})
	ag := New(mock, buildTestRegistry())

	answer, _, err := ag.Run(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "test"},
	})

	if err != nil {
		t.Fatalf("finish_reason='' 应视为 stop，got error: %v", err)
	}
	if answer != "答案" {
		t.Errorf("answer = %q", answer)
	}
}

// ====================================================================
// 并行工具调用测试
// ====================================================================

func TestExecuteToolCalls_Parallel(t *testing.T) {
	// 验证多个工具是并发执行的，而不是顺序执行
	// 方法：每个工具 sleep 100ms，如果并发执行，总耗时应接近 100ms，而不是 N*100ms
	const toolCount = 5
	const sleepDuration = 100 * time.Millisecond

	var callOrder []int
	var mu sync.Mutex

	registry := NewRegistry()
	toolCalls := make([]models.ToolCall, toolCount)

	for i := range toolCount {
		name := fmt.Sprintf("slow_tool_%d", i)
		idx := i
		registry.Register(NewTool(name, "slow", models.ToolParameters{Type: "object"},
			func(_ context.Context, _ string) (string, error) {
				time.Sleep(sleepDuration)
				mu.Lock()
				callOrder = append(callOrder, idx)
				mu.Unlock()
				return fmt.Sprintf("result_%d", idx), nil
			}))
		toolCalls[i] = toolCall(fmt.Sprintf("id_%d", i), name, "{}")
	}

	ag := &Agent{registry: registry}

	start := time.Now()
	results, err := ag.executeToolCalls(context.Background(), toolCalls, ContinueOnError)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("executeToolCalls 失败: %v", err)
	}

	// 并发执行：总耗时应 < 2 * sleepDuration（而不是 toolCount * sleepDuration）
	maxExpected := sleepDuration * 2
	if elapsed > maxExpected {
		t.Errorf("并发执行耗时 %v，超过预期上限 %v（顺序执行会需要约 %v）",
			elapsed, maxExpected, time.Duration(toolCount)*sleepDuration)
	}

	// 验证结果数量正确
	if len(results) != toolCount {
		t.Errorf("结果数量 %d，期望 %d", len(results), toolCount)
	}

	// 验证结果顺序与输入顺序一致（按 index，而不是执行完成顺序）
	for i, result := range results {
		expectedID := fmt.Sprintf("id_%d", i)
		if result.ToolCallID != expectedID {
			t.Errorf("results[%d].ToolCallID = %q，期望 %q（结果顺序必须与输入一致）",
				i, result.ToolCallID, expectedID)
		}
	}
}

func TestExecuteToolCalls_PartialFailure(t *testing.T) {
	// 部分工具失败，其他工具应正常执行
	registry := buildTestRegistry(
		echoTool("ok_tool"),
		errorTool("fail_tool"),
	)
	ag := &Agent{registry: registry}

	calls := []models.ToolCall{
		toolCall("id_ok", "ok_tool", `{"x":1}`),
		toolCall("id_fail", "fail_tool", "{}"),
	}

	results, err := ag.executeToolCalls(context.Background(), calls, ContinueOnError)

	if err != nil {
		t.Fatalf("部分工具失败不应导致 executeToolCalls 返回 error，got: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("结果数量应为 2，got %d", len(results))
	}
	// 成功的工具有正常输出
	if results[0].Content == "" || results[0].Content[:5] == "工具执行失败" {
		t.Errorf("ok_tool 应有正常输出，got %q", results[0].Content)
	}
	// 失败的工具内容包含错误信息
	if results[1].Content == "" {
		t.Errorf("fail_tool 应返回错误信息，got 空字符串")
	}
	// ToolCallID 对应关系保持正确
	if results[0].ToolCallID != "id_ok" || results[1].ToolCallID != "id_fail" {
		t.Errorf("ToolCallID 对应关系错误: %v", results)
	}
}

func TestExecuteToolCalls_Empty(t *testing.T) {
	ag := &Agent{registry: NewRegistry()}
	results, err := ag.executeToolCalls(context.Background(), nil, ContinueOnError)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("空工具调用应返回空结果，got %v", results)
	}
}

// ====================================================================
// Session.AddAgentTurn 测试（作业2）
// ====================================================================

func TestRun_HistoryContainsToolCalls(t *testing.T) {
	// 验证 history 中的 assistant 消息包含 ToolCalls 字段
	tc := toolCall("call_x", "echo", `{"q":"test"}`)
	mock := newMock(
		toolCallMsg(tc),
		finalMsg("完成"),
	)
	ag := New(mock, buildTestRegistry(echoTool("echo")))

	_, history, err := ag.Run(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "test"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// history[1] 是 assistant 消息（含 ToolCalls）
	assistantMsg := history[1]
	if assistantMsg.Role != models.RoleAssistant {
		t.Fatalf("history[1] 应为 assistant，got %s", assistantMsg.Role)
	}
	if len(assistantMsg.ToolCalls) == 0 {
		t.Error("assistant 消息应包含 ToolCalls 字段")
	}
	if assistantMsg.ToolCalls[0].ID != "call_x" {
		t.Errorf("ToolCall ID 应为 call_x，got %s", assistantMsg.ToolCalls[0].ID)
	}

	// history[2] 是 tool 消息
	toolMsg := history[2]
	if toolMsg.Role != models.RoleTool {
		t.Fatalf("history[2] 应为 tool，got %s", toolMsg.Role)
	}
	if toolMsg.ToolCallID != "call_x" {
		t.Errorf("tool 消息 ToolCallID 应为 call_x，got %s", toolMsg.ToolCallID)
	}
}

// ====================================================================
// Registry 测试
// ====================================================================

func TestRegistry_UnknownTool(t *testing.T) {
	r := NewRegistry()
	_, err := r.Execute(context.Background(), "nonexistent", "{}")
	if err == nil {
		t.Error("调用不存在的工具应返回 error")
	}
}

func TestRegistry_OverwriteTool(t *testing.T) {
	r := NewRegistry()
	r.Register(echoTool("my_tool"))
	r.Register(errorTool("my_tool")) // 覆盖

	_, err := r.Execute(context.Background(), "my_tool", "{}")
	if err == nil {
		t.Error("覆盖后的工具应是 errorTool，期望返回 error")
	}
}

// ====================================================================
// 作业1：ErrorStrategy 测试
// ====================================================================

func TestRun_AbortOnError_StopsOnToolFailure(t *testing.T) {
	// AbortOnError：工具失败 → Run 立即返回 error，不再调用 LLM
	tc := toolCall("id_fail", "fail_tool", "{}")
	mock := newMock(toolCallMsg(tc))

	ag := New(mock, buildTestRegistry(errorTool("fail_tool")))

	_, _, err := ag.Run(
		context.Background(),
		[]models.Message{{Role: models.RoleUser, Content: "触发工具"}},
		WithErrorStrategy(AbortOnError),
	)

	if err == nil {
		t.Fatal("AbortOnError 策略下工具失败应返回 error，got nil")
	}
	// 错误信息应包含工具名，方便定位
	if !strings.Contains(err.Error(), "fail_tool") {
		t.Errorf("error 应包含工具名 fail_tool，got: %v", err)
	}
	// LLM 只调用一次：工具失败后不应再调 LLM
	if mock.callCount() != 1 {
		t.Errorf("AbortOnError 应只调用 LLM 1 次，got %d", mock.callCount())
	}
}

func TestRun_AbortOnError_MultipleTools_FirstFailAborts(t *testing.T) {
	// 多工具并发时，任意一个失败 → 整体 abort
	calls := []models.ToolCall{
		toolCall("id_ok", "echo", `{"q":"ok"}`),
		toolCall("id_fail", "fail_tool", "{}"),
	}
	mock := newMock(toolCallMsg(calls...))

	ag := New(mock, buildTestRegistry(echoTool("echo"), errorTool("fail_tool")))

	_, _, err := ag.Run(
		context.Background(),
		[]models.Message{{Role: models.RoleUser, Content: "并发工具"}},
		WithErrorStrategy(AbortOnError),
	)

	if err == nil {
		t.Fatal("AbortOnError 策略下任意工具失败应返回 error")
	}
}

func TestRun_ContinueOnError_PassesErrorAsObservation(t *testing.T) {
	// ContinueOnError（默认）：工具失败 → 错误信息作为 Observation → LLM 继续决策
	tc := toolCall("id_fail", "fail_tool", "{}")
	mock := newMock(
		toolCallMsg(tc),
		finalMsg("工具失败了，但我仍然可以帮您"),
	)

	ag := New(mock, buildTestRegistry(errorTool("fail_tool")))

	answer, _, err := ag.Run(
		context.Background(),
		[]models.Message{{Role: models.RoleUser, Content: "触发工具"}},
		WithErrorStrategy(ContinueOnError),
	)

	if err != nil {
		t.Fatalf("ContinueOnError 下不应返回 error，got: %v", err)
	}
	if answer == "" {
		t.Error("应返回最终答案")
	}
	// LLM 应被调用两次：第一次触发工具，第二次处理工具结果
	if mock.callCount() != 2 {
		t.Errorf("LLM 应被调用 2 次，got %d", mock.callCount())
	}
	// 第二次 LLM 调用的消息历史中应包含工具失败的 Observation
	secondCallMsgs := mock.calls[1].messages
	foundErrorObs := false
	for _, msg := range secondCallMsgs {
		if msg.Role == models.RoleTool && strings.Contains(msg.Content, "工具执行失败") {
			foundErrorObs = true
			break
		}
	}
	if !foundErrorObs {
		t.Error("LLM 第二次调用应在历史中看到工具失败的 Observation")
	}
}
