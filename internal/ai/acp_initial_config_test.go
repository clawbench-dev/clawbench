package ai

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selectOption builds just the parts initialConfigSends reads (id + current
// value); the value list is irrelevant to the decision.
func selectOption(id, current string) acp.SessionConfigOption {
	return acp.SessionConfigOption{Select: &acp.SessionConfigOptionSelect{
		Id:           acp.SessionConfigId(id),
		Name:         id,
		CurrentValue: acp.SessionConfigValueId(current),
	}}
}

// The startup default is a correction, not a write-through: only an option the
// agent advertised AND whose current value differs may cost a round trip.
func TestInitialConfigSends(t *testing.T) {
	desired := map[string]string{"autopilot": "off", "never-advertised": "x"}

	sends := initialConfigSends(desired, []acp.SessionConfigOption{
		selectOption("autopilot", "on"),
	})
	require.Len(t, sends, 1, "only the advertised, differing option is sent")
	assert.Equal(t, "autopilot", sends[0].configID)
	assert.Equal(t, "off", sends[0].value)
}

func TestInitialConfigSends_SkipsAlreadyCorrect(t *testing.T) {
	sends := initialConfigSends(
		map[string]string{"autopilot": "off"},
		[]acp.SessionConfigOption{selectOption("autopilot", "off")},
	)
	assert.Empty(t, sends, "a session that already starts in the wanted state costs no RPC")
}

// An unauthenticated kiro advertises almost nothing; sending the option anyway
// would answer -32602 on every new session.
func TestInitialConfigSends_SkipsUnadvertised(t *testing.T) {
	sends := initialConfigSends(
		map[string]string{"autopilot": "off"},
		[]acp.SessionConfigOption{selectOption("mode", "vibe")},
	)
	assert.Empty(t, sends)
}

// Boolean-shaped options carry their value as a bool; the matcher must compare
// the same string form the rest of the config path uses.
func TestInitialConfigSends_BooleanOption(t *testing.T) {
	opt := acp.SessionConfigOption{Boolean: &acp.SessionConfigOptionBoolean{
		Id:           "fast-mode",
		CurrentValue: true,
	}}
	sends := initialConfigSends(map[string]string{"fast-mode": "false"}, []acp.SessionConfigOption{opt})
	require.Len(t, sends, 1)
	assert.Equal(t, "false", sends[0].value)

	sends = initialConfigSends(map[string]string{"fast-mode": "true"}, []acp.SessionConfigOption{opt})
	assert.Empty(t, sends, "an option already true is left alone")
}

func TestInitialConfigSends_EmptyDeclaration(t *testing.T) {
	assert.Empty(t, initialConfigSends(nil, []acp.SessionConfigOption{selectOption("autopilot", "on")}))
}
