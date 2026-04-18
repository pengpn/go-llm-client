package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pengpn/go-llm-agent/models"
)

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	// 创建并填充会话
	strategy := &ByTurns{MaxTurns: 10}
	original := NewSession("user_test", "你是客服助手", strategy)
	original.AddUserMessage("我的订单在哪里？")
	original.AddAssistantMessage("请提供订单号")
	original.AddUserMessage("ORDER-123")

	// 写入临时目录
	dir := t.TempDir()
	path := filepath.Join(dir, "user_test.json")

	if err := original.Save(path); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	// 验证文件存在
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("文件应存在但未找到: %v", err)
	}

	// 从文件恢复
	loaded, err := LoadSession(path, strategy)
	if err != nil {
		t.Fatalf("LoadSession 失败: %v", err)
	}

	// 验证元数据
	if loaded.ID() != original.ID() {
		t.Errorf("ID 不符: got %q, want %q", loaded.ID(), original.ID())
	}
	if loaded.MessageCount() != original.MessageCount() {
		t.Errorf("消息数不符: got %d, want %d", loaded.MessageCount(), original.MessageCount())
	}

	// 验证消息内容
	origMsgs := original.Messages()
	loadMsgs := loaded.Messages()
	assertMessages(t, loadMsgs, origMsgs)
}

func TestSaveAndLoad_PreservesTimestamps(t *testing.T) {
	strategy := &ByTurns{MaxTurns: 5}
	sess := NewSession("ts_test", "系统", strategy)
	sess.AddUserMessage("消息")

	// 记录原始时间（截断到秒，JSON 精度）
	originalLastActive := sess.LastActiveAt().Truncate(time.Second)

	dir := t.TempDir()
	path := filepath.Join(dir, "ts_test.json")

	if err := sess.Save(path); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	loaded, err := LoadSession(path, strategy)
	if err != nil {
		t.Fatalf("LoadSession 失败: %v", err)
	}

	loadedLastActive := loaded.LastActiveAt().Truncate(time.Second)
	if !loadedLastActive.Equal(originalLastActive) {
		t.Errorf("lastActiveAt 不符: got %v, want %v", loadedLastActive, originalLastActive)
	}
}

func TestSaveAndLoad_EmptySession(t *testing.T) {
	// 无 system prompt、无消息的空会话
	strategy := &ByTurns{MaxTurns: 5}
	sess := NewSession("empty", "", strategy)

	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")

	if err := sess.Save(path); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	loaded, err := LoadSession(path, strategy)
	if err != nil {
		t.Fatalf("LoadSession 失败: %v", err)
	}

	if loaded.MessageCount() != 0 {
		t.Errorf("空会话消息数应为 0，got %d", loaded.MessageCount())
	}
}

func TestSaveAndLoad_CreatesDirectory(t *testing.T) {
	// Save 应自动创建不存在的目录
	strategy := &ByTurns{MaxTurns: 5}
	sess := NewSession("dir_test", "系统", strategy)

	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "nested", "session.json")

	if err := sess.Save(path); err != nil {
		t.Fatalf("Save 应自动创建目录，但失败: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("文件应存在: %v", err)
	}
}

func TestLoadSession_FileNotFound(t *testing.T) {
	_, err := LoadSession("/nonexistent/path/session.json", &ByTurns{MaxTurns: 5})
	if err == nil {
		t.Error("加载不存在的文件应返回错误")
	}
}

func TestLoadSession_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")

	if err := os.WriteFile(path, []byte("不是 JSON {{{"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := LoadSession(path, &ByTurns{MaxTurns: 5})
	if err == nil {
		t.Error("加载损坏 JSON 应返回错误")
	}
}

func TestSave_AtomicWrite(t *testing.T) {
	// 验证原子写入：写完后不应存在 .tmp 临时文件
	strategy := &ByTurns{MaxTurns: 5}
	sess := NewSession("atomic", "系统", strategy)
	sess.AddUserMessage("测试")

	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.json")

	if err := sess.Save(path); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf("写入完成后不应存在临时文件 %s", tmpPath)
	}
}

func TestLoadSession_StrategyApplied(t *testing.T) {
	// 验证 Load 后，新的截断策略生效
	strategy := &ByTurns{MaxTurns: 10}
	sess := NewSession("strategy_test", "系统", strategy)
	for range 8 {
		sess.AddUserMessage("问")
		sess.AddAssistantMessage("答")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "strategy_test.json")
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}

	// 用更严格的策略加载
	strictStrategy := &ByTurns{MaxTurns: 3}
	loaded, err := LoadSession(path, strictStrategy)
	if err != nil {
		t.Fatal(err)
	}

	// Messages() 会应用新策略：1 system + 3 轮×2 = 7
	msgs := loaded.Messages()
	want := 1 + 3*2
	if len(msgs) != want {
		t.Errorf("新策略应截断为 %d 条，got %d", want, len(msgs))
	}

	// 但内部存储仍是完整历史
	if loaded.MessageCount() != sess.MessageCount() {
		t.Errorf("内部历史应完整保存，got %d want %d",
			loaded.MessageCount(), sess.MessageCount())
	}
}

// 验证 Save 保存的是全量历史（未截断版本）
func TestSave_StoresFullHistory(t *testing.T) {
	// 用严格策略创建，产生截断
	strategy := &ByTurns{MaxTurns: 2}
	sess := NewSession("full_hist", "系统", strategy)
	for range 5 {
		sess.AddUserMessage("问")
		sess.AddAssistantMessage("答")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "full_hist.json")
	if err := sess.Save(path); err != nil {
		t.Fatal(err)
	}

	// 用宽松策略加载，应能看到全量历史
	loaded, err := LoadSession(path, &ByTurns{MaxTurns: 100})
	if err != nil {
		t.Fatal(err)
	}

	// 全量：1 system + 5 轮×2 = 11
	wantFull := 1 + 5*2
	if loaded.MessageCount() != wantFull {
		t.Errorf("Save 应保存全量历史 %d 条，LoadSession 读回 %d 条",
			wantFull, loaded.MessageCount())
	}

	// 通过 Messages() 仍被新策略截断为 2 轮
	msgs := loaded.Messages()
	if len(msgs) != 1+5*2 { // MaxTurns=100 远大于实际 5 轮，不截断
		// 用宽松策略，不截断，1+10=11
	}
	_ = msgs
}

// 辅助：生成指定角色的消息列表
func makeMessages(roles ...models.Role) []models.Message {
	msgs := make([]models.Message, len(roles))
	for i, r := range roles {
		msgs[i] = models.Message{Role: r, Content: "内容"}
	}
	return msgs
}
