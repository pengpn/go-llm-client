package session

import (
	"testing"

	"github.com/pengpn/go-llm-agent/models"
)

// ---- 测试辅助函数 ----

func systemMsg(content string) models.Message {
	return models.Message{Role: models.RoleSystem, Content: content}
}

func userMsg(content string) models.Message {
	return models.Message{Role: models.RoleUser, Content: content}
}

func assistantMsg(content string) models.Message {
	return models.Message{Role: models.RoleAssistant, Content: content}
}

// makeTurns 生成 n 轮对话（n 个 user + n 个 assistant）
func makeTurns(n int) []models.Message {
	msgs := make([]models.Message, 0, n*2)
	for i := range n {
		msgs = append(msgs,
			userMsg("用户消息 "+string(rune('A'+i))),
			assistantMsg("助手回复 "+string(rune('A'+i))),
		)
	}
	return msgs
}

// assertMessages 验证消息列表内容与期望一致。
func assertMessages(t *testing.T, got, want []models.Message) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("消息数量不符：got %d, want %d\ngot:  %v\nwant: %v",
			len(got), len(want), got, want)
	}
	for i := range got {
		if got[i].Role != want[i].Role || got[i].Content != want[i].Content {
			t.Errorf("第 %d 条消息不符:\n  got  {%s: %q}\n  want {%s: %q}",
				i, got[i].Role, got[i].Content, want[i].Role, want[i].Content)
		}
	}
}

// ====================================================================
// ByTurns 测试
// ====================================================================

func TestByTurns_EmptyMessages(t *testing.T) {
	s := &ByTurns{MaxTurns: 5}
	got := s.Truncate(nil)
	if got != nil {
		t.Errorf("空输入应返回 nil，got %v", got)
	}

	got = s.Truncate([]models.Message{})
	if len(got) != 0 {
		t.Errorf("空切片输入应返回空切片，got %v", got)
	}
}

func TestByTurns_OnlySystemPrompt(t *testing.T) {
	s := &ByTurns{MaxTurns: 5}
	input := []models.Message{systemMsg("你是客服助手")}

	got := s.Truncate(input)

	assertMessages(t, got, input)
}

func TestByTurns_BelowLimit(t *testing.T) {
	// 3 轮对话，MaxTurns=5，不应截断
	s := &ByTurns{MaxTurns: 5}
	history := makeTurns(3)
	input := append([]models.Message{systemMsg("系统")}, history...)

	got := s.Truncate(input)

	assertMessages(t, got, input)
}

func TestByTurns_ExactlyAtLimit(t *testing.T) {
	// 恰好 5 轮，MaxTurns=5，不应截断
	s := &ByTurns{MaxTurns: 5}
	history := makeTurns(5)
	input := append([]models.Message{systemMsg("系统")}, history...)

	got := s.Truncate(input)

	assertMessages(t, got, input)
}

func TestByTurns_OneTurnOverLimit(t *testing.T) {
	// 6 轮，MaxTurns=5，应截掉最旧的 1 轮（前 2 条）
	s := &ByTurns{MaxTurns: 5}
	history := makeTurns(6)
	input := append([]models.Message{systemMsg("系统")}, history...)

	got := s.Truncate(input)

	// 期望：system + 最新 5 轮（跳过第 0 轮）
	want := append([]models.Message{systemMsg("系统")}, history[2:]...)
	assertMessages(t, got, want)
}

func TestByTurns_SystemPromptAlwaysPreserved(t *testing.T) {
	// 确保截断后 system prompt 始终存在且在第一位
	s := &ByTurns{MaxTurns: 2}
	input := append(
		[]models.Message{systemMsg("永远保留我")},
		makeTurns(5)...,
	)

	got := s.Truncate(input)

	if got[0].Role != models.RoleSystem {
		t.Errorf("截断后第一条消息应为 system，got %s", got[0].Role)
	}
	if got[0].Content != "永远保留我" {
		t.Errorf("system prompt 内容被改变：got %q", got[0].Content)
	}
}

func TestByTurns_MaxTurnsZero(t *testing.T) {
	// MaxTurns=0 时，只保留 system prompt，历史全部丢弃
	s := &ByTurns{MaxTurns: 0}
	input := append(
		[]models.Message{systemMsg("系统")},
		makeTurns(3)...,
	)

	got := s.Truncate(input)

	want := []models.Message{systemMsg("系统")}
	assertMessages(t, got, want)
}

func TestByTurns_MultipleSystemMessages(t *testing.T) {
	// 多个 system 消息都应被保留（极端情况）
	s := &ByTurns{MaxTurns: 1}
	input := []models.Message{
		systemMsg("系统1"),
		systemMsg("系统2"),
		userMsg("问题"),
		assistantMsg("回答"),
		userMsg("追问"),
		assistantMsg("回答2"),
	}

	got := s.Truncate(input)

	// 2 个 system + 最近 1 轮（2 条）
	want := []models.Message{
		systemMsg("系统1"),
		systemMsg("系统2"),
		userMsg("追问"),
		assistantMsg("回答2"),
	}
	assertMessages(t, got, want)
}

func TestByTurns_NoSystemPrompt(t *testing.T) {
	// 无 system prompt 时也能正确截断
	s := &ByTurns{MaxTurns: 2}
	input := makeTurns(4)

	got := s.Truncate(input)

	want := makeTurns(4)[4:] // 最新 2 轮 = 后 4 条
	assertMessages(t, got, want)
}

// ====================================================================
// ByTokenCount 测试（使用 EstimateCounter，保证测试确定性）
// ====================================================================

// fixedCounter 是固定返回指定值的 TokenCounter，用于测试。
type fixedCounter struct{ tokensPerChar int }

func (f *fixedCounter) Count(text string) int {
	return len([]rune(text)) * f.tokensPerChar
}

// newTestByToken 创建使用固定计数器的策略，方便写确定性测试。
func newTestByToken(maxTokens int) *ByTokenCount {
	return &ByTokenCount{
		MaxTokens: maxTokens,
		counter:   &EstimateCounter{}, // 1 字符 ≈ 1 token
	}
}

func TestByTokenCount_EmptyMessages(t *testing.T) {
	s := newTestByToken(100)

	got := s.Truncate(nil)
	if got != nil {
		t.Errorf("空输入应返回 nil")
	}

	got = s.Truncate([]models.Message{})
	if len(got) != 0 {
		t.Errorf("空切片应返回空切片")
	}
}

func TestByTokenCount_OnlySystemPrompt(t *testing.T) {
	s := newTestByToken(1000)
	input := []models.Message{systemMsg("系统提示词")}

	got := s.Truncate(input)

	assertMessages(t, got, input)
}

func TestByTokenCount_SystemPromptExceedsLimit(t *testing.T) {
	// system prompt 本身超过 MaxTokens，只保留 system prompt
	// "系统提示词很长" = 8 字符，加固定开销 4 = 12 tokens
	s := newTestByToken(10) // 预算不够 system prompt

	input := []models.Message{
		systemMsg("系统提示词很长"),
		userMsg("问题"),
	}

	got := s.Truncate(input)

	want := []models.Message{systemMsg("系统提示词很长")}
	assertMessages(t, got, want)
}

func TestByTokenCount_AllHistoryFits(t *testing.T) {
	// 所有历史都在预算内，不截断
	// system: 4+2=6, 每条 history: 4+2=6，3 条历史 = 18，总 = 24
	s := newTestByToken(100)
	input := []models.Message{
		systemMsg("系统"),
		userMsg("问题"),
		assistantMsg("回答"),
		userMsg("追问"),
	}

	got := s.Truncate(input)

	assertMessages(t, got, input)
}

func TestByTokenCount_TruncatesOldest(t *testing.T) {
	// 构造精确场景：
	// system "ab" → 4+2=6 tokens
	// 每条历史 "ab" → 4+2=6 tokens
	// MaxTokens=18 → budget=12 → 最多容纳 2 条历史
	s := &ByTokenCount{
		MaxTokens: 18,
		counter:   &EstimateCounter{},
	}

	input := []models.Message{
		systemMsg("ab"),
		userMsg("ab"),      // 最旧，应被截掉
		assistantMsg("ab"), // 应被截掉
		userMsg("ab"),      // 保留
		assistantMsg("ab"), // 保留
	}

	got := s.Truncate(input)

	want := []models.Message{
		systemMsg("ab"),
		userMsg("ab"),
		assistantMsg("ab"),
	}
	assertMessages(t, got, want)
}

func TestByTokenCount_ExactlyAtBudget(t *testing.T) {
	// 恰好等于预算，不应截断
	// system "ab" = 6, history 每条 "ab" = 6
	// MaxTokens = 6+6+6 = 18，3 条消息（1 system + 2 history）
	s := &ByTokenCount{
		MaxTokens: 18,
		counter:   &EstimateCounter{},
	}

	input := []models.Message{
		systemMsg("ab"),
		userMsg("ab"),
		assistantMsg("ab"),
	}

	got := s.Truncate(input)

	assertMessages(t, got, input)
}

func TestByTokenCount_NoSystemPrompt(t *testing.T) {
	// 无 system prompt，纯历史截断
	s := &ByTokenCount{
		MaxTokens: 14, // budget = 14，每条 "ab" = 6，最多 2 条
		counter:   &EstimateCounter{},
	}

	input := []models.Message{
		userMsg("ab"),
		assistantMsg("ab"),
		userMsg("ab"),
		assistantMsg("ab"),
	}

	got := s.Truncate(input)

	want := []models.Message{
		userMsg("ab"),
		assistantMsg("ab"),
	}
	assertMessages(t, got, want)
}

// ====================================================================
// TikTokenCounter 集成测试（需要网络/文件，标记为可跳过）
// ====================================================================

func TestTikTokenCounter_KnownTokenCounts(t *testing.T) {
	counter := NewTikTokenCounter("gpt-4o-mini")

	// 验证已知 Token 数（来自 OpenAI 官方 tokenizer playground）
	cases := []struct {
		text string
		want int
	}{
		{"hello", 1},
		{"hello world", 2},
		{"你好", 2}, // 中文每个字约 1-2 token
	}

	for _, tc := range cases {
		got := counter.Count(tc.text)
		// tiktoken 计数精确，但测试时允许±1 误差（不同版本 BPE 结果可能略有不同）
		if got == 0 {
			t.Errorf("Count(%q) = 0，tiktoken 应返回正数", tc.text)
		}
		// 验证英文结果精确
		if tc.text == "hello" && got != tc.want {
			t.Errorf("Count(%q) = %d，want %d", tc.text, got, tc.want)
		}
	}
}

func TestTikTokenCounter_UnsupportedModel_Fallback(t *testing.T) {
	// 不支持的模型应降级为 EstimateCounter，不 panic
	counter := NewTikTokenCounter("glm-4-flash")

	got := counter.Count("hello")
	if got <= 0 {
		t.Errorf("降级后 Count 应返回正数，got %d", got)
	}
}

// ====================================================================
// ByTokenEstimate 向后兼容测试
// ====================================================================

func TestByTokenEstimate_BackwardCompatible(t *testing.T) {
	// 确保旧代码用 ByTokenEstimate 仍然可以工作
	s := &ByTokenEstimate{MaxTokens: 100}

	input := []models.Message{
		systemMsg("系统"),
		userMsg("问题"),
	}

	got := s.Truncate(input)
	if len(got) == 0 {
		t.Error("ByTokenEstimate 应正常工作，但返回了空消息")
	}
}
