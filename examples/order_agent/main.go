// order_agent 演示如何用 Agent Loop 实现一个能查询订单状态的客服 Agent。
//
// 运行：OPENAI_API_KEY=xxx go run ./examples/order_agent/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pengpn/go-llm-agent/agent"
	"github.com/pengpn/go-llm-agent/client"
	"github.com/pengpn/go-llm-agent/config"
	"github.com/pengpn/go-llm-agent/models"
)

// ---- 模拟数据库 ----
// 真实系统中这里会查询数据库或调用内部 API

type Order struct {
	ID          string    `json:"id"`
	Status      string    `json:"status"`
	Product     string    `json:"product"`
	Amount      float64   `json:"amount"`
	CreatedAt   time.Time `json:"created_at"`
	TrackingNo  string    `json:"tracking_no,omitempty"`
	Carrier     string    `json:"carrier,omitempty"`
}

type RefundRecord struct {
	OrderID   string    `json:"order_id"`
	Status    string    `json:"status"`
	Amount    float64   `json:"amount"`
	CreatedAt time.Time `json:"created_at"`
	Reason    string    `json:"reason"`
}

var orderDB = map[string]Order{
	"ORDER-001": {
		ID: "ORDER-001", Status: "已发货", Product: "iPhone 16 Pro",
		Amount: 8999, CreatedAt: time.Now().Add(-48 * time.Hour),
		TrackingNo: "SF1234567890", Carrier: "顺丰速运",
	},
	"ORDER-002": {
		ID: "ORDER-002", Status: "待付款", Product: "AirPods Pro",
		Amount: 1799, CreatedAt: time.Now().Add(-1 * time.Hour),
	},
	"ORDER-003": {
		ID: "ORDER-003", Status: "已完成", Product: "MacBook Pro M4",
		Amount: 14999, CreatedAt: time.Now().Add(-7 * 24 * time.Hour),
	},
}

var refundDB = map[string]RefundRecord{
	"ORDER-003": {
		OrderID: "ORDER-003", Status: "退款成功",
		Amount: 14999, Reason: "商品质量问题",
		CreatedAt: time.Now().Add(-2 * 24 * time.Hour),
	},
}

// ---- 工具实现 ----

type getOrderInput struct {
	OrderID string `json:"order_id"`
}

func getOrderStatus(_ context.Context, input string) (string, error) {
	var req getOrderInput
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}

	order, ok := orderDB[strings.ToUpper(req.OrderID)]
	if !ok {
		return agent.BuildToolResult(map[string]string{
			"error": fmt.Sprintf("订单 %s 不存在", req.OrderID),
		})
	}

	return agent.BuildToolResult(order)
}

type getRefundInput struct {
	OrderID string `json:"order_id"`
}

func getRefundStatus(_ context.Context, input string) (string, error) {
	var req getRefundInput
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}

	record, ok := refundDB[strings.ToUpper(req.OrderID)]
	if !ok {
		return agent.BuildToolResult(map[string]string{
			"error": fmt.Sprintf("订单 %s 没有退款记录", req.OrderID),
		})
	}

	return agent.BuildToolResult(record)
}

type listOrdersInput struct {
	UserID string `json:"user_id"`
}

func listUserOrders(_ context.Context, input string) (string, error) {
	var req listOrdersInput
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}

	// 模拟：返回所有订单（真实系统按 user_id 过滤）
	orders := make([]Order, 0, len(orderDB))
	for _, o := range orderDB {
		orders = append(orders, o)
	}

	return agent.BuildToolResult(map[string]any{
		"user_id": req.UserID,
		"orders":  orders,
		"total":   len(orders),
	})
}

// ---- 工具注册 ----

func buildRegistry() *agent.Registry {
	r := agent.NewRegistry()

	r.Register(agent.NewTool(
		"get_order_status",
		"查询指定订单的状态、物流信息。当用户询问某个具体订单时使用。",
		models.ToolParameters{
			Type: "object",
			Properties: map[string]models.ToolProperty{
				"order_id": {
					Type:        "string",
					Description: "订单号，格式如 ORDER-001",
				},
			},
			Required: []string{"order_id"},
		},
		getOrderStatus,
	))

	r.Register(agent.NewTool(
		"get_refund_status",
		"查询指定订单的退款进度和退款金额。当用户询问退款时使用。",
		models.ToolParameters{
			Type: "object",
			Properties: map[string]models.ToolProperty{
				"order_id": {
					Type:        "string",
					Description: "需要查询退款的订单号",
				},
			},
			Required: []string{"order_id"},
		},
		getRefundStatus,
	))

	r.Register(agent.NewTool(
		"list_user_orders",
		"查询用户的所有订单列表。当用户询问我的订单或需要概览时使用。",
		models.ToolParameters{
			Type: "object",
			Properties: map[string]models.ToolProperty{
				"user_id": {
					Type:        "string",
					Description: "用户 ID",
				},
			},
			Required: []string{"user_id"},
		},
		listUserOrders,
	))

	return r
}

// ---- 主程序 ----

const systemPrompt = `你是一位专业的电商客服助手，名字叫"小智"。

你有以下工具可以使用：
- get_order_status: 查询订单状态和物流
- get_refund_status: 查询退款进度
- list_user_orders: 查询用户所有订单

处理用户请求时：
1. 先判断是否需要调用工具获取信息
2. 调用工具后，用友好的语言向用户解释结果
3. 如果用户没有提供订单号，先调用 list_user_orders 查看订单列表
4. 不确定的信息不要猜测

当前用户 ID：USER-001`

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置加载失败: %v\n", err)
		os.Exit(1)
	}

	llmClient := client.NewFromConfig(&cfg.LLM)
	registry := buildRegistry()
	ag := agent.New(llmClient, registry)

	// 测试用例
	queries := []string{
		"我的订单 ORDER-001 到哪里了？",
		"ORDER-003 的退款怎么样了？",
		"帮我查一下我所有的订单",
	}

	fmt.Println("=== 订单查询 Agent ===")
	fmt.Printf("模型: %s | 工具数: %d\n\n", cfg.LLM.Model, len(registry.Names()))

	for _, query := range queries {
		fmt.Printf("用户: %s\n", query)

		messages := []models.Message{
			{Role: models.RoleSystem, Content: systemPrompt},
			{Role: models.RoleUser, Content: query},
		}

		ctx := context.Background()
		answer, trace, err := ag.RunWithTrace(ctx, messages)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Agent 执行失败: %v\n", err)
			continue
		}

		// 打印执行轨迹（调试用）
		for _, step := range trace {
			for _, call := range step.ToolCalls {
				fmt.Printf("  [工具调用] %s(%s)\n", call.Name, call.Input)
				if call.Error != "" {
					fmt.Printf("  [工具错误] %s\n", call.Error)
				} else {
					fmt.Printf("  [工具结果] %s\n", call.Output)
				}
			}
		}

		fmt.Printf("小智: %s\n\n", answer)
		fmt.Println(strings.Repeat("-", 60))
	}

	fmt.Printf("\n%s\n", llmClient.CostSummary())
}
