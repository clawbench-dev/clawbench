package handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The disable hooks run AFTER the switch was persisted, so a cleanup failure is
// reported as a WARNING string rather than an error status: answering 500 would
// make the client roll the switch back to "on" while the server is already off.
//
// The failure is produced with a database that has the project registry but none
// of the tables the cleanup touches — a data dir from an older build, which is
// exactly when this branch runs.
func TestForgeDisableEffect_ReportsAFailedCleanupAsAWarning(t *testing.T) {
	db := setupLiveNotesDB(t) // projects + notifications only
	defer func() { _ = db.Close() }()

	warnings := ForgeDisableEffect()
	assert.Len(t, warnings, 1, "a failed cleanup must be reported, not swallowed")
	assert.Contains(t, warnings[0], "forge disabled, but clearing repository bindings failed")
	assert.Contains(t, warnings[0], "project_forges", "the warning names the table that broke")
}

// Turning sharing off revokes every issued capability link; with the link tables
// missing the revocation fails and says which one.
func TestShareDisableEffect_ReportsAFailedRevocationAsAWarning(t *testing.T) {
	db := setupLiveNotesDB(t)
	defer func() { _ = db.Close() }()

	warnings := ShareDisableEffect()
	assert.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "share disabled, but revoking existing links failed")
	assert.Contains(t, warnings[0], "file_shares")
}
