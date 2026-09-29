package service

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"clawbench/internal/model"
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

// 守护台账读取的错误分支：读失败必须当作「未应用」，否则一次瞬时错误
// （表缺失、连接抖动等）会让迁移被永久跳过。
func TestIsMigrationApplied_ReturnsFalseOnReadError(t *testing.T) {
	d, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	d.SetMaxOpenConns(1)
	restore := SetDBForTest(d, d)
	t.Cleanup(func() {
		restore()
		_ = d.Close()
	})

	// 不建表（若已存在则删掉），使 SELECT COUNT(*) FROM schema_migrations 报错。
	_, err = d.Exec("DROP TABLE IF EXISTS schema_migrations")
	require.NoError(t, err)

	assert.False(t, isMigrationApplied("anything"),
		"read error must not be treated as applied, or a transient failure would permanently skip the migration")
}

func TestRunOnce_DistinctNamesAreIndependent(t *testing.T) {
	setupTestDBForMigrations(t)

	runOnce("test_migration_c", func() bool { return true })
	assert.True(t, isMigrationApplied("test_migration_c"))
	assert.False(t, isMigrationApplied("test_migration_d"))
}

// TestInitDB_DataMigrationsAreGatedByLedger 走真实 InitDB：
// 第一次应转换旧格式数据并记账；清掉台账后第二次应重跑。
func TestInitDB_DataMigrationsAreGatedByLedger(t *testing.T) {
	dir := t.TempDir()
	model.DataDir = dir

	// 第一次启动：空库，4 个迁移无待转换行 → 全部记账。
	require.NoError(t, InitDB(true))
	for _, name := range dataMigrationNames {
		assert.True(t, isMigrationApplied(name), "空库首启也应记账: %s", name)
	}

	// 清台账 = 模拟「尚未应用」。
	ResetSchemaMigrationsForTest()
	for _, name := range dataMigrationNames {
		assert.False(t, isMigrationApplied(name))
	}

	// 第二次启动：应重跑（空库仍是 no-op，但必须再次记账）。
	require.NoError(t, InitDB(true))
	for _, name := range dataMigrationNames {
		assert.True(t, isMigrationApplied(name), "清台账后应重新记账: %s", name)
	}
}
