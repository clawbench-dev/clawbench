package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"clawbench/internal/model"
)

// Model.ConfigInstance is a value, so a fixture that replaces it wholesale (the
// existing withAISummary does: `ConfigInstance = model.Config{}`, restoring by
// t.Cleanup) silently resets this switch. Its cleanup was registered later and
// therefore runs FIRST, handing back a snapshot taken with agent minutes off —
// so a test that sets the knob and then installs the summary fixture would test
// the fallback path while believing it tests the agent path. Hence: call reassertAgentMode() right before the
// call under test, after every fixture has been installed.
// reassertAgentMode installs the switch on the CURRENT ConfigInstance value.
// Call it after any fixture that replaces the config.
func reassertAgentMode(t *testing.T, on bool) {
	t.Helper()
	prevOn := model.ConfigInstance.Meeting.AgentMinutes
	t.Cleanup(func() { model.ConfigInstance.Meeting.AgentMinutes = prevOn })
	model.ConfigInstance.Meeting.AgentMinutes = on
}

// endpointRecorder is a fake OpenAI-compatible streaming endpoint that records
// every request body and answers with a fixed text.
type endpointRecorder struct {
	*httptest.Server
	bodies chan string
	answer string
	status int
}

func newEndpointRecorder(t *testing.T, answer string) *endpointRecorder {
	t.Helper()
	rec := &endpointRecorder{bodies: make(chan string, 64), answer: answer}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		select {
		case rec.bodies <- string(body):
		default:
		}
		if rec.status != 0 {
			http.Error(w, "boom", rec.status)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprintf(w, `data: {"choices":[{"delta":{"content":%q}}]}`+"\n\n", rec.answer)
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(rec.Close)
	return rec
}

// TestGenerateLiveNotesMinutesWithAgent_ChunksAndAssembles pins agent mode's
// whole contract: long transcripts are chunked with the SAME budget the summary
// path uses, every chunk goes through the agent, the progress callback reports
// each step (the panel has no other sign of life during a long run), and the
// final document is the agent's last answer.
func TestGenerateLiveNotesMinutesWithAgent_ChunksAndAssembles(t *testing.T) {
	rec := newEndpointRecorder(t, "## 纪要\n最终版本")
	withBuiltinAgent(t, rec.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})

	// Three chunks' worth of transcript, with a recognizable tail.
	chunk := strings.Repeat("讨论配置加载器的实现。", 2200) // one chunk each (~24k runes)
	transcript := chunk + "\n" + chunk + "\n" + chunk
	if len([]rune(transcript)) <= liveNotesMinutesChunkRunes {
		t.Fatalf("precondition: transcript must exceed one chunk, got %d runes", len([]rune(transcript)))
	}

	var phases []string
	minutes, err := generateLiveNotesMinutesWithAgent(context.Background(), "周会", transcript, "涉及文件：a.go", LiveNotesSpeaker{},
		LiveNotesMinutesOptions{Language: "中文", MaxTokens: 512}, t.TempDir(),
		func(done, total int, phase string) {
			phases = append(phases, fmt.Sprintf("%s:%d/%d", phase, done, total))
		})
	if err != nil {
		t.Fatalf("generateLiveNotesMinutesWithAgent: %v", err)
	}
	if !strings.Contains(minutes, "最终版本") {
		t.Fatalf("the minutes must be the agent's final answer, got %q", minutes)
	}
	// One progress report per chunk, then the final step.
	if len(phases) < 2 {
		t.Fatalf("expected per-chunk progress and a final step, got %v", phases)
	}
	if phases[len(phases)-1] != "final:0/1" {
		t.Fatalf("the last step must be the final one, got %v", phases)
	}
	for _, p := range phases[:len(phases)-1] {
		if !strings.HasPrefix(p, "chunk:") {
			t.Fatalf("chunk steps must be reported before the final one, got %v", phases)
		}
	}
}

// TestGenerateLiveNotesMinutesWithAgent_Errors: an empty transcript is refused
// before any model call, and a failing endpoint is reported with the chunk it
// failed on (which is what tells the user which part of a long meeting broke).
func TestGenerateLiveNotesMinutesWithAgent_Errors(t *testing.T) {
	rec := newEndpointRecorder(t, "x")
	withBuiltinAgent(t, rec.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})

	if _, err := generateLiveNotesMinutesWithAgent(context.Background(), "t", "   ", "", LiveNotesSpeaker{}, LiveNotesMinutesOptions{}, t.TempDir()); err == nil {
		t.Fatal("an empty transcript must not be sent to the model")
	}
	select {
	case <-rec.bodies:
		t.Fatal("an empty transcript must be refused before any request")
	default:
	}

	rec.status = http.StatusInternalServerError
	// One chunk, so the error is the final call's.
	_, err := generateLiveNotesMinutesWithAgent(context.Background(), "t", "说了几句话", "", LiveNotesSpeaker{}, LiveNotesMinutesOptions{}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "agent minutes") {
		t.Fatalf("expected the final-call error, got %v", err)
	}

	// Two chunks: the failure is named as chunk 1.
	long := strings.Repeat("再说一点。", liveNotesMinutesChunkRunes)
	_, err = generateLiveNotesMinutesWithAgent(context.Background(), "t", long, "", LiveNotesSpeaker{}, LiveNotesMinutesOptions{}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "chunk 1") {
		t.Fatalf("expected a chunk-level error, got %v", err)
	}
}

// TestReviseLiveNotesTranscriptWithAgent_AppliesPairs pins agent-mode
// proofreading: the agent answers with `原词 => 改词` pairs and the SAME applier
// merges them, so timestamps and speaker labels survive. The pairs are returned
// by the model, not by the transcript rewrite — a model that echoes the whole
// text back must not be able to change it.
func TestReviseLiveNotesTranscriptWithAgent_AppliesPairs(t *testing.T) {
	rec := newEndpointRecorder(t, "clawbench => ClawBench")
	withBuiltinAgent(t, rec.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})

	in := LiveNotesReviseInput{
		Title:    "周会",
		Segments: []LiveNoteSegment{{Speaker: "我", StartedAt: 1700000000000, DurationMS: 1000, Text: "我们看一下 clawbench 的配置"}},
		Self:     LiveNotesSpeaker{Label: "我", Name: "张三"},
	}
	var steps []string
	out, err := reviseLiveNotesTranscriptWithAgent(context.Background(), in, t.TempDir(), func(done, total int) {
		steps = append(steps, fmt.Sprintf("%d/%d", done, total))
	})
	if err != nil {
		t.Fatalf("reviseLiveNotesTranscriptWithAgent: %v", err)
	}
	if !strings.Contains(out, "ClawBench") || strings.Contains(out, "clawbench") {
		t.Fatalf("the pair must be applied, got %q", out)
	}
	if len(steps) != 1 {
		t.Fatalf("progress must be reported once per chunk, got %v", steps)
	}

	// The system prompt carries the speaker clarification: the proofreader can
	// only fix a name it has been told about.
	var sent string
	for range len(rec.bodies) {
		sent = <-rec.bodies
	}
	if !strings.Contains(sent, "张三") {
		t.Fatalf("the initiator's name must reach the proofreader, body: %.400s", sent)
	}

	// An empty transcript never reaches the model.
	before := len(rec.bodies)
	if _, err := reviseLiveNotesTranscriptWithAgent(context.Background(), LiveNotesReviseInput{Title: "t"}, t.TempDir()); err == nil {
		t.Fatal("an empty transcript must be refused")
	}
	if len(rec.bodies) != before {
		t.Fatal("no request may be made for an empty transcript")
	}
}

// TestReviseLiveNotesTranscriptWithAgent_RejectsFormat: a model answer that is
// neither "no changes" nor a list of pairs must fail loudly. Silently applying
// nothing would report a successful proofread that changed nothing.
func TestReviseLiveNotesTranscriptWithAgent_RejectsFormat(t *testing.T) {
	rec := newEndpointRecorder(t, "我觉得这段转写整体还可以，建议再人工核对一下。")
	withBuiltinAgent(t, rec.URL, model.ToolPolicy{Allow: []string{model.CapFSRead}})

	_, err := reviseLiveNotesTranscriptWithAgent(context.Background(), LiveNotesReviseInput{
		Title:    "t",
		Segments: []LiveNoteSegment{{Speaker: "我", StartedAt: 1700000000000, DurationMS: 1000, Text: "随便说点"}},
	}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "chunk 1") {
		t.Fatalf("expected a format error for chunk 1, got %v", err)
	}
}
