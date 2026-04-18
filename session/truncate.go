// Package session 管理多轮对话的消息历史。
package session

import (
	"github.com/pengpn/go-llm-agent/models"
	"github.com/tiktoken-go/tokenizer"
)

// TruncateStrategy 定义截断策略的接口。
type TruncateStrategy interface {
	Truncate(messages []models.Message) []models.Message
}

// TokenCounter 是 Token 计数的抽象接口。
// 用接口隔离 tiktoken 依赖，方便测试时注入 mock，或切换到其他计数方案。
type TokenCounter interface {
	Count(text string) int
}

// ---- TokenCounter 实现 ----

// TikTokenCounter 使用 tiktoken 精确计算 Token 数。
// 支持 GPT-4、GPT-3.5-Turbo、GPT-4o 等使用 cl100k_base 编码的模型。
type TikTokenCounter struct {
	codec tokenizer.Codec
}

// NewTikTokenCounter 创建基于 tiktoken 的计数器。
// model 传入 OpenAI 模型名，如 "gpt-4o-mini"、"gpt-4"、"gpt-3.5-turbo"。
// 模型不受支持时，退化为估算计数器并不报错（降级处理）。
func NewTikTokenCounter(model string) TokenCounter {
	enc, err := tokenizer.ForModel(tokenizer.Model(model))
	if err != nil {
		// 模型不在支持列表（如智谱、通义），降级到估算
		return &EstimateCounter{}
	}
	return &TikTokenCounter{codec: enc}
}

// Count 精确计算文本的 Token 数。tiktoken 返回 error 时降级为估算。
func (t *TikTokenCounter) Count(text string) int {
	n, err := t.codec.Count(text)
	if err != nil {
		return estimateByRune(text) // 降级
	}
	return n
}

// EstimateCounter 用字符数估算 Token 数，作为 tiktoken 不可用时的降级方案。
// 规则：英文约 4 字符/Token；中文约 1 字符/Token（保守估算）。
type EstimateCounter struct{}

func (e *EstimateCounter) Count(text string) int {
	return estimateByRune(text)
}

// estimateByRune 按 Unicode 字符数估算 Token 数。
func estimateByRune(text string) int {
	return len([]rune(text))
}

// ---- 截断策略 ----

// ByTurns 按对话轮数截断。
// 超过 MaxTurns 轮时，删除最旧的轮次，保留最新的。
// 一轮 = user + assistant 共 2 条消息。
type ByTurns struct {
	MaxTurns int
}

// Truncate 保留 system prompt + 最近 MaxTurns 轮对话。
func (b *ByTurns) Truncate(messages []models.Message) []models.Message {
	if len(messages) == 0 {
		return messages
	}

	systemMsgs, historyMsgs := splitSystem(messages)

	maxHistory := b.MaxTurns * 2 // 每轮 2 条消息
	if len(historyMsgs) <= maxHistory {
		return messages
	}

	truncated := historyMsgs[len(historyMsgs)-maxHistory:]
	result := make([]models.Message, 0, len(systemMsgs)+len(truncated))
	result = append(result, systemMsgs...)
	result = append(result, truncated...)
	return result
}

// ByTokenCount 按精确 Token 数截断（使用 tiktoken 或估算降级）。
// 相比 ByTurns，更精确地控制发送给 LLM 的上下文大小。
type ByTokenCount struct {
	MaxTokens int
	counter   TokenCounter
}

// NewByTokenCount 创建按 Token 截断的策略。
// model 用于选择正确的 tiktoken 编码，空字符串时使用估算。
func NewByTokenCount(maxTokens int, model string) *ByTokenCount {
	var counter TokenCounter
	if model != "" {
		counter = NewTikTokenCounter(model)
	} else {
		counter = &EstimateCounter{}
	}
	return &ByTokenCount{
		MaxTokens: maxTokens,
		counter:   counter,
	}
}

// Truncate 计算 Token 数并截断超出部分。
// OpenAI 的消息格式每条有固定开销，参考：
// https://platform.openai.com/docs/guides/text-generation/managing-tokens
func (b *ByTokenCount) Truncate(messages []models.Message) []models.Message {
	if len(messages) == 0 {
		return messages
	}

	systemMsgs, historyMsgs := splitSystem(messages)

	systemTokens := b.countMessages(systemMsgs)
	budget := b.MaxTokens - systemTokens
	if budget <= 0 {
		return systemMsgs
	}

	// 从最新消息往旧的方向累计，直到超出 budget
	var kept []models.Message
	used := 0

	for i := len(historyMsgs) - 1; i >= 0; i-- {
		t := b.countMessages([]models.Message{historyMsgs[i]})
		if used+t > budget {
			break
		}
		used += t
		kept = append(kept, historyMsgs[i])
	}

	reverseMessages(kept)

	result := make([]models.Message, 0, len(systemMsgs)+len(kept))
	result = append(result, systemMsgs...)
	result = append(result, kept...)
	return result
}

// countMessages 计算消息列表的总 Token 数，含每条消息的固定开销。
// OpenAI 每条消息固定消耗 4 个 token（格式化标记），回复再加 3 个。
func (b *ByTokenCount) countMessages(messages []models.Message) int {
	total := 0
	for _, m := range messages {
		total += 4 // 每条消息的固定格式开销
		total += b.counter.Count(m.Content)
	}
	return total
}

// ByTokenEstimate 保持向后兼容，内部使用估算计数器。
// 新代码推荐使用 NewByTokenCount。
type ByTokenEstimate struct {
	MaxTokens int
	inner     *ByTokenCount
}

func (b *ByTokenEstimate) strategy() *ByTokenCount {
	if b.inner == nil {
		b.inner = &ByTokenCount{
			MaxTokens: b.MaxTokens,
			counter:   &EstimateCounter{},
		}
	}
	return b.inner
}

// Truncate 使用估算 Token 数截断。
func (b *ByTokenEstimate) Truncate(messages []models.Message) []models.Message {
	return b.strategy().Truncate(messages)
}

// ---- 工具函数 ----

func splitSystem(messages []models.Message) (system []models.Message, history []models.Message) {
	for _, m := range messages {
		if m.Role == models.RoleSystem {
			system = append(system, m)
		} else {
			history = append(history, m)
		}
	}
	return
}

func reverseMessages(msgs []models.Message) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}
