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

var schema = structured.Schema{
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

var conversations = [][]models.Message{
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

const rounds = 3

func main() {
	_ = godotenv.Load()

	apiKey := os.Getenv("LLM_API_KEY")
	baseURL := os.Getenv("LLM_BASE_URL")
	model := os.Getenv("LLM_MODEL")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "请设置 LLM_API_KEY")
		os.Exit(1)
	}

	temps := []float64{0.1, 0.8}
	results := make(map[float64][][]Ticket) // temp → [对话索引][轮次]

	for _, temp := range temps {
		llm := client.New(
			client.WithAPIKey(apiKey),
			client.WithBaseURL(baseURL),
			client.WithModel(model),
			client.WithTemperature(temp),
		)
		ext := structured.NewExtractor[Ticket](llm, schema, structured.WithMaxRetries(2))

		convResults := make([][]Ticket, len(conversations))
		for ci, msgs := range conversations {
			for r := range rounds {
				ticket, err := ext.Extract(context.Background(), msgs)
				if err != nil {
					fmt.Printf("[temp=%.1f] 对话%d 第%d轮失败: %v\n", temp, ci+1, r+1, err)
					continue
				}
				convResults[ci] = append(convResults[ci], ticket)
				fmt.Printf("[temp=%.1f] 对话%d 第%d轮: category=%s priority=%d emotion=%s\n",
					temp, ci+1, r+1, ticket.Category, ticket.Priority, ticket.Emotion)
			}
		}
		results[temp] = convResults
	}

	fmt.Println()
	fmt.Println("━━━ 稳定性对比报告 ━━━")
	fmt.Println()

	for _, temp := range temps {
		fmt.Printf("▸ temperature = %.1f\n", temp)
		convResults := results[temp]
		totalFields, consistentFields := 0, 0

		for ci, tickets := range convResults {
			if len(tickets) == 0 {
				fmt.Printf("  对话%d: 无有效结果\n", ci+1)
				continue
			}
			catSame := allSame(tickets, func(t Ticket) string { return t.Category })
			priSame := allSameInt(tickets, func(t Ticket) int { return t.Priority })
			emoSame := allSame(tickets, func(t Ticket) string { return t.Emotion })

			consistent := 0
			for _, same := range []bool{catSame, priSame, emoSame} {
				totalFields++
				if same {
					consistent++
					consistentFields++
				}
			}

			fmt.Printf("  对话%d: category=%s priority=%s emotion=%s (%d/3 一致)\n",
				ci+1, mark(catSame), mark(priSame), mark(emoSame), consistent)
		}

		if totalFields > 0 {
			rate := float64(consistentFields) / float64(totalFields) * 100
			fmt.Printf("  总一致率: %.0f%% (%d/%d)\n", rate, consistentFields, totalFields)
		}
		fmt.Println()
	}
}

func allSame(tickets []Ticket, fn func(Ticket) string) bool {
	if len(tickets) <= 1 {
		return true
	}
	first := fn(tickets[0])
	for _, t := range tickets[1:] {
		if fn(t) != first {
			return false
		}
	}
	return true
}

func allSameInt(tickets []Ticket, fn func(Ticket) int) bool {
	if len(tickets) <= 1 {
		return true
	}
	first := fn(tickets[0])
	for _, t := range tickets[1:] {
		if fn(t) != first {
			return false
		}
	}
	return true
}

func mark(ok bool) string {
	if ok {
		return "STABLE"
	}
	return "DRIFT"
}
