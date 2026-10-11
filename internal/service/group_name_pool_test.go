package service

import (
	"testing"

	"clawbench/internal/model"
)

// 首个实例保留智能体本名，其余实例取名池名。
func TestAssignMemberNames_MultiInstanceAllUsePool(t *testing.T) {
	specs := []GroupMemberSpec{
		{AgentID: "a"}, {AgentID: "a"}, {AgentID: "a"},
	}
	assignMemberNames(specs, nil, nil)
	// 首个保留本名（无注册表时 GetAgentDisplayName("a") 回退到 agent id "a"），
	// 其余按序取自池：pool[0], pool[1]。
	base := GetAgentDisplayName("a")
	want := []string{base, groupNamePool[0], groupNamePool[1]}
	for i := range want {
		if specs[i].DisplayName != want[i] {
			t.Fatalf("names[%d] = %q want %q (all = %q,%q,%q)",
				i, specs[i].DisplayName, want[i], specs[0].DisplayName, specs[1].DisplayName, specs[2].DisplayName)
		}
	}
}

// 单实例（本批次只有一个、且组内此前没有）保留智能体本名。
func TestAssignMemberNames_LoneInstanceKeepsBase(t *testing.T) {
	specs := []GroupMemberSpec{{AgentID: "a"}}
	assignMemberNames(specs, nil, nil)
	// 无注册表时 GetAgentDisplayName("a") 回退到 agent id 本身。
	if specs[0].DisplayName != GetAgentDisplayName("a") {
		t.Fatalf("lone name = %q want %q", specs[0].DisplayName, GetAgentDisplayName("a"))
	}
}

// 已有 1 个实例（本名）+ 本批再加 1 个 → 已存在实例不改名（历史快照），新实例
// （k=2）取池名。
func TestAssignMemberNames_ExistingPlusBatchIsMulti(t *testing.T) {
	specs := []GroupMemberSpec{{AgentID: "a"}}
	assignMemberNames(specs, map[string]int{"a": 1}, nil)
	if specs[0].DisplayName != groupNamePool[0] {
		t.Fatalf("new instance name = %q want %q", specs[0].DisplayName, groupNamePool[0])
	}
}

// 池被占用时跳到下一个未占用名字（首个保留本名，重复实例取池）。
func TestAssignMemberNames_PoolSkipsTaken(t *testing.T) {
	taken := map[string]bool{groupNamePool[0]: true}
	specs := []GroupMemberSpec{{AgentID: "a"}, {AgentID: "a"}}
	assignMemberNames(specs, nil, taken)
	base := GetAgentDisplayName("a")
	if specs[0].DisplayName != base || specs[1].DisplayName != groupNamePool[1] {
		t.Fatalf("names = %q,%q want %q,%q",
			specs[0].DisplayName, specs[1].DisplayName, base, groupNamePool[1])
	}
}

// 池里所有名字都被占用 → 回退本名 + 序号，且绝不产生 "<base> 1"。
func TestAssignMemberNames_PoolExhaustedFallsBack(t *testing.T) {
	taken := map[string]bool{}
	for _, n := range groupNamePool {
		taken[n] = true
	}
	specs := []GroupMemberSpec{{AgentID: "a"}, {AgentID: "a"}}
	assignMemberNames(specs, nil, taken)
	// 无注册表时 GetAgentDisplayName("a") 回退到 agent id 本身；首个保留本名，
	// 重复实例编号从 2 起。
	base := GetAgentDisplayName("a")
	want0 := base
	want1 := base + " 2"
	if specs[0].DisplayName != want0 || specs[1].DisplayName != want1 {
		t.Fatalf("fallback = %q,%q want %q,%q", specs[0].DisplayName, specs[1].DisplayName, want0, want1)
	}
}

// 真实智能体本名不会被池名抢占，且与 specs 顺序无关：本批中真名 "Alice" 的
// 单实例，即使另有一组重复实例先取池名，也仍保留 "Alice"，池跳过它。
func TestAssignMemberNames_ReservesSpecBaseNames(t *testing.T) {
	withTestAgents(t, map[string]*model.Agent{
		"real-alice": {ID: "real-alice", Name: "Alice", Backend: "claude"},
		"x":          {ID: "x", Name: "X", Backend: "claude"},
	})
	// 重复实例在前，真 Alice 单实例在后。
	specs := []GroupMemberSpec{
		{AgentID: "x"}, {AgentID: "x"}, {AgentID: "x"}, {AgentID: "real-alice"},
	}
	assignMemberNames(specs, nil, nil)
	got := []string{specs[0].DisplayName, specs[1].DisplayName, specs[2].DisplayName, specs[3].DisplayName}
	if specs[3].DisplayName != "Alice" {
		t.Fatalf("a real agent must keep its own name, got %q (all: %q)", specs[3].DisplayName, got)
	}
	seen := map[string]bool{}
	for _, n := range got {
		if seen[n] {
			t.Fatalf("duplicate name %q in %q", n, got)
		}
		seen[n] = true
	}
	if seen["Alice 2"] {
		t.Fatalf("real Alice must not be suffixed: %q", got)
	}
}

// 群内已存在的智能体本名同样被池预留：即使该 agent 的成员名是池名（其本名不在
// taken 中），新批次取池名时也绝不取走它的本名。
func TestAssignMemberNames_ReservesExistingAgentBase(t *testing.T) {
	withTestAgents(t, map[string]*model.Agent{
		"real-alice": {ID: "real-alice", Name: "Alice", Backend: "claude"},
		"x":          {ID: "x", Name: "X", Backend: "claude"},
	})
	// 群内已有 real-alice 两个实例（成员名是池名 Bob/Carol，故其本名 Alice 不在 taken）。
	specs := []GroupMemberSpec{{AgentID: "x"}, {AgentID: "x"}}
	assignMemberNames(specs, map[string]int{"real-alice": 2}, map[string]bool{"Bob": true, "Carol": true})
	for _, s := range specs {
		if s.DisplayName == "Alice" {
			t.Fatalf("pool must reserve an existing agent's own name, got %q", s.DisplayName)
		}
	}
}

// 单实例本名与用户昵称冲突时回退本名 + 序号（从 2 起），不产生 "<base> 1"。
func TestAssignMemberNames_LoneCollisionFallsBackToTwo(t *testing.T) {
	base := GetAgentDisplayName("a")
	taken := map[string]bool{base: true}
	specs := []GroupMemberSpec{{AgentID: "a"}}
	assignMemberNames(specs, nil, taken)
	if specs[0].DisplayName != base+" 2" {
		t.Fatalf("collision fallback = %q want %q", specs[0].DisplayName, base+" 2")
	}
}
