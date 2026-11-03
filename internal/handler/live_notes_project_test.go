package handler

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The regeneration tables are keyed by token, not project_id. A project delete
// must still take them: they are not reachable from anywhere else once the
// live_notes row is gone.
func TestDeleteProjectData_ClearsRegenerationState(t *testing.T) {
	env, teardown := setupTestEnv(t)
	defer teardown()

	project := filepath.Join(env.WatchDir, "regen-proj")
	require.NoError(t, os.MkdirAll(project, 0o755))
	id := store.ProjectIDForTest(t, project)
	token := "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, id, "周会", 1_700_000_000_000))
	_, err := service.ClaimArtifactJob(token, []service.ArtifactKind{service.ArtifactMinutes}, time.Now().UnixMilli())
	require.NoError(t, err)
	require.NoError(t, service.RecordLiveNoteAudioChannels(token, []service.LiveNoteAudioChannel{
		{Channel: "mic", File: "mic.webm", BaseAt: 1_700_000_000_000},
	}))

	require.NoError(t, service.DeleteProjectData(id))

	_, exists, err := service.GetLiveNote(token)
	require.NoError(t, err)
	assert.False(t, exists)

	channels, err := service.ListLiveNoteAudioChannels(token)
	require.NoError(t, err)
	assert.Empty(t, channels)

	states, err := service.ArtifactStatesForToken(token)
	require.NoError(t, err)
	for _, kind := range service.ArtifactKinds {
		assert.Equal(t, service.ArtifactJobIdle, states[kind].JobStatus, kind)
	}
}
