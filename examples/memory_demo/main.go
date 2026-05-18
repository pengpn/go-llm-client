// memory_demo 演示 Lesson 14 的两个核心能力：
// 1. 对话摘要压缩 — 超过阈值时 LLM 自动总结旧对话
// 2. 用户画像记忆 — 跨会话保留用户偏好
//
// 运行：go run ./examples/memory_demo/
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pengpn/go-llm-agent/client"
	"github.com/pengpn/go-llm-agent/models"
	"github.com/pengpn/go-llm-agent/session"
)

const systemPrompt = `你是一个智能客服助手。请根据用户画像信息提供个性化服务。
如果用户提到新的偏好（如地址、常买商品、称呼等），请在回复中确认你已记住。`

func main() {
	_ = godotenv.Load()

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "请设置 OPENAI_API_KEY 环境变量")
		os.Exit(1)
	}

	llmClient := client.New(
		client.WithZhipuAI(apiKey, "glm-4-flash-250414"),
	)

	ctx := context.Background()

	// ── 演示 1：用户画像记忆 ──────────────────────
	fmt.Println("=== 演示 1：用户画像记忆 ===")
	fmt.Println()

	memoryDir := "./data/memories"
	store := session.NewFileMemoryStore(memoryDir)

	userID := "demo-user-001"
	memory, err := store.Load(userID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "加载记忆失败: %v\n", err)
		os.Exit(1)
	}

	if memory.Count() > 0 {
		fmt.Println("📋 已加载用户画像（回头客识别）：")
		fmt.Println(memory.FormatForPrompt())
	} else {
		fmt.Println("📋 首次访问，暂无用户画像")
		// 模拟写入一些初始记忆
		memory.Set("姓名", "张先生", 0)
		memory.Set("常用地址", "北京市朝阳区建国路88号", 0)
		memory.Set("偏好商品", "电子产品、书籍", 0)
		if err := store.Save(memory); err != nil {
			fmt.Fprintf(os.Stderr, "保存记忆失败: %v\n", err)
		}
		fmt.Println("✅ 已创建初始用户画像")
		fmt.Println(memory.FormatForPrompt())
	}

	// 构建带画像的 System Prompt
	enhancedPrompt := systemPrompt
	profile := memory.FormatForPrompt()
	if profile != "" {
		enhancedPrompt = systemPrompt + "\n\n" + profile
	}

	// ── 演示 2：对话摘要压缩 ──────────────────────
	fmt.Println("=== 演示 2：对话摘要压缩 ===")
	fmt.Println("（阈值设为 8 条消息，方便快速触发压缩）")
	fmt.Println()

	sess := session.NewSession(userID, enhancedPrompt, &session.ByTurns{MaxTurns: 50})
	summarizer := session.NewSummarizer(llmClient,
		session.WithThreshold(8),
		session.WithKeepRecent(4),
	)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("你: ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "quit" || input == "exit" {
			break
		}

		sess.AddUserMessage(input)

		// 每次对话前检查是否需要压缩
		compressed, err := summarizer.CompressIfNeeded(ctx, sess)
		if err != nil {
			fmt.Printf("⚠️  摘要压缩失败（继续对话不受影响）: %v\n", err)
		}
		if compressed {
			fmt.Println("📝 [系统] 对话历史已自动压缩为摘要")
			fmt.Printf("📝 [系统] 当前消息数: %d\n", sess.MessageCount())
		}

		resp, err := llmClient.Chat(ctx, sess.Messages())
		if err != nil {
			fmt.Printf("❌ LLM 错误: %v\n", err)
			continue
		}

		sess.AddAssistantMessage(resp.Content)
		fmt.Printf("客服: %s\n\n", resp.Content)

		// 如果用户提到新地址，更新记忆（简单的规则匹配示范）
		if strings.Contains(input, "地址") && strings.Contains(input, "改") {
			parts := strings.SplitN(input, "改为", 2)
			if len(parts) == 2 {
				newAddr := strings.TrimSpace(parts[1])
				memory.Set("常用地址", newAddr, 0)
				if err := store.Save(memory); err != nil {
					fmt.Printf("⚠️  保存记忆失败: %v\n", err)
				} else {
					fmt.Println("💾 [系统] 已更新用户地址记忆")
				}
			}
		}
	}

	fmt.Println("\n会话结束，用户画像已保存。下次进入将自动加载。")
}

// 确保 *client.Client 满足 SummaryLLM 接口
var _ session.SummaryLLM = (*client.Client)(nil)

// 确保 *client.Client 有 Chat 方法（已有，因为 Chat 内部调用 ChatWithTools(nil)）
func init() {
	_ = func(c *client.Client) {
		var _ func(context.Context, []models.Message) (*models.Response, error) = c.Chat
	}
}
