package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pengpn/go-llm-agent/models"
)

// Intent 表示 Router 识别出的用户意图
type Intent struct {
	Category string `json:"category"` // 意图类别（如 "order"、"logistics"）
	Reason   string `json:"reason"`   // Router 给出的判断理由
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
	client       LLMClient
	routes       []Route
	fallback     SubAgentFactory // 无法匹配时的兜底 Agent
	systemPrompt string          // 自动生成的路由 System Prompt
}

// RouterOption 路由器的函数式选项
type RouterOption func(*Router)

// WithFallback 设置无法匹配意图时的兜底 Sub-Agent
func WithFallback(factory SubAgentFactory) RouterOption {
	return func(r *Router) { r.fallback = factory }
}

// NewRouter 创建路由器。
// 根据 routes 自动生成 System Prompt，告诉 LLM 有哪些类别可选。
func NewRouter(client LLMClient, routes []Route, opts ...RouterOption) *Router {
	r := &Router{
		client: client,
		routes: routes,
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

	factory := r.findFactory(intent.Category)
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
{"category": "<类别名>", "reason": "<10字以内的判断理由>"}
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
