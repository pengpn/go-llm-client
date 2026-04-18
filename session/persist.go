package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/pengpn/go-llm-agent/models"
)

// sessionSnapshot 是 Session 的可序列化快照。
// 使用独立结构体而不是直接序列化 Session，原因：
//  1. Session 含有 sync.RWMutex，无法被 json 序列化
//  2. TruncateStrategy 是接口，加载时需由调用方传入，不存入文件
//  3. Version 字段为将来格式升级预留空间
type sessionSnapshot struct {
	Version      int              `json:"version"`
	ID           string           `json:"id"`
	SystemPrompt string           `json:"system_prompt"`
	Messages     []models.Message `json:"messages"`
	CreatedAt    time.Time        `json:"created_at"`
	LastActiveAt time.Time        `json:"last_active_at"`
}

const snapshotVersion = 1

// Save 将 Session 的完整消息历史序列化为 JSON 文件。
// 保存的是全量历史（未截断），以便恢复后可用不同截断策略重新计算。
// 如果目标目录不存在，会自动创建。
func (s *Session) Save(path string) error {
	s.mu.RLock()
	snapshot := sessionSnapshot{
		Version:      snapshotVersion,
		ID:           s.id,
		SystemPrompt: s.systemPrompt,
		Messages:     make([]models.Message, len(s.messages)),
		CreatedAt:    s.createdAt,
		LastActiveAt: s.lastActiveAt,
	}
	copy(snapshot.Messages, s.messages)
	s.mu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化 Session 失败: %w", err)
	}

	// 确保目录存在
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
		}
	}

	// 写入临时文件再原子性重命名，防止写到一半程序崩溃导致文件损坏
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("重命名文件失败: %w", err)
	}

	return nil
}

// LoadSession 从 JSON 文件恢复 Session。
// strategy 需由调用方传入（文件中不存储策略，方便灵活替换）。
//
// 使用示例：
//
//	sess, err := session.LoadSession("./sessions/user_001.json",
//	    &session.ByTurns{MaxTurns: 20})
func LoadSession(path string, strategy TruncateStrategy) (*Session, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 Session 文件 %s 失败: %w", path, err)
	}

	var snapshot sessionSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("解析 Session 文件失败: %w", err)
	}

	if snapshot.Version != snapshotVersion {
		return nil, fmt.Errorf("不支持的 Session 文件版本: %d（当前版本: %d）",
			snapshot.Version, snapshotVersion)
	}

	if snapshot.ID == "" {
		return nil, fmt.Errorf("Session 文件格式错误：id 不能为空")
	}

	// 恢复 Session，使用文件中的时间戳
	s := &Session{
		id:           snapshot.ID,
		systemPrompt: snapshot.SystemPrompt,
		messages:     snapshot.Messages,
		strategy:     strategy,
		createdAt:    snapshot.CreatedAt,
		lastActiveAt: snapshot.LastActiveAt,
	}

	return s, nil
}
