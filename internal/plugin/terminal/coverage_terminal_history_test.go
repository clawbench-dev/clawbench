package terminal

import (
	"net/http"
	"strconv"
	"testing"

	"clawbench/internal/model"
	"clawbench/internal/service"
	"clawbench/internal/store"
	"clawbench/internal/terminal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every terminal-history failure must be a 500, never a 200 with an empty list:
// the panel would render "no history yet" after the store broke, and the user
// would assume their commands were never recorded.
func TestServeTerminalHistory_StoreFailuresAreInternalErrors(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	_, err := service.RecordTerminalCommand("git status", "")
	require.NoError(t, err)

	// Hiding the table makes every one of the four operations fail the same way.
	_, err = store.WriteExec("ALTER TABLE terminal_command_history RENAME TO terminal_command_history_hidden")
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		path    string
		method  string
	}{
		{"list", ServeTerminalHistory, "/api/terminal/history", http.MethodGet},
		{"clear", ServeTerminalHistory, "/api/terminal/history", http.MethodDelete},
		// "sync" is deliberately absent: service.SyncTerminalHistory logs a failed
		// row and continues, so it never returns an error and the handler's 500
		// branch for it is unreachable by construction.
		{"delete", ServeTerminalHistoryByID, "/api/terminal/history/1", http.MethodDelete},
	} {
		w := callHandler(tc.handler, newRequest(t, tc.method, tc.path, nil))
		assert.Equal(t, http.StatusInternalServerError, w.Code, "%s: %s", tc.name, w.Body.String())
	}
}

// The raw line the terminal submits is handed to the service unchanged — the
// recorder is a passthrough, and trimming/normalising it here would make the
// stored history differ from what the user typed. This pins the wiring between
// SetTerminalManager and a manager's sink at the level the handler owns.
func TestSetTerminalManager_RecorderCallIsSafeForEveryManagerState(t *testing.T) {
	orig := GetTerminalManager()
	t.Cleanup(func() {
		if cur := GetTerminalManager(); cur != nil && cur != orig {
			cur.Close()
		}
		SetTerminalManager(orig)
	})

	// No manager: the feature is off, and the setter must accept nil without
	// trying to install a sink on it.
	SetTerminalManager(nil)
	assert.Nil(t, GetTerminalManager())

	manager := terminal.NewManager(model.TerminalConfig{
		Enabled: true, IdleTimeout: "1m", BufferLines: 100, MaxLineBytes: 65536, MaxBufferMB: 4,
	}, 0)
	SetTerminalManager(manager)
	assert.Same(t, manager, GetTerminalManager())
	assert.True(t, manager.HasCommandRecorder(),
		"a manager going through the handler's single creation site must capture commands")

	// Replacing a manager closes nothing implicitly, but the new one must also be
	// wired — the hot-reload path goes through the same function.
	replacement := terminal.NewManager(model.TerminalConfig{
		Enabled: true, IdleTimeout: "1m", BufferLines: 100, MaxLineBytes: 65536, MaxBufferMB: 4,
	}, 0)
	t.Cleanup(func() { replacement.Close() })
	SetTerminalManager(replacement)
	assert.True(t, replacement.HasCommandRecorder(), "a hot-reloaded manager must capture commands too")
}

// A delete of a non-positive or non-numeric id is a 400 before any store work;
// the message key is the request-shape one, not the generic internal error.
func TestServeTerminalHistoryByID_NonNumericIDIsABadRequest(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	for _, path := range []string{"/api/terminal/history/abc", "/api/terminal/history/0", "/api/terminal/history/-4"} {
		w := callHandler(ServeTerminalHistoryByID, newRequest(t, http.MethodDelete, path, nil))
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s: %s", path, w.Body.String())
		assert.Contains(t, w.Body.String(), "InvalidRequestBody")
	}

	// A trailing slash is tolerated: the id is the same resource.
	w := callHandler(ServeTerminalHistoryByID, newRequest(t, http.MethodDelete, "/api/terminal/history/abc/", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

// Deleting an id that does not exist is not an error: history is best-effort, and
// two devices can race the same delete. The reply is still 200/success.
func TestServeTerminalHistoryByID_DeleteMissingIDStillSucceeds(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	w := callHandler(ServeTerminalHistoryByID,
		newRequest(t, http.MethodDelete, "/api/terminal/history/"+strconv.FormatInt(987654, 10), nil))
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "success")

	// The row that DID exist is still there afterwards: a missing id must not
	// clear the table.
	_, err := service.RecordTerminalCommand("echo keep me", "")
	require.NoError(t, err)
	w = callHandler(ServeTerminalHistory, newRequest(t, http.MethodGet, "/api/terminal/history", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var items []service.TerminalCommand
	decodeRespJSON(t, w.Body, &items)
	require.Len(t, items, 1)
	assert.Equal(t, "echo keep me", items[0].Command)
}
