package handler

import (
	"net/http"
	"strconv"
	"testing"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A limit that is not a number is ignored rather than refused: the panel sends
// whatever the user typed into the box, and "show everything" is a valid answer.
func TestServeTerminalHistory_NonNumericLimitFallsBackToTheDefault(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	_, err := service.RecordTerminalCommand("git status", "")
	require.NoError(t, err)
	_, err = service.RecordTerminalCommand("ls -la", "")
	require.NoError(t, err)

	req := newRequest(t, http.MethodGet, "/api/terminal/history?limit=many", nil)
	w := callHandler(ServeTerminalHistory, req)
	assertOK(t, w)

	var items []service.TerminalCommand
	decodeRespJSON(t, w.Body, &items)
	assert.Len(t, items, 2, "an unparsable limit must not truncate or fail the list")

	// A negative limit is passed through as a positive page size (or the
	// service's own default): never an error, and never "zero rows".
	req = newRequest(t, http.MethodGet, "/api/terminal/history?limit=-5", nil)
	w = callHandler(ServeTerminalHistory, req)
	assertOK(t, w)
	decodeRespJSON(t, w.Body, &items)
	assert.NotNil(t, items)
}

// /history/sync only accepts POST; a GET is refused before the shell history
// files are read (so it cannot be used as an unauthenticated side channel that
// imports whatever is on disk).
func TestServeTerminalHistoryByID_SyncRejectsOtherMethods(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	for _, method := range []string{http.MethodGet, http.MethodDelete, http.MethodPut} {
		req := newRequest(t, method, "/api/terminal/history/sync", nil)
		w := callHandler(ServeTerminalHistoryByID, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code, method)
	}
}

// Deleting one entry only accepts DELETE; a GET (a mistyped route) must not
// delete anything.
func TestServeTerminalHistoryByID_DeleteRejectsOtherMethods(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	_, err := service.RecordTerminalCommand("git status", "")
	require.NoError(t, err)
	items, err := service.ListTerminalCommands("", "", 10)
	require.NoError(t, err)
	require.Len(t, items, 1)

	req := newRequest(t, http.MethodGet, "/api/terminal/history/"+strconv.FormatInt(items[0].ID, 10), nil)
	w := callHandler(ServeTerminalHistoryByID, req)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)

	items, err = service.ListTerminalCommands("", "", 10)
	require.NoError(t, err)
	assert.Len(t, items, 1, "a refused method must not delete the entry")
}

// An id of zero or a negative id is refused as a bad request: ids are positive
// sequence values, and 0 would address no row (a silent success the panel would
// show as "deleted").
func TestServeTerminalHistoryByID_RejectsNonPositiveIDs(t *testing.T) {
	_, teardown := setupTestEnv(t)
	defer teardown()

	for _, path := range []string{"0", "-3", ""} {
		req := newRequest(t, http.MethodDelete, "/api/terminal/history/"+path, nil)
		w := callHandler(ServeTerminalHistoryByID, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, "id %q", path)
	}
}

// SetTerminalManager(nil) only clears the manager: there is no manager whose
// recorder could be wired, and this must not panic.
func TestSetTerminalManager_NilClearsWithoutPanic(t *testing.T) {
	prev := GetTerminalManager()
	t.Cleanup(func() { SetTerminalManager(prev) })

	SetTerminalManager(nil)
	assert.Nil(t, GetTerminalManager())
}
