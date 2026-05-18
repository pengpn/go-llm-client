package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/pengpn/go-llm-agent/models"
	"github.com/pengpn/go-llm-agent/session"
)

func TestExtractedMemories_Validate_Success(t *testing.T) {
	m := &ExtractedMemories{
		Items: []ExtractedMemoryItem{
			{Key: "地址", Value: "北京", Confidence: 0.9},
			{Key: "偏好", Value: "电子产品", Confidence: 0.7},
		},
	}
	if err := m.Validate(); err != nil {
		t.Fatalf("valid memories should pass: %v", err)
	}
}

func TestExtractedMemories_Validate_EmptyKey(t *testing.T) {
	m := &ExtractedMemories{
		Items: []ExtractedMemoryItem{
			{Key: "", Value: "北京", Confidence: 0.9},
		},
	}
	if err := m.Validate(); err == nil {
		t.Fatal("empty key should fail validation")
	}
}

func TestExtractedMemories_Validate_ConfidenceOutOfRange(t *testing.T) {
	m := &ExtractedMemories{
		Items: []ExtractedMemoryItem{
			{Key: "地址", Value: "北京", Confidence: 1.5},
		},
	}
	if err := m.Validate(); err == nil {
		t.Fatal("confidence > 1 should fail validation")
	}
}

func TestExtractedMemories_Validate_EmptyItems(t *testing.T) {
	m := &ExtractedMemories{Items: []ExtractedMemoryItem{}}
	if err := m.Validate(); err != nil {
		t.Fatalf("empty items should pass: %v", err)
	}
}

func TestMemoryExtractor_Extract_FiltersLowConfidence(t *testing.T) {
	client := &mockExtractorClient{
		resp: &models.Response{
			FinishReason: "tool_calls",
			ToolCalls: []models.ToolCall{
				{
					ID:   "call-1",
					Type: "function",
					Function: models.FunctionCall{
						Name: "extract_user_profile",
						Arguments: `{"items":[
							{"key":"常用地址","value":"北京朝阳区","confidence":0.95},
							{"key":"可能的职业","value":"程序员","confidence":0.5},
							{"key":"偏好商品","value":"电子产品","confidence":0.8}
						]}`,
					},
				},
			},
		},
	}

	me := NewMemoryExtractor(client, WithConfidenceThreshold(0.7))
	items := me.Extract(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "我住在北京朝阳区，想买个手机"},
	})

	// confidence 0.5 的"可能的职业"应被过滤
	if len(items) != 2 {
		t.Fatalf("expected 2 items (filtered 1 low confidence), got %d", len(items))
	}

	keys := make(map[string]bool)
	for _, item := range items {
		keys[item.Key] = true
	}
	if keys["可能的职业"] {
		t.Error("low confidence item should be filtered out")
	}
	if !keys["常用地址"] || !keys["偏好商品"] {
		t.Error("high confidence items should be kept")
	}
}

func TestMemoryExtractor_Extract_LLMError_ReturnsNil(t *testing.T) {
	client := &mockExtractorClient{err: errors.New("LLM 不可用")}
	me := NewMemoryExtractor(client)

	items := me.Extract(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "你好"},
	})
	if items != nil {
		t.Fatal("should return nil on LLM error")
	}
}

func TestMemoryExtractor_Extract_EmptyItems_ReturnsNil(t *testing.T) {
	client := &mockExtractorClient{
		resp: &models.Response{
			FinishReason: "tool_calls",
			ToolCalls: []models.ToolCall{
				{
					ID:   "call-1",
					Type: "function",
					Function: models.FunctionCall{
						Name:      "extract_user_profile",
						Arguments: `{"items":[]}`,
					},
				},
			},
		},
	}

	me := NewMemoryExtractor(client)
	items := me.Extract(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "你好"},
	})
	if items != nil {
		t.Fatal("empty items should return nil")
	}
}

func TestMemoryExtractor_ExtractAndSave(t *testing.T) {
	client := &mockExtractorClient{
		resp: &models.Response{
			FinishReason: "tool_calls",
			ToolCalls: []models.ToolCall{
				{
					ID:   "call-1",
					Type: "function",
					Function: models.FunctionCall{
						Name: "extract_user_profile",
						Arguments: `{"items":[
							{"key":"常用地址","value":"上海浦东","confidence":0.9},
							{"key":"会员等级","value":"金牌","confidence":0.85}
						]}`,
					},
				},
			},
		},
	}

	store := session.NewFileMemoryStore(t.TempDir())
	me := NewMemoryExtractor(client)

	msgs := []models.Message{
		{Role: models.RoleUser, Content: "我住在上海浦东，我是金牌会员"},
		{Role: models.RoleAssistant, Content: "好的，已确认您的信息"},
	}

	count := me.ExtractAndSave(context.Background(), msgs, "u-extract", store)
	if count != 2 {
		t.Fatalf("expected 2 saved, got %d", count)
	}

	// 验证持久化
	memory, err := store.Load("u-extract")
	if err != nil {
		t.Fatal(err)
	}

	val, ok := memory.Get("常用地址")
	if !ok || val != "上海浦东" {
		t.Errorf("expected 常用地址=上海浦东, got %q (ok=%v)", val, ok)
	}
	val, ok = memory.Get("会员等级")
	if !ok || val != "金牌" {
		t.Errorf("expected 会员等级=金牌, got %q (ok=%v)", val, ok)
	}
}

func TestMemoryExtractor_ExtractAndSave_FailureDoesNotPanic(t *testing.T) {
	client := &mockExtractorClient{err: errors.New("LLM timeout")}
	store := session.NewFileMemoryStore(t.TempDir())
	me := NewMemoryExtractor(client)

	count := me.ExtractAndSave(context.Background(), nil, "u-fail", store)
	if count != 0 {
		t.Fatalf("expected 0 on failure, got %d", count)
	}
}

func TestMemoryExtractor_CustomThreshold(t *testing.T) {
	client := &mockExtractorClient{
		resp: &models.Response{
			FinishReason: "tool_calls",
			ToolCalls: []models.ToolCall{
				{
					ID:   "call-1",
					Type: "function",
					Function: models.FunctionCall{
						Name: "extract_user_profile",
						Arguments: `{"items":[
							{"key":"地址","value":"北京","confidence":0.85},
							{"key":"偏好","value":"书籍","confidence":0.6}
						]}`,
					},
				},
			},
		},
	}

	// 高阈值 0.9：两条都被过滤
	me := NewMemoryExtractor(client, WithConfidenceThreshold(0.9))
	items := me.Extract(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "test"},
	})
	if items != nil {
		t.Fatalf("expected nil with high threshold, got %d items", len(items))
	}
}

// ── 集成测试：processChat 中的自动提取 ──

func TestHandleChat_WithMemoryExtractor_SavesProfile(t *testing.T) {
	ag := &mockAgent{answer: "好的，记住了"}
	extractClient := &mockExtractorClient{
		resp: &models.Response{
			FinishReason: "tool_calls",
			ToolCalls: []models.ToolCall{
				{
					ID:   "call-1",
					Type: "function",
					Function: models.FunctionCall{
						Name: "extract_user_profile",
						Arguments: `{"items":[
							{"key":"常用地址","value":"深圳南山区","confidence":0.95}
						]}`,
					},
				},
			},
		},
	}

	dir := t.TempDir()
	store := session.NewFileMemoryStore(dir)
	me := NewMemoryExtractor(extractClient)

	srv := newTestServer(ag, &mockIndexer{}, nil,
		WithMemoryStore(store),
		WithMemoryExtractor(me),
	)

	w := postJSON(srv, "/chat", `{"user_id":"u-auto","message":"我住在深圳南山区"}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d, body: %s", w.Code, w.Body.String())
	}

	// 验证画像被自动保存
	memory, err := store.Load("u-auto")
	if err != nil {
		t.Fatal(err)
	}
	val, ok := memory.Get("常用地址")
	if !ok || val != "深圳南山区" {
		t.Errorf("expected 常用地址=深圳南山区, got %q (ok=%v)", val, ok)
	}
}

func TestHandleChat_MemoryExtractorFails_StillReturnsAnswer(t *testing.T) {
	ag := &mockAgent{answer: "正常回答"}
	extractClient := &mockExtractorClient{err: errors.New("提取失败")}

	store := session.NewFileMemoryStore(t.TempDir())
	me := NewMemoryExtractor(extractClient)

	srv := newTestServer(ag, &mockIndexer{}, nil,
		WithMemoryStore(store),
		WithMemoryExtractor(me),
	)

	w := postJSON(srv, "/chat", `{"user_id":"u-fail2","message":"你好"}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp ChatResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Answer != "正常回答" {
		t.Errorf("expected answer=正常回答, got %q", resp.Answer)
	}
}

func TestHandleChat_NoMemoryExtractor_NoExtraction(t *testing.T) {
	ag := &mockAgent{answer: "ok"}
	store := session.NewFileMemoryStore(t.TempDir())

	// 有 memoryStore 但无 memoryExtractor
	srv := newTestServer(ag, &mockIndexer{}, nil, WithMemoryStore(store))

	w := postJSON(srv, "/chat", `{"user_id":"u-no-ext","message":"我住北京"}`)
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	memory, _ := store.Load("u-no-ext")
	if memory.Count() != 0 {
		t.Error("without extractor, no memories should be saved")
	}
}

func TestMemoryExtractor_AllItemsBelowThreshold_ReturnsNil(t *testing.T) {
	client := &mockExtractorClient{
		resp: &models.Response{
			FinishReason: "tool_calls",
			ToolCalls: []models.ToolCall{
				{
					ID:   "call-1",
					Type: "function",
					Function: models.FunctionCall{
						Name: "extract_user_profile",
						Arguments: `{"items":[
							{"key":"猜测","value":"可能喜欢运动","confidence":0.3}
						]}`,
					},
				},
			},
		},
	}

	me := NewMemoryExtractor(client)
	items := me.Extract(context.Background(), []models.Message{
		{Role: models.RoleUser, Content: "test"},
	})
	if items != nil {
		t.Fatal("all items below threshold should return nil")
	}
}

