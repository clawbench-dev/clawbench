package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"clawbench/internal/service"
	"clawbench/internal/store"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

/**
 * Renaming a meeting after its minutes exist.
 *
 * The decision is deliberately separated from the model call: what matters is
 * WHO owns the title (the user typed it, or it is still the date placeholder),
 * not what the model happens to answer. These cases are the whole contract —
 * the summary model is stubbed out.
 */

func TestNameMeetingFromMinutes(t *testing.T) {
	ctx := context.Background()
	const placeholder = "11-01 17:35"

	t.Run("replaces a placeholder with the generated name", func(t *testing.T) {
		var gotMinutes string
		generate := func(_ context.Context, minutes string) (string, error) {
			gotMinutes = minutes
			return "  发版评审  ", nil
		}

		title := nameMeetingFromMinutes(ctx, placeholder, "## 概要\n讨论了发版", generate)

		assert.Equal(t, "发版评审", title)
		assert.Equal(t, "## 概要\n讨论了发版", gotMinutes, "the minutes are what gets titled")
	})

	t.Run("never overwrites a title the user typed", func(t *testing.T) {
		called := false
		generate := func(context.Context, string) (string, error) {
			called = true
			return "模型起的名字", nil
		}

		title := nameMeetingFromMinutes(ctx, "发版评审", "## 概要", generate)

		assert.Equal(t, "发版评审", title)
		assert.False(t, called, "the model must not even be asked")
	})

	t.Run("keeps the placeholder when the model fails", func(t *testing.T) {
		generate := func(context.Context, string) (string, error) {
			return "", errors.New("provider down")
		}

		assert.Equal(t, placeholder, nameMeetingFromMinutes(ctx, placeholder, "## 概要", generate))
	})

	t.Run("keeps the placeholder when the model answers nothing", func(t *testing.T) {
		generate := func(context.Context, string) (string, error) { return "   ", nil }

		assert.Equal(t, placeholder, nameMeetingFromMinutes(ctx, placeholder, "## 概要", generate))
	})
}

// ── End to end: the minutes job renames the record ──────────────────────────

// meetingTitleServer answers the three summary passes the job makes
// (proofread → minutes → title) and reports whether the title pass ran.
func meetingTitleServer(t *testing.T, title string) (*httptest.Server, *bool) {
	t.Helper()
	asked := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req := string(body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(req, "会议标题生成器"):
			asked = true
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"` + title + `"}}]}`))
		case strings.Contains(req, "待校对转写"):
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"NO_CHANGES"}}]}`))
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要\n\n确认发版计划。"}}]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &asked
}

// runMinutesJob creates a meeting under `title`, runs the detached minutes job
// to completion, and returns the stored record.
func runMinutesJob(t *testing.T, title string) (project string, note service.LiveNote) {
	t.Helper()
	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project = t.TempDir()
	room, err := liveRegistry.Create(projPath(project), title)
	require.NoError(t, err)
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	_, err = service.AppendLiveNoteSegment(context.Background(), room.Token(), pid, service.LiveNoteSegment{
		Channel: "mic", Speaker: "我", StartedAt: 1_700_000_000_000, Text: "确认发版计划",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, liveReq(t, http.MethodPost, "/api/live-share/notes/minutes", project,
		map[string]any{"token": room.Token(), "title": title}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Eventually(t, func() bool {
		n, ok, err := service.GetLiveNote(room.Token())
		return err == nil && ok && n.Status == service.LiveNotesStatusReady
	}, 10*time.Second, 20*time.Millisecond, "the background job must finish")

	note, ok, err := service.GetLiveNote(room.Token())
	require.NoError(t, err)
	require.True(t, ok)
	return project, note
}

func TestMinutesJobNamesAnUnnamedMeeting(t *testing.T) {
	srv, titleAsked := meetingTitleServer(t, "发版评审：上线前的取舍")
	withSummaryModel(t, srv.URL)

	placeholder := service.PlaceholderMeetingTitle(time.Now())
	project, note := runMinutesJob(t, placeholder)

	assert.True(t, *titleAsked, "the summary model must be asked for a name")
	assert.Equal(t, "发版评审：上线前的取舍", note.Title)

	// The minutes file is written AFTER the rename, so its heading carries the
	// real name rather than "11-01 17:35".
	raw, err := os.ReadFile(filepath.Join(service.MeetingDirPath(project, note.DirName), "minutes.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "# 发版评审：上线前的取舍")
}

func TestMinutesJobKeepsATitleTheUserTyped(t *testing.T) {
	srv, titleAsked := meetingTitleServer(t, "模型起的名字")
	withSummaryModel(t, srv.URL)

	_, note := runMinutesJob(t, "发版评审")

	assert.Equal(t, "发版评审", note.Title)
	assert.False(t, *titleAsked, "a name the user typed must not even be re-asked")
}

// A rename that lands while the minutes job runs must survive.
//
// The chain captures its title when the job starts and its last step writes
// that value back. Without re-reading the stored title, a user who renamed the
// meeting while the minutes were generating would watch the placeholder (or the
// model's guess) come back seconds later.
func TestMinutesJobKeepsARenameMadeWhileItRuns(t *testing.T) {
	minutesAsked := make(chan struct{}, 1)
	releaseCh := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseCh) }) }

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		req := string(body)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(req, "会议标题生成器"):
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"模型起的名字"}}]}`))
		case strings.Contains(req, "待校对转写"):
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"NO_CHANGES"}}]}`))
		default:
			// Hold the minutes pass open so the rename below lands mid-chain.
			minutesAsked <- struct{}{}
			<-releaseCh
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"## 概要\n\n确认发版计划。"}}]}`))
		}
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(release)
	withSummaryModel(t, srv.URL)

	setupLiveNotesDB(t)
	resetLiveRegistry(t)

	project := t.TempDir()
	placeholder := service.PlaceholderMeetingTitle(time.Now())
	room, err := liveRegistry.Create(projPath(project), placeholder)
	require.NoError(t, err)
	pid, err := store.ProjectIDForPath(project)
	require.NoError(t, err)
	_, err = service.AppendLiveNoteSegment(context.Background(), room.Token(), pid, service.LiveNoteSegment{
		Channel: "mic", Speaker: "我", StartedAt: 1_700_000_000_000, Text: "确认发版计划",
	})
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ServeLiveShareNotesMinutes(w, liveReq(t, http.MethodPost, "/api/live-share/notes/minutes", project,
		map[string]any{"token": room.Token(), "title": placeholder}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	select {
	case <-minutesAsked:
	case <-time.After(10 * time.Second):
		t.Fatal("the minutes step never asked the model")
	}

	// The user renames the meeting while the model is still answering.
	require.NoError(t, service.SetMeetingTitle(pid, room.Token(), "发版评审"))
	release()

	require.Eventually(t, func() bool {
		n, ok, err := service.GetLiveNote(room.Token())
		return err == nil && ok && n.Status == service.LiveNotesStatusReady
	}, 10*time.Second, 20*time.Millisecond, "the background job must finish")

	note, ok, err := service.GetLiveNote(room.Token())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "发版评审", note.Title,
		"the name the user typed must survive the chain writing its captured title")
}
