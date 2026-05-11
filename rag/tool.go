package rag

import (
	"context"
	"fmt"

	"github.com/pengpn/go-llm-agent/agent"
	"github.com/pengpn/go-llm-agent/models"
)

// searchKBReq 是 search_knowledge_base 工具的输入参数。
type searchKBReq struct {
	Query string `json:"query"`
}

func (r *searchKBReq) Validate() error {
	if r.Query == "" {
		return fmt.Errorf("query 不能为空")
	}
	return nil
}

// NewSearchKBTool 将 Retriever 包装为 Agent Tool。
//
// LLM 自主决定何时调用：只有判断需要查阅知识库时才调用，
// 对于闲聊、已知内容等无需检索的场景 LLM 会直接回答。
// 这比"每次都注入上下文"更节省 Token，更符合 ReAct 模式。
func NewSearchKBTool(retriever *Retriever) *agent.Tool {
	return agent.NewTypedTool(
		"search_knowledge_base",
		"在知识库中搜索与用户问题相关的内容。当用户询问订单、退款、物流、支付等业务问题时调用。如果知识库中没有相关内容，工具会明确告知。",
		models.ToolParameters{
			Type: "object",
			Properties: map[string]models.ToolProperty{
				"query": {
					Type:        "string",
					Description: "要搜索的问题或关键词，应尽量贴近用户的原始表达",
				},
			},
			Required: []string{"query"},
		},
		func(ctx context.Context, req searchKBReq) (string, error) {
			context, hits, err := retriever.Retrieve(ctx, req.Query)
			if err != nil {
				return "", fmt.Errorf("知识库检索失败: %w", err)
			}
			if len(hits) == 0 {
				return "知识库中未找到与该问题相关的内容。", nil
			}
			return context, nil
		},
	)
}
