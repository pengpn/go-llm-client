package client

import (
	"fmt"
	"sync"

	"github.com/pengpn/go-llm-agent/models"
)

// modelPricing 定义每个模型的 Token 单价（美元/百万 token）。
// 数据来源：各 Provider 官方定价页面（2024 年价格）。
var modelPricing = map[string][2]float64{
	// [输入价格, 输出价格] 单位：USD/1M tokens
	"gpt-4o":              {5.00, 15.00},
	"gpt-4o-mini":         {0.15, 0.60},
	"gpt-4-turbo":         {10.00, 30.00},
	"gpt-3.5-turbo":       {0.50, 1.50},
	"glm-4":               {0.10, 0.10},  // 智谱 GLM-4
	"glm-4-flash":         {0.01, 0.01},  // 智谱 GLM-4-Flash（免费）
	"qwen-turbo":          {0.30, 0.60},  // 通义千问 Turbo
	"qwen-plus":           {0.80, 2.00},  // 通义千问 Plus
}

// CostCalculator 累计记录所有请求的 Token 消耗和费用。
// 并发安全：内部使用 Mutex 保护共享状态。
type CostCalculator struct {
	mu              sync.Mutex
	totalPrompt     int
	totalCompletion int
	totalCostUSD    float64
	requestCount    int
}

// NewCostCalculator 创建一个新的费用计算器。
func NewCostCalculator() *CostCalculator {
	return &CostCalculator{}
}

// Add 累加一次请求的 Token 消耗。
// 模型名不在定价表中时，只计 Token 数，费用记为 0。
func (c *CostCalculator) Add(model string, usage models.Usage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalPrompt += usage.PromptTokens
	c.totalCompletion += usage.CompletionTokens
	c.requestCount++

	if pricing, ok := modelPricing[model]; ok {
		inputCost := float64(usage.PromptTokens) / 1_000_000 * pricing[0]
		outputCost := float64(usage.CompletionTokens) / 1_000_000 * pricing[1]
		c.totalCostUSD += inputCost + outputCost
	}
}

// Summary 返回人类可读的费用摘要。
func (c *CostCalculator) Summary() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return fmt.Sprintf(
		"请求次数: %d | 输入 Token: %d | 输出 Token: %d | 预估费用: $%.6f",
		c.requestCount,
		c.totalPrompt,
		c.totalCompletion,
		c.totalCostUSD,
	)
}

// TotalCostUSD 返回累计费用（美元）。
func (c *CostCalculator) TotalCostUSD() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalCostUSD
}

// Reset 重置所有计数器。
func (c *CostCalculator) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalPrompt = 0
	c.totalCompletion = 0
	c.totalCostUSD = 0
	c.requestCount = 0
}
