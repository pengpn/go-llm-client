package agent

// Gate 控制特定用户是否可以调用特定工具。
// Agent 每次 Run 时根据 Gate 过滤工具列表，LLM 只看到它被允许使用的工具。
type Gate interface {
	Allowed(userID, toolName string) bool
}

// AllowAll 是默认策略，允许所有用户调用所有工具（向后兼容）。
type AllowAll struct{}

func (AllowAll) Allowed(_, _ string) bool { return true }

// DenyAll 拒绝所有访问，主要用于测试。
type DenyAll struct{}

func (DenyAll) Allowed(_, _ string) bool { return false }

// RoleGate 基于角色的权限控制。
// 每个角色对应一组可调用的工具名称；用户与角色绑定。
// 未绑定角色的用户使用 defaultRole。
type RoleGate struct {
	userRoles   map[string]string          // userID -> role
	roleTools   map[string]map[string]bool // role -> allowed tool names
	defaultRole string
}

// NewRoleGate 创建一个 RoleGate，defaultRole 用于未显式绑定角色的用户。
func NewRoleGate(defaultRole string) *RoleGate {
	return &RoleGate{
		userRoles:   make(map[string]string),
		roleTools:   make(map[string]map[string]bool),
		defaultRole: defaultRole,
	}
}

// DefineRole 注册角色及其可访问的工具列表。支持链式调用。
func (g *RoleGate) DefineRole(role string, tools ...string) *RoleGate {
	set := make(map[string]bool, len(tools))
	for _, t := range tools {
		set[t] = true
	}
	g.roleTools[role] = set
	return g
}

// AssignRole 将用户绑定到角色。支持链式调用。
func (g *RoleGate) AssignRole(userID, role string) *RoleGate {
	g.userRoles[userID] = role
	return g
}

// Allowed 检查用户是否可以调用指定工具。
func (g *RoleGate) Allowed(userID, toolName string) bool {
	role, ok := g.userRoles[userID]
	if !ok {
		role = g.defaultRole
	}
	tools, ok := g.roleTools[role]
	if !ok {
		return false
	}
	return tools[toolName]
}
