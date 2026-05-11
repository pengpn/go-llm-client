package rag

import (
	"context"
	"testing"
)

// ── mock 定义 ──────────────────────────────────────────────────────────────

type mockEmbed struct{}

func (m *mockEmbed) Embed(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range result {
		result[i] = []float32{0.1, 0.2, 0.3, 0.4}
	}
	return result, nil
}

type mockVectorStore struct {
	results []SearchResult
}

func (m *mockVectorStore) Index(_ context.Context, _ []Document) error { return nil }
func (m *mockVectorStore) Search(_ context.Context, _ []float32, topK int) ([]SearchResult, error) {
	if topK > len(m.results) {
		topK = len(m.results)
	}
	return m.results[:topK], nil
}

// ── filterByScore 测试 ─────────────────────────────────────────────────────

func TestFilterByScore_FiltersLowScores(t *testing.T) {
	hits := []SearchResult{
		{Document: Document{Content: "高相关"}, Score: 0.9},
		{Document: Document{Content: "中相关"}, Score: 0.6},
		{Document: Document{Content: "低相关"}, Score: 0.3},
	}
	got := filterByScore(hits, 0.5)
	if len(got) != 2 {
		t.Fatalf("期望 2 个结果，实际 %d 个", len(got))
	}
	if got[0].Score < 0.5 || got[1].Score < 0.5 {
		t.Error("过滤后的结果 Score 应全部 >= 0.5")
	}
}

func TestFilterByScore_AllPass(t *testing.T) {
	hits := []SearchResult{
		{Score: 0.8},
		{Score: 0.9},
	}
	got := filterByScore(hits, 0.5)
	if len(got) != 2 {
		t.Fatalf("期望全部通过，实际 %d 个", len(got))
	}
}

func TestFilterByScore_AllFiltered(t *testing.T) {
	hits := []SearchResult{
		{Score: 0.2},
		{Score: 0.1},
	}
	got := filterByScore(hits, 0.5)
	if len(got) != 0 {
		t.Fatalf("期望全部过滤，实际 %d 个", len(got))
	}
}

func TestFilterByScore_ExactBoundary(t *testing.T) {
	hits := []SearchResult{
		{Document: Document{Content: "边界值"}, Score: 0.5},
	}
	got := filterByScore(hits, 0.5)
	if len(got) != 1 {
		t.Error("Score == minScore 应该通过（>=）")
	}
}

func TestFilterByScore_DoesNotMutateOriginal(t *testing.T) {
	hits := []SearchResult{
		{Score: 0.9},
		{Score: 0.3},
	}
	original := make([]SearchResult, len(hits))
	copy(original, hits)

	filterByScore(hits, 0.5)

	// 原始 slice 不应被修改
	for i, h := range hits {
		if h.Score != original[i].Score {
			t.Errorf("filterByScore 修改了原始 slice[%d]", i)
		}
	}
}

// ── Retriever 集成测试 ─────────────────────────────────────────────────────

func TestRetriever_WithMinScore_FiltersResults(t *testing.T) {
	store := &mockVectorStore{
		results: []SearchResult{
			{Document: Document{Content: "高相关文档", Meta: map[string]string{"source": "FAQ"}}, Score: 0.9},
			{Document: Document{Content: "低相关文档", Meta: map[string]string{"source": "FAQ"}}, Score: 0.2},
		},
	}
	retriever := NewRetriever(&mockEmbed{}, store, 5, WithMinScore(0.5))

	ctx, hits, err := retriever.Retrieve(context.Background(), "任意问题")
	if err != nil {
		t.Fatalf("Retrieve 失败: %v", err)
	}
	if len(hits) != 1 {
		t.Errorf("期望 1 个结果（Score>=0.5），实际 %d 个", len(hits))
	}
	if ctx == "" {
		t.Error("过滤后仍有结果，上下文不应为空")
	}
}

func TestRetriever_WithMinScore_AllFiltered_EmptyContext(t *testing.T) {
	store := &mockVectorStore{
		results: []SearchResult{
			{Document: Document{Content: "不相关文档"}, Score: 0.1},
		},
	}
	retriever := NewRetriever(&mockEmbed{}, store, 5, WithMinScore(0.8))

	ctx, hits, err := retriever.Retrieve(context.Background(), "任意问题")
	if err != nil {
		t.Fatalf("Retrieve 失败: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("全部低于阈值，期望 0 个结果，实际 %d 个", len(hits))
	}
	if ctx != "" {
		t.Errorf("无结果时上下文应为空字符串，实际: %q", ctx)
	}
}

func TestRetriever_NoMinScore_ReturnsAll(t *testing.T) {
	store := &mockVectorStore{
		results: []SearchResult{
			{Document: Document{Content: "文档A"}, Score: 0.9},
			{Document: Document{Content: "文档B"}, Score: 0.1},
		},
	}
	// 不设置 WithMinScore，默认不过滤
	retriever := NewRetriever(&mockEmbed{}, store, 5)

	_, hits, err := retriever.Retrieve(context.Background(), "问题")
	if err != nil {
		t.Fatalf("Retrieve 失败: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("不设置阈值应返回全部结果，期望 2，实际 %d", len(hits))
	}
}
