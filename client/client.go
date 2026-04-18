package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/pengpn/go-llm-agent/models"
)

// Client 是线程安全的 LLM HTTP Client。
// 内部通过信号量控制并发，通过指数退避处理限流和服务错误。
type Client struct {
	opts    options
	http    *http.Client
	sem     chan struct{} // 信号量：控制最大并发数
	costCal *CostCalculator
}

// New 创建一个新的 Client。
// 示例：
//
//	c := client.New(
//	    client.WithOpenAI(os.Getenv("OPENAI_API_KEY"), "gpt-4o-mini"),
//	    client.WithMaxConcurrent(5),
//	)
func New(opts ...Option) *Client {
	o := defaultOptions()
	for _, opt := range opts {
		opt(&o)
	}

	return &Client{
		opts: o,
		http: &http.Client{
			Timeout: o.timeout,
		},
		// 信号量用带缓冲的 channel 实现：
		// 获取令牌 = 向 channel 写入；释放令牌 = 从 channel 读取
		sem:     make(chan struct{}, o.maxConcurrent),
		costCal: NewCostCalculator(),
	}
}

// Chat 发送对话请求，返回完整响应。
// 自动处理重试逻辑，对 429/5xx 做指数退避。
func (c *Client) Chat(ctx context.Context, messages []models.Message) (*models.Response, error) {
	return c.ChatWithTools(ctx, messages, nil)
}

// ChatWithTools 发送携带工具定义的对话请求。
// tools 不为空时，LLM 可能返回 FinishReason="tool_calls"，需要执行工具后继续循环。
func (c *Client) ChatWithTools(ctx context.Context, messages []models.Message, tools []models.ToolDefinition) (*models.Response, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, fmt.Errorf("获取并发令牌失败: %w", err)
	}
	defer c.release()

	req := models.ChatRequest{
		Model:       c.opts.model,
		Messages:    messages,
		Tools:       tools,
		Temperature: c.opts.temperature,
		MaxTokens:   c.opts.maxTokens,
	}

	return c.doWithRetry(ctx, req)
}

// doWithRetry 执行带指数退避重试的 HTTP 请求。
// 只有 429（限流）和 5xx（服务错误）才重试，4xx 直接返回错误。
func (c *Client) doWithRetry(ctx context.Context, req models.ChatRequest) (*models.Response, error) {
	var lastErr error

	for attempt := 0; attempt <= c.opts.maxRetries; attempt++ {
		if attempt > 0 {
			// 指数退避：1s, 2s, 4s
			wait := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil, fmt.Errorf("请求被取消: %w", ctx.Err())
			}
		}

		resp, err := c.doRequest(ctx, req)
		if err == nil {
			// 成功，累计费用
			c.costCal.Add(c.opts.model, resp.Usage)
			return resp, nil
		}

		// 判断是否可重试
		if !isRetryable(err) {
			return nil, err
		}

		lastErr = err
	}

	return nil, fmt.Errorf("重试 %d 次后仍失败: %w", c.opts.maxRetries, lastErr)
}

// doRequest 执行单次 HTTP 请求。
func (c *Client) doRequest(ctx context.Context, req models.ChatRequest) (*models.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	url := c.opts.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.opts.apiKey)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	var chatResp models.ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("API 返回空 choices")
	}

	choice := chatResp.Choices[0]
	return &models.Response{
		Content:      choice.Message.Content,
		FinishReason: choice.FinishReason,
		ToolCalls:    choice.Message.ToolCalls,
		Usage:        chatResp.Usage,
		Model:        chatResp.Model,
	}, nil
}

// acquire 获取并发令牌。如果 ctx 已取消则立即返回错误。
func (c *Client) acquire(ctx context.Context) error {
	select {
	case c.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release 释放并发令牌。
func (c *Client) release() {
	<-c.sem
}

// CostSummary 返回当前累计费用摘要。
func (c *Client) CostSummary() string {
	return c.costCal.Summary()
}

// ---- 错误类型 ----

// APIError 表示 LLM API 返回的 HTTP 错误。
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API 错误 %d: %s", e.StatusCode, e.Body)
}

// isRetryable 判断错误是否应该重试。
// 429 = Rate Limit，5xx = 服务器错误，这两类值得重试。
// 4xx 的其他错误（如 401 鉴权失败）重试也没用，直接返回。
func isRetryable(err error) bool {
	apiErr, ok := err.(*APIError)
	if !ok {
		// 网络错误（连接超时等）也重试
		return true
	}
	return apiErr.StatusCode == http.StatusTooManyRequests ||
		apiErr.StatusCode >= http.StatusInternalServerError
}
