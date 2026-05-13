package eval

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// AgentRunner 被评估对象的接口（和 server 包的 AgentRunner 相同签名）
type AgentRunner interface {
	Run(ctx context.Context, msgs []interface{}) (string, interface{}, error)
}

// SimpleAgentRunner 适配真实 agent.Agent 的薄包装（避免 eval 包依赖 agent 包）
type SimpleAgentRunner func(ctx context.Context, question string) (string, error)

// Runner 批量运行评估
type Runner struct {
	agentFn       SimpleAgentRunner
	judge         *Judge
	concurrency   int           // 并发数
	timeout       time.Duration // 单个用例超时
	systemPrompt  string        // 记录被测 system prompt（用于报告）
}

// RunnerOption 函数式选项
type RunnerOption func(*Runner)

func WithConcurrency(n int) RunnerOption {
	return func(r *Runner) { r.concurrency = n }
}

func WithTimeout(d time.Duration) RunnerOption {
	return func(r *Runner) { r.timeout = d }
}

func WithSystemPrompt(sp string) RunnerOption {
	return func(r *Runner) { r.systemPrompt = sp }
}

// NewRunner 创建评估 Runner
func NewRunner(agentFn SimpleAgentRunner, judge *Judge, opts ...RunnerOption) *Runner {
	r := &Runner{
		agentFn:     agentFn,
		judge:       judge,
		concurrency: 3,
		timeout:     30 * time.Second,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Run 批量评估所有用例，返回完整报告
func (r *Runner) Run(ctx context.Context, cases []EvalCase) (*EvalReport, error) {
	results := make([]EvalResult, len(cases))
	errs := make([]error, len(cases))

	sem := make(chan struct{}, r.concurrency)
	var wg sync.WaitGroup

	for i, ec := range cases {
		wg.Add(1)
		go func(idx int, c EvalCase) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result, err := r.evalOne(ctx, c)
			results[idx] = result
			errs[idx] = err
		}(i, ec)
	}
	wg.Wait()

	// 收集错误（不因单个用例失败而终止）
	var errMsgs []string
	for i, err := range errs {
		if err != nil {
			errMsgs = append(errMsgs, fmt.Sprintf("[%s] %v", cases[i].ID, err))
		}
	}

	report := buildReport(results)

	if len(errMsgs) > 0 {
		return report, fmt.Errorf("部分用例失败:\n%v", errMsgs)
	}
	return report, nil
}

// evalOne 评估单个用例
func (r *Runner) evalOne(ctx context.Context, ec EvalCase) (EvalResult, error) {
	caseCtx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()

	actual, err := r.agentFn(caseCtx, ec.Question)
	if err != nil {
		return EvalResult{Case: ec}, fmt.Errorf("agent 运行失败: %w", err)
	}

	return r.judge.Score(caseCtx, ec, actual)
}

// buildReport 从结果列表汇总报告
func buildReport(results []EvalResult) *EvalReport {
	report := &EvalReport{
		TotalCases:    len(results),
		CategoryStats: make(map[string]*CategoryStat),
		Results:       results,
	}

	var sumAcc, sumRel, sumComp, sumHal, sumOverall float64
	validCount := 0

	for _, r := range results {
		if r.ActualAnswer == "" {
			continue // 跳过运行失败的用例
		}
		validCount++

		sumAcc += r.AccuracyScore
		sumRel += r.RelevanceScore
		sumComp += r.CompletenessScore
		sumHal += r.HallucinationScore
		sumOverall += r.OverallScore

		if r.Passed {
			report.PassedCases++
		}

		// 按 category 统计
		cat := r.Case.Category
		if _, ok := report.CategoryStats[cat]; !ok {
			report.CategoryStats[cat] = &CategoryStat{}
		}
		cs := report.CategoryStats[cat]
		cs.Total++
		cs.AvgScore += r.OverallScore
		if r.Passed {
			cs.Passed++
		}
	}

	if validCount > 0 {
		report.AvgAccuracy = sumAcc / float64(validCount)
		report.AvgRelevance = sumRel / float64(validCount)
		report.AvgCompleteness = sumComp / float64(validCount)
		report.AvgHallucination = sumHal / float64(validCount)
		report.AvgOverall = sumOverall / float64(validCount)
		report.PassRate = float64(report.PassedCases) / float64(validCount) * 100
	}

	// 各 category 平均分
	for _, cs := range report.CategoryStats {
		if cs.Total > 0 {
			cs.AvgScore /= float64(cs.Total)
		}
	}

	return report
}
