// order_agent 是一个支持多轮对话的订单查询 Agent。
// 演示了：NewTypedTool（类型安全）、参数校验（Validator）、权限控制（RoleGate）、
// 错误恢复策略（ContinueOnError / AbortOnError）。
//
// 运行：OPENAI_API_KEY=xxx go run ./examples/order_agent/
// 管理员模式：ROLE=admin OPENAI_API_KEY=xxx go run ./examples/order_agent/
package main

import (
	"bufio"
	"context"
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

// ---- 类型化参数（使用 NewTypedTool 后，工具函数直接接收这些结构体）----

type OrderIDReq struct {
	OrderID string `json:"order_id"`
}

// Validate 实现 agent.Validator 接口，在解码后自动校验。
func (r *OrderIDReq) Validate() error {
	if strings.TrimSpace(r.OrderID) == "" {
		return fmt.Errorf("order_id 不能为空")
	}
	return nil
}

type UserIDReq struct {
	UserID string `json:"user_id"`
}

func (r *UserIDReq) Validate() error {
	if strings.TrimSpace(r.UserID) == "" {
		return fmt.Errorf("user_id 不能为空")
	}
	return nil
}

type CancelOrderReq struct {
	OrderID string `json:"order_id"`
	Reason  string `json:"reason"`
}

func (r *CancelOrderReq) Validate() error {
	if strings.TrimSpace(r.OrderID) == "" {
		return fmt.Errorf("order_id 不能为空")
	}
	if strings.TrimSpace(r.Reason) == "" {
		return fmt.Errorf("取消原因 reason 不能为空")
	}
	return nil
}

// ---- 工具实现（无 json.Unmarshal 样板代码）----

func getOrderStatus(_ context.Context, req OrderIDReq) (string, error) {
	order, ok := orderDB[strings.ToUpper(req.OrderID)]
	if !ok {
		return agent.BuildToolResult(map[string]string{"error": fmt.Sprintf("订单 %s 不存在", req.OrderID)})
	}
	return agent.BuildToolResult(order)
}

func getRefundStatus(_ context.Context, req OrderIDReq) (string, error) {
	record, ok := refundDB[strings.ToUpper(req.OrderID)]
	if !ok {
		return agent.BuildToolResult(map[string]string{"error": fmt.Sprintf("订单 %s 没有退款记录", req.OrderID)})
	}
	return agent.BuildToolResult(record)
}

func listUserOrders(_ context.Context, req UserIDReq) (string, error) {
	orders := make([]Order, 0, len(orderDB))
	for _, o := range orderDB {
		orders = append(orders, o)
	}
	return agent.BuildToolResult(map[string]any{"user_id": req.UserID, "orders": orders})
}

// cancelOrder 是管理员专属工具。
// 演示 AbortOnError 场景：取消前必须先查订单状态，若查询失败则不应继续取消。
func cancelOrder(_ context.Context, req CancelOrderReq) (string, error) {
	id := strings.ToUpper(req.OrderID)
	order, ok := orderDB[id]
	if !ok {
		return agent.BuildToolResult(map[string]string{"error": fmt.Sprintf("订单 %s 不存在", req.OrderID)})
	}
	if order.Status == "已发货" || order.Status == "已完成" {
		return agent.BuildToolResult(map[string]string{
			"error": fmt.Sprintf("订单 %s 状态为「%s」，无法取消", req.OrderID, order.Status),
		})
	}
	// 模拟取消成功
	order.Status = "已取消"
	orderDB[id] = order
	return agent.BuildToolResult(map[string]string{
		"order_id": req.OrderID,
		"status":   "已取消",
		"reason":   req.Reason,
	})
}

// ---- 权限控制 ----

func buildGate() *agent.RoleGate {
	return agent.NewRoleGate("user"). // 未指定角色的用户默认为 "user"
						DefineRole("user",
			"get_order_status",
			"get_refund_status",
			"list_user_orders",
		).
		DefineRole("admin",
			"get_order_status",
			"get_refund_status",
			"list_user_orders",
			"cancel_order", // 管理员额外可用
		).
		AssignRole("ADMIN-001", "admin").
		AssignRole("USER-001", "user")
}

// ---- 工具注册（使用 NewTypedTool）----

func buildRegistry() *agent.Registry {
	r := agent.NewRegistry()

	r.Register(agent.NewTypedTool("get_order_status",
		"查询指定订单的状态、物流信息。当用户询问某个具体订单时使用。",
		models.ToolParameters{
			Type: "object",
			Properties: map[string]models.ToolProperty{
				"order_id": {Type: "string", Description: "订单号，格式如 ORDER-001"},
			},
			Required: []string{"order_id"},
		},
		getOrderStatus,
	))

	r.Register(agent.NewTypedTool("get_refund_status",
		"查询指定订单的退款进度和退款金额。当用户询问退款时使用。",
		models.ToolParameters{
			Type: "object",
			Properties: map[string]models.ToolProperty{
				"order_id": {Type: "string", Description: "需要查询退款的订单号"},
			},
			Required: []string{"order_id"},
		},
		getRefundStatus,
	))

	r.Register(agent.NewTypedTool("list_user_orders",
		"查询用户的所有订单列表。当用户询问所有订单或需要概览时使用。",
		models.ToolParameters{
			Type: "object",
			Properties: map[string]models.ToolProperty{
				"user_id": {Type: "string", Description: "用户 ID"},
			},
			Required: []string{"user_id"},
		},
		listUserOrders,
	))

	r.Register(agent.NewTypedTool("cancel_order",
		"取消指定订单。仅在用户明确要求取消且订单状态允许时使用。需要提供取消原因。",
		models.ToolParameters{
			Type: "object",
			Properties: map[string]models.ToolProperty{
				"order_id": {Type: "string", Description: "要取消的订单号"},
				"reason":   {Type: "string", Description: "取消原因"},
			},
			Required: []string{"order_id", "reason"},
		},
		cancelOrder,
	))

	return r
}

// ---- Session 工具调用历史写入 ----

func applyHistoryToSession(sess *session.Session, history []models.Message, initialLen int) {
	newMsgs := history[initialLen:]
	i := 0
	for i < len(newMsgs) {
		msg := newMsgs[i]
		if msg.Role == models.RoleAssistant && len(msg.ToolCalls) > 0 {
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
- cancel_order: 取消订单（仅管理员可见）

处理用户请求时：
1. 先判断是否需要调用工具获取信息
2. 调用工具后，用友好的语言向用户解释结果
3. 如果用户没有提供订单号，先调用 list_user_orders 查看订单列表
4. 不确定的信息不要猜测`

const sessionPath = "./data/sessions/user_001.json"

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置加载失败: %v\n", err)
		os.Exit(1)
	}

	llmClient := client.NewFromConfig(&cfg.LLM)
	registry := buildRegistry()
	gate := buildGate()

	// 通过环境变量切换角色，演示权限控制
	userID := "USER-001"
	if os.Getenv("ROLE") == "admin" {
		userID = "ADMIN-001"
	}

	// Agent 设置默认 Gate（也可以在每次 Run 时通过 WithGate 覆盖）
	ag := agent.New(llmClient, registry, agent.WithDefaultGate(gate))

	strategy := &session.ByTurns{MaxTurns: 20}
	systemPrompt := systemPromptBase + "\n\n当前用户 ID：" + userID +
		"\n当前时间：" + time.Now().Format("2006-01-02 15:04")

	sess, err := session.LoadSession(sessionPath, strategy)
	if err != nil {
		sess = session.NewSession(userID, systemPrompt, strategy)
		fmt.Println("[新会话]")
	} else {
		fmt.Printf("[已恢复上次会话，共 %d 条历史消息]\n", sess.MessageCount())
	}

	role := "普通用户"
	if userID == "ADMIN-001" {
		role = "管理员（可取消订单）"
	}
	fmt.Println("=== 订单查询 Agent（多轮对话）===")
	fmt.Printf("用户：%s（%s）| 模型：%s\n", userID, role, cfg.LLM.Model)
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
			_ = os.Remove(sessionPath)
			fmt.Println("[对话历史已清除]")
			fmt.Println()
			continue
		}

		sess.AddUserMessage(input)

		initialMsgs := sess.Messages()
		initialLen := len(initialMsgs)

		ctx := context.Background()

		// 普通用户用默认 ContinueOnError；管理员取消订单用 AbortOnError 更安全
		// 这里统一用 WithUser 注入权限，Agent 自动过滤工具
		answer, history, err := ag.Run(ctx, initialMsgs, agent.WithUser(userID))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Agent 执行失败: %v\n", err)
			continue
		}

		applyHistoryToSession(sess, history, initialLen)

		if err := sess.Save(sessionPath); err != nil {
			fmt.Fprintf(os.Stderr, "  [警告] Session 保存失败: %v\n", err)
		}

		// 打印工具调用轨迹
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
