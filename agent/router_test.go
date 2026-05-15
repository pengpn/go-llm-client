package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/pengpn/go-llm-agent/models"
)

// ── parseIntent 单元测试 ────────────────────────────────────────

func TestParseIntent_ValidJSON(t *testing.T) {
	intent, err := parseIntent(`{"category": "order", "reason": "查订单"}`)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if intent.Category != "order" {
		t.Errorf("Category 期望 order，实际 %q", intent.Category)
	}
	if intent.Reason != "查订单" {
		t.Errorf("Reason 期望 '查订单'，实际 %q", intent.Reason)
	}
}

func TestParseIntent_MarkdownWrapped(t *testing.T) {
	content := "```json\n{\"category\": \"logistics\", \"reason\": \"物流\"}\n```"
	intent, err := parseIntent(content)
	if err != nil {
		t.Fatalf("解析 markdown 包裹失败: %v", err)
	}
	if intent.Category != "logistics" {
		t.Errorf("Category 期望 logistics，实际 %q", intent.Category)
	}
}

func TestParseIntent_EmptyCategory(t *testing.T) {
	_, err := parseIntent(`{"category": "", "reason": "不知道"}`)
	if err == nil {
		t.Fatal("空 category 应该报错")
	}
}

func TestParseIntent_InvalidJSON(t *testing.T) {
	_, err := parseIntent("这不是 JSON")
	if err == nil {
		t.Fatal("无效 JSON 应该报错")
	}
}

// ── buildRouterPrompt 测试 ──────────────────────────────────────

func TestBuildRouterPrompt_ContainsAllCategories(t *testing.T) {
	routes := []Route{
		{Category: "order", Description: "订单查询"},
		{Category: "refund", Description: "退款问题"},
	}
	prompt := buildRouterPrompt(routes)
	for _, r := range routes {
		if !containsSubstr(prompt, r.Category) {
			t.Errorf("prompt 中缺少类别: %q", r.Category)
		}
		if !containsSubstr(prompt, r.Description) {
			t.Errorf("prompt 中缺少描述: %q", r.Description)
		}
	}
	if !containsSubstr(prompt, "JSON") {
		t.Error("prompt 中应包含 JSON 输出要求")
	}
}

// ── Router.Route 集成测试 ───────────────────────────────────────

func TestRouter_Route_OrderIntent(t *testing.T) {
	// Router LLM：返回意图分类 JSON
	// Sub-Agent LLM：返回最终回答
	routerClient := &mockClient{
		responses: []*models.Response{
			{Content: `{"category": "order", "reason": "查订单"}`, FinishReason: "stop"},
		},
	}
	subAgentClient := &mockClient{
		responses: []*models.Response{
			{Content: "ORDER-001 已发货", FinishReason: "stop"},
		},
	}

	routes := []Route{
		{
			Category:    "order",
			Description: "订单相关",
			Factory: func() *Agent {
				reg := NewRegistry()
				return New(subAgentClient, reg)
			},
		},
	}

	router := NewRouter(routerClient, routes)
	msgs := []models.Message{{Role: models.RoleUser, Content: "我的订单到哪了"}}

	result, err := router.Route(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Route 失败: %v", err)
	}

	if result.Intent.Category != "order" {
		t.Errorf("意图类别期望 order，实际 %q", result.Intent.Category)
	}
	if result.Answer != "ORDER-001 已发货" {
		t.Errorf("回答期望 'ORDER-001 已发货'，实际 %q", result.Answer)
	}
}

func TestRouter_Route_Fallback(t *testing.T) {
	routerClient := &mockClient{
		responses: []*models.Response{
			{Content: `{"category": "unknown_category", "reason": "无法识别"}`, FinishReason: "stop"},
		},
	}
	fallbackClient := &mockClient{
		responses: []*models.Response{
			{Content: "我是FAQ助手，请问有什么可以帮您？", FinishReason: "stop"},
		},
	}

	routes := []Route{
		{Category: "order", Description: "订单", Factory: func() *Agent {
			return New(&mockClient{}, NewRegistry())
		}},
	}
	router := NewRouter(routerClient, routes, WithFallback(func() *Agent {
		return New(fallbackClient, NewRegistry())
	}))

	msgs := []models.Message{{Role: models.RoleUser, Content: "今天天气怎么样"}}
	result, err := router.Route(context.Background(), msgs)
	if err != nil {
		t.Fatalf("Fallback Route 失败: %v", err)
	}

	if result.Answer != "我是FAQ助手，请问有什么可以帮您？" {
		t.Errorf("Fallback 回答不匹配: %q", result.Answer)
	}
}

func TestRouter_Route_NoFallback_ReturnsError(t *testing.T) {
	routerClient := &mockClient{
		responses: []*models.Response{
			{Content: `{"category": "unknown", "reason": "不认识"}`, FinishReason: "stop"},
		},
	}

	routes := []Route{
		{Category: "order", Description: "订单", Factory: func() *Agent {
			return New(&mockClient{}, NewRegistry())
		}},
	}
	// 不设置 fallback
	router := NewRouter(routerClient, routes)

	msgs := []models.Message{{Role: models.RoleUser, Content: "随便说点什么"}}
	_, err := router.Route(context.Background(), msgs)
	if err == nil {
		t.Fatal("无 fallback 且类别不匹配时应返回错误")
	}
}

func TestRouter_Route_CaseInsensitive(t *testing.T) {
	routerClient := &mockClient{
		responses: []*models.Response{
			// LLM 返回大写
			{Content: `{"category": "ORDER", "reason": "订单"}`, FinishReason: "stop"},
		},
	}
	subAgentClient := &mockClient{
		responses: []*models.Response{
			{Content: "匹配成功", FinishReason: "stop"},
		},
	}

	routes := []Route{
		{Category: "order", Description: "订单", Factory: func() *Agent {
			return New(subAgentClient, NewRegistry())
		}},
	}
	router := NewRouter(routerClient, routes)

	result, err := router.Route(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "查订单"},
	})
	if err != nil {
		t.Fatalf("大小写不敏感匹配失败: %v", err)
	}
	if result.Answer != "匹配成功" {
		t.Errorf("回答不匹配: %q", result.Answer)
	}
}

func TestRouter_Route_PassesRunOptions(t *testing.T) {
	routerClient := &mockClient{
		responses: []*models.Response{
			{Content: `{"category": "order", "reason": "查订单"}`, FinishReason: "stop"},
		},
	}

	// Sub-Agent 使用 RoleGate：只允许 admin 调用 cancel_order
	subAgentClient := &mockClient{
		responses: []*models.Response{
			{Content: "您无权取消订单", FinishReason: "stop"},
		},
	}

	reg := NewRegistry()
	reg.Register(NewTool("get_order", "查订单", models.ToolParameters{}, func(_ context.Context, _ string) (string, error) {
		return "ok", nil
	}))
	reg.Register(NewTool("cancel_order", "取消订单", models.ToolParameters{}, func(_ context.Context, _ string) (string, error) {
		return "cancelled", nil
	}))

	routes := []Route{
		{Category: "order", Description: "订单", Factory: func() *Agent {
			gate := NewRoleGate("user").
				DefineRole("admin", "get_order", "cancel_order").
				DefineRole("user", "get_order").
				AssignRole("user", "user").
				AssignRole("admin", "admin")
			return New(subAgentClient, reg, WithDefaultGate(gate))
		}},
	}
	router := NewRouter(routerClient, routes)

	// 传递 WithUser("user") — Sub-Agent 应该只看到 get_order
	result, err := router.Route(context.Background(),
		[]models.Message{{Role: models.RoleUser, Content: "取消我的订单"}},
		WithUser("user"),
	)
	if err != nil {
		t.Fatalf("Route with RunOptions 失败: %v", err)
	}

	// 验证 Sub-Agent 被调用时只看到 1 个工具（get_order）
	if len(subAgentClient.calls) == 0 {
		t.Fatal("Sub-Agent 未被调用")
	}
	toolCount := len(subAgentClient.calls[0].tools)
	if toolCount != 1 {
		t.Errorf("user 角色应只看到 1 个工具，实际看到 %d 个", toolCount)
	}

	fmt.Println("Answer:", result.Answer)
}

func TestRouter_Route_WithToolCalls(t *testing.T) {
	// Router 识别意图
	routerClient := &mockClient{
		responses: []*models.Response{
			{Content: `{"category": "order", "reason": "查订单"}`, FinishReason: "stop"},
		},
	}

	// Sub-Agent：先调工具，再给最终回答（模拟完整 Agent Loop）
	subAgentClient := &mockClient{
		responses: []*models.Response{
			// 第一轮：LLM 决定调用工具
			{
				Content:      "",
				FinishReason: "tool_calls",
				ToolCalls: []models.ToolCall{
					{ID: "call-1", Function: models.FunctionCall{Name: "get_order", Arguments: `{"order_id":"ORDER-001"}`}},
				},
			},
			// 第二轮：收到工具结果后给出最终回答
			{Content: "订单 ORDER-001 已发货，预计明天送达", FinishReason: "stop"},
		},
	}

	reg := NewRegistry()
	reg.Register(NewTool("get_order", "查询订单", models.ToolParameters{}, func(_ context.Context, _ string) (string, error) {
		return `{"order_id":"ORDER-001","status":"已发货"}`, nil
	}))

	routes := []Route{
		{Category: "order", Description: "订单", Factory: func() *Agent {
			return New(subAgentClient, reg)
		}},
	}
	router := NewRouter(routerClient, routes)

	result, err := router.Route(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "ORDER-001 什么状态"},
	})
	if err != nil {
		t.Fatalf("Route with tool calls 失败: %v", err)
	}

	if result.Intent.Category != "order" {
		t.Errorf("意图类别期望 order，实际 %q", result.Intent.Category)
	}
	if result.Answer != "订单 ORDER-001 已发货，预计明天送达" {
		t.Errorf("回答不匹配: %q", result.Answer)
	}

	// Sub-Agent 应被调用两轮（工具调用 + 最终回答）
	if len(subAgentClient.calls) != 2 {
		t.Errorf("Sub-Agent 应被调用 2 次，实际 %d 次", len(subAgentClient.calls))
	}
}

// ── 辅助函数 ────────────────────────────────────────────────────

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
