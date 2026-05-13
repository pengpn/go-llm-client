package eval

import (
	"context"
	"testing"
)

// mockJudge 在测试中直接给固定分数，不调用真实 LLM
type mockJudge struct {
	score  float64
	passed bool
}

func (m *mockJudge) Score(_ context.Context, ec EvalCase, actual string) (EvalResult, error) {
	return EvalResult{
		Case:               ec,
		ActualAnswer:       actual,
		AccuracyScore:      m.score,
		RelevanceScore:     m.score,
		CompletenessScore:  m.score,
		HallucinationScore: m.score,
		OverallScore:       m.score,
		Reason:             "mock reason",
		Passed:             m.passed,
	}, nil
}

func TestBuildReport_PassRate(t *testing.T) {
	results := []EvalResult{
		{Case: EvalCase{ID: "1", Category: "A"}, ActualAnswer: "ans", OverallScore: 4.0, Passed: true},
		{Case: EvalCase{ID: "2", Category: "A"}, ActualAnswer: "ans", OverallScore: 2.0, Passed: false},
		{Case: EvalCase{ID: "3", Category: "B"}, ActualAnswer: "ans", OverallScore: 5.0, Passed: true},
	}

	report := buildReport(results)

	if report.PassedCases != 2 {
		t.Errorf("PassedCases 期望 2，实际 %d", report.PassedCases)
	}
	if report.TotalCases != 3 {
		t.Errorf("TotalCases 期望 3，实际 %d", report.TotalCases)
	}
	// 通过率应约为 66.7%
	if report.PassRate < 66 || report.PassRate > 67 {
		t.Errorf("PassRate 期望约 66.7，实际 %.1f", report.PassRate)
	}
}

func TestBuildReport_CategoryStats(t *testing.T) {
	results := []EvalResult{
		{Case: EvalCase{Category: "订单"}, ActualAnswer: "a", OverallScore: 4.0, Passed: true},
		{Case: EvalCase{Category: "订单"}, ActualAnswer: "b", OverallScore: 2.0, Passed: false},
		{Case: EvalCase{Category: "退款"}, ActualAnswer: "c", OverallScore: 5.0, Passed: true},
	}

	report := buildReport(results)

	orderStat, ok := report.CategoryStats["订单"]
	if !ok {
		t.Fatal("缺少 '订单' 分类统计")
	}
	if orderStat.Total != 2 || orderStat.Passed != 1 {
		t.Errorf("订单分类: 期望 Total=2 Passed=1, 实际 Total=%d Passed=%d",
			orderStat.Total, orderStat.Passed)
	}
	// 平均分应为 (4.0+2.0)/2 = 3.0
	if orderStat.AvgScore != 3.0 {
		t.Errorf("订单均分期望 3.0，实际 %.2f", orderStat.AvgScore)
	}
}

func TestBuildReport_SkipsFailedCases(t *testing.T) {
	// ActualAnswer 为空表示 agent 运行失败，不计入统计
	results := []EvalResult{
		{Case: EvalCase{Category: "A"}, ActualAnswer: "ans", OverallScore: 4.0, Passed: true},
		{Case: EvalCase{Category: "A"}, ActualAnswer: "", OverallScore: 0, Passed: false}, // 失败
	}

	report := buildReport(results)

	// validCount=1，所以均分 = 4.0
	if report.AvgOverall != 4.0 {
		t.Errorf("AvgOverall 期望 4.0，实际 %.2f", report.AvgOverall)
	}
}

func TestBuildReport_JudgeWeights(t *testing.T) {
	j := NewJudge(nil, WithWeights(0.5, 0.2, 0.2, 0.1))
	// accuracy=5, relevance=3, completeness=3, hallucination=3
	// expected = 0.5*5 + 0.2*3 + 0.2*3 + 0.1*3 = 2.5+0.6+0.6+0.3 = 4.0
	expected := 0.5*5.0 + 0.2*3.0 + 0.2*3.0 + 0.1*3.0
	got := j.weights[0]*5.0 + j.weights[1]*3.0 + j.weights[2]*3.0 + j.weights[3]*3.0
	if got != expected {
		t.Errorf("权重计算错误: 期望 %.2f, 实际 %.2f", expected, got)
	}
}

func TestDefaultTestCases_Count(t *testing.T) {
	if len(DefaultTestCases) != 20 {
		t.Errorf("期望 20 个测试用例，实际 %d 个", len(DefaultTestCases))
	}
}

func TestDefaultTestCases_NoDuplicateIDs(t *testing.T) {
	seen := make(map[string]bool)
	for _, c := range DefaultTestCases {
		if seen[c.ID] {
			t.Errorf("重复的用例 ID: %s", c.ID)
		}
		seen[c.ID] = true
	}
}

func TestDefaultTestCases_AllFieldsPresent(t *testing.T) {
	for _, c := range DefaultTestCases {
		if c.ID == "" {
			t.Errorf("用例缺少 ID: %+v", c)
		}
		if c.Question == "" {
			t.Errorf("[%s] 缺少 Question", c.ID)
		}
		if c.ExpectedAnswer == "" {
			t.Errorf("[%s] 缺少 ExpectedAnswer", c.ID)
		}
		if c.Category == "" {
			t.Errorf("[%s] 缺少 Category", c.ID)
		}
	}
}
