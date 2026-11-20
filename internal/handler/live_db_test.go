package handler

import (
	"database/sql"
	"testing"

	"clawbench/internal/store"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// setupLiveNotesDB 造一个只有 projects 表的内存库，用来复现"旧版本数据目录
// 缺表"的场景（关闭功能的清理会失败并报告 warning）。
//
// 它原先定义在会议纪要的测试文件里，随会议纪要搬走了；handler 里仍有不依赖
// 会议纪要、只需要一个缺表库的用例（分享关闭时的撤销失败）。
func setupLiveNotesDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	_, err = db.Exec(store.ProjectsDDL)
	require.NoError(t, err)

	restore := store.SetDBForTest(db, db)
	t.Cleanup(func() {
		restore()
		_ = db.Close()
	})
	return db
}
