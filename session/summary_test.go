package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pengpn/go-llm-agent/models"
)

// mockSummaryLLM 模拟摘要 LLM
type mockSummaryLLM struct {
	response string
	err      error
	called   int
	lastMsgs []models.Message
}

func (m *mockSummaryLLM) Chat(ctx context.Context, messages []models.Message) (*models.Response, error) {
	m.called++
	m.lastMsgs = messages
	if m.err != nil {
		return nil, m.err
	}
	return &models.Response{Content: m.response}, nil
}

// buildSession 构建包含 N 条消息的测试 Session
func buildSession(systemPrompt string, msgCount int) *Session {
	sess := NewSession("test", systemPrompt, &ByTurns{MaxTurns: 100})
	for i := range msgCount {
		if i%2 == 0 {
			sess.AddUserMessage(fmt.Sprintf("用户消息 %d", i/2+1))
		} else {
			sess.AddAssistantMessage(fmt.Sprintf("助手回复 %d", (i+1)/2))
		}
	}
	return sess
}

func TestSummarizer_BelowThreshold(t *testing.T) {
	llm := &mockSummaryLLM{response: "摘要内容"}
	sm := NewSummarizer(llm, WithThreshold(20))

	// 10 条消息 + 1 system = 11 < 20，不应触发
	sess := buildSession("你是客服", 10)
	compressed, err := sm.CompressIfNeeded(context.Background(), sess)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if compressed {
		t.Fatal("should not compress below threshold")
	}
	if llm.called != 0 {
		t.Fatal("LLM should not be called")
	}
}

func TestSummarizer_AboveThreshold_Compresses(t *testing.T) {
	llm := &mockSummaryLLM{response: "用户咨询了订单问题，客服已回复"}
	sm := NewSummarizer(llm, WithThreshold(10), WithKeepRecent(4))

	// 1 system + 12 history = 13 条 > 阈值 10
	sess := buildSession("你是客服", 12)

	compressed, err := sm.CompressIfNeeded(context.Background(), sess)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !compressed {
		t.Fatal("should have compressed")
	}
	if llm.called != 1 {
		t.Fatalf("LLM should be called once, got %d", llm.called)
	}

	// 压缩后：1 system + 1 摘要 + 4 最近 = 6 条
	msgs := sess.Messages()
	if len(msgs) != 6 {
		t.Fatalf("expected 6 messages after compression, got %d", len(msgs))
	}

	// 第一条是 system prompt
	if msgs[0].Role != models.RoleSystem || msgs[0].Content != "你是客服" {
		t.Fatalf("first message should be system prompt, got %+v", msgs[0])
	}

	// 第二条是摘要
	if msgs[1].Role != models.RoleSystem || !strings.Contains(msgs[1].Content, "[对话摘要]") {
		t.Fatalf("second message should be summary, got %+v", msgs[1])
	}

	// 最后 4 条是最近的消息
	if msgs[2].Role != models.RoleUser {
		t.Fatalf("third message should be user, got %s", msgs[2].Role)
	}
}

func TestSummarizer_LLMError(t *testing.T) {
	llm := &mockSummaryLLM{err: errors.New("LLM 服务不可用")}
	sm := NewSummarizer(llm, WithThreshold(5))

	sess := buildSession("你是客服", 10)
	_, err := sm.CompressIfNeeded(context.Background(), sess)
	if err == nil {
		t.Fatal("expected error when LLM fails")
	}
	if !strings.Contains(err.Error(), "生成摘要失败") {
		t.Fatalf("error should mention 摘要失败, got: %v", err)
	}

	// 失败后消息不应被修改
	if sess.MessageCount() != 11 { // 10 history + 1 system
		t.Fatalf("messages should not be modified on error, count=%d", sess.MessageCount())
	}
}

func TestSummarizer_EmptyResponse(t *testing.T) {
	llm := &mockSummaryLLM{response: ""}
	sm := NewSummarizer(llm, WithThreshold(5))

	sess := buildSession("你是客服", 10)
	_, err := sm.CompressIfNeeded(context.Background(), sess)
	if err == nil {
		t.Fatal("expected error when LLM returns empty summary")
	}
}

func TestSummarizer_KeepRecentGreaterThanHistory(t *testing.T) {
	llm := &mockSummaryLLM{response: "摘要"}
	// keepRecent=100 > 实际历史消息数，不应压缩
	sm := NewSummarizer(llm, WithThreshold(3), WithKeepRecent(100))

	sess := buildSession("你是客服", 10)
	compressed, err := sm.CompressIfNeeded(context.Background(), sess)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if compressed {
		t.Fatal("should not compress when keepRecent >= history count")
	}
}

func TestSummarizer_SummaryPromptContainsConversation(t *testing.T) {
	llm := &mockSummaryLLM{response: "摘要结果"}
	sm := NewSummarizer(llm, WithThreshold(5), WithKeepRecent(2))

	sess := buildSession("你是客服", 6)

	_, err := sm.CompressIfNeeded(context.Background(), sess)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证发送给 LLM 的 prompt 包含对话内容
	if len(llm.lastMsgs) != 1 {
		t.Fatalf("expected 1 message to LLM, got %d", len(llm.lastMsgs))
	}
	prompt := llm.lastMsgs[0].Content
	if !strings.Contains(prompt, "用户:") || !strings.Contains(prompt, "客服:") {
		t.Fatalf("prompt should contain conversation, got: %s", prompt)
	}
}

func TestFormatConversation(t *testing.T) {
	msgs := []models.Message{
		{Role: models.RoleUser, Content: "你好"},
		{Role: models.RoleAssistant, Content: "您好！有什么可以帮您？"},
		{Role: models.RoleTool, Content: "查询结果：订单已发货"},
	}

	result := formatConversation(msgs)
	if !strings.Contains(result, "用户: 你好") {
		t.Fatal("should contain user prefix")
	}
	if !strings.Contains(result, "客服: 您好") {
		t.Fatal("should contain assistant prefix")
	}
	if !strings.Contains(result, "工具结果: 查询结果") {
		t.Fatal("should contain tool prefix")
	}
}

// ---- SummaryStrategy 测试 ----

func TestSummaryStrategy_Truncate_WithCache(t *testing.T) {
	llm := &mockSummaryLLM{response: "摘要：用户咨询了订单问题"}
	strategy := NewSummaryStrategy(llm,
		WithSummaryThreshold(8),
		WithSummaryKeepRecent(4),
		WithFallbackTurns(5),
	)

	sess := buildSession("你是客服", 12)

	// 阶段1：预生成摘要
	prepared, err := strategy.PrepareSummary(context.Background(), sess)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !prepared {
		t.Fatal("should have prepared a summary")
	}
	if !strategy.HasCachedSummary() {
		t.Fatal("cache should have summary after PrepareSummary")
	}

	// 阶段2：Truncate 使用缓存摘要
	msgs := strategy.Truncate(sess.messages)

	// 1 system + 1 摘要 + 4 最近 = 6
	if len(msgs) != 6 {
		t.Fatalf("expected 6 messages, got %d", len(msgs))
	}
	if !strings.Contains(msgs[1].Content, "[对话摘要]") {
		t.Fatalf("second message should be summary, got %q", msgs[1].Content)
	}

	// 使用后缓存应被清除
	if strategy.HasCachedSummary() {
		t.Fatal("cache should be cleared after Truncate")
	}
}

func TestSummaryStrategy_Truncate_WithoutCache_FallsBackToByTurns(t *testing.T) {
	llm := &mockSummaryLLM{response: "摘要"}
	strategy := NewSummaryStrategy(llm,
		WithSummaryThreshold(8),
		WithFallbackTurns(3),
	)

	sess := buildSession("你是客服", 12)

	// 不调用 PrepareSummary，直接 Truncate → 应该退化为 ByTurns(3)
	msgs := strategy.Truncate(sess.messages)

	// ByTurns{MaxTurns: 3} → 1 system + 6 history = 7
	if len(msgs) != 7 {
		t.Fatalf("expected 7 messages (fallback ByTurns=3), got %d", len(msgs))
	}

	// 不应该有摘要消息
	for _, m := range msgs {
		if strings.Contains(m.Content, "[对话摘要]") {
			t.Fatal("should not contain summary when no cache")
		}
	}
}

func TestSummaryStrategy_PrepareSummary_BelowThreshold(t *testing.T) {
	llm := &mockSummaryLLM{response: "摘要"}
	strategy := NewSummaryStrategy(llm, WithSummaryThreshold(20))

	sess := buildSession("你是客服", 6)

	prepared, err := strategy.PrepareSummary(context.Background(), sess)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prepared {
		t.Fatal("should not prepare below threshold")
	}
	if llm.called != 0 {
		t.Fatal("LLM should not be called")
	}
}

func TestSummaryStrategy_PrepareSummary_LLMError(t *testing.T) {
	llm := &mockSummaryLLM{err: errors.New("LLM 超时")}
	strategy := NewSummaryStrategy(llm, WithSummaryThreshold(5))

	sess := buildSession("你是客服", 10)

	_, err := strategy.PrepareSummary(context.Background(), sess)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "预生成摘要失败") {
		t.Fatalf("error should mention 预生成, got: %v", err)
	}

	// 失败后无缓存，Truncate 应退化
	if strategy.HasCachedSummary() {
		t.Fatal("cache should be empty after failed prepare")
	}
}

func TestSummaryStrategy_IntegrationWithSession(t *testing.T) {
	llm := &mockSummaryLLM{response: "用户张先生咨询了 ORDER-123 的退款进度"}
	strategy := NewSummaryStrategy(llm,
		WithSummaryThreshold(8),
		WithSummaryKeepRecent(4),
		WithFallbackTurns(5),
	)

	// 用 SummaryStrategy 作为 Session 的截断策略
	sess := NewSession("user-001", "你是客服助手", strategy)
	for i := range 12 {
		if i%2 == 0 {
			sess.AddUserMessage(fmt.Sprintf("用户消息 %d", i/2+1))
		} else {
			sess.AddAssistantMessage(fmt.Sprintf("客服回复 %d", (i+1)/2))
		}
	}

	// 预生成摘要
	prepared, err := strategy.PrepareSummary(context.Background(), sess)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !prepared {
		t.Fatal("should have prepared")
	}

	// session.Messages() 内部会调用 strategy.Truncate()
	msgs := sess.Messages()

	// 1 system + 1 摘要 + 4 最近 = 6
	if len(msgs) != 6 {
		t.Fatalf("expected 6 messages from session.Messages(), got %d", len(msgs))
	}
	if msgs[0].Content != "你是客服助手" {
		t.Fatalf("first should be system prompt, got %q", msgs[0].Content)
	}
	if !strings.Contains(msgs[1].Content, "ORDER-123") {
		t.Fatalf("summary should contain ORDER-123, got %q", msgs[1].Content)
	}
}

func TestSummaryStrategy_CacheUsedOnce(t *testing.T) {
	llm := &mockSummaryLLM{response: "第一次摘要"}
	strategy := NewSummaryStrategy(llm,
		WithSummaryThreshold(5),
		WithSummaryKeepRecent(2),
		WithFallbackTurns(3),
	)

	sess := buildSession("你是客服", 8)

	// 预生成摘要
	strategy.PrepareSummary(context.Background(), sess)

	// 第一次 Truncate 使用摘要
	msgs1 := strategy.Truncate(sess.messages)
	hasSummary := false
	for _, m := range msgs1 {
		if strings.Contains(m.Content, "[对话摘要]") {
			hasSummary = true
		}
	}
	if !hasSummary {
		t.Fatal("first Truncate should use summary")
	}

	// 第二次 Truncate（缓存已清除）应退化为 ByTurns
	msgs2 := strategy.Truncate(sess.messages)
	for _, m := range msgs2 {
		if strings.Contains(m.Content, "[对话摘要]") {
			t.Fatal("second Truncate should NOT use summary (cache cleared)")
		}
	}
}

func TestSummarizer_NoSystemPrompt(t *testing.T) {
	llm := &mockSummaryLLM{response: "用户咨询了一些问题"}
	sm := NewSummarizer(llm, WithThreshold(5), WithKeepRecent(2))

	sess := NewSession("test", "", &ByTurns{MaxTurns: 100})
	for i := range 8 {
		if i%2 == 0 {
			sess.AddUserMessage(fmt.Sprintf("消息 %d", i))
		} else {
			sess.AddAssistantMessage(fmt.Sprintf("回复 %d", i))
		}
	}

	compressed, err := sm.CompressIfNeeded(context.Background(), sess)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !compressed {
		t.Fatal("should have compressed")
	}

	msgs := sess.Messages()
	// 无 system + 1 摘要 + 2 最近 = 3 条
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "[对话摘要]") {
		t.Fatalf("first message should be summary, got %+v", msgs[0])
	}
}
