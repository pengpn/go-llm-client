package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/pengpn/go-llm-agent/client"
	"github.com/pengpn/go-llm-agent/config"
	"github.com/pengpn/go-llm-agent/models"
	"github.com/pengpn/go-llm-agent/rag"
)

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

	store, err := rag.NewQdrantStore(ctx,
		"http://localhost:6333",
		"order_faq",
		1024, // 与 text-embedding-v3 默认维度一致
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接 Qdrant 失败（确认 Docker 已启动）: %v\n", err)
		os.Exit(1)
	}

	pipeline := rag.NewPipeline(
		rag.NewFixedSizeChunker(500, 50),
		embedder,
		store,
	)
	retriever := rag.NewRetriever(embedder, store, 3)

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
	fmt.Println("索引完成，开始对话。\n")

	// ── 初始化 LLM Client（Provider 和模型由 config.yaml / .env 控制）──
	llm := client.NewFromConfig(&cfg.LLM)

	systemPrompt := `你是一个专业的订单客服助手。
回答时必须严格基于提供的参考资料，不要编造信息。
如果参考资料中没有答案，请直接说"这个问题我需要转接人工客服"。`

	messages := []models.Message{
		{Role: "system", Content: systemPrompt},
	}

	// ── 对话循环 ─────────────────────────────────────
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("订单客服（输入 exit 退出）：")

	for {
		fmt.Print("\n你：")
		if !scanner.Scan() {
			break
		}
		query := strings.TrimSpace(scanner.Text())
		if query == "" {
			continue
		}
		if query == "exit" {
			break
		}

		// 检索相关知识
		context, hits, err := retriever.Retrieve(ctx, query)
		if err != nil {
			fmt.Fprintf(os.Stderr, "检索失败: %v\n", err)
			continue
		}

		// 构造带知识上下文的用户消息
		var userMsg string
		if context != "" {
			userMsg = fmt.Sprintf("参考资料：\n%s\n\n用户问题：%s", context, query)
		} else {
			userMsg = query
		}

		messages = append(messages, models.Message{Role: "user", Content: userMsg})

		resp, err := llm.Chat(ctx, messages)
		if err != nil {
			fmt.Fprintf(os.Stderr, "LLM 调用失败: %v\n", err)
			messages = messages[:len(messages)-1] // 回滚
			continue
		}

		answer := resp.Content
		messages = append(messages, models.Message{Role: "assistant", Content: answer})

		fmt.Printf("\n客服：%s\n", answer)

		// 显示检索来源（调试用，生产可去掉）
		if len(hits) > 0 {
			fmt.Print("\n[参考来源]")
			for _, h := range hits {
				fmt.Printf(" %s(%.2f)", h.Document.Meta["source"], h.Score)
			}
			fmt.Println()
		}
	}

	fmt.Println("\n再见！")
}
