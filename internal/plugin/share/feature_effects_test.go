package share

import (
	"testing"

	"clawbench/internal/handlertest"

	"github.com/stretchr/testify/assert"
)

// 从 handler/feature_effects_shared_test.go 搬来：它测的是分享的关闭效果。
// handlertest.Setup 只建 projects 表，正好复现"缺 file_shares 的旧数据目录"。
func TestShareDisableEffect_ReportsAFailedRevocationAsAWarning(t *testing.T) {
	_, teardown := handlertest.Setup(t)
	t.Cleanup(teardown)

	warnings := ShareDisableEffect()
	assert.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "share disabled, but revoking existing links failed")
	assert.Contains(t, warnings[0], "file_shares")
}
