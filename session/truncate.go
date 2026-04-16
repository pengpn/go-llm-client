// Package session 管理多轮对话的消息历史。
package session

import "github.com/pengpn/go-llm-agent/models"

// TruncateStrategy 定义截断策略的接口。
// 设计为接口而不是固定实现，方便将来接入精确的 tiktoken 计算。
type TruncateStrategy interface {
	// Truncate 接收完整的消息列表，返回截断后的列表。
	// systemPrompt 永远不被截断，始终保留在第一位。
	Truncate(messages []models.Message) []models.Message
}

// ByTurns 按对话轮数截断。
// 最简单的策略：超过 N 轮就删掉最旧的。
// 适合对精确 Token 数不敏感的场景。
type ByTurns struct {
	MaxTurns int // 保留最近 N 轮（一轮 = user + assistant 共 2 条消息）
}

// Truncate 保留 system prompt + 最近 MaxTurns 轮对话。
func (b *ByTurns) Truncate(messages []models.Message) []models.Message {
	if len(messages) == 0 {
		return messages
	}

	// 分离 system prompt 和对话历史
	systemMsgs, historyMsgs := splitSystem(messages)

	maxHistory := b.MaxTurns * 2 // 每轮 2 条消息
	if len(historyMsgs) <= maxHistory {
		return messages // 未超限，无需截断
	}

	// 保留最新的 maxHistory 条，丢弃最旧的
	truncated := historyMsgs[len(historyMsgs)-maxHistory:]

	// 拼回：system + 截断后的历史
	return append(systemMsgs, truncated...)
}

// ByTokenEstimate 按 Token 数估算截断。
// 精确的 Token 计数需要 tiktoken，这里用字符数 / 3 估算（适用于英文）。
// 中文每个字约 1-2 个 Token，这里保守估算为 1 字 = 1 Token。
//
// 为什么不直接用 tiktoken？
// 因为 tiktoken 的 Go 移植库依赖较重，教学阶段先用估算，后续可替换。
type ByTokenEstimate struct {
	MaxTokens int // 保留的最大 Token 数（估算）
}

// Truncate 估算 Token 数并截断超出部分。
func (b *ByTokenEstimate) Truncate(messages []models.Message) []models.Message {
	if len(messages) == 0 {
		return messages
	}

	systemMsgs, historyMsgs := splitSystem(messages)

	// 计算 system prompt 占用的 Token（不可截断）
	systemTokens := estimateTokens(systemMsgs)
	budget := b.MaxTokens - systemTokens
	if budget <= 0 {
		// system prompt 本身超出限制，只保留 system（极端情况）
		return systemMsgs
	}

	// 从最新消息往旧消息方向累计，直到超出 budget
	var kept []models.Message
	usedTokens := 0

	for i := len(historyMsgs) - 1; i >= 0; i-- {
		t := estimateTokens([]models.Message{historyMsgs[i]})
		if usedTokens+t > budget {
			break
		}
		usedTokens += t
		// 注意：这里是逆序追加，最后需要 reverse
		kept = append(kept, historyMsgs[i])
	}

	// 反转恢复时间顺序
	reverseMessages(kept)

	return append(systemMsgs, kept...)
}

// splitSystem 将消息列表拆分为 system 消息和非 system 消息。
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

// estimateTokens 估算消息列表的 Token 数。
// 英文：字符数 / 4；中文：字符数 * 1.5（粗略估算）。
// 这个函数故意简单，实际项目应替换为精确的 tiktoken 实现。
func estimateTokens(messages []models.Message) int {
	total := 0
	for _, m := range messages {
		// 每条消息有约 4 个 token 的固定开销（角色标记等）
		total += 4
		total += len([]rune(m.Content)) // 按 Unicode 字符数估算
	}
	return total
}

// reverseMessages 原地反转消息切片。
func reverseMessages(msgs []models.Message) {
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
}
