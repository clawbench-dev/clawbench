package main

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

/**
 * The ACP permission callback must hand the GROUP timeline to EmitSessionEvent,
 * not the member row it is given.
 *
 * A group member owns its own ACP connection, keyed by the hidden MEMBER row id
 * (decision #61) — so the callback's `clawbenchSID` is a member row for group
 * turns. A member row has no title, is absent from the session list, and opening
 * it shows an empty standalone agent chat. That id feeds three UI-facing things
 * at once (the WS broadcast a browser/Android notification navigates on, the
 * pending-events replay buffer, and the notification log), so leaving it
 * unresolved made a group notification land on a session the user cannot use.
 *
 * This is a source guard rather than a behavioral test because the wiring is an
 * inline closure inside main(), which cannot be invoked in isolation.
 */
func TestPermissionCallbackResolvesMemberToGroup(t *testing.T) {
	src, err := os.ReadFile("main.go")
	require.NoError(t, err, "main.go must be readable from the package dir")
	text := string(src)

	// Locate the callback body (comment-tolerant: just find the wiring block).
	idx := strings.Index(text, "ai.SetPermissionStateChangeCallback(")
	require.NotEqual(t, -1, idx, "the permission callback must be wired")

	// Take a generous window after the call site; the closure is short.
	end := idx + 1200
	if end > len(text) {
		end = len(text)
	}
	block := text[idx:end]

	// The EmitSessionEvent argument must be wrapped in the resolver. Assert the
	// resolver is applied to the callback's id, and that the raw id is NOT passed
	// straight through (which is the regression).
	require.Regexp(t,
		regexp.MustCompile(`EmitSessionEvent\(\s*service\.ResolveGroupTimelineID\(`),
		block,
		"the callback must resolve a member row to its group before emitting")

	require.NotRegexp(t,
		regexp.MustCompile(`EmitSessionEvent\(\s*clawbenchSID\b`),
		block,
		"the raw member id must not be emitted (it navigates to a hidden session)")
}
