package service

import "testing"

// 重复实例取池名，第一个实例保留本名；池按顺序取未占用者。
func TestAssignMemberNames_DuplicatesUsePool(t *testing.T) {
	specs := []GroupMemberSpec{
		{AgentID: "a"}, {AgentID: "a"}, {AgentID: "a"},
	}
	assignMemberNames(specs, nil, nil)
	want := []string{"", groupNamePool[0], groupNamePool[1]}
	// 第一个实例的 base 由 GetAgentDisplayName 决定（测试环境无注册表 → 空）
	// 这里只断言重复实例名来自池。
	if specs[1].DisplayName != want[1] || specs[2].DisplayName != want[2] {
		t.Fatalf("dup names = %q,%q want %q,%q", specs[1].DisplayName, specs[2].DisplayName, want[1], want[2])
	}
}

// 池被占用时跳到下一个未占用名字。
func TestAssignMemberNames_PoolSkipsTaken(t *testing.T) {
	taken := map[string]bool{groupNamePool[0]: true}
	specs := []GroupMemberSpec{{AgentID: "a"}, {AgentID: "a"}}
	assignMemberNames(specs, nil, taken)
	if specs[1].DisplayName != groupNamePool[1] {
		t.Fatalf("dup name = %q want %q", specs[1].DisplayName, groupNamePool[1])
	}
}

// 池里所有名字都被占用 → 回退本名 + 序号。
func TestAssignMemberNames_PoolExhaustedFallsBack(t *testing.T) {
	taken := map[string]bool{}
	for _, n := range groupNamePool {
		taken[n] = true
	}
	specs := []GroupMemberSpec{{AgentID: "a"}, {AgentID: "a"}}
	assignMemberNames(specs, nil, taken)
	// 无注册表时 GetAgentDisplayName("a") 回退到 agent id 本身，故为 "a 2"。
	want := GetAgentDisplayName("a") + " 2"
	if specs[1].DisplayName != want {
		t.Fatalf("fallback = %q want %q", specs[1].DisplayName, want)
	}
}
