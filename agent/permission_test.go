package agent

import "testing"

// ---- AllowAll / DenyAll ----

func TestAllowAll(t *testing.T) {
	gate := AllowAll{}
	if !gate.Allowed("any_user", "any_tool") {
		t.Error("AllowAll 应允许所有请求")
	}
}

func TestDenyAll(t *testing.T) {
	gate := DenyAll{}
	if gate.Allowed("any_user", "any_tool") {
		t.Error("DenyAll 应拒绝所有请求")
	}
}

// ---- RoleGate ----

func buildTestGate() *RoleGate {
	return NewRoleGate("user").
		DefineRole("user", "read_tool").
		DefineRole("admin", "read_tool", "write_tool").
		AssignRole("alice", "admin").
		AssignRole("bob", "user")
}

func TestRoleGate_AdminCanAccessAllTools(t *testing.T) {
	gate := buildTestGate()
	if !gate.Allowed("alice", "read_tool") {
		t.Error("admin 应可访问 read_tool")
	}
	if !gate.Allowed("alice", "write_tool") {
		t.Error("admin 应可访问 write_tool")
	}
}

func TestRoleGate_UserCanOnlyReadTool(t *testing.T) {
	gate := buildTestGate()
	if !gate.Allowed("bob", "read_tool") {
		t.Error("user 应可访问 read_tool")
	}
	if gate.Allowed("bob", "write_tool") {
		t.Error("user 不应访问 write_tool")
	}
}

func TestRoleGate_UnknownUserUsesDefaultRole(t *testing.T) {
	gate := buildTestGate() // defaultRole = "user"
	if !gate.Allowed("charlie", "read_tool") {
		t.Error("未知用户应使用默认角色 user，可访问 read_tool")
	}
	if gate.Allowed("charlie", "write_tool") {
		t.Error("未知用户（默认 user）不应访问 write_tool")
	}
}

func TestRoleGate_UndefinedRoleAllowsNothing(t *testing.T) {
	gate := NewRoleGate("ghost") // defaultRole 未在 DefineRole 中定义
	if gate.Allowed("anyone", "any_tool") {
		t.Error("未定义的角色应拒绝所有工具")
	}
}

func TestRoleGate_ChainedCalls(t *testing.T) {
	// 链式调用最终结果正确
	gate := NewRoleGate("viewer").
		DefineRole("viewer", "search").
		DefineRole("editor", "search", "write").
		AssignRole("u1", "editor")

	if !gate.Allowed("u1", "write") {
		t.Error("editor 应可访问 write")
	}
	if gate.Allowed("u2", "write") { // u2 使用默认 viewer
		t.Error("viewer 不应访问 write")
	}
}

// ---- filterDefinitions（Agent 内部逻辑）----

func TestFilterDefinitions_EmptyUserID_NoFilter(t *testing.T) {
	defs := makeDefs("tool_a", "tool_b")
	gate := DenyAll{}
	result := filterDefinitions(defs, gate, "")
	if len(result) != 2 {
		t.Errorf("userID 为空时不应过滤，got %d 个工具", len(result))
	}
}

func TestFilterDefinitions_FiltersCorrectly(t *testing.T) {
	defs := makeDefs("read", "write", "delete")
	gate := NewRoleGate("user").
		DefineRole("user", "read").
		AssignRole("u1", "user")

	result := filterDefinitions(defs, gate, "u1")
	if len(result) != 1 || result[0].Function.Name != "read" {
		t.Errorf("应只返回 read，got %+v", result)
	}
}

func TestFilterDefinitions_DenyAll_ReturnsEmpty(t *testing.T) {
	defs := makeDefs("tool_a", "tool_b")
	result := filterDefinitions(defs, DenyAll{}, "some_user")
	if len(result) != 0 {
		t.Errorf("DenyAll 应过滤所有工具，got %d 个", len(result))
	}
}

// ---- UserGate ----

func TestUserGate_AllowedTools(t *testing.T) {
	gate := NewUserGate().
		Allow("alice", "read", "write").
		Allow("bob", "read")

	if !gate.Allowed("alice", "read") {
		t.Error("alice 应可访问 read")
	}
	if !gate.Allowed("alice", "write") {
		t.Error("alice 应可访问 write")
	}
	if !gate.Allowed("bob", "read") {
		t.Error("bob 应可访问 read")
	}
	if gate.Allowed("bob", "write") {
		t.Error("bob 不应访问 write")
	}
}

func TestUserGate_UnknownUserDeniedAll(t *testing.T) {
	// 未配置的用户应拒绝所有工具（最小权限原则）
	gate := NewUserGate().Allow("alice", "read")
	if gate.Allowed("charlie", "read") {
		t.Error("未配置的用户应拒绝所有工具访问")
	}
}

func TestUserGate_MultipleAllowCallsAccumulate(t *testing.T) {
	// 多次 Allow 调用应累加，而非覆盖
	gate := NewUserGate().
		Allow("alice", "tool_a").
		Allow("alice", "tool_b")

	if !gate.Allowed("alice", "tool_a") {
		t.Error("tool_a 应可访问（第一次 Allow）")
	}
	if !gate.Allowed("alice", "tool_b") {
		t.Error("tool_b 应可访问（第二次 Allow 追加，非覆盖）")
	}
}

func TestUserGate_EmptyAllowList(t *testing.T) {
	// Allow 不传工具时，用户被配置了但白名单为空
	gate := NewUserGate().Allow("alice") // 没有传工具名
	if gate.Allowed("alice", "any_tool") {
		t.Error("空白名单应拒绝所有工具")
	}
}

func TestUserGate_EmptyUserID(t *testing.T) {
	gate := NewUserGate().Allow("alice", "tool_a")
	if gate.Allowed("", "tool_a") {
		t.Error("空 userID 应拒绝访问（未配置用户）")
	}
}

func TestUserGate_ImplementsGateInterface(t *testing.T) {
	// 编译期验证 UserGate 实现了 Gate 接口
	var _ Gate = NewUserGate()
}

func TestUserGate_WithFilterDefinitions(t *testing.T) {
	// UserGate 与 filterDefinitions 集成：过滤工具列表
	defs := makeDefs("tool_a", "tool_b", "tool_c")
	gate := NewUserGate().Allow("u1", "tool_a", "tool_c")

	result := filterDefinitions(defs, gate, "u1")
	if len(result) != 2 {
		t.Fatalf("应过滤出 2 个工具，got %d", len(result))
	}
	names := make(map[string]bool)
	for _, d := range result {
		names[d.Function.Name] = true
	}
	if !names["tool_a"] || !names["tool_c"] {
		t.Errorf("结果应包含 tool_a 和 tool_c，got %v", names)
	}
	if names["tool_b"] {
		t.Error("tool_b 未被授权，不应出现在结果中")
	}
}
