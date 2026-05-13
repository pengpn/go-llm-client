# Lesson 11：评估体系（AI Quality Evaluation）

## 为什么要学

没有评估就没有可靠的迭代。怎么知道修改 Prompt 或换模型后效果变好了还是变差了？
需要一套可量化的评估框架，把"感觉还不错"变成具体的通过率数字。

## 学习目标

- 理解 LLM 评估的核心思路：用 LLM 评估 LLM（LLM-as-Judge）
- 构建测试数据集（问题 + 期望答案）
- 实现自动化评估 Pipeline
- 理解常见评估维度：准确性、相关性、完整性、幻觉率

## 计划内容

### 原理

**LLM-as-Judge**：用一个强模型（如 GPT-4o）给另一个模型的输出打分：

```
[评估 Prompt]
用户问题：退款要多久？
标准答案：支付宝/微信1-3天，银行卡3-5个工作日
模型回答：{actual_answer}

请评估模型回答的准确性（0-5分）、完整性（0-5分），并说明理由。
```

**评估 Pipeline**：
```
测试集（问题+期望答案）
    ↓
跑 Agent 得到实际回答
    ↓
LLM Judge 打分
    ↓
输出通过率报告
```

### 要实现的东西

```go
type EvalCase struct {
    Question       string
    ExpectedAnswer string  // 关键信息点
}

type EvalResult struct {
    Case     EvalCase
    Answer   string
    Score    float64
    Reason   string
}

// 运行评估
func RunEval(ctx context.Context, cases []EvalCase, ag *agent.Agent) []EvalResult
```

### 课后作业
- 作业1（入门）： 运行评估并找出分数最低的 3 个用例，分析原因（是 RAG 问题还是模型问题？）
- 作业2（中等）： 对比 v1 和 v2 两个 System Prompt 的评估结果，写出分析报告（哪个维度提升最明显？）
- 作业3（挑战）： 扩展 cmd/eval/main.go，支持将结果输出为 CSV 文件（方便用 Excel 分析趋势）

### 总结

核心设计思想

  1. LLM-as-Judge 的本质是"用语义相似度替代精确匹配"
  传统：assertEqual("3天内", actual)  → 完全匹配才过
  Judge：score("3天内", "预计72小时到达") → 语义等价也过

  2. 四维度分离，定位问题更准确
  - accuracy_score 低 → 事实错误，可能 RAG 召回了错误文档
  - relevance_score 低 → 答非所问，可能 System Prompt 意图理解有偏差
  - completeness_score 低 → 信息不全，可能截断或知识库缺失
  - hallucination_score 低 → 幻觉严重，需要更强的模型或更严格的 Prompt

  3. A/B 对比实验的正确姿势
  # 测 v1 Prompt
  ./bin/eval -prompt v1 -key $API_KEY

  # 测 v2 Prompt（增强版）
  ./bin/eval -prompt v2 -key $API_KEY

  # 对比两份报告的通过率和均分差异

  4. 为什么要并发执行？
  20 个用例 × 2 次 LLM 调用（agent + judge）= 40 次 API 调用
  串行大约需要 40×2s=80s；并发 3 个约 28s，提速 3 倍

  使用方法

  export LLM_API_KEY=your-key
  export LLM_BASE_URL=https://api.openai.com/v1
  ./bin/eval -model gpt-4o-mini -prompt v1 -threshold 3.0