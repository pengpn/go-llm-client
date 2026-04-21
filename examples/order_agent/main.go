// order_agent 是一个支持多轮对话的订单查询 Agent。
// 每一轮对话的工具调用历史都存入 Session，后续问题可以引用前面的上下文。
//
// 运行：OPENAI_API_KEY=xxx go run ./examples/order_agent/
package main

import (
	"bufio"
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
	"github.com/pengpn/go-llm-agent/session"
)

// ---- 模拟数据库 ----

type Order struct {
	ID         string    `json:"id"`
	Status     string    `json:"status"`
	Product    string    `json:"product"`
	Amount     float64   `json:"amount"`
	CreatedAt  time.Time `json:"created_at"`
	TrackingNo string    `json:"tracking_no,omitempty"`
	Carrier    string    `json:"carrier,omitempty"`
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

func getOrderStatus(_ context.Context, input string) (string, error) {
	var req struct {
		OrderID string `json:"order_id"`
	}
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	order, ok := orderDB[strings.ToUpper(req.OrderID)]
	if !ok {
		return agent.BuildToolResult(map[string]string{"error": fmt.Sprintf("订单 %s 不存在", req.OrderID)})
	}
	return agent.BuildToolResult(order)
}

func getRefundStatus(_ context.Context, input string) (string, error) {
	var req struct {
		OrderID string `json:"order_id"`
	}
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	record, ok := refundDB[strings.ToUpper(req.OrderID)]
	if !ok {
		return agent.BuildToolResult(map[string]string{"error": fmt.Sprintf("订单 %s 没有退款记录", req.OrderID)})
	}
	return agent.BuildToolResult(record)
}

func listUserOrders(_ context.Context, input string) (string, error) {
	var req struct {
		UserID string `json:"user_id"`
	}
	if err := json.Unmarshal([]byte(input), &req); err != nil {
		return "", fmt.Errorf("参数解析失败: %w", err)
	}
	orders := make([]Order, 0, len(orderDB))
	for _, o := range orderDB {
		orders = append(orders, o)
	}
	return agent.BuildToolResult(map[string]any{"user_id": req.UserID, "orders": orders})
}

func buildRegistry() *agent.Registry {
	r := agent.NewRegistry()
	r.Register(agent.NewTool(
		"get_order_status",
		"查询指定订单的状态、物流信息。当用户询问某个具体订单时使用。",
		models.ToolParameters{
			Type:     "object",
			Properties: map[string]models.ToolProperty{
				"order_id": {Type: "string", Description: "订单号，格式如 ORDER-001"},
			},
			Required: []string{"order_id"},
		},
		getOrderStatus,
	))
	r.Register(agent.NewTool(
		"get_refund_status",
		"查询指定订单的退款进度和退款金额。当用户询问退款时使用。",
		models.ToolParameters{
			Type:     "object",
			Properties: map[string]models.ToolProperty{
				"order_id": {Type: "string", Description: "需要查询退款的订单号"},
			},
			Required: []string{"order_id"},
		},
		getRefundStatus,
	))
	r.Register(agent.NewTool(
		"list_user_orders",
		"查询用户的所有订单列表。当用户询问我的订单或需要概览时使用。",
		models.ToolParameters{
			Type:     "object",
			Properties: map[string]models.ToolProperty{
				"user_id": {Type: "string", Description: "用户 ID"},
			},
			Required: []string{"user_id"},
		},
		listUserOrders,
	))
	return r
}

// ---- Session 工具调用历史写入 ----

// applyHistoryToSession 将 Agent Run 返回的新消息写入 Session。
//
// Run 返回的 history = 我们传入的 initialMsgs + 本轮新增消息。
// 新增消息的结构是：
//   [assistant(ToolCalls)] [tool] [tool] ... [assistant(final)]
//
// 规则：
//   - assistant + 紧随其后的 tool 消息 → 一起用 AddAgentTurn 存入（保持协议顺序）
//   - 最终 assistant 消息（无 ToolCalls）→ AddAssistantMessage
func applyHistoryToSession(sess *session.Session, history []models.Message, initialLen int) {
	newMsgs := history[initialLen:]
	i := 0
	for i < len(newMsgs) {
		msg := newMsgs[i]
		if msg.Role == models.RoleAssistant && len(msg.ToolCalls) > 0 {
			// 收集紧随其后的所有 tool 消息
			j := i + 1
			for j < len(newMsgs) && newMsgs[j].Role == models.RoleTool {
				j++
			}
			sess.AddAgentTurn(msg, newMsgs[i+1:j])
			i = j
		} else if msg.Role == models.RoleAssistant {
			sess.AddAssistantMessage(msg.Content)
			i++
		} else {
			i++
		}
	}
}

// ---- 主程序 ----

const systemPromptBase = `你是一位专业的电商客服助手，名字叫"小智"。

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

	// 用 Session 维护多轮对话历史（含工具调用记录）
	systemPrompt := systemPromptBase + "\n\n当前时间：" + time.Now().Format("2006-01-02 15:04")
	sess := session.NewSession("user_001", systemPrompt, &session.ByTurns{MaxTurns: 20})

	fmt.Println("=== 订单查询 Agent（多轮对话）===")
	fmt.Printf("模型: %s | 工具数: %d\n", cfg.LLM.Model, len(registry.Names()))
	fmt.Println("输入 'quit' 退出，'history' 查看消息历史，'clear' 清空历史")
	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("你: ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		switch input {
		case "quit":
			fmt.Printf("\n%s\n", llmClient.CostSummary())
			return
		case "history":
			msgs := sess.Messages()
			fmt.Printf("[Session 消息数: %d]\n", len(msgs))
			for i, m := range msgs {
				if len(m.ToolCalls) > 0 {
					fmt.Printf("  [%d] %s (含 %d 个工具调用)\n", i, m.Role, len(m.ToolCalls))
				} else {
					preview := m.Content
					if len([]rune(preview)) > 40 {
						preview = string([]rune(preview)[:40]) + "..."
					}
					fmt.Printf("  [%d] %s: %s\n", i, m.Role, preview)
				}
			}
			fmt.Println()
			continue
		case "clear":
			sess.Clear()
			fmt.Println("[对话历史已清除]")
			fmt.Println()
			continue
		}

		// 1. 把用户消息存入 Session
		sess.AddUserMessage(input)

		// 2. 用 Session 当前消息（含完整历史）作为 Agent 的输入
		initialMsgs := sess.Messages()
		initialLen := len(initialMsgs)

		ctx := context.Background()
		answer, history, err := ag.Run(ctx, initialMsgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Agent 执行失败: %v\n", err)
			// 执行失败：把刚加入的 user 消息回滚（清空最新一条）
			// 简单处理：不回滚，下轮用户重试时自然覆盖
			continue
		}

		// 3. 把本轮 Agent 产生的所有消息（工具调用 + 最终回答）写回 Session
		applyHistoryToSession(sess, history, initialLen)

		// 打印工具调用轨迹（仅新增部分）
		for _, msg := range history[initialLen:] {
			if msg.Role == models.RoleAssistant && len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					fmt.Printf("  [调用工具] %s %s\n", tc.Function.Name, tc.Function.Arguments)
				}
			}
		}

		fmt.Printf("小智: %s\n\n", answer)
	}
}
