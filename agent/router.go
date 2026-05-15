package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/pengpn/go-llm-agent/models"
)

// Intent 表示 Router 识别出的用户意图
type Intent struct {
	Category   string  `json:"category"`   // 意图类别（如 "order"、"logistics"）
	Confidence float64 `json:"confidence"` // 置信度 0.0-1.0，低于阈值走 fallback
	Reason     string  `json:"reason"`     // Router 给出的判断理由
}

// SubAgentFactory 是创建 Sub-Agent 的工厂函数。
// 为什么不直接存 *Agent？
// → 每次对话可能需要独立的 Session 和 RunOption（如不同的 userID），
//   工厂函数让每次路由都能创建干净的 Sub-Agent 实例。
type SubAgentFactory func() *Agent

// Route 绑定一个意图类别到对应的 Sub-Agent
type Route struct {
	Category    string          // 意图类别名（需与 Router prompt 中的名称一致）
	Description string          // 描述，用于 Router prompt
	Factory     SubAgentFactory // 创建该领域 Sub-Agent
}

// Router 是意图识别 + 分发的顶层 Agent。
// 它自身不持有任何工具，只通过 LLM 判断意图后分发给 Sub-Agent。
type Router struct {
	client              LLMClient
	routes              []Route
	fallback            SubAgentFactory // 无法匹配时的兜底 Agent
	confidenceThreshold float64         // 低于此值走 fallback（默认 0.5）
	systemPrompt        string          // 自动生成的路由 System Prompt
}

// RouterOption 路由器的函数式选项
type RouterOption func(*Router)

// WithFallback 设置无法匹配意图时的兜底 Sub-Agent
func WithFallback(factory SubAgentFactory) RouterOption {
	return func(r *Router) { r.fallback = factory }
}

// WithConfidenceThreshold 设置置信度阈值，低于此值自动走 fallback（默认 0.5）
func WithConfidenceThreshold(t float64) RouterOption {
	return func(r *Router) { r.confidenceThreshold = t }
}

// NewRouter 创建路由器。
// 根据 routes 自动生成 System Prompt，告诉 LLM 有哪些类别可选。
func NewRouter(client LLMClient, routes []Route, opts ...RouterOption) *Router {
	r := &Router{
		client:              client,
		routes:              routes,
		confidenceThreshold: 0.5,
	}
	for _, o := range opts {
		o(r)
	}
	r.systemPrompt = buildRouterPrompt(routes)
	return r
}

// Route 识别用户意图并分发给对应的 Sub-Agent 执行
//
// 流程：
//  1. 用 LLM 分析用户消息 → 输出 {"category": "...", "reason": "..."}
//  2. 在 routes 中查找匹配的 Sub-Agent
//  3. 把原始消息转发给 Sub-Agent 执行
//  4. 返回 Sub-Agent 的回答 + 路由信息
func (r *Router) Route(ctx context.Context, messages []models.Message, opts ...RunOption) (RouterResult, error) {
	intent, err := r.classify(ctx, messages)
	if err != nil {
		return RouterResult{}, fmt.Errorf("意图识别失败: %w", err)
	}

	// 置信度低于阈值 → 降级到 fallback
	var factory SubAgentFactory
	if intent.Confidence < r.confidenceThreshold && r.fallback != nil {
		intent.Category = "fallback"
		factory = r.fallback
	} else {
		factory = r.findFactory(intent.Category)
	}
	if factory == nil {
		return RouterResult{}, fmt.Errorf("未找到类别 %q 对应的 Sub-Agent，也没有配置 fallback", intent.Category)
	}

	subAgent := factory()
	answer, history, err := subAgent.Run(ctx, messages, opts...)
	if err != nil {
		return RouterResult{}, fmt.Errorf("Sub-Agent [%s] 执行失败: %w", intent.Category, err)
	}

	return RouterResult{
		Intent:  intent,
		Answer:  answer,
		History: history,
	}, nil
}

// RouterResult 包含路由 + 执行的完整结果
type RouterResult struct {
	Intent  Intent            // Router 识别的意图
	Answer  string            // Sub-Agent 的最终回答
	History []models.Message  // Sub-Agent 的完整对话历史
}

// RouteParallel 支持多意图并行分发。
// 当用户消息涉及多个领域（如"查订单状态和物流"），Router 拆成多个子任务并发执行。
// 单意图时行为与 Route 一致，多意图时并发执行并合并结果。
func (r *Router) RouteParallel(ctx context.Context, messages []models.Message, opts ...RunOption) (ParallelResult, error) {
	intents, err := r.classifyMulti(ctx, messages)
	if err != nil {
		return ParallelResult{}, fmt.Errorf("意图识别失败: %w", err)
	}

	// 去重：同一 category 只分发一次
	intents = deduplicateIntents(intents)

	// 为每个意图分配 factory（置信度过低的降级到 fallback）
	type dispatchItem struct {
		intent  Intent
		factory SubAgentFactory
	}
	var items []dispatchItem
	for _, intent := range intents {
		var factory SubAgentFactory
		if intent.Confidence < r.confidenceThreshold && r.fallback != nil {
			intent.Category = "fallback"
			factory = r.fallback
		} else {
			factory = r.findFactory(intent.Category)
		}
		if factory == nil {
			continue
		}
		items = append(items, dispatchItem{intent: intent, factory: factory})
	}

	if len(items) == 0 {
		return ParallelResult{}, fmt.Errorf("所有意图均无匹配的 Sub-Agent，也没有配置 fallback")
	}

	// 并发执行各 Sub-Agent
	subResults := make([]RouterResult, len(items))
	subErrors := make([]error, len(items))
	var wg sync.WaitGroup

	for i, item := range items {
		wg.Add(1)
		go func(idx int, it dispatchItem) {
			defer wg.Done()
			subAgent := it.factory()
			answer, history, err := subAgent.Run(ctx, messages, opts...)
			subResults[idx] = RouterResult{
				Intent:  it.intent,
				Answer:  answer,
				History: history,
			}
			subErrors[idx] = err
		}(i, item)
	}
	wg.Wait()

	// 合并结果
	var results []RouterResult
	var errMsgs []string
	for i, res := range subResults {
		if subErrors[i] != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("[%s] %v", items[i].intent.Category, subErrors[i]))
			continue
		}
		results = append(results, res)
	}

	merged := mergeAnswers(results)

	pr := ParallelResult{
		Intents:    intents,
		SubResults: results,
		Merged:     merged,
	}

	if len(errMsgs) > 0 {
		return pr, fmt.Errorf("部分 Sub-Agent 执行失败: %s", strings.Join(errMsgs, "; "))
	}
	return pr, nil
}

// ParallelResult 并行分发的结果
type ParallelResult struct {
	Intents    []Intent       // Router 识别出的所有意图
	SubResults []RouterResult // 各 Sub-Agent 的独立结果
	Merged     string         // 合并后的最终回答
}

// mergeAnswers 将多个 Sub-Agent 的回答拼接成一个完整回复
func mergeAnswers(results []RouterResult) string {
	if len(results) == 1 {
		return results[0].Answer
	}
	var sb strings.Builder
	for i, r := range results {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(fmt.Sprintf("【%s】%s", r.Intent.Category, r.Answer))
	}
	return sb.String()
}

// deduplicateIntents 按 category 去重，保留第一个
func deduplicateIntents(intents []Intent) []Intent {
	seen := make(map[string]bool)
	result := make([]Intent, 0, len(intents))
	for _, intent := range intents {
		cat := strings.ToLower(strings.TrimSpace(intent.Category))
		if seen[cat] {
			continue
		}
		seen[cat] = true
		result = append(result, intent)
	}
	return result
}

// classifyMulti 调用 LLM 识别用户意图（支持多意图）
func (r *Router) classifyMulti(ctx context.Context, messages []models.Message) ([]Intent, error) {
	classifyMsgs := make([]models.Message, 0, len(messages)+1)
	classifyMsgs = append(classifyMsgs, models.Message{
		Role:    models.RoleSystem,
		Content: r.multiIntentPrompt(),
	})

	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == models.RoleUser {
			classifyMsgs = append(classifyMsgs, models.Message{
				Role:    models.RoleUser,
				Content: messages[i].Content,
			})
			break
		}
	}

	resp, err := r.client.ChatWithTools(ctx, classifyMsgs, nil)
	if err != nil {
		return nil, fmt.Errorf("LLM 请求失败: %w", err)
	}

	return parseIntents(resp.Content)
}

// multiIntentPrompt 生成支持多意图识别的 Prompt
func (r *Router) multiIntentPrompt() string {
	var sb strings.Builder
	sb.WriteString(`你是一个意图分类器。用户消息可能包含一个或多个意图，请识别所有意图。只输出 JSON，不要其他文字。

可选类别：
`)
	for _, route := range r.routes {
		sb.WriteString(fmt.Sprintf("- %s：%s\n", route.Category, route.Description))
	}
	sb.WriteString(`
输出格式（只输出 JSON 数组）：
[{"category": "<类别名>", "confidence": <0.0-1.0>, "reason": "<10字以内>"}]

规则：
- 单意图返回只有一个元素的数组
- 多意图返回多个元素（如用户同时问订单和物流）
- 每个意图独立给出 confidence
`)
	return sb.String()
}

// parseIntents 解析 LLM 返回的意图数组（兼容单对象和数组两种格式）
func parseIntents(content string) ([]Intent, error) {
	content = strings.TrimSpace(content)

	// 剥离 markdown 包裹：找到第一个 [ 或 {，截取到最后一个 ] 或 }
	if idx := strings.Index(content, "["); idx >= 0 {
		end := strings.LastIndex(content, "]")
		if end > idx {
			content = content[idx : end+1]
		}
	} else if idx := strings.Index(content, "{"); idx >= 0 {
		end := strings.LastIndex(content, "}")
		if end > idx {
			content = content[idx : end+1]
		}
	}

	// 尝试解析为数组
	var intents []Intent
	if err := json.Unmarshal([]byte(content), &intents); err == nil {
		return validateIntents(intents)
	}

	// 降级：尝试解析为单个对象
	var single Intent
	if err := json.Unmarshal([]byte(content), &single); err != nil {
		return nil, fmt.Errorf("解析意图 JSON 失败: %w, 原文: %s", err, content)
	}
	return validateIntents([]Intent{single})
}

func validateIntents(intents []Intent) ([]Intent, error) {
	if len(intents) == 0 {
		return nil, fmt.Errorf("意图列表为空")
	}
	for i, intent := range intents {
		if intent.Category == "" {
			return nil, fmt.Errorf("第 %d 个意图类别为空", i+1)
		}
	}
	return intents, nil
}

// classify 调用 LLM 识别用户意图
func (r *Router) classify(ctx context.Context, messages []models.Message) (Intent, error) {
	classifyMsgs := make([]models.Message, 0, len(messages)+1)
	classifyMsgs = append(classifyMsgs, models.Message{
		Role:    models.RoleSystem,
		Content: r.systemPrompt,
	})

	// 只取最后一条用户消息用于分类（避免历史消息干扰意图判断）
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == models.RoleUser {
			classifyMsgs = append(classifyMsgs, models.Message{
				Role:    models.RoleUser,
				Content: messages[i].Content,
			})
			break
		}
	}

	resp, err := r.client.ChatWithTools(ctx, classifyMsgs, nil)
	if err != nil {
		return Intent{}, fmt.Errorf("LLM 请求失败: %w", err)
	}

	return parseIntent(resp.Content)
}

// findFactory 根据 category 名称查找对应的工厂函数
func (r *Router) findFactory(category string) SubAgentFactory {
	cat := strings.ToLower(strings.TrimSpace(category))
	for _, route := range r.routes {
		if strings.ToLower(route.Category) == cat {
			return route.Factory
		}
	}
	return r.fallback
}

// buildRouterPrompt 根据路由配置自动生成 Router 的 System Prompt
func buildRouterPrompt(routes []Route) string {
	var sb strings.Builder
	sb.WriteString(`你是一个意图分类器。根据用户消息判断属于以下哪个类别，只输出 JSON，不要其他文字。

可选类别：
`)
	for _, route := range routes {
		sb.WriteString(fmt.Sprintf("- %s：%s\n", route.Category, route.Description))
	}
	sb.WriteString(`
输出格式（只输出 JSON）：
{"category": "<类别名>", "confidence": <0.0-1.0的置信度>, "reason": "<10字以内的判断理由>"}

confidence 说明：
- 1.0 = 非常确定属于该类别
- 0.5-0.9 = 较为确定
- 0.0-0.5 = 不确定，可能不属于任何类别
`)
	return sb.String()
}

// parseIntent 从 LLM 响应中提取意图 JSON
func parseIntent(content string) (Intent, error) {
	content = strings.TrimSpace(content)

	// 剥离 markdown 包裹
	if idx := strings.Index(content, "{"); idx > 0 {
		content = content[idx:]
	}
	if idx := strings.LastIndex(content, "}"); idx >= 0 && idx < len(content)-1 {
		content = content[:idx+1]
	}

	var intent Intent
	if err := json.Unmarshal([]byte(content), &intent); err != nil {
		return Intent{}, fmt.Errorf("解析意图 JSON 失败: %w, 原文: %s", err, content)
	}

	if intent.Category == "" {
		return Intent{}, fmt.Errorf("意图类别为空, 原文: %s", content)
	}

	return intent, nil
}
