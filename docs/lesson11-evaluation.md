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

### 课后作业（预计）
- 作业1：准备 20 个测试问答对（覆盖所有 FAQ 类型）
- 作业2：实现 `cmd/eval/main.go`，输出每个 case 的得分和原因
- 作业3：对比两个不同 System Prompt 的评估结果，分析差异
