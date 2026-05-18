package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUserMemory_SetAndGet(t *testing.T) {
	m := NewUserMemory("u1")

	m.Set("preferred_address", "北京市朝阳区", 0)
	val, ok := m.Get("preferred_address")
	if !ok || val != "北京市朝阳区" {
		t.Fatalf("expected '北京市朝阳区', got %q (ok=%v)", val, ok)
	}
}

func TestUserMemory_GetNonExistent(t *testing.T) {
	m := NewUserMemory("u1")

	_, ok := m.Get("non_existent")
	if ok {
		t.Fatal("should return false for non-existent key")
	}
}

func TestUserMemory_Update_PreservesCreatedAt(t *testing.T) {
	m := NewUserMemory("u1")

	m.Set("name", "旧名字", 0)
	m.mu.RLock()
	created := m.entries["name"].CreatedAt
	m.mu.RUnlock()

	time.Sleep(1 * time.Millisecond)

	m.Set("name", "新名字", 0)
	m.mu.RLock()
	entry := m.entries["name"]
	m.mu.RUnlock()

	if entry.Value != "新名字" {
		t.Fatalf("value should be updated, got %q", entry.Value)
	}
	if !entry.CreatedAt.Equal(created) {
		t.Fatal("CreatedAt should be preserved on update")
	}
	if !entry.UpdatedAt.After(created) {
		t.Fatal("UpdatedAt should be newer than CreatedAt")
	}
}

func TestUserMemory_Expiration(t *testing.T) {
	m := NewUserMemory("u1")

	// TTL 为 1 毫秒，设置后等待过期
	m.Set("temp_data", "临时", 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	_, ok := m.Get("temp_data")
	if ok {
		t.Fatal("expired entry should not be returned")
	}
}

func TestUserMemory_NoExpiration(t *testing.T) {
	m := NewUserMemory("u1")

	m.Set("permanent", "永久数据", 0)
	val, ok := m.Get("permanent")
	if !ok || val != "永久数据" {
		t.Fatal("entry with ttl=0 should never expire")
	}
}

func TestUserMemory_Delete(t *testing.T) {
	m := NewUserMemory("u1")

	m.Set("key", "value", 0)
	m.Delete("key")

	_, ok := m.Get("key")
	if ok {
		t.Fatal("deleted key should not be found")
	}
}

func TestUserMemory_All_ExcludesExpired(t *testing.T) {
	m := NewUserMemory("u1")

	m.Set("active", "活跃", 0)
	m.Set("expired", "过期", 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	entries := m.All()
	if len(entries) != 1 {
		t.Fatalf("expected 1 active entry, got %d", len(entries))
	}
	if entries[0].Key != "active" {
		t.Fatalf("expected 'active', got %q", entries[0].Key)
	}
}

func TestUserMemory_Count(t *testing.T) {
	m := NewUserMemory("u1")

	m.Set("a", "1", 0)
	m.Set("b", "2", 0)
	m.Set("c", "3", 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	if m.Count() != 2 {
		t.Fatalf("expected 2, got %d", m.Count())
	}
}

func TestUserMemory_Cleanup(t *testing.T) {
	m := NewUserMemory("u1")

	m.Set("keep", "保留", 0)
	m.Set("expire1", "过期1", 1*time.Millisecond)
	m.Set("expire2", "过期2", 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	removed := m.Cleanup()
	if removed != 2 {
		t.Fatalf("expected 2 removed, got %d", removed)
	}

	m.mu.RLock()
	count := len(m.entries)
	m.mu.RUnlock()
	if count != 1 {
		t.Fatalf("expected 1 entry remaining in map, got %d", count)
	}
}

func TestUserMemory_FormatForPrompt(t *testing.T) {
	m := NewUserMemory("u1")
	m.Set("常用地址", "北京市朝阳区", 0)
	m.Set("偏好", "电子产品", 0)

	text := m.FormatForPrompt()
	if !strings.Contains(text, "[用户画像]") {
		t.Fatal("should contain header")
	}
	if !strings.Contains(text, "常用地址: 北京市朝阳区") {
		t.Fatal("should contain address")
	}
	if !strings.Contains(text, "偏好: 电子产品") {
		t.Fatal("should contain preference")
	}
}

func TestUserMemory_FormatForPrompt_Empty(t *testing.T) {
	m := NewUserMemory("u1")
	if m.FormatForPrompt() != "" {
		t.Fatal("empty memory should return empty string")
	}
}

// ---- FileMemoryStore 测试 ----

func TestFileMemoryStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewFileMemoryStore(dir)

	m := NewUserMemory("user-001")
	m.Set("地址", "上海市浦东新区", 0)
	m.Set("会员等级", "金牌", 24*time.Hour)

	if err := store.Save(m); err != nil {
		t.Fatalf("save error: %v", err)
	}

	// 验证文件存在
	filePath := filepath.Join(dir, "user-001.json")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatal("memory file should exist")
	}

	loaded, err := store.Load("user-001")
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	val, ok := loaded.Get("地址")
	if !ok || val != "上海市浦东新区" {
		t.Fatalf("expected '上海市浦东新区', got %q", val)
	}

	val, ok = loaded.Get("会员等级")
	if !ok || val != "金牌" {
		t.Fatalf("expected '金牌', got %q", val)
	}
}

func TestFileMemoryStore_LoadNonExistent(t *testing.T) {
	dir := t.TempDir()
	store := NewFileMemoryStore(dir)

	m, err := store.Load("non-existent")
	if err != nil {
		t.Fatalf("loading non-existent should not error: %v", err)
	}
	if m.UserID() != "non-existent" {
		t.Fatalf("expected userID 'non-existent', got %q", m.UserID())
	}
	if m.Count() != 0 {
		t.Fatal("loaded memory should be empty")
	}
}

func TestFileMemoryStore_CorruptedFile(t *testing.T) {
	dir := t.TempDir()
	store := NewFileMemoryStore(dir)

	filePath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(filePath, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := store.Load("bad")
	if err == nil {
		t.Fatal("should error on corrupted file")
	}
}
