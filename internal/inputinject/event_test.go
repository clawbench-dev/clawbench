package inputinject

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLine_AcceptsPointerAndKeyboard(t *testing.T) {
	ev, err := ParseLine(`{"kind":"move","x":0.25,"y":0.75}`)
	require.NoError(t, err)
	assert.Equal(t, "move", ev.Kind)
	assert.InDelta(t, 0.25, ev.X, 1e-9)

	ev, err = ParseLine(`{"kind":"key","key":"Enter","modifiers":["ctrl"]}`)
	require.NoError(t, err)
	assert.Equal(t, "Enter", ev.Key)
	assert.Equal(t, []string{"ctrl"}, ev.Modifiers)
}

func TestParseLine_RejectsOutOfRangeAndUnknownKinds(t *testing.T) {
	// A coordinate outside 0..1 would fling the pointer to a screen edge.
	_, err := ParseLine(`{"kind":"move","x":1.4,"y":0.5}`)
	assert.Error(t, err)

	_, err = ParseLine(`{"kind":"teleport"}`)
	assert.Error(t, err)

	_, err = ParseLine(`{"kind":"key","key":"Nope"}`)
	assert.Error(t, err)

	_, err = ParseLine(`{"kind":"move","x":0.1,"y":0.1,"extra":1}`)
	assert.Error(t, err, "unknown fields must not be silently ignored")
}

func TestParseLine_TruncatesLongText(t *testing.T) {
	line := `{"kind":"text","text":"` + strings.Repeat("a", 900) + `"}`
	ev, err := ParseLine(line)
	require.NoError(t, err)
	assert.Len(t, []rune(ev.Text), 512)
}

func TestVirtualKey_MapsNamedAndSingleCharacters(t *testing.T) {
	cases := map[string]uint16{
		"enter":  0x0D,
		"ENTER":  0x0D,
		"esc":    0x1B,
		"f5":     0x74,
		"a":      0x41,
		"A":      0x41,
		"7":      0x37,
		"delete": 0x2E,
	}
	for name, want := range cases {
		got, ok := VirtualKey(name)
		assert.True(t, ok, name)
		assert.Equal(t, want, got, name)
	}

	_, ok := VirtualKey("not-a-key")
	assert.False(t, ok)
	_, ok = VirtualKey("")
	assert.False(t, ok)
}

func TestModifierKey(t *testing.T) {
	for _, name := range []string{"ctrl", "shift", "alt", "meta", "win"} {
		_, ok := ModifierKey(name)
		assert.True(t, ok, name)
	}
	_, ok := ModifierKey("hyper")
	assert.False(t, ok)
}
