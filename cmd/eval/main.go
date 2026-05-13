package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/joho/godotenv"
	"github.com/pengpn/go-llm-agent/client"
	"github.com/pengpn/go-llm-agent/eval"
	"github.com/pengpn/go-llm-agent/session"
)

func main() {
	// 自动加载项目根目录的 .env 文件，找不到时静默跳过（不影响已设置的系统环境变量）
	_ = godotenv.Load()

	var (
		apiKey    = flag.String("key", os.Getenv("LLM_API_KEY"), "LLM API Key")
		baseURL   = flag.String("url", os.Getenv("LLM_BASE_URL"), "LLM Base URL")
		model     = flag.String("model", os.Getenv("LLM_MODEL"), "Agent 使用的模型")
		judgeKey  = flag.String("judge-key", os.Getenv("JUDGE_API_KEY"), "Judge LLM API Key（默认与 agent 相同）")
		judgeURL  = flag.String("judge-url", os.Getenv("JUDGE_BASE_URL"), "Judge LLM Base URL")
		judgeModel = flag.String("judge-model", os.Getenv("JUDGE_MODEL"), "Judge 使用的模型（建议用更强的模型）")
		prompt    = flag.String("prompt", "v1", "System Prompt 版本标签（用于区分对比实验）")
		threshold = flag.Float64("threshold", 3.0, "通过阈值（0-5）")
		concurrency = flag.Int("concurrency", 3, "并发评估数")
		csvFile     = flag.String("csv", "", "输出 CSV 文件路径（为空则不输出）")
	)
	flag.Parse()

	if *apiKey == "" {
		fmt.Fprintln(os.Stderr, "错误：请设置 LLM_API_KEY 或使用 -key 参数")
		os.Exit(1)
	}
	if *baseURL == "" {
		*baseURL = "https://api.openai.com/v1"
	}
	if *model == "" {
		*model = "gpt-4o-mini"
	}

	// Judge 默认复用 agent 的配置，但可以指定更强的模型
	if *judgeKey == "" {
		*judgeKey = *apiKey
	}
	if *judgeURL == "" {
		*judgeURL = *baseURL
	}
	if *judgeModel == "" {
		*judgeModel = *model // 默认用更强的模型做 judge,这里用回原来的模型来测试
	}

	// ── 创建 Agent（被评估对象） ─────────────────────────────────
	agentClient := client.New(
		client.WithAPIKey(*apiKey),
		client.WithBaseURL(*baseURL),
		client.WithModel(*model),
	)

	systemPrompt := getSystemPrompt(*prompt)
	fmt.Printf("🔧 被测模型: %s | System Prompt: %s\n", *model, *prompt)

	agentFn := func(ctx context.Context, question string) (string, error) {
		sess := session.NewSession("eval-session", systemPrompt, &session.ByTurns{MaxTurns: 20})
		sess.AddUserMessage(question)
		msgs := sess.Messages()
		resp, err := agentClient.Chat(ctx, msgs)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	}

	// ── 创建 Judge ───────────────────────────────────────────────
	judgeClient := client.New(
		client.WithAPIKey(*judgeKey),
		client.WithBaseURL(*judgeURL),
		client.WithModel(*judgeModel),
	)
	fmt.Printf("⚖️  Judge 模型: %s\n\n", *judgeModel)

	judge := eval.NewJudge(judgeClient, eval.WithPassThreshold(*threshold))
	runner := eval.NewRunner(agentFn, judge,
		eval.WithConcurrency(*concurrency),
		eval.WithTimeout(60*time.Second),
		eval.WithSystemPrompt(systemPrompt),
	)

	// ── 运行评估 ─────────────────────────────────────────────────
	fmt.Printf("🚀 开始评估 %d 个用例...\n", len(eval.DefaultTestCases))
	start := time.Now()

	report, err := runner.Run(context.Background(), eval.DefaultTestCases)
	elapsed := time.Since(start)

	// ── 输出报告 ─────────────────────────────────────────────────
	printReport(report, *prompt, elapsed)

	if *csvFile != "" {
		if csvErr := writeCSV(*csvFile, report, *prompt); csvErr != nil {
			fmt.Fprintf(os.Stderr, "\n❌ CSV 写入失败: %v\n", csvErr)
			os.Exit(1)
		}
		fmt.Printf("\n📄 CSV 已保存至: %s\n", *csvFile)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "\n⚠️  部分用例出错:\n%v\n", err)
		os.Exit(1)
	}
}

// printReport 打印格式化报告
func printReport(report *eval.EvalReport, promptLabel string, elapsed time.Duration) {
	fmt.Println(strings.Repeat("═", 60))
	fmt.Printf("  评估报告 | Prompt: %-10s | 耗时: %.1fs\n", promptLabel, elapsed.Seconds())
	fmt.Println(strings.Repeat("═", 60))

	// 总体得分
	fmt.Printf("\n📊 整体结果：%d/%d 通过 (通过率 %.1f%%)\n",
		report.PassedCases, report.TotalCases, report.PassRate)
	fmt.Printf("   准确性: %.2f  相关性: %.2f  完整性: %.2f  幻觉率: %.2f  综合: %.2f\n\n",
		report.AvgAccuracy, report.AvgRelevance, report.AvgCompleteness,
		report.AvgHallucination, report.AvgOverall)

	// 按 category 分组
	fmt.Println("📂 分类统计：")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "   类别\t通过\t总数\t通过率\t均分")
	for cat, cs := range report.CategoryStats {
		fmt.Fprintf(w, "   %s\t%d\t%d\t%.0f%%\t%.2f\n",
			cat, cs.Passed, cs.Total,
			float64(cs.Passed)/float64(cs.Total)*100, cs.AvgScore)
	}
	w.Flush()

	// 逐条结果
	fmt.Println("\n📋 逐条结果：")
	for _, r := range report.Results {
		status := "✅"
		if !r.Passed {
			status = "❌"
		}
		fmt.Printf("\n%s [%s] %s\n", status, r.Case.ID, r.Case.Question)
		fmt.Printf("   得分: %.2f  (准%.1f/关%.1f/完%.1f/幻%.1f)\n",
			r.OverallScore, r.AccuracyScore, r.RelevanceScore,
			r.CompletenessScore, r.HallucinationScore)
		fmt.Printf("   理由: %s\n", r.Reason)
		if !r.Passed {
			fmt.Printf("   期望: %s\n", r.Case.ExpectedAnswer)
			fmt.Printf("   实际: %s\n", r.ActualAnswer)
		}
	}
	fmt.Println(strings.Repeat("─", 60))
}

// getSystemPrompt 根据版本标签返回不同的 system prompt（用于 A/B 对比）
func getSystemPrompt(version string) string {
	prompts := map[string]string{
		"v1": `你是一个友好的电商客服助手。请简洁、准确地回答用户问题。
如果不知道答案，直接说不知道，不要编造信息。`,

		"v2": `你是一个专业的电商客服助手，服务于一家综合电商平台。

【核心原则】
1. 准确优先：不确定时说"我需要查一下"，绝不编造
2. 简洁有力：回答在 50 字内，除非用户需要详细说明
3. 主动引导：回答后询问"还有其他问题吗？"

【工具使用】
- 查询订单状态：调用 get_order 工具（需要订单号）
- 检索知识库：调用 search_knowledge_base 工具`,
	}

	if sp, ok := prompts[version]; ok {
		return sp
	}
	return prompts["v1"]
}

// writeCSV 将评估结果写入 CSV 文件
// BOM 头确保 Excel 打开中文不乱码
func writeCSV(path string, report *eval.EvalReport, promptLabel string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建文件失败: %w", err)
	}
	defer f.Close()

	// UTF-8 BOM，让 Excel 正确识别编码
	if _, err := f.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return fmt.Errorf("写入 BOM 失败: %w", err)
	}

	w := csv.NewWriter(f)
	defer w.Flush()

	header := []string{
		"ID", "类别", "问题", "期望答案", "实际回答",
		"准确性", "相关性", "完整性", "幻觉率", "综合分",
		"通过", "评分理由", "Prompt版本",
	}
	if err := w.Write(header); err != nil {
		return fmt.Errorf("写入表头失败: %w", err)
	}

	for _, r := range report.Results {
		passed := "否"
		if r.Passed {
			passed = "是"
		}
		row := []string{
			r.Case.ID,
			r.Case.Category,
			r.Case.Question,
			r.Case.ExpectedAnswer,
			r.ActualAnswer,
			fmt.Sprintf("%.2f", r.AccuracyScore),
			fmt.Sprintf("%.2f", r.RelevanceScore),
			fmt.Sprintf("%.2f", r.CompletenessScore),
			fmt.Sprintf("%.2f", r.HallucinationScore),
			fmt.Sprintf("%.2f", r.OverallScore),
			passed,
			r.Reason,
			promptLabel,
		}
		if err := w.Write(row); err != nil {
			return fmt.Errorf("写入行 [%s] 失败: %w", r.Case.ID, err)
		}
	}

	// 追加汇总行
	if err := w.Write([]string{}); err != nil {
		return err
	}
	summary := []string{
		"汇总", "", "",
		fmt.Sprintf("总计 %d 例", report.TotalCases),
		fmt.Sprintf("通过 %d 例", report.PassedCases),
		fmt.Sprintf("%.2f", report.AvgAccuracy),
		fmt.Sprintf("%.2f", report.AvgRelevance),
		fmt.Sprintf("%.2f", report.AvgCompleteness),
		fmt.Sprintf("%.2f", report.AvgHallucination),
		fmt.Sprintf("%.2f", report.AvgOverall),
		fmt.Sprintf("%.1f%%", report.PassRate),
		"", promptLabel,
	}
	return w.Write(summary)
}
