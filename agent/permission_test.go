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
