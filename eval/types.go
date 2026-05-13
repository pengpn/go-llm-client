package eval

// EvalCase 一个测试用例：输入问题 + 期望答案
type EvalCase struct {
	ID             string
	Question       string
	ExpectedAnswer string
	// Category 用于分组统计（如 "订单查询" / "退款政策" / "物流"）
	Category string
}

// EvalResult 单个用例的评估结果
type EvalResult struct {
	Case EvalCase

	ActualAnswer string // Agent 实际输出

	// 各维度得分（0-5）
	AccuracyScore      float64
	RelevanceScore     float64
	CompletenessScore  float64
	HallucinationScore float64 // 5=完全无幻觉, 0=严重幻觉

	OverallScore float64 // 四维度加权平均
	Reason       string  // Judge 给出的评分理由
	Passed       bool    // OverallScore >= 阈值则通过
}

// EvalReport 整体评估报告
type EvalReport struct {
	TotalCases  int
	PassedCases int
	PassRate    float64

	AvgAccuracy      float64
	AvgRelevance     float64
	AvgCompleteness  float64
	AvgHallucination float64
	AvgOverall       float64

	// 按 Category 分组的通过率
	CategoryStats map[string]*CategoryStat

	Results []EvalResult
}

// CategoryStat 某一类别的统计
type CategoryStat struct {
	Total    int
	Passed   int
	AvgScore float64
}
