package server

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/pengpn/go-llm-agent/agent"
	"github.com/pengpn/go-llm-agent/models"
)

// TicketStatus 代表工单的处理状态。
type TicketStatus string

const (
	TicketStatusPending  TicketStatus = "pending"
	TicketStatusResolved TicketStatus = "resolved"
)

// Ticket 是人工转接工单，记录转接原因和对话摘要。
type Ticket struct {
	ID        string       `json:"id"`
	UserID    string       `json:"user_id"`
	Reason    string       `json:"reason"`
	Summary   string       `json:"summary"`
	Status    TicketStatus `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
}

// TicketStore 是线程安全的内存工单存储。
// 生产环境可替换为 PostgreSQL / MySQL 实现，接口保持一致。
type TicketStore struct {
	mu      sync.Mutex
	tickets []*Ticket
	counter int
}

// NewTicketStore 创建空的工单存储。
func NewTicketStore() *TicketStore {
	return &TicketStore{}
}

// Create 新建一条工单并返回。
func (ts *TicketStore) Create(userID, reason, summary string) *Ticket {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.counter++
	t := &Ticket{
		ID:        fmt.Sprintf("TK-%04d", ts.counter),
		UserID:    userID,
		Reason:    reason,
		Summary:   summary,
		Status:    TicketStatusPending,
		CreatedAt: time.Now(),
	}
	ts.tickets = append(ts.tickets, t)
	return t
}

// List 返回所有工单的副本（可选按状态过滤，空字符串表示不过滤）。
func (ts *TicketStore) List(statusFilter TicketStatus) []*Ticket {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	result := make([]*Ticket, 0, len(ts.tickets))
	for _, t := range ts.tickets {
		if statusFilter == "" || t.Status == statusFilter {
			result = append(result, t)
		}
	}
	return result
}

// userIDContextKey 是注入 userID 到 context 的专属类型键，防止与其他包冲突。
type userIDContextKey struct{}

// ContextWithUserID 将 userID 注入 context，供工具函数读取。
func ContextWithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDContextKey{}, userID)
}

// userIDFromContext 从 context 中读取 userID，未注入时返回空字符串。
func userIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(userIDContextKey{}).(string)
	return v
}

// TransferInput 是 transfer_to_human 工具的输入参数。
type TransferInput struct {
	Reason  string `json:"reason"`
	Summary string `json:"summary"`
}

// NewTransferTool 创建 transfer_to_human 工具，写入到 store。
// userID 通过 context 传入（由 processChat/handleChatStream 在调用 ag.Run 前注入）。
func NewTransferTool(store *TicketStore) *agent.Tool {
	return agent.NewTypedTool[TransferInput](
		"transfer_to_human",
		"当无法解决用户问题时，将对话转接给人工客服并生成工单。"+
			"适用场景：知识库无答案、退款争议、用户明确要求人工、涉及法律风险。",
		models.ToolParameters{
			Type: "object",
			Properties: map[string]models.ToolProperty{
				"reason": {
					Type:        "string",
					Description: "转接原因，简明说明为何需要人工介入（20字以内）",
				},
				"summary": {
					Type:        "string",
					Description: "对话摘要，给人工客服的背景信息，包含用户诉求和已尝试的解决方案（100字以内）",
				},
			},
			Required: []string{"reason"},
		},
		func(ctx context.Context, input TransferInput) (string, error) {
			userID := userIDFromContext(ctx)
			ticket := store.Create(userID, input.Reason, input.Summary)
			return fmt.Sprintf(
				"已为您生成工单 #%s，人工客服将在工作时间内联系您。如需加急处理，可拨打客服热线 400-888-8888。",
				ticket.ID,
			), nil
		},
	)
}
