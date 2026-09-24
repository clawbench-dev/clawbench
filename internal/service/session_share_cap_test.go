package service_test

import (
	"encoding/json"
	"strings"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionSharePayload_EnforcesSizeCap is the regression guard for a trimmer
// that never trimmed.
//
// trimPayloadToCap decoded each message body into a NEW wrapper map, mutated
// block["output"] there, then re-marshaled the payload — whose Content is a
// string, so the mutation was discarded and the returned bytes were byte-for-byte
// identical to the input (measured 18874595 in, 18874595 out against a 16 MiB
// cap). The truncated flag was discarded the same way, so the viewer's
// truncation notice could never appear. The function had no test coverage at
// all, which is why it shipped.
//
// The payload is built directly rather than through the DB: the cap applies to
// the marshaled bytes, and seeding 18 MiB through chat_history would only make
// the test slower without exercising anything more.
func TestSessionSharePayload_EnforcesSizeCap(t *testing.T) {
	huge := strings.Repeat("x", 18<<20) // 18 MiB, comfortably over the cap
	body, err := json.Marshal(map[string]any{
		"blocks": []any{
			map[string]any{"type": "tool_use", "id": "t1", "name": "Read", "output": huge},
		},
	})
	require.NoError(t, err)

	payload := &service.SessionSharePayload{
		Version: 1,
		Messages: []service.SessionShareMessage{
			{ID: 1, Role: "assistant", Content: string(body)},
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.Greater(t, len(raw), 16<<20, "precondition: the fixture must start over the cap")

	out := service.TrimPayloadToCapForTest(raw, payload)

	// The stored snapshot must fit, or an anonymous caller can pull an
	// arbitrarily large body from a single share link.
	assert.LessOrEqual(t, len(out), 16<<20, "the cap must be enforced")
	assert.NotEqual(t, len(raw), len(out), "the trimmer must actually change the bytes")

	// The trim must land in the STORED body, not a discarded copy — this is what
	// the old code got wrong. Re-parse the output and check the marker survived.
	var back service.SessionSharePayload
	require.NoError(t, json.Unmarshal(out, &back))
	require.Len(t, back.Messages, 1)
	assert.Contains(t, back.Messages[0].Content, "truncated for sharing",
		"the truncation marker must be present in the stored body")

	blocks := blocksOf(t, back.Messages[0].Content)
	require.NotEmpty(t, blocks)
	assert.Equal(t, true, blocks[0]["truncated"],
		"the block must be flagged so the viewer can say the content was cut")
}

// A payload under the cap must be returned untouched — the trimmer must not
// rewrite healthy snapshots (re-encoding would waste CPU and could reorder keys).
func TestSessionSharePayload_UnderCapIsUntouched(t *testing.T) {
	payload := &service.SessionSharePayload{
		Version: 1,
		Messages: []service.SessionShareMessage{
			{ID: 1, Role: "assistant", Content: `{"blocks":[{"type":"tool_use","output":"small"}]}`},
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)

	out := service.TrimPayloadToCapForTest(raw, payload)
	assert.Equal(t, string(raw), string(out), "an under-cap payload must be returned as-is")
}

// A payload over the cap whose size comes from untrimmable content (long prose,
// many small blocks) is stored as-is rather than gutted: degrading a share beats
// destroying the content the user chose to share.
func TestSessionSharePayload_OverCapWithoutToolOutputIsKept(t *testing.T) {
	payload := &service.SessionSharePayload{
		Version: 1,
		Messages: []service.SessionShareMessage{
			{ID: 1, Role: "user", Content: strings.Repeat("prose ", 3<<20)},
		},
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.Greater(t, len(raw), 16<<20, "precondition")

	out := service.TrimPayloadToCapForTest(raw, payload)
	assert.Equal(t, string(raw), string(out),
		"nothing is trimmable here, so the content must be preserved")
}

// blocksOf decodes a message body's blocks.
func blocksOf(t *testing.T, content string) []map[string]any {
	t.Helper()
	var wrapper struct {
		Blocks []map[string]any `json:"blocks"`
	}
	require.NoError(t, json.Unmarshal([]byte(content), &wrapper))
	return wrapper.Blocks
}
