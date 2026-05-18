package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/pengpn/go-llm-agent/models"
)

// SummaryLLM 摘要压缩器依赖的 LLM 接口。
// 只需要普通对话能力（不需要 Function Calling），所以定义最小接口。
//
// 为什么不复用 agent.LLMClient？
// → 接口隔离原则：摘要只需要 Chat，不需要 ChatWithTools。
// → session 包不应依赖 agent 包（依赖方向错误）。
type SummaryLLM interface {
	Chat(ctx context.Context, messages []models.Message) (*models.Response, error)
}

// Summarizer 对话摘要压缩器。
// 当消息数超过阈值时，用 LLM 将旧消息压缩为一段摘要。
//
// 设计思路（Sliding Window + Summary）：
//
//	[system] [旧消息...] [最近 N 条消息]
//	         ↓ LLM 总结
//	[system] [摘要消息] [最近 N 条消息]
//
// 压缩后的消息总数 = 1(system) + 1(摘要) + keepRecent，远小于原始消息数。
type Summarizer struct {
	llm        SummaryLLM
	threshold  int // 消息数达到此值时触发压缩（含 system prompt）
	keepRecent int // 压缩时保留最近多少条消息不参与总结
}

// SummarizerOption 函数式选项
type SummarizerOption func(*Summarizer)

// WithThreshold 设置触发压缩的消息数阈值（默认 20）
func WithThreshold(n int) SummarizerOption {
	return func(s *Summarizer) { s.threshold = n }
}

// WithKeepRecent 设置保留最近多少条消息（默认 6，即最近 3 轮）
func WithKeepRecent(n int) SummarizerOption {
	return func(s *Summarizer) { s.keepRecent = n }
}

// NewSummarizer 创建对话摘要压缩器。
// threshold=20 意味着超过 20 条消息时触发压缩。
// keepRecent=6 意味着最近 3 轮（6 条）不参与总结，保持原文精度。
func NewSummarizer(llm SummaryLLM, opts ...SummarizerOption) *Summarizer {
	s := &Summarizer{
		llm:        llm,
		threshold:  20,
		keepRecent: 6,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

const summaryPrompt = `请将以下对话历史压缩为一段简洁的摘要。

要求：
1. 保留所有关键信息（订单号、用户诉求、已解决/未解决的问题）
2. 保留用户情绪和态度的关键变化
3. 使用第三方叙述视角（"用户询问了..."、"客服回答了..."）
4. 摘要长度控制在 200 字以内
5. 不要遗漏任何具体的数字、编号、日期等事实信息

对话历史：
%s`

// CompressIfNeeded 检查 Session 消息数，超过阈值时执行摘要压缩。
// 返回 true 表示执行了压缩，false 表示未触发。
//
// 这个方法直接修改 Session 内部状态（替换旧消息为摘要）。
// 为什么不做成 TruncateStrategy？
// → TruncateStrategy.Truncate 是同步的，但摘要需要调用 LLM（异步+可能失败）。
// → 摘要是"有损压缩"——一旦执行，原始消息不可恢复。TruncateStrategy 是无损的（原始消息仍在）。
func (sm *Summarizer) CompressIfNeeded(ctx context.Context, sess *Session) (bool, error) {
	sess.mu.RLock()
	msgCount := len(sess.messages)
	sess.mu.RUnlock()

	if msgCount < sm.threshold {
		return false, nil
	}

	sess.mu.RLock()
	msgsCopy := make([]models.Message, len(sess.messages))
	copy(msgsCopy, sess.messages)
	sess.mu.RUnlock()

	systemMsgs, historyMsgs := splitSystem(msgsCopy)

	if len(historyMsgs) <= sm.keepRecent {
		return false, nil
	}

	// 分割：需要总结的旧消息 + 保留的近期消息
	toSummarize := historyMsgs[:len(historyMsgs)-sm.keepRecent]
	recentMsgs := historyMsgs[len(historyMsgs)-sm.keepRecent:]

	summary, err := sm.summarize(ctx, toSummarize)
	if err != nil {
		return false, fmt.Errorf("生成摘要失败: %w", err)
	}

	// 组装新的消息列表：system + 摘要 + 最近消息
	summaryMsg := models.Message{
		Role:    models.RoleSystem,
		Content: fmt.Sprintf("[对话摘要] %s", summary),
	}

	newMessages := make([]models.Message, 0, len(systemMsgs)+1+len(recentMsgs))
	newMessages = append(newMessages, systemMsgs...)
	newMessages = append(newMessages, summaryMsg)
	newMessages = append(newMessages, recentMsgs...)

	// 原子替换消息列表
	sess.mu.Lock()
	sess.messages = newMessages
	sess.mu.Unlock()

	return true, nil
}

// summarize 调用 LLM 生成对话摘要
func (sm *Summarizer) summarize(ctx context.Context, messages []models.Message) (string, error) {
	conversationText := formatConversation(messages)

	prompt := fmt.Sprintf(summaryPrompt, conversationText)
	resp, err := sm.llm.Chat(ctx, []models.Message{
		{Role: models.RoleUser, Content: prompt},
	})
	if err != nil {
		return "", fmt.Errorf("LLM 调用失败: %w", err)
	}

	summary := strings.TrimSpace(resp.Content)
	if summary == "" {
		return "", fmt.Errorf("LLM 返回空摘要")
	}

	return summary, nil
}

// formatConversation 将消息列表格式化为可读文本
func formatConversation(messages []models.Message) string {
	var b strings.Builder
	for _, m := range messages {
		switch m.Role {
		case models.RoleUser:
			b.WriteString("用户: ")
		case models.RoleAssistant:
			b.WriteString("客服: ")
		case models.RoleTool:
			b.WriteString("工具结果: ")
		default:
			b.WriteString(string(m.Role) + ": ")
		}
		b.WriteString(m.Content)
		b.WriteString("\n")
	}
	return b.String()
}
