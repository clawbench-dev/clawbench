package service

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupTestDBForMigrations 建一个内存库，装上 schema_migrations 表，并把
// 包级 db/dbRead 指过去（runOnce 读 dbRead、写 db）。
func setupTestDBForMigrations(t *testing.T) {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	// 内存库是 per-connection 的，必须限制为单连接。
	d.SetMaxOpenConns(1)
	restore := SetDBForTest(d, d)
	t.Cleanup(func() {
		restore()
		_ = d.Close()
	})
	require.NoError(t, ensureSchemaMigrationsTable())
}

func TestRunOnce_ExecutesOnlyOnce(t *testing.T) {
	setupTestDBForMigrations(t)

	runs := 0
	fn := func() bool { runs++; return true }

	runOnce("test_migration_a", fn)
	runOnce("test_migration_a", fn)

	assert.Equal(t, 1, runs, "第二次调用必须被台账拦住")
	assert.True(t, isMigrationApplied("test_migration_a"))
}

func TestRunOnce_FailedMigrationIsRetried(t *testing.T) {
	setupTestDBForMigrations(t)

	runs := 0
	runOnce("test_migration_b", func() bool { runs++; return false })

	assert.Equal(t, 1, runs)
	assert.False(t, isMigrationApplied("test_migration_b"),
		"未完成的迁移不得记账，下次启动必须重试")
}

func TestRunOnce_DistinctNamesAreIndependent(t *testing.T) {
	setupTestDBForMigrations(t)

	runOnce("test_migration_c", func() bool { return true })
	assert.True(t, isMigrationApplied("test_migration_c"))
	assert.False(t, isMigrationApplied("test_migration_d"))
}
