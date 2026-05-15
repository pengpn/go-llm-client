package structured

import (
	"context"
	"testing"

	"github.com/pengpn/go-llm-agent/models"
)

func TestSentimentReport_Validate(t *testing.T) {
	tests := []struct {
		name    string
		report  SentimentReport
		wantErr bool
	}{
		{
			name: "valid report",
			report: SentimentReport{
				Sentiment:      "negative",
				Confidence:     0.85,
				KeyEmotions:    []string{"愤怒", "失望"},
				Trigger:        "订单延迟一周未收到",
				Recommendation: "优先安抚情绪，提供物流查询",
			},
			wantErr: false,
		},
		{
			name: "invalid sentiment value",
			report: SentimentReport{
				Sentiment:      "angry",
				Confidence:     0.9,
				KeyEmotions:    []string{"愤怒"},
				Trigger:        "test",
				Recommendation: "test",
			},
			wantErr: true,
		},
		{
			name: "confidence out of range",
			report: SentimentReport{
				Sentiment:      "negative",
				Confidence:     1.5,
				KeyEmotions:    []string{"焦急"},
				Trigger:        "test",
				Recommendation: "test",
			},
			wantErr: true,
		},
		{
			name: "empty key_emotions",
			report: SentimentReport{
				Sentiment:      "neutral",
				Confidence:     0.5,
				KeyEmotions:    []string{},
				Trigger:        "test",
				Recommendation: "test",
			},
			wantErr: true,
		},
		{
			name: "empty trigger",
			report: SentimentReport{
				Sentiment:      "positive",
				Confidence:     0.9,
				KeyEmotions:    []string{"满意"},
				Trigger:        "",
				Recommendation: "test",
			},
			wantErr: true,
		},
		{
			name: "empty recommendation",
			report: SentimentReport{
				Sentiment:      "mixed",
				Confidence:     0.6,
				KeyEmotions:    []string{"疑惑", "期待"},
				Trigger:        "test",
				Recommendation: "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.report.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSentimentSchema_FieldsComplete(t *testing.T) {
	s := SentimentSchema()
	if s.Name != "extract_sentiment" {
		t.Errorf("Schema Name 期望 extract_sentiment，实际 %q", s.Name)
	}
	expectedFields := []string{"sentiment", "confidence", "key_emotions", "trigger", "recommendation"}
	for _, field := range expectedFields {
		if _, ok := s.Properties[field]; !ok {
			t.Errorf("Schema Properties 缺少字段 %q", field)
		}
	}
	if len(s.Required) != 5 {
		t.Errorf("Required 字段数期望 5，实际 %d", len(s.Required))
	}
}

func TestExtract_SentimentReport_Success(t *testing.T) {
	mc := &mockClient{
		responses: []*models.Response{
			{
				FinishReason: "tool_calls",
				ToolCalls: []models.ToolCall{
					{
						ID:   "call-1",
						Type: "function",
						Function: models.FunctionCall{
							Name: "extract_sentiment",
							Arguments: `{
								"sentiment": "negative",
								"confidence": 0.9,
								"key_emotions": ["愤怒", "焦急"],
								"trigger": "订单一周未送达",
								"recommendation": "安抚情绪，提供物流追踪信息"
							}`,
						},
					},
				},
			},
		},
	}

	ext := NewExtractor[SentimentReport](mc, SentimentSchema())
	result, err := ext.Extract(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "我的订单一周了还没到，你们到底行不行！"},
	})
	if err != nil {
		t.Fatalf("Extract 失败: %v", err)
	}
	if result.Sentiment != "negative" {
		t.Errorf("Sentiment 期望 negative，实际 %q", result.Sentiment)
	}
	if result.Confidence != 0.9 {
		t.Errorf("Confidence 期望 0.9，实际 %.2f", result.Confidence)
	}
	if len(result.KeyEmotions) != 2 {
		t.Errorf("KeyEmotions 期望 2 个，实际 %d", len(result.KeyEmotions))
	}
}

func TestExtract_SentimentReport_ValidationRetry(t *testing.T) {
	mc := &mockClient{
		responses: []*models.Response{
			{
				FinishReason: "tool_calls",
				ToolCalls: []models.ToolCall{
					{
						ID: "call-1",
						Function: models.FunctionCall{
							Name: "extract_sentiment",
							Arguments: `{
								"sentiment": "angry",
								"confidence": 0.8,
								"key_emotions": ["愤怒"],
								"trigger": "延迟",
								"recommendation": "安抚"
							}`,
						},
					},
				},
			},
			{
				FinishReason: "tool_calls",
				ToolCalls: []models.ToolCall{
					{
						ID: "call-2",
						Function: models.FunctionCall{
							Name: "extract_sentiment",
							Arguments: `{
								"sentiment": "negative",
								"confidence": 0.8,
								"key_emotions": ["愤怒"],
								"trigger": "延迟",
								"recommendation": "安抚"
							}`,
						},
					},
				},
			},
		},
	}

	ext := NewExtractor[SentimentReport](mc, SentimentSchema(), WithMaxRetries(1))
	result, err := ext.Extract(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "太气人了"},
	})
	if err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if result.Sentiment != "negative" {
		t.Errorf("修正后 Sentiment 期望 negative，实际 %q", result.Sentiment)
	}
	if mc.callCount() != 2 {
		t.Errorf("应调用 2 次，实际 %d", mc.callCount())
	}
}
