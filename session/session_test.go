package session

import (
	"testing"

	"github.com/pengpn/go-llm-agent/models"
)

func TestAddAgentTurn_StoresToolCallsAndResults(t *testing.T) {
	sess := NewSession("test", "系统", &ByTurns{MaxTurns: 10})

	assistantMsg := models.Message{
		Role:    models.RoleAssistant,
		Content: "",
		ToolCalls: []models.ToolCall{
			{ID: "call_1", Type: "function", Function: models.FunctionCall{Name: "get_order", Arguments: `{"id":"1"}`}},
		},
	}
	toolResults := []models.Message{
		{Role: models.RoleTool, Content: `{"status":"已发货"}`, ToolCallID: "call_1"},
	}

	sess.AddAgentTurn(assistantMsg, toolResults)

	msgs := sess.Messages()
	// system + assistant(tool_calls) + tool
	if len(msgs) != 3 {
		t.Fatalf("消息数应为 3，got %d: %v", len(msgs), msgs)
	}
	if msgs[1].Role != models.RoleAssistant || len(msgs[1].ToolCalls) == 0 {
		t.Error("msgs[1] 应为含 ToolCalls 的 assistant 消息")
	}
	if msgs[2].Role != models.RoleTool || msgs[2].ToolCallID != "call_1" {
		t.Errorf("msgs[2] 应为 tool 消息且 ToolCallID=call_1，got %+v", msgs[2])
	}
}

func TestAddAgentTurn_Atomic(t *testing.T) {
	// 验证 assistant + tool 消息之间不会被 AddUserMessage 插入（原子性）
	// 这里用单线程验证接口语义——并发安全由锁保证
	sess := NewSession("test", "", &ByTurns{MaxTurns: 10})

	assistantMsg := models.Message{
		Role: models.RoleAssistant,
		ToolCalls: []models.ToolCall{
			{ID: "c1", Function: models.FunctionCall{Name: "tool"}},
		},
	}
	toolResults := []models.Message{
		{Role: models.RoleTool, ToolCallID: "c1", Content: "result"},
	}

	sess.AddAgentTurn(assistantMsg, toolResults)

	msgs := sess.Messages()
	// assistant 和 tool 必须相邻
	for i, m := range msgs {
		if m.Role == models.RoleAssistant && len(m.ToolCalls) > 0 {
			if i+1 >= len(msgs) || msgs[i+1].Role != models.RoleTool {
				t.Errorf("assistant(ToolCalls) 消息后必须紧跟 tool 消息，但 msgs[%d+1]=%v", i, msgs[i+1])
			}
		}
	}
}

func TestAddAgentTurn_MultipleToolCalls(t *testing.T) {
	// 一次 agent turn 包含多个工具调用
	sess := NewSession("test", "系统", &ByTurns{MaxTurns: 10})

	assistantMsg := models.Message{
		Role: models.RoleAssistant,
		ToolCalls: []models.ToolCall{
			{ID: "c1", Function: models.FunctionCall{Name: "tool_a"}},
			{ID: "c2", Function: models.FunctionCall{Name: "tool_b"}},
		},
	}
	toolResults := []models.Message{
		{Role: models.RoleTool, ToolCallID: "c1", Content: "result_a"},
		{Role: models.RoleTool, ToolCallID: "c2", Content: "result_b"},
	}

	sess.AddAgentTurn(assistantMsg, toolResults)

	msgs := sess.Messages()
	// system + assistant + tool×2 = 4
	if len(msgs) != 4 {
		t.Fatalf("消息数应为 4，got %d", len(msgs))
	}
	if msgs[2].ToolCallID != "c1" || msgs[3].ToolCallID != "c2" {
		t.Errorf("tool 消息顺序或 ID 错误: %v", msgs[2:])
	}
}

func TestAddAgentTurn_PreservesInPersistence(t *testing.T) {
	// 验证含 ToolCalls 的消息能正确序列化/反序列化
	sess := NewSession("persist_test", "系统", &ByTurns{MaxTurns: 10})

	sess.AddUserMessage("查询订单")
	sess.AddAgentTurn(
		models.Message{
			Role: models.RoleAssistant,
			ToolCalls: []models.ToolCall{
				{ID: "c1", Type: "function", Function: models.FunctionCall{Name: "get_order", Arguments: `{"id":"1"}`}},
			},
		},
		[]models.Message{
			{Role: models.RoleTool, ToolCallID: "c1", Content: `{"status":"已发货"}`},
		},
	)
	sess.AddAssistantMessage("您的订单已发货")

	dir := t.TempDir()
	path := dir + "/session.json"

	if err := sess.Save(path); err != nil {
		t.Fatalf("Save 失败: %v", err)
	}

	loaded, err := LoadSession(path, &ByTurns{MaxTurns: 10})
	if err != nil {
		t.Fatalf("LoadSession 失败: %v", err)
	}

	origMsgs := sess.Messages()
	loadMsgs := loaded.Messages()

	if len(origMsgs) != len(loadMsgs) {
		t.Fatalf("消息数不符: orig=%d loaded=%d", len(origMsgs), len(loadMsgs))
	}

	// 验证工具调用消息正确还原
	for i, m := range origMsgs {
		if len(m.ToolCalls) != len(loadMsgs[i].ToolCalls) {
			t.Errorf("msgs[%d] ToolCalls 数量不符: orig=%d loaded=%d", i, len(m.ToolCalls), len(loadMsgs[i].ToolCalls))
		}
		if m.ToolCallID != loadMsgs[i].ToolCallID {
			t.Errorf("msgs[%d] ToolCallID 不符: orig=%q loaded=%q", i, m.ToolCallID, loadMsgs[i].ToolCallID)
		}
	}
}
