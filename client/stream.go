package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/pengpn/go-llm-agent/models"
)

// StreamChunk 是流式输出的单个片段。
type StreamChunk struct {
	Content string // 当前片段的文本内容
	Err     error  // 如果非 nil，表示流出错或结束
	Done    bool   // true 表示流正常结束
}

// ChatStream 发送流式对话请求，返回一个 channel。
// 调用方从 channel 读取 StreamChunk 直到 Done=true 或 Err!=nil。
//
// 使用 channel 而不是回调函数的原因：
// - channel 天然支持 context 取消
// - 调用方可以用 for-range 优雅迭代
// - 解耦生产者（HTTP）和消费者（业务逻辑）
func (c *Client) ChatStream(ctx context.Context, messages []models.Message) (<-chan StreamChunk, error) {
	if err := c.acquire(ctx); err != nil {
		return nil, fmt.Errorf("获取并发令牌失败: %w", err)
	}

	req := models.ChatRequest{
		Model:       c.opts.model,
		Messages:    messages,
		Temperature: c.opts.temperature,
		MaxTokens:   c.opts.maxTokens,
		Stream:      true,
	}

	body, err := json.Marshal(req)
	if err != nil {
		c.release()
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	url := c.opts.baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		c.release()
		return nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.opts.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		c.release()
		return nil, fmt.Errorf("HTTP 请求失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		c.release()
		return nil, &APIError{StatusCode: resp.StatusCode, Body: "流式请求失败"}
	}

	ch := make(chan StreamChunk, 32) // 缓冲 channel，避免消费者慢时阻塞 goroutine

	// 启动 goroutine 异步读取 SSE 流
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		defer c.release()

		c.readSSE(ctx, resp, ch)
	}()

	return ch, nil
}

// readSSE 解析 SSE（Server-Sent Events）格式的流式响应。
// SSE 格式：每行以 "data: " 开头，以 "[DONE]" 结束。
// 示例：
//
//	data: {"choices":[{"delta":{"content":"你好"}}]}
//	data: [DONE]
func (c *Client) readSSE(ctx context.Context, resp *http.Response, ch chan<- StreamChunk) {
	scanner := bufio.NewScanner(resp.Body)

	for scanner.Scan() {
		// 检查 ctx 是否已取消
		select {
		case <-ctx.Done():
			ch <- StreamChunk{Err: fmt.Errorf("流被取消: %w", ctx.Err())}
			return
		default:
		}

		line := scanner.Text()

		// 跳过空行和注释行
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// 去掉 "data: " 前缀
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		// 流结束标志
		if data == "[DONE]" {
			ch <- StreamChunk{Done: true}
			return
		}

		// 解析 JSON
		var chatResp models.ChatResponse
		if err := json.Unmarshal([]byte(data), &chatResp); err != nil {
			// 解析失败跳过这一帧，不中断整个流
			continue
		}

		if len(chatResp.Choices) == 0 {
			continue
		}

		content := chatResp.Choices[0].Delta.Content
		if content != "" {
			ch <- StreamChunk{Content: content}
		}
	}

	if err := scanner.Err(); err != nil {
		ch <- StreamChunk{Err: fmt.Errorf("读取 SSE 流失败: %w", err)}
	}
}
