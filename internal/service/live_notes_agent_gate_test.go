package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

// TestReviseLiveNotesTranscript_AgentModeEntryPoint pins the switch that routes
// proofreading through the built-in agent.
//
// The agent path is only taken when the setting is on, a working directory is
// known AND the agent is usable. Any of the three missing must fall back to the
// summary path — a meeting the user is waiting on must not fail because a
// background capability is off. This runs the real entry point, so the routing
// itself is covered rather than only the agent function behind it.
func TestReviseLiveNotesTranscript_AgentModeEntryPoint(t *testing.T) {
	base := int64(1_700_000_000_000)
	segments := []LiveNoteSegment{
		seg(1, "mic", "我", base, "我们看一下 clawbench 的配置"),
		seg(2, "mic", "我", base+8_000, "接口还没联调完"),
	}

	t.Run("agent mode on with a project path goes through the agent", func(t *testing.T) {
		withAISummary(t, "") // the summary path would fail: only the agent can answer
		rec := newEndpointRecorder(t, "clawbench => ClawBench")
		withBuiltinAgent(t, rec.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})
		reassertAgentMode(t, true) // after every fixture that replaces the config

		out, err := ReviseLiveNotesTranscript(context.Background(), LiveNotesReviseInput{
			Title:       "周会",
			ProjectPath: t.TempDir(),
			Segments:    segments,
		})
		require.NoError(t, err)
		require.Contains(t, out, "ClawBench")
		require.Contains(t, out, "[00:00] 我：", "the timeline survives the agent path too")
	})

	t.Run("no project path falls back to the summary path", func(t *testing.T) {
		withAISummary(t, "")
		// An agent that WOULD answer is installed: if the gate were missing, the
		// call below would succeed through it.
		rec := newEndpointRecorder(t, "clawbench => ClawBench")
		withBuiltinAgent(t, rec.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})
		reassertAgentMode(t, true)

		_, err := ReviseLiveNotesTranscript(context.Background(), LiveNotesReviseInput{
			Title:    "周会",
			Segments: segments,
		})
		require.ErrorIs(t, err, ErrSummaryModelNotConfigured,
			"without a workdir the agent is skipped, so the summary path's own precondition decides")
		select {
		case <-rec.bodies:
			t.Fatal("the agent must not be called without a working directory")
		default:
		}
	})

	t.Run("agent mode off never calls the agent endpoint", func(t *testing.T) {
		rec := newEndpointRecorder(t, "x")
		withBuiltinAgent(t, rec.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"clawbench => ClawBench"}}]}`)
		}))
		defer srv.Close()
		withAISummary(t, srv.URL)
		reassertAgentMode(t, false)

		out, err := ReviseLiveNotesTranscript(context.Background(), LiveNotesReviseInput{
			Title:       "周会",
			ProjectPath: t.TempDir(),
			Segments:    segments,
		})
		require.NoError(t, err)
		require.Contains(t, out, "ClawBench", "the summary path produces the same pairs")
		select {
		case <-rec.bodies:
			t.Fatal("agent mode is off: the agent endpoint must not be used")
		default:
		}
	})
}

// TestGenerateLiveNotesMinutes_AgentModeEntryPoint is the same routing check for
// minutes, whose gate lives in GenerateLiveNotesMinutesWithOptions.
func TestGenerateLiveNotesMinutes_AgentModeEntryPoint(t *testing.T) {
	transcript := "[00:00] 我：先确认时间"

	t.Run("agent mode on goes through the agent", func(t *testing.T) {
		withAISummary(t, "")
		rec := newEndpointRecorder(t, "## 纪要\n由 agent 生成")
		withBuiltinAgent(t, rec.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})
		reassertAgentMode(t, true)

		out, err := GenerateLiveNotesMinutesWithOptions(context.Background(), "周会", transcript, "", LiveNotesSpeaker{},
			LiveNotesMinutesOptions{WorkDir: t.TempDir()})
		require.NoError(t, err)
		require.Contains(t, out, "由 agent 生成")
	})

	t.Run("no working directory falls back to the summary path", func(t *testing.T) {
		withAISummary(t, "")
		rec := newEndpointRecorder(t, "## 纪要")
		withBuiltinAgent(t, rec.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})
		reassertAgentMode(t, true)

		_, err := GenerateLiveNotesMinutesWithOptions(context.Background(), "周会", transcript, "", LiveNotesSpeaker{},
			LiveNotesMinutesOptions{})
		require.ErrorIs(t, err, ErrSummaryModelNotConfigured)
		select {
		case <-rec.bodies:
			t.Fatal("the agent must not be called without a working directory")
		default:
		}
	})

	t.Run("agent mode off uses the summary path", func(t *testing.T) {
		rec := newEndpointRecorder(t, "x")
		withBuiltinAgent(t, rec.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"## 纪要\n由摘要模型生成"}}]}`)
		}))
		defer srv.Close()
		withAISummary(t, srv.URL)
		reassertAgentMode(t, false)

		out, err := GenerateLiveNotesMinutesWithOptions(context.Background(), "周会", transcript, "", LiveNotesSpeaker{},
			LiveNotesMinutesOptions{WorkDir: t.TempDir()})
		require.NoError(t, err)
		require.True(t, strings.Contains(out, "由摘要模型生成"), out)
		select {
		case <-rec.bodies:
			t.Fatal("agent mode is off: the agent endpoint must not be used")
		default:
		}
	})
}
