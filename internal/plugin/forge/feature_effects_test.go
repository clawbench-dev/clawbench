package forge

import (
	"testing"

	"clawbench/internal/handlertest"
	"clawbench/internal/service"
	"github.com/stretchr/testify/require"

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

// TestForgeDisableEffect_ClearsBindings pins the success path: a bound repo is
// actually removed, and nothing is reported as a warning. The warning path is
// covered above; without this test "off" could quietly leave the binding in
// place while still returning no warnings.
func TestForgeDisableEffect_ClearsBindings(t *testing.T) {
	env, teardown := setupTestEnv(t)
	t.Cleanup(teardown)

	require.NoError(t, service.UpsertProjectForge(service.ProjectForge{
		ProjectPath: env.ProjectDir,
		Platform:    "github",
		Host:        "github.com",
		Scheme:      "https",
		Owner:       "acme",
		Repo:        "widget",
		Source:      "manual",
	}))

	warnings := ForgeDisableEffect()
	assert.Empty(t, warnings, "a successful cleanup must not report a warning")

	pf, err := service.GetProjectForge(env.ProjectDir)
	require.NoError(t, err)
	assert.Nil(t, pf, "the binding must be gone after disabling the feature")
}
