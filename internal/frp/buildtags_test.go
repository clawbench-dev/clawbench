package frp

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Guards the build-tag split that makes `nofrp` work.
//
// The mechanism is subtle: a build tag on manager.go is what keeps
// github.com/fatedier/frp out of the binary, and a build tag on
// manager_nofrp.go is what supplies the replacement. If either tag is dropped —
// or the stub accidentally gains the real import — the no-FRP build either
// fails to compile or silently links the very library it exists to exclude.
// Neither failure is visible from the other build variant, so the shape is
// asserted here as text, which runs under both.

// readSource returns the contents of a file in this package directory.
func readSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	require.NoError(t, err, "reading %s", name)
	return string(b)
}

// buildConstraint returns the leading //go:build line, or "" when the file has none.
func buildConstraint(src string) string {
	for _, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			if strings.HasPrefix(trimmed, "//go:build ") {
				return strings.TrimPrefix(trimmed, "//go:build ")
			}
			continue
		}
		// First non-comment line reached (package clause) — no constraint.
		break
	}
	return ""
}

// importBlock returns the contents of the file's `import ( ... )` block.
// Assertions about dependencies must be scoped to this block: the package doc
// comment names the library on purpose (to explain why the build tag exists),
// so a whole-file substring check would match prose rather than a real
// dependency.
//
// It FAILS the test when the block cannot be found rather than returning "".
// An empty string would make the stub's `NotContains` assertion pass
// vacuously — the guard would silently stop guarding exactly when the file it
// guards was reformatted.
func importBlock(t *testing.T, src string) string {
	t.Helper()
	_, rest, found := strings.Cut(src, "\nimport (")
	require.True(t, found, "no `import (` block found — the dependency guard below cannot run")
	block, _, found := strings.Cut(rest, "\n)")
	require.True(t, found, "unterminated `import (` block — the dependency guard below cannot run")
	return block
}

func TestBuildTagSplit_RealManagerExcludedFromNoFRP(t *testing.T) {
	src := readSource(t, "manager.go")
	assert.Equal(t, "!nofrp", buildConstraint(src),
		"manager.go must be excluded by the nofrp tag — it is the only file importing fatedier/frp")
	assert.Contains(t, importBlock(t, src), "github.com/fatedier/frp/client",
		"manager.go is expected to be the real implementation")
}

func TestBuildTagSplit_StubHasNoFRPDependency(t *testing.T) {
	src := readSource(t, "manager_nofrp.go")
	assert.Equal(t, "nofrp", buildConstraint(src),
		"manager_nofrp.go must only build under the nofrp tag")

	// The whole point: if the stub imports the library, the dependency survives
	// into the no-FRP binary and the detection this feature exists to avoid
	// comes right back.
	assert.NotContains(t, importBlock(t, src), "fatedier/frp",
		"the no-FRP stub must not import fatedier/frp — that would defeat the entire build tag")
}

func TestBuildTagSplit_TypesAreShared(t *testing.T) {
	// types.go carries the exported surface both variants compile against, so
	// it must be unconditional. A tag here would leave one variant without
	// State/Status and break the build.
	src := readSource(t, "types.go")
	assert.Empty(t, buildConstraint(src), "types.go must build under every tag combination")
	assert.NotContains(t, importBlock(t, src), "fatedier/frp")
}

func TestBuildTagSplit_TestFilesMatchTheirSubject(t *testing.T) {
	// Tests must follow their subject: manager_test.go drives the real
	// implementation (and imports the library), while the stub's tests only
	// make sense in the no-FRP build.
	assert.Equal(t, "!nofrp", buildConstraint(readSource(t, "manager_test.go")))
	assert.Equal(t, "nofrp", buildConstraint(readSource(t, "manager_nofrp_test.go")))

	// The shared-helper tests must run in both builds, or the no-FRP variant
	// would compile without being exercised.
	assert.Empty(t, buildConstraint(readSource(t, "types_test.go")))
}

func TestBuildTagSplit_AvailableExistsInBothVariants(t *testing.T) {
	// Available() is the signal the handler reads to hide the settings panel.
	// It must be defined in exactly one file per variant — a missing definition
	// breaks the build, a duplicate breaks it differently — so assert each
	// variant defines it and neither imports the library.
	realSrc := readSource(t, "manager.go")
	stub := readSource(t, "manager_nofrp.go")
	assert.Contains(t, realSrc, "func Available() bool { return true }")
	assert.Contains(t, stub, "func Available() bool { return false }")
}
