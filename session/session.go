package session

import (
	"sync"
	"time"

	"github.com/pengpn/go-llm-agent/models"
)

// Session 代表单个用户的对话会话。
// 维护消息历史，并在超出上下文限制时自动截断。
// 并发安全：读写 messages 时使用 RWMutex。
type Session struct {
	mu           sync.RWMutex
	id           string
	messages     []models.Message
	systemPrompt string
	strategy     TruncateStrategy
	createdAt    time.Time
	lastActiveAt time.Time
}

// NewSession 创建一个新的会话。
// systemPrompt 是 AI 的"人设"，永远不会被截断。
// strategy 决定历史消息的截断方式。
func NewSession(id, systemPrompt string, strategy TruncateStrategy) *Session {
	now := time.Now()
	s := &Session{
		id:           id,
		systemPrompt: systemPrompt,
		strategy:     strategy,
		createdAt:    now,
		lastActiveAt: now,
	}

	// System Prompt 是第一条消息，始终存在
	if systemPrompt != "" {
		s.messages = []models.Message{
			{Role: models.RoleSystem, Content: systemPrompt},
		}
	}

	return s
}

// AddUserMessage 追加用户消息。
func (s *Session) AddUserMessage(content string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, models.Message{
		Role:    models.RoleUser,
		Content: content,
	})
	s.lastActiveAt = time.Now()
}

// AddAssistantMessage 追加 AI 回复消息。
func (s *Session) AddAssistantMessage(content string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messages = append(s.messages, models.Message{
		Role:    models.RoleAssistant,
		Content: content,
	})
	s.lastActiveAt = time.Now()
}

// Messages 返回截断后的消息列表，用于发送给 LLM。
// 返回新切片，不暴露内部状态（不可变原则）。
func (s *Session) Messages() []models.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()

	truncated := s.strategy.Truncate(s.messages)

	// 返回副本，防止调用方修改内部状态
	result := make([]models.Message, len(truncated))
	copy(result, truncated)
	return result
}

// ID 返回会话 ID。
func (s *Session) ID() string {
	return s.id
}

// MessageCount 返回当前消息总数（含 system prompt）。
func (s *Session) MessageCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.messages)
}

// LastActiveAt 返回最后活跃时间。
func (s *Session) LastActiveAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastActiveAt
}

// Clear 清除对话历史，保留 system prompt。
// 场景：用户主动开始新话题，或管理员重置会话。
func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.systemPrompt != "" {
		s.messages = []models.Message{
			{Role: models.RoleSystem, Content: s.systemPrompt},
		}
	} else {
		s.messages = nil
	}
	s.lastActiveAt = time.Now()
}
