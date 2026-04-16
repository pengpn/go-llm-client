// chatbot 演示如何用 Config + Client + Session 构建命令行客服系统。
//
// 运行方式：
//
//	# 方式一：.env 文件（推荐开发环境）
//	cp .env.example .env  # 填入 API Key
//	go run ./examples/chatbot/
//
//	# 方式二：直接设置环境变量
//	OPENAI_API_KEY=xxx go run ./examples/chatbot/
//
//	# 方式三：自定义配置文件路径
//	CONFIG_FILE=./my-config.yaml go run ./examples/chatbot/
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/pengpn/go-llm-agent/client"
	"github.com/pengpn/go-llm-agent/config"
	"github.com/pengpn/go-llm-agent/session"
)

const systemPromptBase = `你是一位专业的电商客服助手，名字叫"小智"。
你的职责：
1. 回答用户关于订单、退款、物流的常见问题
2. 遇到复杂问题，告知用户转接人工客服
3. 保持友好、简洁的沟通风格
4. 不确定的信息不要乱猜，直接说不知道`

func main() {
	// 加载配置（CONFIG_FILE 环境变量可指定路径，默认 config.yaml）
	configFile := os.Getenv("CONFIG_FILE")
	if configFile == "" {
		configFile = "config.yaml"
	}

	cfg, err := config.Load(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "配置加载失败: %v\n", err)
		os.Exit(1)
	}

	// 初始化 LLM Client
	llmClient := client.NewFromConfig(&cfg.LLM)

	// System Prompt 注入运行时信息（当前时间）
	systemPrompt := systemPromptBase + "\n\n当前时间：" + time.Now().Format("2006-01-02 15:04")

	// 初始化 Session 管理器
	manager := session.NewManagerFromConfig(&cfg.Session, systemPrompt)
	defer manager.Stop()

	// 模拟用户 ID（实际系统从 JWT/Cookie 中提取）
	userID := "user_001"
	sess := manager.GetOrCreate(userID)

	fmt.Println("=== 智能客服系统 ===")
	fmt.Printf("Provider: %s | 模型: %s\n", cfg.LLM.Provider, cfg.LLM.Model)
	fmt.Println("输入 'quit' 退出，输入 'clear' 清除对话历史")
	fmt.Printf("Session ID: %s\n\n", sess.ID())

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
		if input == "quit" {
			fmt.Printf("\n%s\n", llmClient.CostSummary())
			break
		}
		if input == "clear" {
			sess.Clear()
			fmt.Println("[对话历史已清除]")
			continue
		}

		sess.AddUserMessage(input)

		ctx := context.Background()
		stream, err := llmClient.ChatStream(ctx, sess.Messages())
		if err != nil {
			fmt.Fprintf(os.Stderr, "请求失败: %v\n", err)
			continue
		}

		fmt.Print("小智: ")
		var fullResponse strings.Builder

		for chunk := range stream {
			if chunk.Err != nil {
				fmt.Fprintf(os.Stderr, "\n流式输出错误: %v\n", chunk.Err)
				break
			}
			if chunk.Done {
				break
			}
			fmt.Print(chunk.Content)
			fullResponse.WriteString(chunk.Content)
		}
		fmt.Println()

		if fullResponse.Len() > 0 {
			sess.AddAssistantMessage(fullResponse.String())
		}

		fmt.Printf("[历史消息数: %d]\n\n", sess.MessageCount())
	}
}
