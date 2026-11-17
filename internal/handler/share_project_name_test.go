package handler

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

// withShareProjectNamePolicy pins the share.show_project_name switch for one
// test. It is a package global (model.FeaturePolicy), so leaving it set would
// leak into every later test that renders a share page.
func withShareProjectNamePolicy(t *testing.T, on bool) {
	t.Helper()
	prev := model.FeaturePolicy.ShareShowProjectName
	t.Cleanup(func() { model.FeaturePolicy.ShareShowProjectName = prev })
	model.FeaturePolicy.ShareShowProjectName = on
}

// TestShareProjectName_ObeysTheOptOut pins the privacy switch itself. The source
// project's directory name is published on a public page (og:site_name and the
// noscript block), so "off" has to mean an empty string — never the basename of
// the path by accident.
func TestShareProjectName_ObeysTheOptOut(t *testing.T) {
	withShareProjectNamePolicy(t, false)
	require.Empty(t, shareProjectName("/home/alice/work/super-secret-project"),
		"the switch must win over the path")

	withShareProjectNamePolicy(t, true)
	require.Equal(t, "super-secret-project", shareProjectName("/home/alice/work/super-secret-project"))
	// Trailing separators and "." are cleaned first, so the label is the real
	// directory and not "" or "/".
	require.Equal(t, "super-secret-project", shareProjectName("/home/alice/work/super-secret-project/"))
	require.Empty(t, shareProjectName(""), "an unknown project publishes nothing")
}

// TestShareProjectNameForLiveRoom is the path a RUNNING meeting's share page
// takes. It used to hand out Room.ProjectKey() raw, which published the project
// directory name to anyone holding the link even after the owner opted out.
func TestShareProjectNameForLiveRoom(t *testing.T) {
	resetLiveRegistry(t)
	withShareProjectNamePolicy(t, true)

	room, err := liveRegistry.Create("/home/alice/work/live-project", "周会")
	require.NoError(t, err)
	require.Equal(t, "live-project", shareProjectNameForLiveRoom(room))

	withShareProjectNamePolicy(t, false)
	require.Empty(t, shareProjectNameForLiveRoom(room),
		"opting out must hide the project name from the live share page too")

	require.Empty(t, shareProjectNameForLiveRoom(nil), "no room publishes nothing")
}
