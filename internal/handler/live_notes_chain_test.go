package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const chainFixtureTitle = "周会"

func chainFixture(t *testing.T) (token string, pid int64, project string) {
	t.Helper()
	setupLiveNotesDB(t)
	project = t.TempDir()
	pid = cacheProjectID(t, project)
	token = "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, chainFixtureTitle, 1_700_000_000_000))
	seedLiveNoteSegments(t, token, pid, []service.LiveNoteSegment{
		{Channel: "mic", Speaker: "我", StartedAt: 1_700_000_000_000, Text: "内容"},
	})
	return token, pid, project
}

func TestRunArtifactChain_RecordsEveryArtifactState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		// The proofreader answers with replacement pairs (or the sentinel);
		// every other pass is the minutes.
		if strings.Contains(string(body), "待校对转写") {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"NO_CHANGES"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要"}}]}`))
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	token, pid, project := chainFixture(t)
	runArtifactChain(token, pid, project, "周会", service.PlanForInitialJob(), false)

	states, err := service.ArtifactStatesForToken(token)
	require.NoError(t, err)
	for _, kind := range service.ArtifactKinds {
		assert.Equal(t, service.ArtifactJobIdle, states[kind].JobStatus, kind)
		assert.NotZero(t, states[kind].ContentAt, kind)
	}
	note, ok, err := service.GetLiveNote(token)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, service.LiveNotesStatusReady, note.Status)
	assert.Contains(t, note.Minutes, "## 概要")
}

func TestRunArtifactChain_StrictStopsAtFailedStep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	token, pid, project := chainFixture(t)
	plan := []service.RegenStep{
		{Kind: service.ArtifactRevised, Mode: service.RegenModeModel, Reason: service.RegenReasonTarget},
		{Kind: service.ArtifactMinutes, Mode: service.RegenModeModel, Reason: service.RegenReasonDownstream},
	}
	runArtifactChain(token, pid, project, "周会", plan, true)

	states, err := service.ArtifactStatesForToken(token)
	require.NoError(t, err)
	assert.Equal(t, service.ArtifactJobFailed, states[service.ArtifactRevised].JobStatus)
	assert.Equal(t, service.ArtifactErrModel, states[service.ArtifactRevised].ErrorCode)
	assert.Equal(t, service.ArtifactJobIdle, states[service.ArtifactMinutes].JobStatus,
		"a strict chain must not run the next step after a failure")
	note, ok, err := service.GetLiveNote(token)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Empty(t, note.Minutes)
}

func TestRunArtifactChain_NonStrictFallsBackToRawTranscript(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, "proofread down", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要"}}]}`))
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	token, pid, project := chainFixture(t)
	plan := []service.RegenStep{
		{Kind: service.ArtifactRevised, Mode: service.RegenModeModel, Reason: service.RegenReasonTarget},
		{Kind: service.ArtifactMinutes, Mode: service.RegenModeModel, Reason: service.RegenReasonDownstream},
	}
	runArtifactChain(token, pid, project, "周会", plan, false)

	states, err := service.ArtifactStatesForToken(token)
	require.NoError(t, err)
	assert.Equal(t, service.ArtifactJobFailed, states[service.ArtifactRevised].JobStatus)
	assert.Equal(t, service.ArtifactJobIdle, states[service.ArtifactMinutes].JobStatus)
	assert.Greater(t, states[service.ArtifactMinutes].ContentAt, int64(0))
	note, ok, err := service.GetLiveNote(token)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Contains(t, note.Minutes, "## 概要", "the minutes must use the raw transcript when the proofread fails")
}

func TestCancelLiveNoteJob_SettlesActiveArtifactsWithoutFailure(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		// Return when the request is cancelled, or when the test releases the
		// handler. The second arm keeps srv.Close() from waiting forever if the
		// transport does not propagate the cancellation.
		select {
		case <-r.Context().Done():
		case <-release:
		}
		http.Error(w, "cancelled", http.StatusInternalServerError)
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	token, pid, project := chainFixture(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runArtifactChain(token, pid, project, "周会", service.PlanForInitialJob(), false)
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the chain never reached the model")
	}
	assert.True(t, cancelLiveNoteJob(token))
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the chain did not return after cancellation")
	}

	states, err := service.ArtifactStatesForToken(token)
	require.NoError(t, err)
	assert.Equal(t, service.ArtifactJobIdle, states[service.ArtifactTranscript].JobStatus)
	assert.Equal(t, service.ArtifactJobIdle, states[service.ArtifactRevised].JobStatus)
	assert.Equal(t, service.ArtifactJobIdle, states[service.ArtifactMinutes].JobStatus)
	assert.Empty(t, states[service.ArtifactRevised].ErrorCode, "cancelling is not a failure")
}
