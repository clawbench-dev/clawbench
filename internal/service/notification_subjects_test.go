package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNotificationSubjects_EveryKindRegisteredOnce is the guard that makes the
// registry the single per-kind extension point.
//
// Adding a NotificationKind* constant without an entry here would leave the
// kind unprobed (its rows would never be marked stale) and would make the
// writer log an "unregistered kind" warning. The test's literal list is the
// only way to enumerate constants in Go, so adding a kind intentionally fails
// here until it is declared — that is the point, not a nuisance.
func TestNotificationSubjects_EveryKindRegisteredOnce(t *testing.T) {
	kinds := []string{
		NotificationKindSession,
		NotificationKindTask,
		NotificationKindForge,
		NotificationKindUsage,
		NotificationKindMeeting,
		NotificationKindMail,
	}

	seen := map[string]bool{}
	for _, spec := range notificationSubjects {
		assert.NotEmpty(t, spec.kind, "registry entry with no kind")
		assert.False(t, seen[spec.kind], "duplicate registry entry for %q", spec.kind)
		seen[spec.kind] = true
	}
	for _, kind := range kinds {
		_, ok := notificationSubjectFor(kind)
		assert.True(t, ok, "kind %q has no entry in notificationSubjects", kind)
	}
	assert.Len(t, notificationSubjects, len(kinds),
		"registry declares a kind missing from this test's list")
}

// TestNotificationSubjects_SourceDeclaration pins which kinds can go stale.
// Forge items live upstream and a usage threshold is an aggregate, so neither
// may acquire a probe by accident.
func TestNotificationSubjects_SourceDeclaration(t *testing.T) {
	for _, kind := range []string{NotificationKindForge, NotificationKindUsage} {
		spec, ok := notificationSubjectFor(kind)
		assert.True(t, ok, "%q must be registered", kind)
		assert.False(t, spec.hasSource(), "%q must declare no local source", kind)
	}
	for _, kind := range []string{
		NotificationKindSession, NotificationKindTask,
		NotificationKindMeeting, NotificationKindMail,
	} {
		spec, ok := notificationSubjectFor(kind)
		assert.True(t, ok, "%q must be registered", kind)
		assert.True(t, spec.hasSource(), "%q must declare a source", kind)
	}
}

// TestMarkMissingSources_UnknownKindIsNeverStale covers a row written by a
// newer build (or a typo'd kind): it must stay clickable rather than be marked
// "source deleted" by a probe that does not know what its id means.
func TestMarkMissingSources_UnknownKindIsNeverStale(t *testing.T) {
	cleanup := notificationsTestDB(t)
	defer cleanup()

	rows := []Notification{{ID: 1, Kind: "plugin_new", SessionID: "gone"}}
	MarkMissingSources(rows)

	assert.False(t, rows[0].SourceMissing)
}
