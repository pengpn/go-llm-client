package structured

import (
	"fmt"

	"github.com/pengpn/go-llm-agent/models"
)

// SentimentReport 从对话中提取的情绪分析报告。
// 与 Ticket 是完全不同的业务类型，但复用同一个 Extractor[T] 机制。
type SentimentReport struct {
	Sentiment      string   `json:"sentiment"`
	Confidence     float64  `json:"confidence"`
	KeyEmotions    []string `json:"key_emotions"`
	Trigger        string   `json:"trigger"`
	Recommendation string   `json:"recommendation"`
}

func (s *SentimentReport) Validate() error {
	validSentiments := map[string]bool{
		"positive": true, "negative": true, "neutral": true, "mixed": true,
	}
	if !validSentiments[s.Sentiment] {
		return fmt.Errorf("sentiment 必须是 positive/negative/neutral/mixed 之一，当前: %q", s.Sentiment)
	}
	if s.Confidence < 0 || s.Confidence > 1 {
		return fmt.Errorf("confidence 必须在 0-1 之间，当前: %.2f", s.Confidence)
	}
	if len(s.KeyEmotions) == 0 {
		return fmt.Errorf("key_emotions 不能为空")
	}
	if s.Trigger == "" {
		return fmt.Errorf("trigger 不能为空")
	}
	if s.Recommendation == "" {
		return fmt.Errorf("recommendation 不能为空")
	}
	return nil
}

// SentimentSchema 返回情绪分析报告的 Schema 定义。
func SentimentSchema() Schema {
	return Schema{
		Name:        "extract_sentiment",
		Description: "从客服对话中提取用户情绪分析报告，包含整体情绪、置信度、关键情绪词、触发原因和应对建议",
		Properties: map[string]models.ToolProperty{
			"sentiment": {
				Type:        "string",
				Description: "整体情绪倾向：positive（满意）/ negative（不满）/ neutral（中立）/ mixed（复杂）",
			},
			"confidence": {
				Type:        "number",
				Description: "判断置信度 0.0-1.0，1.0 表示非常确定",
			},
			"key_emotions": {
				Type:        "array",
				Description: "关键情绪词列表，如 [\"愤怒\", \"焦虑\", \"失望\"]",
			},
			"trigger": {
				Type:        "string",
				Description: "引发该情绪的核心原因，50字以内",
			},
			"recommendation": {
				Type:        "string",
				Description: "客服应对建议，如 '优先安抚情绪，再提供解决方案'，50字以内",
			},
		},
		Required: []string{"sentiment", "confidence", "key_emotions", "trigger", "recommendation"},
	}
}
