// Package client 提供生产级 LLM HTTP Client。
package client

import (
	"time"

	"github.com/pengpn/go-llm-agent/config"
)

// options 保存 Client 的所有配置项。
// 使用私有结构体，外部只能通过 With* 函数修改，避免直接操作字段。
type options struct {
	baseURL     string
	apiKey      string
	model       string
	temperature float64
	maxTokens   int
	timeout     time.Duration
	maxRetries  int
	maxConcurrent int // 最大并发请求数，防触发 Rate Limit
}

// defaultOptions 返回合理的默认配置。
func defaultOptions() options {
	return options{
		baseURL:       "https://api.openai.com/v1",
		model:         "gpt-4o-mini",
		temperature:   0.7,
		maxTokens:     2048,
		timeout:       60 * time.Second,
		maxRetries:    3,
		maxConcurrent: 10,
	}
}

// Option 是函数式选项的类型别名。
// 这个模式的好处：新增配置项不会破坏现有调用方的代码。
type Option func(*options)

// WithBaseURL 设置 API 基础地址。切换 Provider 只需改这一个值。
func WithBaseURL(url string) Option {
	return func(o *options) {
		o.baseURL = url
	}
}

// WithAPIKey 设置鉴权密钥。
func WithAPIKey(key string) Option {
	return func(o *options) {
		o.apiKey = key
	}
}

// WithModel 设置使用的模型名称。
func WithModel(model string) Option {
	return func(o *options) {
		o.model = model
	}
}

// WithTemperature 设置生成温度（0.0 ~ 2.0）。
// 越低越确定，越高越随机。客服场景通常用 0.3~0.5。
func WithTemperature(t float64) Option {
	return func(o *options) {
		o.temperature = t
	}
}

// WithMaxTokens 设置单次响应最大 Token 数。
func WithMaxTokens(n int) Option {
	return func(o *options) {
		o.maxTokens = n
	}
}

// WithTimeout 设置单次 HTTP 请求超时时间。
func WithTimeout(d time.Duration) Option {
	return func(o *options) {
		o.timeout = d
	}
}

// WithMaxRetries 设置最大重试次数（默认 3）。
func WithMaxRetries(n int) Option {
	return func(o *options) {
		o.maxRetries = n
	}
}

// WithMaxConcurrent 设置最大并发请求数（默认 10）。
func WithMaxConcurrent(n int) Option {
	return func(o *options) {
		o.maxConcurrent = n
	}
}

// ---- Provider 预设 ----
// 使用预设只需传入对应的 With 函数，内部配置对调用方透明。

// WithOpenAI 使用 OpenAI 官方端点。
func WithOpenAI(apiKey, model string) Option {
	return func(o *options) {
		o.baseURL = "https://api.openai.com/v1"
		o.apiKey = apiKey
		o.model = model
	}
}

// WithZhipuAI 使用智谱 GLM（OpenAI 兼容协议）。
func WithZhipuAI(apiKey, model string) Option {
	return func(o *options) {
		o.baseURL = "https://open.bigmodel.cn/api/paas/v4"
		o.apiKey = apiKey
		o.model = model
	}
}

// WithTongyi 使用阿里通义千问（OpenAI 兼容协议）。
func WithTongyi(apiKey, model string) Option {
	return func(o *options) {
		o.baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
		o.apiKey = apiKey
		o.model = model
	}
}

// WithOllama 使用本地 Ollama（无需 API Key）。
func WithOllama(model string) Option {
	return func(o *options) {
		o.baseURL = "http://localhost:11434/v1"
		o.apiKey = "ollama" // Ollama 要求非空，但不校验
		o.model = model
	}
}

// WithDeepSeek 使用 DeepSeek（OpenAI 兼容协议）。
// 常用模型：deepseek-chat（性价比高）、deepseek-reasoner（深度推理）
func WithDeepSeek(apiKey, model string) Option {
	return func(o *options) {
		o.baseURL = "https://api.deepseek.com/v1"
		o.apiKey = apiKey
		o.model = model
	}
}

// ---- 从 Config 构建 ----

// providerBaseURLs 记录各 Provider 的默认端点。
var providerBaseURLs = map[string]string{
	"openai":   "https://api.openai.com/v1",
	"zhipu":    "https://open.bigmodel.cn/api/paas/v4",
	"tongyi":   "https://dashscope.aliyuncs.com/compatible-mode/v1",
	"ollama":   "http://localhost:11434/v1",
	"deepseek": "https://api.deepseek.com/v1",
}

// NewFromConfig 根据 Config 创建 Client。
// 这是生产代码推荐的创建方式，避免在业务代码中散落硬编码值。
func NewFromConfig(cfg *config.LLMConfig) *Client {
	// 确定 BaseURL：自定义 > Provider 默认
	baseURL := cfg.BaseURL
	if baseURL == "" {
		if url, ok := providerBaseURLs[cfg.Provider]; ok {
			baseURL = url
		} else {
			baseURL = providerBaseURLs["openai"] // 未知 Provider 降级到 OpenAI
		}
	}

	// Ollama 不需要真实 API Key
	apiKey := cfg.APIKey
	if cfg.Provider == "ollama" && apiKey == "" {
		apiKey = "ollama"
	}

	return New(
		WithBaseURL(baseURL),
		WithAPIKey(apiKey),
		WithModel(cfg.Model),
		WithTemperature(cfg.Temperature),
		WithMaxTokens(cfg.MaxTokens),
		WithTimeout(cfg.Timeout),
		WithMaxRetries(cfg.MaxRetries),
		WithMaxConcurrent(cfg.MaxConcurrent),
	)
}
