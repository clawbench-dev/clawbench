package vecli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"clawbench/internal/ai"
	"clawbench/internal/model"
)

// These tests drive VeCLIBackend end to end against a stand-in `vecli` script.
//
// The wrapper exists for one reason: vecli writes token accounting to a FILE
// after it exits (--session-summary), so the numbers can only be reported by
// reading that file once the process is gone. Everything else about the backend
// is plain text on stdout. Testing the wrapper without a process would leave
// exactly the post-stream half — the part that was added — unexercised.

// writeFakeVeCLI installs a shell script that behaves like the agent: it prints
// the lines it is told to, optionally writes the summary file it was asked for,
// and exits with the given status.
func writeFakeVeCLI(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "vecli")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// summaryArgs extracts the value that follows --session-summary in "$@".
const summaryArgs = `prev=""
for a in "$@"; do
  if [ "$prev" = "--session-summary" ]; then SUMMARY="$a"; fi
  prev="$a"
done
`

// withDataDir points the summary directory at a temp dir for the test.
func withDataDir(t *testing.T) {
	t.Helper()
	prev := model.DataDir
	model.DataDir = t.TempDir()
	t.Cleanup(func() { model.DataDir = prev })
}

func runVeCLI(t *testing.T, path string, req ai.ChatRequest) []ai.StreamEvent {
	t.Helper()
	req.Command = path
	ch, err := newVeCLIBackendImpl().ExecuteStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ExecuteStream: %v", err)
	}
	var out []ai.StreamEvent
	for ev := range ch {
		out = append(out, ev)
	}
	return out
}

// TestVeCLIBackend_ReportsTheSummaryAsMetadata pins the whole point of the
// wrapper: the text reaches the transcript, and the token counts written to the
// summary file arrive as one metadata event placed before done.
func TestVeCLIBackend_ReportsTheSummaryAsMetadata(t *testing.T) {
	withDataDir(t)
	path := writeFakeVeCLI(t, `echo "working on it"
echo "all done"
`+summaryArgs+`cat > "$SUMMARY" <<'EOF'
{"sessionMetrics":{"models":{"deepseek-v3":{"api":{"totalRequests":2,"totalErrors":0,"totalLatencyMs":321},"tokens":{"prompt":400,"candidates":100,"total":500}}}}}
EOF`)

	events := runVeCLI(t, path, ai.ChatRequest{SessionID: "sess-1", Prompt: "do it", Model: "deepseek-v3"})

	var text strings.Builder
	var meta *ai.Metadata
	types := make([]string, 0, len(events))
	for _, ev := range events {
		types = append(types, ev.Type)
		switch ev.Type {
		case "content":
			text.WriteString(ev.Content)
		case "metadata":
			meta = ev.Meta
		}
	}
	if text.String() != "working on it\nall done\n" {
		t.Fatalf("stdout must reach the transcript line by line, got %q", text.String())
	}
	if meta == nil {
		t.Fatalf("the session summary must be reported, got %v", types)
	}
	if meta.InputTokens != 400 || meta.OutputTokens != 100 || meta.DurationMs != 321 {
		t.Fatalf("meta = %+v, want the summary's numbers", meta)
	}
	if meta.Model != "deepseek-v3" || meta.StopReason != "stop" {
		t.Fatalf("meta = %+v", meta)
	}
	if last := types[len(types)-1]; last != "done" {
		t.Fatalf("the stream must end with done, got %v", types)
	}
	if strings.Count(strings.Join(types, ","), "done") != 1 {
		t.Fatalf("exactly one done must be emitted, got %v", types)
	}

	// The summary file is removed once read, so a long-lived server does not
	// accumulate one file per session.
	left, err := os.ReadDir(filepath.Join(model.DataDir, "vecli-summary"))
	if err == nil && len(left) != 0 {
		t.Fatalf("the summary file must be cleaned up, %d left", len(left))
	}
}

// TestVeCLIBackend_MissingSummaryStillCompletes pins the ordinary case: vecli may
// not write the file at all (an older build, or a run that produced no calls).
// The turn must still end cleanly with no metadata event.
func TestVeCLIBackend_MissingSummaryStillCompletes(t *testing.T) {
	withDataDir(t)
	path := writeFakeVeCLI(t, `echo "hi"`)

	events := runVeCLI(t, path, ai.ChatRequest{SessionID: "sess-2", Prompt: "x"})
	for _, ev := range events {
		if ev.Type == "metadata" {
			t.Fatalf("no summary file means no metadata, got %+v", ev.Meta)
		}
	}
	if events[len(events)-1].Type != "done" {
		t.Fatalf("the turn must still finish, got %v", events[len(events)-1].Type)
	}
}

// TestVeCLIBackend_UnparseableSummaryIsNotFatal pins the corrupt-file case: a
// half-written summary must not turn a successful answer into a failure.
func TestVeCLIBackend_UnparseableSummaryIsNotFatal(t *testing.T) {
	withDataDir(t)
	path := writeFakeVeCLI(t, `echo "answer"
`+summaryArgs+`printf 'not json' > "$SUMMARY"`)

	events := runVeCLI(t, path, ai.ChatRequest{SessionID: "sess-3", Prompt: "x"})
	var sawContent bool
	for _, ev := range events {
		if ev.Type == "content" && strings.Contains(ev.Content, "answer") {
			sawContent = true
		}
		if ev.Type == "metadata" {
			t.Fatalf("an unparseable summary must not produce metadata, got %+v", ev.Meta)
		}
	}
	if !sawContent {
		t.Fatal("the answer must survive a bad summary")
	}
}

// TestVeCLIBackend_CancellationSkipsTheSummary pins the cancel path: the file is
// never written (or is garbage after the kill), so it must not be read, and the
// stream still ends with exactly one done.
func TestVeCLIBackend_CancellationSkipsTheSummary(t *testing.T) {
	withDataDir(t)
	// The script waits long enough for the test to cancel it.
	path := writeFakeVeCLI(t, `echo "starting"
sleep 30
echo "never"`)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := newVeCLIBackendImpl().ExecuteStream(ctx, ai.ChatRequest{
		Command: path, SessionID: "sess-4", Prompt: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Cancel once the first line has arrived, so the process is really running.
	for ev := range ch {
		if ev.Type == "content" {
			cancel()
			break
		}
	}
	// Drain the rest so the producer goroutine can finish.
	for range ch {
	}
	if ctx.Err() == nil {
		t.Fatal("the turn must have been cancelled")
	}
	// The summary directory must not keep a file for the cancelled session.
	if entries, err := os.ReadDir(filepath.Join(model.DataDir, "vecli-summary")); err == nil {
		for _, e := range entries {
			if e.Name() == "sess-4.json" {
				t.Fatal("a cancelled turn's summary file must be removed")
			}
		}
	}
}

// TestVeCLIBackend_GeneratesASessionIDWhenMissing pins the keying contract:
// --session-summary is keyed by req.SessionID, so an empty one has to be
// replaced before the process starts or the summary can never be found again.
func TestVeCLIBackend_GeneratesASessionIDWhenMissing(t *testing.T) {
	withDataDir(t)
	// The script echoes the summary path it was given, which must contain the
	// generated session id rather than an empty file name ("/.json").
	path := writeFakeVeCLI(t, summaryArgs+`echo "$SUMMARY"`)

	events := runVeCLI(t, path, ai.ChatRequest{Prompt: "x"})
	var printed string
	for _, ev := range events {
		if ev.Type == "content" {
			printed += ev.Content
		}
	}
	if !strings.Contains(printed, "vecli-") || strings.Contains(printed, "/.json") {
		t.Fatalf("a generated session id must key the summary path, got %q", strings.TrimSpace(printed))
	}
}

// TestBuildVeCLIArgs pins the argv: the system prompt is folded into the user
// prompt (this CLI has no flag for it), the working directory and the model are
// passed through, and neither --output-format nor --resume is ever sent (VeCLI
// removed / never supported them).
func TestBuildVeCLIArgs(t *testing.T) {
	args := buildVeCLIArgs(ai.ChatRequest{
		Prompt:       "hello",
		SystemPrompt: "be brief",
		WorkDir:      "/tmp/proj",
		Model:        "deepseek-v3",
	})

	joined := strings.Join(args, " ")
	if args[0] != "--yolo" {
		t.Fatalf("the non-interactive flag must come first, got %v", args)
	}
	if !strings.Contains(joined, "be brief") || !strings.Contains(joined, "hello") {
		t.Fatalf("the system prompt must be folded into the prompt, got %v", args)
	}
	if !strings.Contains(joined, "--include-directories /tmp/proj") {
		t.Fatalf("the working directory must be passed, got %v", args)
	}
	if !strings.Contains(joined, "--model deepseek-v3") {
		t.Fatalf("the model override must be passed, got %v", args)
	}
	for _, forbidden := range []string{"--output-format", "--resume", "--session-summary"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("%s must not be sent from the argv builder, got %v", forbidden, args)
		}
	}
}

// TestBuildVeCLIArgs_OmitsOptionalFlags pins the minimal argv: no directory, no
// model, nothing.
func TestBuildVeCLIArgs_OmitsOptionalFlags(t *testing.T) {
	args := buildVeCLIArgs(ai.ChatRequest{Prompt: "hi"})
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "--include-directories") || strings.Contains(joined, "--model") {
		t.Fatalf("unset options must not be sent, got %v", args)
	}
	if args[len(args)-1] != "hi" {
		t.Fatalf("the prompt must be the last pair, got %v", args)
	}
}

// TestVeCLISessionSummary_ExtractMetadataPrefersTheRequestedModel pins the model
// choice: the map has no order, so without matching on the request the reported
// model (and its numbers) can be another model's.
func TestVeCLISessionSummary_ExtractMetadataPrefersTheRequestedModel(t *testing.T) {
	var summary VeCLISessionSummary
	raw := `{"sessionMetrics":{"models":{
		"alpha":{"api":{"totalLatencyMs":1},"tokens":{"prompt":10,"candidates":1}},
		"beta":{"api":{"totalLatencyMs":2},"tokens":{"prompt":20,"candidates":2}}
	}}}`
	if err := json.Unmarshal([]byte(raw), &summary); err != nil {
		t.Fatal(err)
	}

	// Whatever the map order is, asking for beta must report beta.
	meta := summary.extractMetadata("beta")
	if meta.Model != "beta" || meta.InputTokens != 20 || meta.OutputTokens != 2 {
		t.Fatalf("meta = %+v, want the requested model", meta)
	}

	// No model requested: some entry is reported rather than an empty one.
	first := summary.extractMetadata("")
	if first.Model == "" || first.InputTokens == 0 {
		t.Fatalf("an unrequested model must still report the summary, got %+v", first)
	}

	// No entries at all: the request's own model name is the only honest answer.
	var empty VeCLISessionSummary
	if got := empty.extractMetadata("requested"); got.Model != "requested" {
		t.Fatalf("got %+v, want the requested name as a fallback", got)
	}
	if got := (&VeCLISessionSummary{}).extractMetadata(""); got.Model != "" {
		t.Fatalf("nothing known must stay empty, got %q", got.Model)
	}
}

// TestVeCLIBackend_ReadsTheSummaryKeyedBySessionID pins the correlation: the
// wrapper deletes the entry by the session id it captured BEFORE the goroutine
// started, so a request with a session id must report that session's numbers and
// no other file may be read.
func TestVeCLIBackend_ReadsTheSummaryKeyedBySessionID(t *testing.T) {
	withDataDir(t)
	path := writeFakeVeCLI(t, `echo "x"
`+summaryArgs+`cat > "$SUMMARY" <<'EOF'
{"sessionMetrics":{"models":{"m":{"tokens":{"prompt":7,"candidates":3}}}}}
EOF`)
	events := runVeCLI(t, path, ai.ChatRequest{SessionID: "keyed-session", Prompt: "x"})

	var found bool
	for _, ev := range events {
		if ev.Type == "metadata" {
			found = true
			if ev.Meta.InputTokens != 7 || ev.Meta.OutputTokens != 3 {
				t.Fatalf("meta = %+v, want this session's numbers", ev.Meta)
			}
		}
	}
	if !found {
		t.Fatal("a request with an explicit session id must still report its summary")
	}
}
