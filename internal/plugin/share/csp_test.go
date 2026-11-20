package share

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 从 handler/file_test.go 搬来：它验证分享 doc 的 CSP 必须带 sandbox 且不带
// allow-same-origin（否则共享的 .html 能在同源下执行脚本）。

// Every response from the /doc/ endpoint must carry the sandbox CSP, and that
// CSP must never grant same-origin. A shared document that could reach the
// reader's origin would run with their session — the exact XSS the separate
// MIME table above exists to prevent.
func TestShareDocSandboxCSP_IsSandboxedWithoutSameOrigin(t *testing.T) {
	assert.Contains(t, shareDocSandboxCSP, "sandbox")
	assert.Contains(t, shareDocSandboxCSP, "allow-scripts")
	assert.NotContains(t, shareDocSandboxCSP, "allow-same-origin")
}
