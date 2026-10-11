package service

import (
	"testing"

	"clawbench/internal/store"
)

// InitDB 必须为 chat_sessions 加上 agent_avatar 列（幂等）。
func TestInitDB_AddsAgentAvatarColumn(t *testing.T) {
	setupGroupDB(t)
	var cnt int
	if err := store.ReadDB().QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('chat_sessions') WHERE name='agent_avatar'",
	).Scan(&cnt); err != nil {
		t.Fatalf("pragma_table_info: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("agent_avatar column count = %d, want 1", cnt)
	}
}

// 重复调用 InitDB 不报错（守卫式迁移）。
func TestInitDB_AgentAvatarMigrationIdempotent(t *testing.T) {
	setupGroupDB(t)
	if err := InitDB(); err != nil {
		t.Fatalf("second InitDB: %v", err)
	}
}
