package live

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"clawbench/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func regenFixture(t *testing.T, ended bool, segments bool) (token string, project string) {
	t.Helper()
	setupLiveNotesDB(t)
	resetLiveRegistry(t)
	project = t.TempDir()
	pid := cacheProjectID(t, project)
	token = "0123456789abcdef0123456789abcdef"
	require.NoError(t, service.StartLiveNote(token, pid, "周会", 1_700_000_000_000))
	if ended {
		require.NoError(t, service.EndLiveNote(token, 1_700_000_600_000))
	}
	if segments {
		seedLiveNoteSegments(t, token, pid, []service.LiveNoteSegment{
			{Channel: "mic", Speaker: "我", StartedAt: 1_700_000_000_000, Text: "内容"},
		})
	}
	return token, project
}

func TestServeLiveShareNotesRegeneratePlan_MinutesCascades(t *testing.T) {
	token, project := regenFixture(t, true, true)

	req := liveReq(t, http.MethodGet,
		"/api/live-share/notes/regenerate/plan?token="+token+"&target=minutes", project, nil)
	rec := httptest.NewRecorder()
	ServeLiveShareNotesRegeneratePlan(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var plan service.RegenPlan
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &plan))
	assert.Equal(t, []service.RegenStep{
		{Kind: service.ArtifactTranscript, Mode: service.RegenModeRender, Reason: service.RegenReasonMissing},
		{Kind: service.ArtifactRevised, Mode: service.RegenModeModel, Reason: service.RegenReasonMissing},
		{Kind: service.ArtifactMinutes, Mode: service.RegenModeModel, Reason: service.RegenReasonTarget},
	}, plan.Steps)
	assert.Nil(t, plan.Blocked)
}

func TestServeLiveShareNotesRegeneratePlan_InvalidTarget(t *testing.T) {
	token, project := regenFixture(t, true, true)
	req := liveReq(t, http.MethodGet,
		"/api/live-share/notes/regenerate/plan?token="+token+"&target=nope", project, nil)
	rec := httptest.NewRecorder()
	ServeLiveShareNotesRegeneratePlan(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServeLiveShareNotesRegenerate_StartsAndCompletes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body), "待校对转写") {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"NO_CHANGES"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要"}}]}`))
	}))
	defer srv.Close()
	withSummaryModel(t, srv.URL)

	token, project := regenFixture(t, true, true)
	rec := httptest.NewRecorder()
	ServeLiveShareNotesRegenerate(rec, liveReq(t, http.MethodPost,
		"/api/live-share/notes/regenerate", project,
		map[string]any{"token": token, "target": "minutes"}))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Session service.MeetingRecord `json:"session"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	// The job runs on a goroutine, so the record may already have been reported
	// finished by the time this response is decoded — asserting "the transcript
	// step is running" pinned a moment that a fast machine skips. What the
	// handler must do is START the job; the eventual completion is asserted
	// below.
	if resp.Session.Generation != nil {
		// ...and it may already be on a LATER step: the three steps run back to
		// back, so "still on transcript" is another moment a fast machine
		// skips (observed as a flake: expected transcript, got revised). Pin
		// only that the step is one of this plan's, which is what the handler
		// can be held to.
		assert.Contains(t, []service.ArtifactKind{
			service.ArtifactTranscript, service.ArtifactRevised, service.ArtifactMinutes,
		}, resp.Session.Generation.Step)
	}

	pid := cacheProjectID(t, project)
	require.Eventually(t, func() bool {
		record, ok, err := service.GetMeetingRecord(project, pid, token)
		return err == nil && ok && record.Generation == nil &&
			record.Artifacts[service.ArtifactMinutes].JobStatus == service.ArtifactJobIdle &&
			record.HasMinutes
	}, 5*time.Second, 20*time.Millisecond)
}

func TestServeLiveShareNotesRegenerate_BlockedWithoutSource(t *testing.T) {
	token, project := regenFixture(t, true, false)
	rec := httptest.NewRecorder()
	ServeLiveShareNotesRegenerate(rec, liveReq(t, http.MethodPost,
		"/api/live-share/notes/regenerate", project,
		map[string]any{"token": token, "target": "minutes"}))
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestServeLiveShareNotesRegenerate_RejectsLiveMeeting(t *testing.T) {
	token, project := regenFixture(t, false, true)
	rec := httptest.NewRecorder()
	ServeLiveShareNotesRegenerate(rec, liveReq(t, http.MethodPost,
		"/api/live-share/notes/regenerate", project,
		map[string]any{"token": token, "target": "minutes"}))
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestServeLiveShareNotesRegenerateCancel_NoJob(t *testing.T) {
	token, project := regenFixture(t, true, true)
	rec := httptest.NewRecorder()
	ServeLiveShareNotesRegenerateCancel(rec, liveReq(t, http.MethodPost,
		"/api/live-share/notes/regenerate/cancel", project,
		map[string]any{"token": token}))
	assert.Equal(t, http.StatusConflict, rec.Code)
}
