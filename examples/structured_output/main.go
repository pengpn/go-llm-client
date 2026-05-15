package main

import (
	"context"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/pengpn/go-llm-agent/client"
	"github.com/pengpn/go-llm-agent/models"
	"github.com/pengpn/go-llm-agent/structured"
)

// Ticket 从客服对话中提取的工单结构
type Ticket struct {
	Category string `json:"category"`
	Priority int    `json:"priority"`
	Summary  string `json:"summary"`
	Emotion  string `json:"emotion"`
}

func (t *Ticket) Validate() error {
	validCategories := map[string]bool{"订单": true, "退款": true, "物流": true, "商品": true, "其他": true}
	if !validCategories[t.Category] {
		return fmt.Errorf("category 必须是 订单/退款/物流/商品/其他 之一，当前: %q", t.Category)
	}
	if t.Priority < 1 || t.Priority > 5 {
		return fmt.Errorf("priority 必须在 1-5 之间，当前: %d", t.Priority)
	}
	if t.Summary == "" {
		return fmt.Errorf("summary 不能为空")
	}
	validEmotions := map[string]bool{"平静": true, "不满": true, "愤怒": true, "焦急": true}
	if !validEmotions[t.Emotion] {
		return fmt.Errorf("emotion 必须是 平静/不满/愤怒/焦急 之一，当前: %q", t.Emotion)
	}
	return nil
}

func main() {
	_ = godotenv.Load()

	apiKey := os.Getenv("LLM_API_KEY")
	baseURL := os.Getenv("LLM_BASE_URL")
	model := os.Getenv("LLM_MODEL")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "请设置 LLM_API_KEY")
		os.Exit(1)
	}

	llmClient := client.New(
		client.WithAPIKey(apiKey),
		client.WithBaseURL(baseURL),
		client.WithModel(model),
		client.WithTemperature(0.1),
	)

	schema := structured.Schema{
		Name:        "extract_ticket",
		Description: "从客服对话中提取工单信息",
		Properties: map[string]models.ToolProperty{
			"category": {Type: "string", Description: "工单类别：订单/退款/物流/商品/其他"},
			"priority": {Type: "number", Description: "优先级 1-5（5=最紧急）"},
			"summary":  {Type: "string", Description: "问题摘要，50字以内"},
			"emotion":  {Type: "string", Description: "用户情绪：平静/不满/愤怒/焦急"},
		},
		Required: []string{"category", "priority", "summary", "emotion"},
	}

	ext := structured.NewExtractor[Ticket](llmClient, schema, structured.WithMaxRetries(2))

	// 测试几段对话
	conversations := [][]models.Message{
		{
			{Role: models.RoleSystem, Content: "你是一个客服工单提取助手。请根据对话内容提取工单信息。"},
			{Role: models.RoleUser, Content: "我的订单 ORDER-001 已经一周了还没收到，太慢了！"},
		},
		{
			{Role: models.RoleSystem, Content: "你是一个客服工单提取助手。请根据对话内容提取工单信息。"},
			{Role: models.RoleUser, Content: "收到的耳机左边没声音，要求退货退款"},
		},
		{
			{Role: models.RoleSystem, Content: "你是一个客服工单提取助手。请根据对话内容提取工单信息。"},
			{Role: models.RoleUser, Content: "请问你们家充电宝支持快充吗？"},
		},
	}

	for i, msgs := range conversations {
		userMsg := msgs[len(msgs)-1].Content
		fmt.Printf("━━━ 对话 %d ━━━\n用户: %s\n\n", i+1, userMsg)

		ticket, err := ext.Extract(context.Background(), msgs)
		if err != nil {
			fmt.Printf("  [工单提取失败] %v\n\n", err)
			continue
		}

		fmt.Printf("  [工单]\n")
		fmt.Printf("   类别: %s\n", ticket.Category)
		fmt.Printf("   优先级: %d/5\n", ticket.Priority)
		fmt.Printf("   情绪: %s\n", ticket.Emotion)
		fmt.Printf("   摘要: %s\n\n", ticket.Summary)
	}

	// ── 作业2：SentimentReport 情绪分析 ──
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("情绪分析报告（SentimentReport）")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	sentimentExt := structured.NewExtractor[structured.SentimentReport](
		llmClient, structured.SentimentSchema(), structured.WithMaxRetries(2),
	)

	for i, msgs := range conversations {
		userMsg := msgs[len(msgs)-1].Content
		fmt.Printf("━━━ 对话 %d ━━━\n用户: %s\n\n", i+1, userMsg)

		report, err := sentimentExt.Extract(context.Background(), msgs)
		if err != nil {
			fmt.Printf("  [情绪分析失败] %v\n\n", err)
			continue
		}

		fmt.Printf("  [情绪报告]\n")
		fmt.Printf("   整体倾向: %s (置信度: %.0f%%)\n", report.Sentiment, report.Confidence*100)
		fmt.Printf("   关键情绪: %v\n", report.KeyEmotions)
		fmt.Printf("   触发原因: %s\n", report.Trigger)
		fmt.Printf("   应对建议: %s\n\n", report.Recommendation)
	}
}
