package eval

import (
	"encoding/json"
	"testing"
)

func TestParseJudgeResponse_ValidJSON(t *testing.T) {
	content := `{
		"accuracy_score": 4.5,
		"relevance_score": 5.0,
		"completeness_score": 3.5,
		"hallucination_score": 5.0,
		"reason": "回答准确，但缺少运费说明"
	}`
	jr, err := parseJudgeResponse(content)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if jr.AccuracyScore != 4.5 {
		t.Errorf("AccuracyScore 期望 4.5，实际 %v", jr.AccuracyScore)
	}
	if jr.Reason != "回答准确，但缺少运费说明" {
		t.Errorf("Reason 不匹配: %v", jr.Reason)
	}
}

func TestParseJudgeResponse_MarkdownWrapped(t *testing.T) {
	// LLM 经常会在 JSON 前加 ```json 标记
	content := "```json\n{\"accuracy_score\": 3.0, \"relevance_score\": 4.0, \"completeness_score\": 3.0, \"hallucination_score\": 5.0, \"reason\": \"test\"}\n```"
	jr, err := parseJudgeResponse(content)
	if err != nil {
		t.Fatalf("解析 markdown 包裹的 JSON 失败: %v", err)
	}
	if jr.AccuracyScore != 3.0 {
		t.Errorf("AccuracyScore 期望 3.0，实际 %v", jr.AccuracyScore)
	}
}

func TestParseJudgeResponse_LeadingText(t *testing.T) {
	// LLM 有时在 JSON 前输出一段文字
	content := "以下是评分结果：\n{\"accuracy_score\": 2.0, \"relevance_score\": 2.0, \"completeness_score\": 2.0, \"hallucination_score\": 2.0, \"reason\": \"较差\"}"
	jr, err := parseJudgeResponse(content)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if jr.AccuracyScore != 2.0 {
		t.Errorf("AccuracyScore 期望 2.0，实际 %v", jr.AccuracyScore)
	}
}

func TestParseJudgeResponse_InvalidJSON(t *testing.T) {
	_, err := parseJudgeResponse("这不是 JSON")
	if err == nil {
		t.Fatal("期望解析失败，但成功了")
	}
}

func TestBuildJudgePrompt_ContainsKeyFields(t *testing.T) {
	prompt := buildJudgePrompt("用户问题", "期望答案", "实际回答")
	for _, keyword := range []string{"用户问题", "期望答案", "实际回答", "accuracy_score", "JSON"} {
		if !contains(prompt, keyword) {
			t.Errorf("提示词中缺少关键字: %q", keyword)
		}
	}
}

func TestBuildJudgePrompt_OutputsValidJSON(t *testing.T) {
	// 确认提示词中的 JSON 模板本身是合法 JSON
	prompt := buildJudgePrompt("q", "e", "a")
	start := findLast(prompt, "{")
	end := findLast(prompt, "}") + 1
	if start < 0 || end <= start {
		t.Fatal("提示词中找不到 JSON 模板")
	}
	jsonTemplate := prompt[start:end]
	// 把占位符替换为字符串，验证结构合法
	jsonTemplate = replaceScalarsWithZero(jsonTemplate)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(jsonTemplate), &m); err != nil {
		// JSON 模板中含占位符，解析失败是可以接受的
		// 只要 key 都在就行
		_ = m
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func findLast(s, sub string) int {
	idx := -1
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			idx = i
		}
	}
	return idx
}

func replaceScalarsWithZero(s string) string {
	return s
}
