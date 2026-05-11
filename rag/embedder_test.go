package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// makeEmbedServer 启动一个 mock HTTP 服务器，返回指定的 embedding 响应体。
// 测试结束时自动关闭（通过 t.Cleanup 注册）。
func makeEmbedServer(t *testing.T, responseBody any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(responseBody)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTestEmbedder 创建指向 mock server 的 QwenEmbedder，不需要真实 API Key。
func newTestEmbedder(serverURL string) *QwenEmbedder {
	return NewQwenEmbedder("test-key", WithEmbedBaseURL(serverURL))
}

// ── 空输入 ─────────────────────────────────────────────────────────────────

func TestQwenEmbedder_EmptyInput_ReturnsNil(t *testing.T) {
	// 空输入不应发起 HTTP 请求，直接返回 nil
	e := NewQwenEmbedder("test-key")
	result, err := e.Embed(context.Background(), nil)
	if err != nil {
		t.Fatalf("空输入不应返回错误: %v", err)
	}
	if result != nil {
		t.Errorf("空输入应返回 nil，实际: %v", result)
	}
}

func TestQwenEmbedder_EmptySlice_ReturnsNil(t *testing.T) {
	e := NewQwenEmbedder("test-key")
	result, err := e.Embed(context.Background(), []string{})
	if err != nil {
		t.Fatalf("空 slice 不应返回错误: %v", err)
	}
	if result != nil {
		t.Errorf("空 slice 应返回 nil，实际: %v", result)
	}
}

// ── 正常响应 ────────────────────────────────────────────────────────────────

func TestQwenEmbedder_SingleInput(t *testing.T) {
	srv := makeEmbedServer(t, embeddingResponse{
		Data: []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}{
			{Index: 0, Embedding: []float32{0.1, 0.2, 0.3}},
		},
	})

	e := newTestEmbedder(srv.URL)
	result, err := e.Embed(context.Background(), []string{"你好"})
	if err != nil {
		t.Fatalf("Embed 失败: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("期望 1 个向量，实际 %d 个", len(result))
	}
	if len(result[0]) != 3 {
		t.Errorf("向量维度应为 3，实际 %d", len(result[0]))
	}
}

// ── 批量顺序保证（核心测试）─────────────────────────────────────────────────

func TestQwenEmbedder_BatchOrder_ResponseOutOfOrder(t *testing.T) {
	// API 返回 index=1 在前，index=0 在后（乱序）
	// Embed 必须按输入顺序返回：result[0] ↔ texts[0]，result[1] ↔ texts[1]
	vec0 := []float32{1.0, 0.0, 0.0}
	vec1 := []float32{0.0, 1.0, 0.0}

	srv := makeEmbedServer(t, embeddingResponse{
		Data: []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}{
			{Index: 1, Embedding: vec1}, // 故意把 index=1 放在前面
			{Index: 0, Embedding: vec0},
		},
	})

	e := newTestEmbedder(srv.URL)
	result, err := e.Embed(context.Background(), []string{"第一句", "第二句"})
	if err != nil {
		t.Fatalf("Embed 失败: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("期望 2 个向量，实际 %d 个", len(result))
	}

	// result[0] 应是 vec0（对应 texts[0]）
	if result[0][0] != 1.0 || result[0][1] != 0.0 {
		t.Errorf("result[0] 应对应 texts[0] 的向量 %v，实际 %v", vec0, result[0])
	}
	// result[1] 应是 vec1（对应 texts[1]）
	if result[1][0] != 0.0 || result[1][1] != 1.0 {
		t.Errorf("result[1] 应对应 texts[1] 的向量 %v，实际 %v", vec1, result[1])
	}
}

// ── API 错误处理 ────────────────────────────────────────────────────────────

func TestQwenEmbedder_APIError_ReturnsError(t *testing.T) {
	srv := makeEmbedServer(t, embeddingResponse{
		Error: &struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		}{
			Message: "Invalid API key",
			Code:    "InvalidApiKey",
		},
	})

	e := newTestEmbedder(srv.URL)
	_, err := e.Embed(context.Background(), []string{"测试"})
	if err == nil {
		t.Fatal("API 返回 error 字段时，应返回错误")
	}
	// 错误信息应包含 code 和 message
	if !containsAll(err.Error(), "InvalidApiKey", "Invalid API key") {
		t.Errorf("错误信息应包含 code 和 message，实际: %v", err)
	}
}

func TestQwenEmbedder_InvalidJSON_ReturnsError(t *testing.T) {
	// 服务器返回非法 JSON
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	t.Cleanup(srv.Close)

	e := newTestEmbedder(srv.URL)
	_, err := e.Embed(context.Background(), []string{"测试"})
	if err == nil {
		t.Fatal("非法 JSON 应返回解析错误")
	}
}

func TestQwenEmbedder_ServerDown_ReturnsError(t *testing.T) {
	// 指向一个不存在的端口
	e := NewQwenEmbedder("test-key", WithEmbedBaseURL("http://127.0.0.1:19999"))
	_, err := e.Embed(context.Background(), []string{"测试"})
	if err == nil {
		t.Fatal("服务器不可达时应返回错误")
	}
}

// ── 响应 index 越界 ─────────────────────────────────────────────────────────

func TestQwenEmbedder_IndexOutOfRange_ReturnsError(t *testing.T) {
	// 发送 1 个文本，但响应返回 index=5（越界）
	srv := makeEmbedServer(t, embeddingResponse{
		Data: []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}{
			{Index: 5, Embedding: []float32{0.1, 0.2}},
		},
	})

	e := newTestEmbedder(srv.URL)
	_, err := e.Embed(context.Background(), []string{"只有一句"})
	if err == nil {
		t.Fatal("响应 index 超出输入范围时应返回错误")
	}
}

// ── 请求参数验证 ────────────────────────────────────────────────────────────

func TestQwenEmbedder_SendsCorrectRequest(t *testing.T) {
	var receivedBody embeddingRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		// 验证 Authorization header
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("Authorization header 错误: %s", r.Header.Get("Authorization"))
		}
		// 返回合法响应，避免 Embed 报错
		_ = json.NewEncoder(w).Encode(embeddingResponse{
			Data: []struct {
				Embedding []float32 `json:"embedding"`
				Index     int       `json:"index"`
			}{
				{Index: 0, Embedding: []float32{0.1}},
				{Index: 1, Embedding: []float32{0.2}},
			},
		})
	}))
	t.Cleanup(srv.Close)

	e := newTestEmbedder(srv.URL)
	_, _ = e.Embed(context.Background(), []string{"文本A", "文本B"})

	if len(receivedBody.Input) != 2 {
		t.Errorf("请求应包含 2 个文本，实际 %d 个", len(receivedBody.Input))
	}
	if receivedBody.Input[0] != "文本A" || receivedBody.Input[1] != "文本B" {
		t.Errorf("请求文本顺序错误: %v", receivedBody.Input)
	}
}

// ── 工具函数 ────────────────────────────────────────────────────────────────

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
