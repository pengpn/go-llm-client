package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/pengpn/go-llm-agent/models"
	"github.com/pengpn/go-llm-agent/structured"
)

// ExtractedTicket 是自动从对话中提取的工单结构。
// 与 server.Ticket（人工转接工单）不同，这里是结构化输出的提取结果。
type ExtractedTicket struct {
	Category string `json:"category"`
	Priority int    `json:"priority"`
	Summary  string `json:"summary"`
	Emotion  string `json:"emotion"`
}

func (t *ExtractedTicket) Validate() error {
	// 宽松校验：不强制枚举值，仅检查必填
	if t.Category == "" {
		return errorf("category 不能为空")
	}
	if t.Priority < 1 || t.Priority > 5 {
		return errorf("priority 必须在 1-5 之间，当前: %d", t.Priority)
	}
	if t.Summary == "" {
		return errorf("summary 不能为空")
	}
	return nil
}

// TicketExtractor 封装 Extractor[ExtractedTicket]，提供对话级别的工单自动提取。
type TicketExtractor struct {
	ext *structured.Extractor[ExtractedTicket]
}

// NewTicketExtractor 创建工单提取器。
// client 应与 Agent 共享同一个 LLM Client（或使用更便宜的模型以降低成本）。
func NewTicketExtractor(client structured.LLMClient) *TicketExtractor {
	schema := structured.Schema{
		Name:        "extract_ticket",
		Description: "从客服对话中提取工单信息，用于自动分类和分析",
		Properties: map[string]models.ToolProperty{
			"category": {Type: "string", Description: "工单类别：订单/退款/物流/商品/其他"},
			"priority": {Type: "number", Description: "优先级 1-5（5=最紧急）"},
			"summary":  {Type: "string", Description: "问题摘要，50字以内"},
			"emotion":  {Type: "string", Description: "用户情绪：平静/不满/愤怒/焦急"},
		},
		Required: []string{"category", "priority", "summary", "emotion"},
	}
	return &TicketExtractor{
		ext: structured.NewExtractor[ExtractedTicket](client, schema, structured.WithMaxRetries(1)),
	}
}

// Extract 从对话消息中提取工单。
// 提取失败不影响主流程（仅日志记录），返回 nil。
func (te *TicketExtractor) Extract(ctx context.Context, msgs []models.Message) *ExtractedTicket {
	ticket, err := te.ext.Extract(ctx, msgs)
	if err != nil {
		slog.Warn("auto extract ticket failed", "err", err)
		return nil
	}
	return &ticket
}

func errorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
