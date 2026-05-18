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
