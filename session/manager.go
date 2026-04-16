package session

import (
	"fmt"
	"sync"
	"time"

	"github.com/pengpn/go-llm-agent/config"
)

// Manager 管理所有用户的 Session，支持 TTL 过期自动清理。
// 并发安全：内部用 sync.Map 存储 Session（读多写少场景性能更好）。
type Manager struct {
	sessions     sync.Map      // key: sessionID (string), value: *Session
	ttl          time.Duration // Session 不活跃超过 ttl 后被清理
	systemPrompt string        // 新 Session 默认使用的 System Prompt
	strategy     TruncateStrategy
	stopCh       chan struct{} // 关闭信号
	once         sync.Once    // 确保 Stop 只执行一次
}

// ManagerOption 是 Manager 的函数式选项。
type ManagerOption func(*Manager)

// WithTTL 设置 Session 过期时间（默认 30 分钟）。
func WithTTL(d time.Duration) ManagerOption {
	return func(m *Manager) {
		m.ttl = d
	}
}

// WithDefaultSystemPrompt 设置新 Session 的默认 System Prompt。
func WithDefaultSystemPrompt(prompt string) ManagerOption {
	return func(m *Manager) {
		m.systemPrompt = prompt
	}
}

// WithTruncateStrategy 设置截断策略（默认按 20 轮截断）。
func WithTruncateStrategy(s TruncateStrategy) ManagerOption {
	return func(m *Manager) {
		m.strategy = s
	}
}

// NewManager 创建并启动 Session 管理器。
// 后台会定期扫描并清理过期 Session。
func NewManager(opts ...ManagerOption) *Manager {
	m := &Manager{
		ttl:    30 * time.Minute,
		strategy: &ByTurns{MaxTurns: 20},
		stopCh: make(chan struct{}),
	}

	for _, opt := range opts {
		opt(m)
	}

	// 启动后台清理 goroutine
	go m.cleanupLoop()

	return m
}

// GetOrCreate 获取已有 Session，不存在时创建新的。
// double-check 防止并发竞争下重复创建。
//
// 为什么不直接用 sync.Map.LoadOrStore？
// 因为 Session 的创建有初始化逻辑，需要在"确认不存在"后才创建，
// 避免创建了又丢弃导致资源浪费。
func (m *Manager) GetOrCreate(sessionID string) *Session {
	// 第一次检查（快速路径，无锁）
	if val, ok := m.sessions.Load(sessionID); ok {
		return val.(*Session)
	}

	// 创建新 Session
	newSession := NewSession(sessionID, m.systemPrompt, m.strategy)

	// LoadOrStore：如果并发下已有其他 goroutine 创建了，返回已有的
	actual, _ := m.sessions.LoadOrStore(sessionID, newSession)
	return actual.(*Session)
}

// Get 获取已有 Session，不存在返回 nil。
func (m *Manager) Get(sessionID string) *Session {
	if val, ok := m.sessions.Load(sessionID); ok {
		return val.(*Session)
	}
	return nil
}

// Delete 立即删除指定 Session。
func (m *Manager) Delete(sessionID string) {
	m.sessions.Delete(sessionID)
}

// Count 返回当前活跃 Session 数量。
func (m *Manager) Count() int {
	count := 0
	m.sessions.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// Stop 停止后台清理 goroutine。
// 应在程序退出时调用，防止 goroutine 泄漏。
func (m *Manager) Stop() {
	m.once.Do(func() {
		close(m.stopCh)
	})
}

// cleanupLoop 后台定期清理过期 Session。
// 清理间隔 = TTL / 2，在 TTL 和 2*TTL 之间完成清理。
func (m *Manager) cleanupLoop() {
	ticker := time.NewTicker(m.ttl / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.cleanup()
		case <-m.stopCh:
			return
		}
	}
}

// cleanup 扫描并删除所有过期 Session。
func (m *Manager) cleanup() {
	now := time.Now()
	m.sessions.Range(func(key, val any) bool {
		s := val.(*Session)
		if now.Sub(s.LastActiveAt()) > m.ttl {
			m.sessions.Delete(key)
		}
		return true // 继续遍历
	})
}

// Stats 返回管理器状态摘要。
func (m *Manager) Stats() string {
	return fmt.Sprintf("活跃 Session 数: %d | TTL: %v", m.Count(), m.ttl)
}

// NewManagerFromConfig 根据 Config 创建 Manager。
func NewManagerFromConfig(cfg *config.SessionConfig, systemPrompt string) *Manager {
	var strategy TruncateStrategy
	switch cfg.TruncateMode {
	case "tokens":
		strategy = &ByTokenEstimate{MaxTokens: cfg.MaxTokens}
	default: // "turns" 或其他值均使用按轮数截断
		strategy = &ByTurns{MaxTurns: cfg.MaxTurns}
	}

	return NewManager(
		WithTTL(cfg.TTL),
		WithDefaultSystemPrompt(systemPrompt),
		WithTruncateStrategy(strategy),
	)
}
