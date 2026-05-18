package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MemoryEntry 单条用户记忆。
// 每条记忆是一个 key-value 对，带有时间戳和可选的过期时间。
//
// 为什么 key-value 而不是非结构化文本？
// → key 可以精确覆盖更新（"preferred_address" 只保留最新的）
// → 检索时可以按 key 筛选，避免把所有记忆都塞进 prompt
// → 过期淘汰可以按条目粒度执行
type MemoryEntry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"` // 零值表示永不过期
}

// IsExpired 判断记忆是否已过期
func (e MemoryEntry) IsExpired() bool {
	if e.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(e.ExpiresAt)
}

// UserMemory 单个用户的画像记忆。
// 跨会话持久化：Session 销毁后记忆仍在，下次创建 Session 时加载。
//
// 并发安全：读写时使用 RWMutex（与 Session 相同策略）。
type UserMemory struct {
	mu      sync.RWMutex
	userID  string
	entries map[string]MemoryEntry
}

// NewUserMemory 创建空的用户记忆
func NewUserMemory(userID string) *UserMemory {
	return &UserMemory{
		userID:  userID,
		entries: make(map[string]MemoryEntry),
	}
}

// Set 写入或更新一条记忆。
// 如果 key 已存在，更新 value 和 UpdatedAt，保留 CreatedAt。
// ttl 为 0 表示永不过期。
func (m *UserMemory) Set(key, value string, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()

	existing, exists := m.entries[key]

	entry := MemoryEntry{
		Key:       key,
		Value:     value,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// 保留原始创建时间
	if exists {
		entry.CreatedAt = existing.CreatedAt
	}

	if ttl > 0 {
		entry.ExpiresAt = now.Add(ttl)
	}

	m.entries[key] = entry
}

// Get 获取一条记忆，自动跳过已过期的。
// 返回 value 和 ok（是否存在且未过期）。
func (m *UserMemory) Get(key string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entry, exists := m.entries[key]
	if !exists || entry.IsExpired() {
		return "", false
	}
	return entry.Value, true
}

// Delete 删除一条记忆
func (m *UserMemory) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, key)
}

// All 返回所有未过期的记忆（副本，不暴露内部状态）
func (m *UserMemory) All() []MemoryEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]MemoryEntry, 0, len(m.entries))
	for _, e := range m.entries {
		if !e.IsExpired() {
			result = append(result, e)
		}
	}
	return result
}

// Count 返回未过期记忆的条数
func (m *UserMemory) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for _, e := range m.entries {
		if !e.IsExpired() {
			count++
		}
	}
	return count
}

// Cleanup 清除所有已过期的记忆条目，返回清除的条数
func (m *UserMemory) Cleanup() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	removed := 0
	for key, e := range m.entries {
		if e.IsExpired() {
			delete(m.entries, key)
			removed++
		}
	}
	return removed
}

// FormatForPrompt 将用户画像格式化为可注入 System Prompt 的文本。
// 格式示例：
//
//	[用户画像]
//	- 常用地址: 北京市朝阳区xxx
//	- 偏好商品类别: 电子产品
//
// 为什么注入到 System Prompt 而不是单独的 message？
// → System Prompt 是 LLM 最"信任"的信息源，注入其中效果最好。
// → 避免消息顺序问题（user/assistant 交替可能被打乱）。
func (m *UserMemory) FormatForPrompt() string {
	entries := m.All()
	if len(entries) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("[用户画像]\n")
	for _, e := range entries {
		b.WriteString(fmt.Sprintf("- %s: %s\n", e.Key, e.Value))
	}
	return b.String()
}

// UserID 返回关联的用户 ID
func (m *UserMemory) UserID() string {
	return m.userID
}

// ---- 持久化 ----

// memorySnapshot 是 UserMemory 的可序列化快照（与 sessionSnapshot 同模式）
type memorySnapshot struct {
	Version int                    `json:"version"`
	UserID  string                 `json:"user_id"`
	Entries map[string]MemoryEntry `json:"entries"`
}

const memorySnapshotVersion = 1

// MemoryStore 记忆持久化接口。
// 为什么用接口？
// → 当前用文件存储，将来可以换 Redis/数据库，不改业务代码。
type MemoryStore interface {
	Save(memory *UserMemory) error
	Load(userID string) (*UserMemory, error)
}

// FileMemoryStore 基于 JSON 文件的记忆持久化。
// 每个用户一个文件：{dir}/{userID}.json
type FileMemoryStore struct {
	dir string
}

// NewFileMemoryStore 创建文件存储，dir 是存储目录
func NewFileMemoryStore(dir string) *FileMemoryStore {
	return &FileMemoryStore{dir: dir}
}

// Save 将用户记忆持久化到文件（原子写入，与 Session.Save 同策略）
func (fs *FileMemoryStore) Save(memory *UserMemory) error {
	memory.mu.RLock()
	snapshot := memorySnapshot{
		Version: memorySnapshotVersion,
		UserID:  memory.userID,
		Entries: make(map[string]MemoryEntry, len(memory.entries)),
	}
	for k, v := range memory.entries {
		snapshot.Entries[k] = v
	}
	memory.mu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化记忆失败: %w", err)
	}

	if err := os.MkdirAll(fs.dir, 0o755); err != nil {
		return fmt.Errorf("创建记忆目录失败: %w", err)
	}

	filePath := filepath.Join(fs.dir, memory.userID+".json")
	tmpPath := filePath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("重命名文件失败: %w", err)
	}

	return nil
}

// Load 从文件加载用户记忆。文件不存在返回空记忆（不报错）。
func (fs *FileMemoryStore) Load(userID string) (*UserMemory, error) {
	filePath := filepath.Join(fs.dir, userID+".json")

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return NewUserMemory(userID), nil
		}
		return nil, fmt.Errorf("读取记忆文件失败: %w", err)
	}

	var snapshot memorySnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("解析记忆文件失败: %w", err)
	}

	if snapshot.Version != memorySnapshotVersion {
		return nil, fmt.Errorf("不支持的记忆文件版本: %d", snapshot.Version)
	}

	m := &UserMemory{
		userID:  userID,
		entries: snapshot.Entries,
	}
	if m.entries == nil {
		m.entries = make(map[string]MemoryEntry)
	}

	return m, nil
}
