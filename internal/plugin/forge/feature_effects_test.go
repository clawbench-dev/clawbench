package forge

import (
	"testing"

	"clawbench/internal/handlertest"

	"github.com/stretchr/testify/assert"
)

// 从 handler/feature_effects_shared_test.go 搬来：它测的是 forge 的关闭效果。
//
// handlertest.Setup 只建 projects 表，正好复现"缺 project_forges 的旧数据目录"
// —— 那正是这个分支会跑的场合。
func TestForgeDisableEffect_ReportsAFailedCleanupAsAWarning(t *testing.T) {
	_, teardown := handlertest.Setup(t)
	t.Cleanup(teardown)

	warnings := ForgeDisableEffect()
	assert.Len(t, warnings, 1, "a failed cleanup must be reported, not swallowed")
	assert.Contains(t, warnings[0], "forge disabled, but clearing repository bindings failed")
	assert.Contains(t, warnings[0], "project_forges", "the warning names the table that broke")
}
