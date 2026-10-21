//go:build integration

package ai

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCodebuddyACP_ChunkMetaIdentityProbe answers ONE question with real wire
// data: do CodeBuddy's model-output chunks carry the identity pair our
// duplicate-delivery guard keys on?
//
// The guard (internal/ai/acp_dup_chunk.go) drops a repeated chunk only when it
// can build a key from `_meta["codebuddy.ai/messageId"]` AND `_meta["timestamp"]`.
// If the agent omits either, every key is empty and the guard is a no-op — the
// production duplication would persist silently. This probe measures that
// directly instead of trusting the bundle read.
//
// Run:
//
//	go test -v -run 'TestCodebuddyACP_ChunkMetaIdentityProbe' -tags integration \
//	    -timeout 300s ./internal/ai/
//
// Requires a locally installed and logged-in `codebuddy` CLI.
func TestCodebuddyACP_ChunkMetaIdentityProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	rec, _, _, _, notifications, promptErr := acpMetadataProbe(
		t, ctx, []string{"codebuddy", "--acp"},
		"用一句话说明 Go 的 goroutine 和线程的区别。",
	)
	require.NoError(t, promptErr, "prompt should complete")
	require.NotEmpty(t, notifications, "probe must capture notifications")

	var textChunks, thinkChunks int
	var textWithMsgID, textWithTS, textWithBoth int
	var thinkWithMsgID, thinkWithTS, thinkWithBoth int
	var sampleKeys []string

	for _, n := range notifications {
		var variant string
		var meta map[string]any
		switch {
		case n.Update.AgentMessageChunk != nil:
			variant = "agent_message_chunk"
			meta = n.Update.AgentMessageChunk.Meta
			textChunks++
		case n.Update.AgentThoughtChunk != nil:
			variant = "agent_thought_chunk"
			meta = n.Update.AgentThoughtChunk.Meta
			thinkChunks++
		default:
			continue
		}
		msgID := metaString(meta[metaKeyCodeBuddyMessageID])
		ts := metaString(meta[metaKeyCodeBuddyTimestamp])

		switch variant {
		case "agent_message_chunk":
			if msgID != "" {
				textWithMsgID++
			}
			if ts != "" {
				textWithTS++
			}
			if msgID != "" && ts != "" {
				textWithBoth++
			}
		case "agent_thought_chunk":
			if msgID != "" {
				thinkWithMsgID++
			}
			if ts != "" {
				thinkWithTS++
			}
			if msgID != "" && ts != "" {
				thinkWithBoth++
			}
		}

		if len(sampleKeys) < 3 && len(meta) > 0 {
			raw, _ := json.Marshal(meta)
			sampleKeys = append(sampleKeys, variant+" meta="+string(raw))
		}
	}

	t.Logf("=== chunk identity coverage ===")
	t.Logf("agent_message_chunk: %d chunks | messageId=%d timestamp=%d BOTH=%d",
		textChunks, textWithMsgID, textWithTS, textWithBoth)
	t.Logf("agent_thought_chunk: %d chunks | messageId=%d timestamp=%d BOTH=%d",
		thinkChunks, thinkWithMsgID, thinkWithTS, thinkWithBoth)
	for _, s := range sampleKeys {
		t.Logf("  sample %s", s)
	}

	// The guard is only real when the identity pair is present. Assert it on the
	// text chunks (the ones that carry the user-visible duplication).
	if textChunks > 0 {
		require.Equal(t, textChunks, textWithBoth,
			"every agent_message_chunk must carry messageId AND timestamp, or the duplicate-chunk guard cannot key on it")
	}
	if thinkChunks > 0 {
		require.Equal(t, thinkChunks, thinkWithBoth,
			"every agent_thought_chunk must carry messageId AND timestamp, or the duplicate-chunk guard cannot key on it")
	}

	// Raw wire sanity: the key must appear as a literal on the wire, so this is
	// not an artefact of the typed decode.
	var rawHits int
	for _, m := range rec.fromAgent {
		var env struct {
			Params struct {
				Update struct {
					SessionUpdate string         `json:"sessionUpdate"`
					Meta          map[string]any `json:"_meta"`
				} `json:"update"`
			} `json:"params"`
		}
		if json.Unmarshal(m, &env) != nil {
			continue
		}
		if env.Params.Update.SessionUpdate != "agent_message_chunk" &&
			env.Params.Update.SessionUpdate != "agent_thought_chunk" {
			continue
		}
		if _, ok := env.Params.Update.Meta["timestamp"]; ok {
			rawHits++
		}
	}
	t.Logf("raw wire chunks carrying _meta.timestamp: %d", rawHits)
	require.Greater(t, rawHits, 0, "_meta.timestamp must be present on the raw wire")
}
