package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/pengpn/go-llm-agent/client"
	"github.com/pengpn/go-llm-agent/models"
)

// judgeResponseSchema 是我们要求 Judge LLM 返回的 JSON 结构
// 用 JSON Mode / 结构化 Prompt 确保可解析
type judgeResponse struct {
	AccuracyScore      float64 `json:"accuracy_score"`
	RelevanceScore     float64 `json:"relevance_score"`
	CompletenessScore  float64 `json:"completeness_score"`
	HallucinationScore float64 `json:"hallucination_score"`
	Reason             string  `json:"reason"`
}

// Judge 使用 LLM 对一个回答打分
type Judge struct {
	client *client.Client
	// passThreshold OverallScore 高于此值视为通过
	passThreshold float64
	// weights 四个维度的权重（加起来应为 1.0）
	weights [4]float64
}

// NewJudge 创建 Judge，默认阈值 3.0，等权重
func NewJudge(c *client.Client, opts ...JudgeOption) *Judge {
	j := &Judge{
		client:        c,
		passThreshold: 3.0,
		weights:       [4]float64{0.35, 0.25, 0.25, 0.15},
	}
	for _, o := range opts {
		o(j)
	}
	return j
}

// JudgeOption Judge 的函数式选项
type JudgeOption func(*Judge)

func WithPassThreshold(t float64) JudgeOption {
	return func(j *Judge) { j.passThreshold = t }
}

// WithWeights 设置 [准确性, 相关性, 完整性, 幻觉] 四个维度权重
func WithWeights(accuracy, relevance, completeness, hallucination float64) JudgeOption {
	return func(j *Judge) {
		j.weights = [4]float64{accuracy, relevance, completeness, hallucination}
	}
}

// Score 对 actualAnswer 打分，返回评估结果（不含 EvalCase 字段，由 runner 填充）
func (j *Judge) Score(ctx context.Context, ec EvalCase, actualAnswer string) (EvalResult, error) {
	prompt := buildJudgePrompt(ec.Question, ec.ExpectedAnswer, actualAnswer)

	resp, err := j.client.Chat(ctx, []models.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return EvalResult{}, fmt.Errorf("judge LLM 调用失败: %w", err)
	}

	jr, err := parseJudgeResponse(resp.Content)
	if err != nil {
		return EvalResult{}, fmt.Errorf("解析 judge 响应失败: %w", err)
	}

	overall := j.weights[0]*jr.AccuracyScore +
		j.weights[1]*jr.RelevanceScore +
		j.weights[2]*jr.CompletenessScore +
		j.weights[3]*jr.HallucinationScore

	return EvalResult{
		Case:               ec,
		ActualAnswer:       actualAnswer,
		AccuracyScore:      jr.AccuracyScore,
		RelevanceScore:     jr.RelevanceScore,
		CompletenessScore:  jr.CompletenessScore,
		HallucinationScore: jr.HallucinationScore,
		OverallScore:       overall,
		Reason:             jr.Reason,
		Passed:             overall >= j.passThreshold,
	}, nil
}

// buildJudgePrompt 构建评分提示词
// 关键：要求 LLM 只返回 JSON，方便解析
func buildJudgePrompt(question, expected, actual string) string {
	return strings.TrimSpace(`
你是一个严格的 AI 客服质量评估专家。请根据以下信息对"实际回答"打分。

## 用户问题
` + question + `

## 期望答案（参考）
` + expected + `

## 实际回答（待评分）
` + actual + `

## 评分标准（每项 0-5 分，5 最好）
- accuracy_score（准确性）：实际回答与期望答案的事实一致程度
- relevance_score（相关性）：是否真正回答了用户的问题
- completeness_score（完整性）：关键信息是否都覆盖了
- hallucination_score（幻觉率）：5=完全无捏造信息，0=严重捏造

## 输出要求
只输出 JSON，不要任何额外文字：

{
  "accuracy_score": <0-5>,
  "relevance_score": <0-5>,
  "completeness_score": <0-5>,
  "hallucination_score": <0-5>,
  "reason": "<50字以内的总体评语>"
}
`)
}

// parseJudgeResponse 从 LLM 响应中提取 JSON
// LLM 有时会在 JSON 前后加 markdown 代码块，需要剥离
func parseJudgeResponse(content string) (judgeResponse, error) {
	// 剥离 ```json ... ``` 包裹
	content = strings.TrimSpace(content)
	if idx := strings.Index(content, "{"); idx > 0 {
		content = content[idx:]
	}
	if idx := strings.LastIndex(content, "}"); idx >= 0 && idx < len(content)-1 {
		content = content[:idx+1]
	}

	var jr judgeResponse
	if err := json.Unmarshal([]byte(content), &jr); err != nil {
		return judgeResponse{}, fmt.Errorf("JSON 解析失败: %w, 原文: %s", err, content)
	}
	return jr, nil
}
