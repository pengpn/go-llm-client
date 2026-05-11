package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/pengpn/go-llm-agent/agent"
	"github.com/pengpn/go-llm-agent/client"
	"github.com/pengpn/go-llm-agent/config"
	"github.com/pengpn/go-llm-agent/models"
	"github.com/pengpn/go-llm-agent/rag"
	"github.com/pengpn/go-llm-agent/session"
)

const systemPrompt = `你是一个专业的订单客服助手。

你有一个工具可以使用：
- search_knowledge_base：在知识库中搜索相关内容

处理用户问题时：
1. 先判断是否需要查阅知识库（订单、退款、物流、支付等业务问题需要查）
2. 对于闲聊、感谢等简单交互，直接回答无需查询
3. 检索到内容后，严格基于参考资料回答，不要编造信息
4. 如果知识库中没有答案，直接告知用户"这个问题我需要转接人工客服"`

const sessionPath = "./data/sessions/rag_user.json"

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置加载失败: %v\n", err)
		os.Exit(1)
	}

	// config.Load() 内部已调用 godotenv.Load()，此处可直接读取 .env 里的值
	embedAPIKey := os.Getenv("DASHSCOPE_API_KEY")
	if embedAPIKey == "" {
		fmt.Fprintln(os.Stderr, "错误：请在 .env 中设置 DASHSCOPE_API_KEY")
		os.Exit(1)
	}

	ctx := context.Background()

	// ── 初始化 RAG 组件 ──────────────────────────────
	embedder := rag.NewQwenEmbedder(embedAPIKey)

	store, err := rag.NewQdrantStore(ctx, "http://localhost:6333", "order_faq", 1024)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接 Qdrant 失败（确认 Docker 已启动）: %v\n", err)
		os.Exit(1)
	}

	pipeline := rag.NewPipeline(rag.NewFixedSizeChunker(500, 50), embedder, store)
	retriever := rag.NewRetriever(embedder, store, 3, rag.WithMinScore(0.5))

	// ── 建立知识库索引 ───────────────────────────────
	fmt.Println("正在建立知识库索引...")
	for _, faq := range orderFAQ {
		text := faq.Title + "\n" + faq.Content
		if err := pipeline.IndexText(ctx, text, faq.Title); err != nil {
			fmt.Fprintf(os.Stderr, "索引 [%s] 失败: %v\n", faq.Title, err)
			os.Exit(1)
		}
		fmt.Printf("  ✓ %s\n", faq.Title)
	}
	fmt.Println("索引完成。")

	// ── 初始化 LLM Client（Provider 和模型由 config.yaml / .env 控制）──
	llmClient := client.NewFromConfig(&cfg.LLM)

	registry := agent.NewRegistry()
	registry.Register(rag.NewSearchKBTool(retriever))

	ag := agent.New(llmClient, registry)

	// ── Session 管理 ─────────────────────────────────
	strategy := &session.ByTurns{MaxTurns: 20}
	sess, err := session.LoadSession(sessionPath, strategy)
	if err != nil {
		sess = session.NewSession("rag_user", systemPrompt, strategy)
		fmt.Println("[新会话]")
	} else {
		fmt.Printf("[已恢复上次会话，共 %d 条历史消息]\n", sess.MessageCount())
	}

	fmt.Println("=== 订单知识库客服（RAG Agent）===")
	fmt.Printf("模型：%s | 输入 exit 退出\n\n", cfg.LLM.Model)

	// ── 对话循环 ─────────────────────────────────────
	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("你：")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "exit" {
			break
		}

		sess.AddUserMessage(input)

		msgs := sess.Messages()
		initialLen := len(msgs)

		answer, history, err := ag.Run(ctx, msgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Agent 执行失败: %v\n", err)
			continue
		}

		applyHistoryToSession(sess, history, initialLen)

		if err := sess.Save(sessionPath); err != nil {
			fmt.Fprintf(os.Stderr, "  [警告] Session 保存失败: %v\n", err)
		}

		// 显示工具调用情况（调试用）
		for _, msg := range history[initialLen:] {
			if msg.Role == models.RoleAssistant && len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					fmt.Printf("  [检索] query=%s\n", extractQuery(tc.Function.Arguments))
				}
			}
		}

		fmt.Printf("客服：%s\n\n", answer)
	}

	fmt.Println("再见！")
}

// applyHistoryToSession 将 Agent Run 返回的新增消息写入 Session。
// assistant+tool 消息必须原子写入，保证历史一致性。
func applyHistoryToSession(sess *session.Session, history []models.Message, initialLen int) {
	newMsgs := history[initialLen:]
	i := 0
	for i < len(newMsgs) {
		msg := newMsgs[i]
		if msg.Role == models.RoleAssistant && len(msg.ToolCalls) > 0 {
			j := i + 1
			for j < len(newMsgs) && newMsgs[j].Role == models.RoleTool {
				j++
			}
			sess.AddAgentTurn(msg, newMsgs[i+1:j])
			i = j
		} else if msg.Role == models.RoleAssistant {
			sess.AddAssistantMessage(msg.Content)
			i++
		} else {
			i++
		}
	}
}

// extractQuery 从 JSON 参数中提取 query 值，仅用于日志展示。
func extractQuery(args string) string {
	const key = `"query":"`
	idx := strings.Index(args, key)
	if idx == -1 {
		return args
	}
	start := idx + len(key)
	end := strings.Index(args[start:], `"`)
	if end == -1 {
		return args[start:]
	}
	return args[start : start+end]
}
