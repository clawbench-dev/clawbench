package handler

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestKBSearchPrecheck pins the three ways the /cb-kbsearch command can be
// refused. Each one leaves the AI without anything to search, so the cost of
// getting them wrong is either a pointless prompt (injecting a template for a
// query that does not exist) or a dead feature (refusing a command that works).
func TestClawbenchCommandPrecheck_KBSearchBranches(t *testing.T) {
	run := func(t *testing.T, rawMsg string) int {
		t.Helper()
		req := newRequest(t, http.MethodPost, "/api/ai/chat", nil)
		w := callHandler(func(w http.ResponseWriter, r *http.Request) {
			if !clawbenchCommandPrecheck(w, r, rawMsg) {
				return
			}
			w.WriteHeader(http.StatusOK)
		}, req)
		return w.Code
	}

	// Feature off: 503, so the panel can say "knowledge base is disabled"
	// instead of forwarding a command that will find nothing.
	require.Equal(t, http.StatusServiceUnavailable, run(t, "/cb-kbsearch 报销"))

	setupKBHandlerTest(t)

	// Bare command: nothing to search for.
	require.Equal(t, http.StatusBadRequest, run(t, "/cb-kbsearch"),
		"a bare command must be refused, not injected with an empty query")
	require.Equal(t, http.StatusBadRequest, run(t, "/cb-kbsearch   "))

	// Enabled + a query: allowed.
	require.Equal(t, http.StatusOK, run(t, "/cb-kbsearch 报销流程"))

	// A different command is unaffected by the KB switch.
	require.Equal(t, http.StatusOK, run(t, "/cb-task daily"))
}

// TestProcessClawbenchCommand_KBSearchBareReturnsRaw: on the queue-drain path
// the precheck never runs, so the renderer itself has to leave a bare command
// alone. Returning the template would tell the AI to call a search endpoint with
// no query.
func TestProcessClawbenchCommand_KBSearchBareReturnsRaw(t *testing.T) {
	withServerPort(t, 20000)
	out, err := processClawbenchCommand("/cb-kbsearch", "/project", "s1")
	require.NoError(t, err)
	require.Equal(t, "/cb-kbsearch", out)

	out, err = processClawbenchCommand("/cb-kbsearch  ", "/project", "s1")
	require.NoError(t, err)
	require.Equal(t, "/cb-kbsearch  ", out)

	// With a query every placeholder is substituted — an unsubstituted
	// {{AI_TOKEN}} would make the AI authenticate with the literal string.
	out, err = processClawbenchCommand("/cb-kbsearch 报销流程", "/project", "s1")
	require.NoError(t, err)
	require.Contains(t, out, "POST /api/kb/search")
	require.NotContains(t, out, "{{", "every placeholder must be replaced")
	require.Contains(t, out, "clawbench_project=/project")
}
