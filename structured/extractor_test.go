package structured

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/pengpn/go-llm-agent/models"
)

// ── mock LLM Client ────────────────────────────────────────────

type mockClient struct {
	mu        sync.Mutex
	responses []*models.Response
	idx       int
	calls     []mockCall
}

type mockCall struct {
	messages []models.Message
	tools    []models.ToolDefinition
}

func (m *mockClient) ChatWithTools(_ context.Context, msgs []models.Message, tools []models.ToolDefinition) (*models.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, mockCall{messages: msgs, tools: tools})

	if m.idx >= len(m.responses) {
		return nil, fmt.Errorf("mock: 没有更多预设响应（已消耗 %d 个）", m.idx)
	}
	resp := m.responses[m.idx]
	m.idx++
	return resp, nil
}

func (m *mockClient) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

// ── 测试用目标类型 ──────────────────────────────────────────────

type Ticket struct {
	Category string `json:"category"`
	Priority int    `json:"priority"`
	Summary  string `json:"summary"`
}

type TicketWithValidation struct {
	Category string `json:"category"`
	Priority int    `json:"priority"`
	Summary  string `json:"summary"`
}

func (t *TicketWithValidation) Validate() error {
	validCategories := map[string]bool{"订单": true, "退款": true, "物流": true, "其他": true}
	if !validCategories[t.Category] {
		return fmt.Errorf("category 必须是 订单/退款/物流/其他 之一，当前值: %q", t.Category)
	}
	if t.Priority < 1 || t.Priority > 5 {
		return fmt.Errorf("priority 必须在 1-5 之间，当前值: %d", t.Priority)
	}
	if t.Summary == "" {
		return fmt.Errorf("summary 不能为空")
	}
	return nil
}

func ticketSchema() Schema {
	return Schema{
		Name:        "extract_ticket",
		Description: "从客服对话中提取工单信息",
		Properties: map[string]models.ToolProperty{
			"category": {Type: "string", Description: "工单类别：订单/退款/物流/其他"},
			"priority": {Type: "number", Description: "优先级 1-5，5最高"},
			"summary":  {Type: "string", Description: "问题摘要，50字以内"},
		},
		Required: []string{"category", "priority", "summary"},
	}
}

// ── 测试 ────────────────────────────────────────────────────────

func TestExtract_Success(t *testing.T) {
	client := &mockClient{
		responses: []*models.Response{
			{
				FinishReason: "tool_calls",
				ToolCalls: []models.ToolCall{
					{
						ID:   "call-1",
						Type: "function",
						Function: models.FunctionCall{
							Name:      "extract_ticket",
							Arguments: `{"category":"订单","priority":3,"summary":"查询ORDER-001状态"}`,
						},
					},
				},
			},
		},
	}

	ext := NewExtractor[Ticket](client, ticketSchema())
	msgs := []models.Message{{Role: models.RoleUser, Content: "我的 ORDER-001 到哪了"}}

	result, err := ext.Extract(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}

	if result.Category != "订单" {
		t.Errorf("Category 期望 '订单'，实际 %q", result.Category)
	}
	if result.Priority != 3 {
		t.Errorf("Priority 期望 3，实际 %d", result.Priority)
	}
	if result.Summary != "查询ORDER-001状态" {
		t.Errorf("Summary 不匹配: %q", result.Summary)
	}
}

func TestExtract_WithValidation_Success(t *testing.T) {
	client := &mockClient{
		responses: []*models.Response{
			{
				FinishReason: "tool_calls",
				ToolCalls: []models.ToolCall{
					{
						ID: "call-1",
						Function: models.FunctionCall{
							Name:      "extract_ticket",
							Arguments: `{"category":"退款","priority":4,"summary":"商品破损要求退款"}`,
						},
					},
				},
			},
		},
	}

	ext := NewExtractor[TicketWithValidation](client, ticketSchema())
	result, err := ext.Extract(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "商品坏了想退款"},
	})
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	if result.Category != "退款" || result.Priority != 4 {
		t.Errorf("结果不匹配: %+v", result)
	}
}

func TestExtract_ValidationFailed_Retries(t *testing.T) {
	client := &mockClient{
		responses: []*models.Response{
			// 第一次：priority 超出范围
			{
				FinishReason: "tool_calls",
				ToolCalls: []models.ToolCall{
					{
						ID: "call-1",
						Function: models.FunctionCall{
							Name:      "extract_ticket",
							Arguments: `{"category":"订单","priority":10,"summary":"查询订单"}`,
						},
					},
				},
			},
			// 第二次：LLM 修正后通过
			{
				FinishReason: "tool_calls",
				ToolCalls: []models.ToolCall{
					{
						ID: "call-2",
						Function: models.FunctionCall{
							Name:      "extract_ticket",
							Arguments: `{"category":"订单","priority":3,"summary":"查询订单"}`,
						},
					},
				},
			},
		},
	}

	ext := NewExtractor[TicketWithValidation](client, ticketSchema(), WithMaxRetries(2))
	result, err := ext.Extract(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "查下订单"},
	})
	if err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if result.Priority != 3 {
		t.Errorf("修正后 Priority 期望 3，实际 %d", result.Priority)
	}
	if client.callCount() != 2 {
		t.Errorf("应调用 LLM 2 次（首次+重试），实际 %d 次", client.callCount())
	}

	// 验证重试时的消息包含校验错误信息
	secondCall := client.calls[1]
	lastMsg := secondCall.messages[len(secondCall.messages)-1]
	if lastMsg.Role != models.RoleTool {
		t.Errorf("重试消息最后一条应为 tool role，实际 %q", lastMsg.Role)
	}
	if !contains(lastMsg.Content, "priority") {
		t.Errorf("重试消息应包含校验错误信息，实际: %q", lastMsg.Content)
	}
}

func TestExtract_AllRetriesFail(t *testing.T) {
	// 每次都返回无效的 priority
	badResp := &models.Response{
		FinishReason: "tool_calls",
		ToolCalls: []models.ToolCall{
			{
				ID: "call-x",
				Function: models.FunctionCall{
					Name:      "extract_ticket",
					Arguments: `{"category":"订单","priority":99,"summary":"测试"}`,
				},
			},
		},
	}

	client := &mockClient{
		responses: []*models.Response{badResp, badResp, badResp},
	}

	ext := NewExtractor[TicketWithValidation](client, ticketSchema(), WithMaxRetries(2))
	_, err := ext.Extract(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "测试"},
	})
	if err == nil {
		t.Fatal("所有重试都失败时应返回错误")
	}
	// 总共应调用 3 次（1 首次 + 2 重试）
	if client.callCount() != 3 {
		t.Errorf("应调用 3 次，实际 %d 次", client.callCount())
	}
}

func TestExtract_NoToolCalls_ReturnsError(t *testing.T) {
	client := &mockClient{
		responses: []*models.Response{
			{Content: "我无法提取结构化数据", FinishReason: "stop"},
		},
	}

	ext := NewExtractor[Ticket](client, ticketSchema())
	_, err := ext.Extract(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "测试"},
	})
	if err == nil {
		t.Fatal("LLM 未返回 tool_calls 时应报错")
	}
}

func TestExtract_InvalidJSON_Retries(t *testing.T) {
	client := &mockClient{
		responses: []*models.Response{
			// 第一次：非法 JSON
			{
				FinishReason: "tool_calls",
				ToolCalls: []models.ToolCall{
					{
						ID: "call-1",
						Function: models.FunctionCall{
							Name:      "extract_ticket",
							Arguments: `{invalid json`,
						},
					},
				},
			},
			// 第二次：合法 JSON
			{
				FinishReason: "tool_calls",
				ToolCalls: []models.ToolCall{
					{
						ID: "call-2",
						Function: models.FunctionCall{
							Name:      "extract_ticket",
							Arguments: `{"category":"其他","priority":1,"summary":"测试"}`,
						},
					},
				},
			},
		},
	}

	ext := NewExtractor[Ticket](client, ticketSchema(), WithMaxRetries(1))
	result, err := ext.Extract(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "测试"},
	})
	if err != nil {
		t.Fatalf("JSON 错误重试后应成功: %v", err)
	}
	if result.Category != "其他" {
		t.Errorf("Category 期望 '其他'，实际 %q", result.Category)
	}
}

func TestExtract_ToolDefinitionPassedToLLM(t *testing.T) {
	client := &mockClient{
		responses: []*models.Response{
			{
				FinishReason: "tool_calls",
				ToolCalls: []models.ToolCall{
					{
						ID: "call-1",
						Function: models.FunctionCall{
							Name:      "extract_ticket",
							Arguments: `{"category":"订单","priority":1,"summary":"测试"}`,
						},
					},
				},
			},
		},
	}

	ext := NewExtractor[Ticket](client, ticketSchema())
	_, _ = ext.Extract(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "测试"},
	})

	if client.callCount() != 1 {
		t.Fatal("应调用 LLM 1 次")
	}

	// 验证传给 LLM 的工具定义
	tools := client.calls[0].tools
	if len(tools) != 1 {
		t.Fatalf("应传 1 个工具定义，实际 %d", len(tools))
	}
	if tools[0].Function.Name != "extract_ticket" {
		t.Errorf("工具名期望 extract_ticket，实际 %q", tools[0].Function.Name)
	}
	if len(tools[0].Function.Parameters.Required) != 3 {
		t.Errorf("Required 字段数期望 3，实际 %d", len(tools[0].Function.Parameters.Required))
	}
}

func TestExtract_DoesNotMutateOriginalMessages(t *testing.T) {
	client := &mockClient{
		responses: []*models.Response{
			{
				FinishReason: "tool_calls",
				ToolCalls: []models.ToolCall{
					{ID: "c1", Function: models.FunctionCall{Name: "extract_ticket", Arguments: `{"category":"订单","priority":99,"summary":"x"}`}},
				},
			},
			{
				FinishReason: "tool_calls",
				ToolCalls: []models.ToolCall{
					{ID: "c2", Function: models.FunctionCall{Name: "extract_ticket", Arguments: `{"category":"订单","priority":3,"summary":"x"}`}},
				},
			},
		},
	}

	msgs := []models.Message{{Role: models.RoleUser, Content: "test"}}
	originalLen := len(msgs)

	ext := NewExtractor[TicketWithValidation](client, ticketSchema())
	_, _ = ext.Extract(context.Background(), msgs)

	if len(msgs) != originalLen {
		t.Errorf("原始 messages 不应被修改，原始长度 %d，当前 %d", originalLen, len(msgs))
	}
}

// ── 辅助函数 ────────────────────────────────────────────────────

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
