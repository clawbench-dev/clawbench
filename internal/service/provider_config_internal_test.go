package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The "is this our value" test decides whether the key may be restored or
// dropped at all, so every shape a shared file can produce has to be answered
// correctly: a false positive deletes another tool's provider, a false negative
// leaves the key pointing at a table the strip already removed.
func TestTomlValueIs(t *testing.T) {
	cases := []struct {
		raw  string
		want bool
	}{
		{`"clawbench"`, true},
		{`'clawbench'`, true},
		{`clawbench`, true},
		{`  "clawbench"  `, true},
		{`"clawbench" # note`, true},
		{`"clawbench"# note`, true},
		{`"clawbenchX"`, false},
		{`"clawbench "`, false},
		{`"Clawbench"`, false},
		{`"clawbench#1"`, false},
		{`""`, false},
		{``, false},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, tomlValueIs(c.raw, "clawbench"), "raw=%q", c.raw)
	}
}
