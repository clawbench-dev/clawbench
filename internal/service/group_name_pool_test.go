package service

import "testing"

// 一个智能体有多个实例时，其全部实例（含第一个）都取池名，不保留本名。
func TestAssignMemberNames_MultiInstanceAllUsePool(t *testing.T) {
	specs := []GroupMemberSpec{
		{AgentID: "a"}, {AgentID: "a"}, {AgentID: "a"},
	}
	assignMemberNames(specs, nil, nil)
	// 全部取自池，按序：pool[0], pool[1], pool[2]。
	want := []string{groupNamePool[0], groupNamePool[1], groupNamePool[2]}
	for i := range want {
		if specs[i].DisplayName != want[i] {
			t.Fatalf("names[%d] = %q want %q (all = %q,%q,%q)",
				i, specs[i].DisplayName, want[i], specs[0].DisplayName, specs[1].DisplayName, specs[2].DisplayName)
		}
	}
}

// 单实例（本批次只有一个、且组内此前没有）仍保留智能体本名。
func TestAssignMemberNames_LoneInstanceKeepsBase(t *testing.T) {
	specs := []GroupMemberSpec{{AgentID: "a"}}
	assignMemberNames(specs, nil, nil)
	// 无注册表时 GetAgentDisplayName("a") 回退到 agent id 本身。
	if specs[0].DisplayName != GetAgentDisplayName("a") {
		t.Fatalf("lone name = %q want %q", specs[0].DisplayName, GetAgentDisplayName("a"))
	}
}

// 已有 1 个实例（本名）+ 本批再加 1 个 → 该 agent 变成多实例，但已存在的实例
// 不改名（历史快照），新实例取池名。
func TestAssignMemberNames_ExistingPlusBatchIsMulti(t *testing.T) {
	specs := []GroupMemberSpec{{AgentID: "a"}}
	assignMemberNames(specs, map[string]int{"a": 1}, nil)
	if specs[0].DisplayName != groupNamePool[0] {
		t.Fatalf("new instance name = %q want %q", specs[0].DisplayName, groupNamePool[0])
	}
}

// 池被占用时跳到下一个未占用名字（多实例全部取池）。
func TestAssignMemberNames_PoolSkipsTaken(t *testing.T) {
	taken := map[string]bool{groupNamePool[0]: true}
	specs := []GroupMemberSpec{{AgentID: "a"}, {AgentID: "a"}}
	assignMemberNames(specs, nil, taken)
	if specs[0].DisplayName != groupNamePool[1] || specs[1].DisplayName != groupNamePool[2] {
		t.Fatalf("names = %q,%q want %q,%q",
			specs[0].DisplayName, specs[1].DisplayName, groupNamePool[1], groupNamePool[2])
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
	// 无注册表时 GetAgentDisplayName("a") 回退到 agent id 本身；多实例编号从 2 起。
	base := GetAgentDisplayName("a")
	want0 := base + " 2"
	want1 := base + " 3"
	if specs[0].DisplayName != want0 || specs[1].DisplayName != want1 {
		t.Fatalf("fallback = %q,%q want %q,%q", specs[0].DisplayName, specs[1].DisplayName, want0, want1)
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
