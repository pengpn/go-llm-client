package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pengpn/go-llm-agent/agent"
	"github.com/pengpn/go-llm-agent/models"
	"github.com/pengpn/go-llm-agent/session"
)

// mockAgent 实现 AgentRunner 接口，用于测试时替代真实 LLM 调用。
type mockAgent struct {
	answer string
	err    error
}

func (m *mockAgent) Run(_ context.Context, msgs []models.Message, _ ...agent.RunOption) (string, []models.Message, error) {
	if m.err != nil {
		return "", nil, m.err
	}
	// 把 assistant 回复追加到历史，模拟真实 Agent 行为
	history := append(msgs, models.Message{Role: models.RoleAssistant, Content: m.answer})
	return m.answer, history, nil
}

// mockIndexer 实现 KBIndexer 接口，记录索引调用次数。
type mockIndexer struct {
	err     error
	indexed int
}

func (m *mockIndexer) IndexText(_ context.Context, _, _ string) error {
	if m.err != nil {
		return m.err
	}
	m.indexed++
	return nil
}

// newTestServer 创建用于测试的 Server，使用 mock 依赖。
func newTestServer(ag AgentRunner, indexer KBIndexer, docs []FAQDoc, opts ...ServerOption) *Server {
	sessions := session.NewManager()
	return New(ag, sessions, indexer, docs, opts...)
}

// postJSON 发送 POST JSON 请求，返回 ResponseRecorder。
func postJSON(srv *Server, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(w, req)
	return w
}

// getRequest 发送 GET 请求，返回 ResponseRecorder。
func getRequest(srv *Server, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	srv.ServeHTTP(w, req)
	return w
}

// --- 作业1：handleHealth 测试 ---

func TestHandleHealth_ReturnsRequiredFields(t *testing.T) {
	srv := newTestServer(&mockAgent{answer: "ok"}, &mockIndexer{}, nil)

	w := getRequest(srv, "/health")

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d", w.Code)
	}

	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)

	if body["status"] != "ok" {
		t.Errorf("期望 status=ok，得到 %v", body["status"])
	}
	if _, ok := body["uptime"]; !ok {
		t.Error("响应缺少 uptime 字段")
	}
	if _, ok := body["sessions"]; !ok {
		t.Error("响应缺少 sessions 字段")
	}
}

// --- 作业1：handleChat 测试 ---

func TestHandleChat_Success(t *testing.T) {
	ag := &mockAgent{answer: "退款需要3-5个工作日"}
	srv := newTestServer(ag, &mockIndexer{}, nil)

	w := postJSON(srv, "/chat", `{"user_id":"u001","message":"退款要多久？"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d，body: %s", w.Code, w.Body.String())
	}

	var resp ChatResponse
	json.NewDecoder(w.Body).Decode(&resp)

	if resp.Answer != ag.answer {
		t.Errorf("期望 answer=%q，得到 %q", ag.answer, resp.Answer)
	}
	if resp.UserID != "u001" {
		t.Errorf("期望 user_id=u001，得到 %q", resp.UserID)
	}
}

func TestHandleChat_MissingUserID(t *testing.T) {
	srv := newTestServer(&mockAgent{}, &mockIndexer{}, nil)

	// 缺少 user_id 字段
	w := postJSON(srv, "/chat", `{"message":"退款要多久？"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，得到 %d", w.Code)
	}
}

func TestHandleChat_MissingMessage(t *testing.T) {
	srv := newTestServer(&mockAgent{}, &mockIndexer{}, nil)

	// 缺少 message 字段
	w := postJSON(srv, "/chat", `{"user_id":"u001"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，得到 %d", w.Code)
	}
}

func TestHandleChat_AgentError(t *testing.T) {
	ag := &mockAgent{err: errors.New("LLM 超时")}
	srv := newTestServer(ag, &mockIndexer{}, nil)

	w := postJSON(srv, "/chat", `{"user_id":"u001","message":"你好"}`)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("期望 500，得到 %d", w.Code)
	}
}

// --- 作业1：handleReload 测试 ---

func TestHandleReload_Success(t *testing.T) {
	indexer := &mockIndexer{}
	docs := []FAQDoc{
		{Title: "退款", Content: "3-5个工作日"},
		{Title: "物流", Content: "3-7天"},
	}
	srv := newTestServer(&mockAgent{}, indexer, docs)

	w := postJSON(srv, "/reload", "")

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d，body: %s", w.Code, w.Body.String())
	}

	var body map[string]any
	json.NewDecoder(w.Body).Decode(&body)

	if body["status"] != "ok" {
		t.Errorf("期望 status=ok，得到 %v", body["status"])
	}
	// JSON number → float64
	if int(body["indexed"].(float64)) != len(docs) {
		t.Errorf("期望 indexed=%d，得到 %v", len(docs), body["indexed"])
	}
	if indexer.indexed != len(docs) {
		t.Errorf("期望实际索引 %d 次，执行了 %d 次", len(docs), indexer.indexed)
	}
}

func TestHandleReload_IndexerError(t *testing.T) {
	indexer := &mockIndexer{err: errors.New("Qdrant 不可达")}
	docs := []FAQDoc{{Title: "退款", Content: "..."}}
	srv := newTestServer(&mockAgent{}, indexer, docs)

	w := postJSON(srv, "/reload", "")

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("期望 500，得到 %d", w.Code)
	}
}

// --- 作业2：handleHistory 测试 ---

func TestHandleHistory_UserNotFound(t *testing.T) {
	srv := newTestServer(&mockAgent{}, &mockIndexer{}, nil)

	w := getRequest(srv, "/history/unknown_user")

	if w.Code != http.StatusNotFound {
		t.Fatalf("期望 404，得到 %d", w.Code)
	}
}

func TestHandleHistory_WithMessages(t *testing.T) {
	ag := &mockAgent{answer: "退款需要3-5个工作日"}
	srv := newTestServer(ag, &mockIndexer{}, nil)

	// 先发消息建立 session
	postJSON(srv, "/chat", `{"user_id":"u002","message":"退款要多久？"}`)

	// 查历史
	w := getRequest(srv, "/history/u002")

	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d，body: %s", w.Code, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	if resp["user_id"] != "u002" {
		t.Errorf("期望 user_id=u002，得到 %v", resp["user_id"])
	}

	msgs, ok := resp["messages"].([]any)
	if !ok {
		t.Fatal("响应缺少 messages 字段")
	}
	// 至少有用户消息 + assistant 回复
	if len(msgs) < 2 {
		t.Errorf("期望至少2条消息，得到 %d", len(msgs))
	}

	// 第一条应该是 user 角色
	firstMsg := msgs[0].(map[string]any)
	if firstMsg["role"] != "user" {
		t.Errorf("第一条消息期望 role=user，得到 %v", firstMsg["role"])
	}
}

func TestHandleHistory_NoSystemPrompt(t *testing.T) {
	ag := &mockAgent{answer: "你好"}
	srv := newTestServer(ag, &mockIndexer{}, nil)

	postJSON(srv, "/chat", `{"user_id":"u003","message":"你好"}`)

	w := getRequest(srv, "/history/u003")
	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)

	msgs := resp["messages"].([]any)
	// 历史中不应含 system 消息
	for _, m := range msgs {
		msg := m.(map[string]any)
		if msg["role"] == "system" {
			t.Error("历史消息中不应包含 system prompt")
		}
	}
}

// --- 作业1（Lesson 07）：handleChatStream 测试 ---

// mockStreamingAgent 实现 AgentRunner + StreamingAgentRunner 双接口。
type mockStreamingAgent struct {
	mockAgent
	tokens []string // 模拟流式输出的 token 序列
}

func (m *mockStreamingAgent) RunStream(_ context.Context, msgs []models.Message, tokenCh chan<- string, _ ...agent.RunOption) (string, []models.Message, error) {
	defer close(tokenCh)
	var full strings.Builder
	for _, t := range m.tokens {
		tokenCh <- t
		full.WriteString(t)
	}
	history := append(msgs, models.Message{Role: models.RoleAssistant, Content: full.String()})
	return full.String(), history, nil
}

// newRealServer 创建绑定在真实 TCP 端口上的测试服务器。
// 用于需要 http.CloseNotifier（如 c.Stream SSE）的测试场景。
func newRealServer(t *testing.T, ag AgentRunner, indexer KBIndexer, docs []FAQDoc, opts ...ServerOption) *httptest.Server {
	t.Helper()
	srv := newTestServer(ag, indexer, docs, opts...)
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)
	return ts
}

func TestHandleChatStream_SSEEvents(t *testing.T) {
	ag := &mockStreamingAgent{tokens: []string{"退款", "需要", "3-5天"}}
	ts := newRealServer(t, ag, &mockIndexer{}, nil)

	resp, err := http.Post(ts.URL+"/chat/stream", "application/json",
		bytes.NewBufferString(`{"user_id":"s001","message":"退款要多久？"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("期望 200，得到 %d", resp.StatusCode)
	}

	// 读取全部 SSE 响应
	bodyBytes := make([]byte, 4096)
	n, _ := resp.Body.Read(bodyBytes)
	body := string(bodyBytes[:n])

	// 验证有 token 事件（Gin SSE 格式：event:token，无空格）
	if !strings.Contains(body, "event:token") {
		t.Errorf("响应中缺少 SSE token 事件，body: %s", body)
	}
	// 验证所有 token 都在响应中
	for _, tok := range ag.tokens {
		if !strings.Contains(body, tok) {
			t.Errorf("响应中缺少 token %q，body: %s", tok, body)
		}
	}
	// 验证有 done 事件
	if !strings.Contains(body, "event:done") {
		t.Errorf("响应中缺少 SSE done 事件，body: %s", body)
	}
}

func TestHandleChatStream_FallbackWhenNoStreaming(t *testing.T) {
	// 普通 mockAgent 不实现 StreamingAgentRunner，应降级为非流式
	ag := &mockAgent{answer: "直接回答"}
	ts := newRealServer(t, ag, &mockIndexer{}, nil)

	resp, err := http.Post(ts.URL+"/chat/stream", "application/json",
		bytes.NewBufferString(`{"user_id":"s002","message":"你好"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("降级应返回 200，得到 %d", resp.StatusCode)
	}

	bodyBytes := make([]byte, 4096)
	n, _ := resp.Body.Read(bodyBytes)
	body := string(bodyBytes[:n])

	// 降级后响应是 JSON，不是 SSE
	if strings.Contains(body, "event:") {
		t.Errorf("降级后不应有 SSE 格式，body: %s", body)
	}
	var chatResp ChatResponse
	if err := json.Unmarshal(bodyBytes[:n], &chatResp); err != nil {
		t.Errorf("降级后应返回 JSON，解析失败: %v，body: %s", err, body)
	}
}

func TestHandleChatStream_MissingFields(t *testing.T) {
	ag := &mockStreamingAgent{}
	srv := newTestServer(ag, &mockIndexer{}, nil)

	w := postJSON(srv, "/chat/stream", `{"message":"缺少user_id"}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，得到 %d", w.Code)
	}
}

// --- 作业3：RateLimiter 测试 ---

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)

	// 前3次允许
	for i := 0; i < 3; i++ {
		if !rl.Allow("user-x") {
			t.Fatalf("第%d次请求应允许", i+1)
		}
	}
	// 第4次超限
	if rl.Allow("user-x") {
		t.Error("第4次请求应被拒绝")
	}
}

func TestRateLimiter_DifferentUsersDontInterfere(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)

	if !rl.Allow("user-a") {
		t.Error("user-a 第1次应允许")
	}
	// user-a 已满额，但 user-b 不受影响
	if !rl.Allow("user-b") {
		t.Error("user-b 不应受 user-a 限额影响")
	}
}

func TestHandleChat_RateLimited(t *testing.T) {
	ag := &mockAgent{answer: "ok"}
	srv := newTestServer(ag, &mockIndexer{}, nil,
		WithRateLimiter(2, time.Minute),
	)

	sendChat := func(userID string) int {
		w := postJSON(srv, "/chat", `{"user_id":"`+userID+`","message":"hi"}`)
		return w.Code
	}

	// 第1次、第2次：允许
	if code := sendChat("rl-user"); code != http.StatusOK {
		t.Fatalf("第1次期望 200，得到 %d", code)
	}
	if code := sendChat("rl-user"); code != http.StatusOK {
		t.Fatalf("第2次期望 200，得到 %d", code)
	}
	// 第3次：超限
	if code := sendChat("rl-user"); code != http.StatusTooManyRequests {
		t.Fatalf("第3次期望 429，得到 %d", code)
	}
	// 不同用户不受影响
	if code := sendChat("other-user"); code != http.StatusOK {
		t.Fatalf("other-user 期望 200，得到 %d", code)
	}
}
