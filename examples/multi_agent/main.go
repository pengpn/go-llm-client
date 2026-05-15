package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pengpn/go-llm-agent/agent"
	"github.com/pengpn/go-llm-agent/client"
	"github.com/pengpn/go-llm-agent/models"
	"github.com/pengpn/go-llm-agent/session"
)

func main() {
	_ = godotenv.Load()

	apiKey := os.Getenv("LLM_API_KEY")
	baseURL := os.Getenv("LLM_BASE_URL")
	model := os.Getenv("LLM_MODEL")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "请设置 LLM_API_KEY 环境变量")
		os.Exit(1)
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	if model == "" {
		model = "gpt-4o-mini"
	}

	llmClient := client.New(
		client.WithAPIKey(apiKey),
		client.WithBaseURL(baseURL),
		client.WithModel(model),
	)

	// ── 定义各领域的工具 ────────────────────────────────────────

	orderTools := buildOrderTools()
	logisticsTools := buildLogisticsTools()
	refundTools := buildRefundTools()

	// ── 定义路由 ────────────────────────────────────────────────

	routes := []agent.Route{
		{
			Category:    "order",
			Description: "订单查询、订单状态、下单相关问题",
			Factory: func() *agent.Agent {
				reg := agent.NewRegistry()
				for _, t := range orderTools {
					reg.Register(t)
				}
				return agent.New(llmClient, reg)
			},
		},
		{
			Category:    "logistics",
			Description: "物流追踪、快递查询、配送时间",
			Factory: func() *agent.Agent {
				reg := agent.NewRegistry()
				for _, t := range logisticsTools {
					reg.Register(t)
				}
				return agent.New(llmClient, reg)
			},
		},
		{
			Category:    "refund",
			Description: "退款、退货、取消订单、售后问题",
			Factory: func() *agent.Agent {
				reg := agent.NewRegistry()
				for _, t := range refundTools {
					reg.Register(t)
				}
				return agent.New(llmClient, reg)
			},
		},
	}

	// FAQ Agent 作为 fallback（无工具，纯 LLM 回答）
	faqFactory := func() *agent.Agent {
		return agent.New(llmClient, agent.NewRegistry())
	}

	router := agent.NewRouter(llmClient, routes, agent.WithFallback(faqFactory))

	// ── 交互式对话循环 ──────────────────────────────────────────

	sess := session.NewSession("multi-agent-demo", `你是一个友好的电商客服助手。`, &session.ByTurns{MaxTurns: 20})
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Println("🤖 多 Agent 客服系统已启动")
	fmt.Println("   支持：订单查询 | 物流追踪 | 退款售后 | 通用FAQ")
	fmt.Println("   输入 'quit' 退出")
	fmt.Println()

	for {
		fmt.Print("你: ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "quit" || input == "exit" {
			fmt.Println("再见！")
			break
		}

		sess.AddUserMessage(input)
		msgs := sess.Messages()

		result, err := router.Route(context.Background(), msgs)
		if err != nil {
			fmt.Printf("❌ 错误: %v\n\n", err)
			continue
		}

		fmt.Printf("\n[Router → %s] %s\n", result.Intent.Category, result.Intent.Reason)
		fmt.Printf("助手: %s\n\n", result.Answer)

		sess.AddAssistantMessage(result.Answer)
	}
}

// ── 各领域工具定义 ──────────────────────────────────────────────

func buildOrderTools() []*agent.Tool {
	// 模拟数据
	orders := map[string]map[string]string{
		"ORDER-001": {"status": "已发货", "amount": "299元", "item": "无线耳机"},
		"ORDER-002": {"status": "处理中", "amount": "599元", "item": "机械键盘"},
	}

	getOrder := agent.NewTool("get_order", "根据订单号查询订单详情",
		models.ToolParameters{
			Type: "object",
			Properties: map[string]models.ToolProperty{
				"order_id": {Type: "string", Description: "订单号，如 ORDER-001"},
			},
			Required: []string{"order_id"},
		},
		func(_ context.Context, input string) (string, error) {
			var req struct{ OrderID string `json:"order_id"` }
			if err := decodeJSON(input, &req); err != nil {
				return "", err
			}
			order, ok := orders[req.OrderID]
			if !ok {
				return fmt.Sprintf("未找到订单 %s", req.OrderID), nil
			}
			return fmt.Sprintf("订单 %s：商品=%s，状态=%s，金额=%s",
				req.OrderID, order["item"], order["status"], order["amount"]), nil
		},
	)

	listOrders := agent.NewTool("list_orders", "列出用户所有订单",
		models.ToolParameters{Type: "object", Properties: map[string]models.ToolProperty{}},
		func(_ context.Context, _ string) (string, error) {
			var sb strings.Builder
			sb.WriteString("用户订单列表：\n")
			for id, o := range orders {
				sb.WriteString(fmt.Sprintf("- %s: %s (%s)\n", id, o["item"], o["status"]))
			}
			return sb.String(), nil
		},
	)

	return []*agent.Tool{getOrder, listOrders}
}

func buildLogisticsTools() []*agent.Tool {
	trackings := map[string]map[string]string{
		"ORDER-001": {"carrier": "顺丰", "tracking": "SF1234567890", "status": "广州 → 上海，在途"},
	}

	trackLogistics := agent.NewTool("track_logistics", "查询订单的物流信息",
		models.ToolParameters{
			Type: "object",
			Properties: map[string]models.ToolProperty{
				"order_id": {Type: "string", Description: "订单号"},
			},
			Required: []string{"order_id"},
		},
		func(_ context.Context, input string) (string, error) {
			var req struct{ OrderID string `json:"order_id"` }
			if err := decodeJSON(input, &req); err != nil {
				return "", err
			}
			info, ok := trackings[req.OrderID]
			if !ok {
				return fmt.Sprintf("订单 %s 暂无物流信息", req.OrderID), nil
			}
			return fmt.Sprintf("订单 %s 物流：%s 快递 %s，当前 %s",
				req.OrderID, info["carrier"], info["tracking"], info["status"]), nil
		},
	)

	return []*agent.Tool{trackLogistics}
}

func buildRefundTools() []*agent.Tool {
	applyRefund := agent.NewTool("apply_refund", "申请退款",
		models.ToolParameters{
			Type: "object",
			Properties: map[string]models.ToolProperty{
				"order_id": {Type: "string", Description: "订单号"},
				"reason":   {Type: "string", Description: "退款原因"},
			},
			Required: []string{"order_id", "reason"},
		},
		func(_ context.Context, input string) (string, error) {
			var req struct {
				OrderID string `json:"order_id"`
				Reason  string `json:"reason"`
			}
			if err := decodeJSON(input, &req); err != nil {
				return "", err
			}
			return fmt.Sprintf("退款申请已提交：订单 %s，原因：%s。预计 3-5 个工作日处理。",
				req.OrderID, req.Reason), nil
		},
	)

	checkRefundPolicy := agent.NewTool("check_refund_policy", "查询退款政策",
		models.ToolParameters{Type: "object", Properties: map[string]models.ToolProperty{}},
		func(_ context.Context, _ string) (string, error) {
			return "退款政策：七天无理由退货（商品需未拆封）；已发货订单需收货后申请退货退款；退款 3-5 个工作日原路退回。", nil
		},
	)

	return []*agent.Tool{applyRefund, checkRefundPolicy}
}

func decodeJSON(input string, v any) error {
	return json.Unmarshal([]byte(input), v)
}
