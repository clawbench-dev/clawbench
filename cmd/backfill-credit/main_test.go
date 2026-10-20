package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"clawbench/internal/store"
)

// writeFile creates a transcript file (and any parent directories) under root.
func writeFile(t *testing.T, root, rel, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// usageLine builds one transcript line carrying a model call.
func usageLine(rid string, prompt, completion int, credit float64) string {
	return `{"type":"message","providerData":{"conversationRequestId":"` + rid +
		`","rawUsage":{"prompt_tokens":` + strconv.Itoa(prompt) + `,"completion_tokens":` + strconv.Itoa(completion) +
		`,"total_tokens":` + strconv.Itoa(prompt+completion) + `,"prompt_cache_hit_tokens":7,"prompt_cache_miss_tokens":3,"credit":` +
		strconv.FormatFloat(credit, 'f', -1, 64) + `}}}`
}

func TestParseTranscriptCall(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		wantOK bool
		wantID string
		wantIn int64
	}{
		{
			name:   "conversationRequestId wins over requestId",
			line:   `{"providerData":{"conversationRequestId":"conv","requestId":"req","rawUsage":{"prompt_tokens":10,"completion_tokens":5}}}`,
			wantOK: true,
			wantID: "conv",
			wantIn: 10,
		},
		{
			name:   "falls back to requestId",
			line:   `{"providerData":{"requestId":"req","rawUsage":{"prompt_tokens":11,"completion_tokens":1}}}`,
			wantOK: true,
			wantID: "req",
			wantIn: 11,
		},
		{
			name:   "falls back to usage when rawUsage is null",
			line:   `{"providerData":{"requestId":"req","rawUsage":null,"usage":{"prompt_tokens":12,"completion_tokens":2}}}`,
			wantOK: true,
			wantID: "req",
			wantIn: 12,
		},
		{
			name:   "no request id is not a usage record",
			line:   `{"providerData":{"rawUsage":{"prompt_tokens":12}}}`,
			wantOK: false,
		},
		{
			name:   "no usage block is not a usage record",
			line:   `{"providerData":{"requestId":"req"}}`,
			wantOK: false,
		},
		{
			name:   "all-zero usage is skipped",
			line:   `{"providerData":{"requestId":"req","rawUsage":{"prompt_tokens":0,"completion_tokens":0,"credit":0}}}`,
			wantOK: false,
		},
		{
			name:   "credit alone is enough",
			line:   `{"providerData":{"requestId":"req","rawUsage":{"credit":1.5}}}`,
			wantOK: true,
			wantID: "req",
		},
		{
			name:   "malformed JSON",
			line:   `{"providerData":`,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseTranscriptCall([]byte(tt.line))
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got.rid != tt.wantID {
				t.Errorf("rid = %q, want %q", got.rid, tt.wantID)
			}
			if tt.wantIn != 0 && got.u.PromptTokens != tt.wantIn {
				t.Errorf("prompt tokens = %d, want %d", got.u.PromptTokens, tt.wantIn)
			}
		})
	}
}

func TestIsForkedCompactionSession(t *testing.T) {
	if !isForkedCompactionSession([]byte(`{"type":"session-meta","meta":{"isForkedCompactionSession":true}}`)) {
		t.Error("forked compaction meta must be detected")
	}
	// The flag only counts on the session-meta record: a message that happens to
	// carry the field must not exclude the whole file.
	if isForkedCompactionSession([]byte(`{"type":"message","meta":{"isForkedCompactionSession":true}}`)) {
		t.Error("flag on a non-meta record must not mark the session")
	}
	if isForkedCompactionSession([]byte(`{"type":"session-meta","meta":{"isForkedCompactionSession":false}}`)) {
		t.Error("false flag must not mark the session")
	}
	if isForkedCompactionSession([]byte(`not json`)) {
		t.Error("malformed line must not mark the session")
	}
}

func TestIsSubagentTranscript(t *testing.T) {
	if !isSubagentTranscript("/logs/proj/ses_1/subagents/agent-a.jsonl") {
		t.Error("subagent transcript must be excluded")
	}
	if isSubagentTranscript("/logs/proj/ses_1.jsonl") {
		t.Error("top-level transcript must be included")
	}
}

func TestReadTranscripts_GroupsByRequestAndSkipsExcluded(t *testing.T) {
	root := t.TempDir()

	// Two calls in the same request: the whole point of the tool is summing
	// them, so this fixture is the regression guard for LAST-call-only.
	writeFile(t, root, "proj/ses_1.jsonl", strings.Join([]string{
		usageLine("rid-1", 100, 10, 1.25),
		usageLine("rid-1", 200, 20, 2),
		usageLine("rid-2", 50, 5, 0.5),
	}, "\n")+"\n")

	// A sub-agent transcript bills the same request; counting it would double.
	writeFile(t, root, "proj/ses_1/subagents/agent-a.jsonl", usageLine("rid-1", 999, 999, 9))

	// A forked-compaction session is excluded from the agent's own accounting.
	writeFile(t, root, "proj/ses_forked.jsonl",
		usageLine("rid-3", 10, 1, 0.1)+"\n"+
			`{"type":"session-meta","meta":{"isForkedCompactionSession":true}}`+"\n")

	// A file that is not a transcript at all.
	writeFile(t, root, "proj/notes.txt", "ignore me")

	turns := readTranscripts(root)

	if len(turns) != 2 {
		t.Fatalf("want 2 requests, got %d: %+v", len(turns), turns)
	}
	if _, ok := turns["rid-3"]; ok {
		t.Error("forked-compaction session must be skipped entirely")
	}

	r1 := turns["rid-1"]
	if r1 == nil {
		t.Fatal("rid-1 missing")
	}
	if r1.Calls != 2 {
		t.Errorf("rid-1 calls = %d, want 2 (subagent must not be counted)", r1.Calls)
	}
	if r1.InputTokens != 300 {
		t.Errorf("rid-1 input = %d, want 300 (sum over calls)", r1.InputTokens)
	}
	if r1.OutputTokens != 30 {
		t.Errorf("rid-1 output = %d, want 30", r1.OutputTokens)
	}
	if r1.CacheHit != 14 {
		t.Errorf("rid-1 cache hit = %d, want 14", r1.CacheHit)
	}
	if r1.CacheMiss != 6 {
		t.Errorf("rid-1 cache miss = %d, want 6", r1.CacheMiss)
	}
	if diffFloat(r1.Credit, 3.25) > 0.001 {
		t.Errorf("rid-1 credit = %v, want 3.25", r1.Credit)
	}

	if got := countCalls(turns); got != 3 {
		t.Errorf("countCalls = %d, want 3", got)
	}
}

func TestReadOneTranscript_SkipsMalformedLines(t *testing.T) {
	root := t.TempDir()
	path := writeFile(t, root, "ses.jsonl", strings.Join([]string{
		`not json at all`,
		``,
		usageLine("rid-a", 10, 1, 0),
		`{"providerData":{"requestId":"rid-b","rawUsage":"not-an-object"}}`,
	}, "\n")+"\n")

	turns := map[string]*turnTotals{}
	if err := readOneTranscript(path, turns); err != nil {
		t.Fatalf("readOneTranscript: %v", err)
	}
	if len(turns) != 1 || turns["rid-a"] == nil {
		t.Fatalf("want only rid-a, got %+v", turns)
	}
	if turns["rid-a"].InputTokens != 10 {
		t.Errorf("input = %d, want 10", turns["rid-a"].InputTokens)
	}
}

func TestReadOneTranscript_MissingFile(t *testing.T) {
	if err := readOneTranscript(filepath.Join(t.TempDir(), "nope.jsonl"), map[string]*turnTotals{}); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

func TestScanTranscript_LongLine(t *testing.T) {
	// Transcripts embed tool results; the scanner buffer must be raised past the
	// 64KiB default or the line is silently truncated into an unparseable record.
	long := strings.Repeat("x", 200*1024)
	line := `{"providerData":{"requestId":"rid-long","rawUsage":{"prompt_tokens":5,"completion_tokens":1,"pad":"` + long + `"}}}`
	sc := bufio.NewScanner(strings.NewReader(line + "\n"))
	sc.Buffer(make([]byte, 0, 256*1024), 32*1024*1024)

	calls, forked, err := scanTranscript(sc)
	if err != nil {
		t.Fatalf("scanTranscript: %v", err)
	}
	if forked {
		t.Error("not a forked session")
	}
	if len(calls) != 1 || calls[0].rid != "rid-long" {
		t.Fatalf("want one call for rid-long, got %+v", calls)
	}
}

func TestDiffFloat(t *testing.T) {
	if got := diffFloat(1.5, 1.25); got != 0.25 {
		t.Errorf("diffFloat(1.5, 1.25) = %v, want 0.25", got)
	}
	if got := diffFloat(1.25, 1.5); got != 0.25 {
		t.Errorf("diffFloat must be absolute, got %v", got)
	}
}

// openTestDB opens an in-memory-backed SQLite file and creates the slice of
// chat_metadata the backfill reads.
func openTestDB(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	if err := store.Open(dbPath); err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(store.Close)
	if _, err := store.WriteExec(`CREATE TABLE chat_metadata (
		message_id INTEGER PRIMARY KEY,
		backend TEXT DEFAULT '',
		model TEXT DEFAULT '',
		created_at TEXT DEFAULT '',
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		total_tokens INTEGER DEFAULT 0,
		credit REAL DEFAULT 0,
		cache_hit_tokens INTEGER DEFAULT 0,
		cache_miss_tokens INTEGER DEFAULT 0,
		request_id TEXT DEFAULT ''
	)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	return dbPath
}

func insertRow(t *testing.T, msgID int64, backend, requestID string, in, out int64, credit float64) {
	t.Helper()
	if _, err := store.WriteExec(`INSERT INTO chat_metadata
		(message_id, backend, model, created_at, input_tokens, output_tokens, total_tokens, credit, request_id)
		VALUES (?, ?, 'glm', '2026-10-01', ?, ?, ?, ?, ?)`,
		msgID, backend, in, out, in+out, credit, requestID); err != nil {
		t.Fatalf("insert row %d: %v", msgID, err)
	}
}

func TestBuildPlan_ClassifiesEveryRow(t *testing.T) {
	openTestDB(t)

	turns := map[string]*turnTotals{
		"rid-match": {Calls: 2, InputTokens: 300, OutputTokens: 30, CacheHit: 14, CacheMiss: 6, Credit: 3.25},
		"rid-same":  {Calls: 1, InputTokens: 100, OutputTokens: 10, Credit: 1},
	}

	insertRow(t, 1, "codebuddy", "rid-match", 100, 10, 1) // wrong -> repaired
	insertRow(t, 2, "codebuddy", "rid-same", 100, 10, 1)  // already correct
	insertRow(t, 3, "codebuddy", "", 50, 5, 0)            // no request_id
	insertRow(t, 4, "codebuddy", "rid-gone", 50, 5, 0)    // transcript gone
	insertRow(t, 5, "claude", "rid-match", 50, 5, 0)      // other backend, ignored

	plan, stats, err := buildPlan(turns)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}

	if stats.TotalRows != 4 {
		t.Errorf("TotalRows = %d, want 4 (only codebuddy rows)", stats.TotalRows)
	}
	if stats.NoRequestID != 1 {
		t.Errorf("NoRequestID = %d, want 1", stats.NoRequestID)
	}
	if stats.Unmatched != 1 {
		t.Errorf("Unmatched = %d, want 1", stats.Unmatched)
	}
	if stats.Unchanged != 1 {
		t.Errorf("Unchanged = %d, want 1", stats.Unchanged)
	}
	if stats.Matched != 2 {
		t.Errorf("Matched = %d, want 2", stats.Matched)
	}
	if len(plan) != 1 {
		t.Fatalf("want 1 planned repair, got %d: %+v", len(plan), plan)
	}

	row := plan[0]
	if row.MessageID != 1 {
		t.Errorf("MessageID = %d, want 1", row.MessageID)
	}
	if row.NewInput != 300 || row.NewOutput != 30 {
		t.Errorf("new usage = %d/%d, want 300/30", row.NewInput, row.NewOutput)
	}
	if row.NewCacheHit != 14 || row.NewCacheMiss != 6 {
		t.Errorf("new cache = %d/%d, want 14/6", row.NewCacheHit, row.NewCacheMiss)
	}
	if diffFloat(row.NewCredit, 3.25) > 0.001 {
		t.Errorf("new credit = %v, want 3.25", row.NewCredit)
	}
	if stats.IncreaseInput != 200 {
		t.Errorf("IncreaseInput = %d, want 200", stats.IncreaseInput)
	}
	if diffFloat(stats.IncreaseCredit, 2.25) > 0.001 {
		t.Errorf("IncreaseCredit = %v, want 2.25", stats.IncreaseCredit)
	}
}

func TestBuildPlan_DecreaseIsCountedSeparately(t *testing.T) {
	openTestDB(t)

	// The pre-ACP CLI parser wrote session-cumulative input, so a repair can
	// LOWER a row; the report separates the two directions so a large negative
	// is never mistaken for a large positive.
	turns := map[string]*turnTotals{
		"rid": {Calls: 1, InputTokens: 1000, OutputTokens: 10, Credit: 0.5},
	}
	insertRow(t, 1, "codebuddy", "rid", 50000, 10, 0.5)

	plan, stats, err := buildPlan(turns)
	if err != nil {
		t.Fatalf("buildPlan: %v", err)
	}
	if len(plan) != 1 {
		t.Fatalf("want 1 repair, got %d", len(plan))
	}
	if stats.DecreaseInput != -49000 {
		t.Errorf("DecreaseInput = %d, want -49000", stats.DecreaseInput)
	}
	if stats.IncreaseInput != 0 {
		t.Errorf("IncreaseInput = %d, want 0", stats.IncreaseInput)
	}
}

func TestApplyPlan_WritesRowsAndKeepsTotalInvariant(t *testing.T) {
	openTestDB(t)
	insertRow(t, 1, "codebuddy", "rid", 100, 10, 1)

	plan := []backfillRow{{
		MessageID: 1, Calls: 2,
		NewInput: 300, NewOutput: 30, NewCredit: 3.25,
		NewCacheHit: 14, NewCacheMiss: 6,
	}}
	if err := applyPlan(plan); err != nil {
		t.Fatalf("applyPlan: %v", err)
	}

	var in, out, total, hit, miss int64
	var credit float64
	if err := store.ReadDB().QueryRow(`SELECT input_tokens, output_tokens, total_tokens,
		cache_hit_tokens, cache_miss_tokens, credit FROM chat_metadata WHERE message_id = 1`).
		Scan(&in, &out, &total, &hit, &miss, &credit); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if in != 300 || out != 30 || total != 330 {
		t.Errorf("tokens = %d/%d/%d, want 300/30/330", in, out, total)
	}
	if hit != 14 || miss != 6 {
		t.Errorf("cache = %d/%d, want 14/6", hit, miss)
	}
	if diffFloat(credit, 3.25) > 0.001 {
		t.Errorf("credit = %v, want 3.25", credit)
	}
}

func TestApplyPlan_EmptyPlanIsNoop(t *testing.T) {
	if err := applyPlan(nil); err != nil {
		t.Fatalf("empty plan must be a no-op, got %v", err)
	}
}

func TestRun_DryRunDoesNotWrite(t *testing.T) {
	dbPath := openTestDB(t)
	insertRow(t, 1, "codebuddy", "rid", 100, 10, 1)

	turns := map[string]*turnTotals{
		"rid": {Calls: 2, InputTokens: 300, OutputTokens: 30, Credit: 3.25},
	}
	if err := run(dbPath, turns, false); err != nil {
		t.Fatalf("run dry: %v", err)
	}

	// run() closes the handle it opened, so reading back needs a fresh open.
	reopen := func() int64 {
		t.Helper()
		if err := store.Open(dbPath); err != nil {
			t.Fatalf("reopen: %v", err)
		}
		defer store.Close()
		var in int64
		if err := store.ReadDB().QueryRow(`SELECT input_tokens FROM chat_metadata WHERE message_id = 1`).
			Scan(&in); err != nil {
			t.Fatalf("read back: %v", err)
		}
		return in
	}

	if in := reopen(); in != 100 {
		t.Errorf("dry run must not write: input = %d, want 100", in)
	}
}

func TestRun_ApplyWrites(t *testing.T) {
	dbPath := openTestDB(t)
	insertRow(t, 1, "codebuddy", "rid", 100, 10, 1)

	turns := map[string]*turnTotals{
		"rid": {Calls: 2, InputTokens: 300, OutputTokens: 30, Credit: 3.25},
	}
	if err := run(dbPath, turns, true); err != nil {
		t.Fatalf("run apply: %v", err)
	}

	if err := store.Open(dbPath); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store.Close()
	var in, total int64
	if err := store.ReadDB().QueryRow(`SELECT input_tokens, total_tokens FROM chat_metadata WHERE message_id = 1`).
		Scan(&in, &total); err != nil {
		t.Fatalf("read back: %v", err)
	}
	if in != 300 {
		t.Errorf("apply must write: input = %d, want 300", in)
	}
	if total != 330 {
		t.Errorf("total_tokens must be input+output: got %d, want 330", total)
	}
}

func TestRun_OpenFailure(t *testing.T) {
	// A directory can never be opened as a SQLite file.
	if err := run(t.TempDir(), nil, false); err == nil {
		t.Fatal("expected an error when the db path is a directory")
	}
}

func TestReportPlan_PrintsWithoutPlan(t *testing.T) {
	t.Parallel()
	// Smoke test: the report must not panic on an empty plan (the common
	// "nothing to repair" case) or on a plan shorter than the top-5 window.
	reportPlan(nil, planStats{TotalRows: 3, Matched: 3, Unchanged: 3})
	reportPlan([]backfillRow{{
		MessageID: 1, Calls: 1, Model: "glm",
		OldInput: 10, NewInput: 20, OldCredit: 0.1, NewCredit: 0.2,
	}}, planStats{TotalRows: 1, Matched: 1, IncreaseInput: 10, IncreaseCredit: 0.1})
}

func TestReportPlan_SortsByAbsoluteDeltaAndCapsTopFive(t *testing.T) {
	t.Parallel()
	// The "largest repairs" list ranks by |new-old| so the 43x-over rows (huge
	// negatives) are not buried under the small positive ones, and it stops at
	// five regardless of how many rows are planned.
	plan := []backfillRow{
		{MessageID: 1, OldInput: 10, NewInput: 20},       // +10
		{MessageID: 2, OldInput: 100, NewInput: 40},      // -60
		{MessageID: 3, OldInput: 1, NewInput: 2},         // +1
		{MessageID: 4, OldInput: 0, NewInput: 500},       // +500
		{MessageID: 5, OldInput: 9000, NewInput: 100},    // -8900
		{MessageID: 6, OldInput: 7, NewInput: 7 + 30},    // +30
		{MessageID: 7, OldInput: 200, NewInput: 200 + 1}, // +1
	}
	reportPlan(plan, planStats{TotalRows: 7, Matched: 7, IncreaseInput: 542, DecreaseInput: -8960})
}

func TestRunCLI_RequiresBothFlags(t *testing.T) {
	if err := runCLI(nil); err == nil {
		t.Fatal("expected an error when -db/-logs are missing")
	}
	if err := runCLI([]string{"-db", filepath.Join(t.TempDir(), "x.db")}); err == nil {
		t.Fatal("expected an error when -logs is missing")
	}
}

func TestRunCLI_RejectsUnknownFlag(t *testing.T) {
	if err := runCLI([]string{"-nope"}); err == nil {
		t.Fatal("expected a flag parse error")
	}
}

func TestRunCLI_EmptyTranscriptsFailsLoud(t *testing.T) {
	// A -logs path that contains no CodeBuddy usage must NOT report a clean
	// zero-row run: that is the signature of pointing at the wrong directory.
	err := runCLI([]string{
		"-db", filepath.Join(t.TempDir(), "x.db"),
		"-logs", t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected an error for an empty transcript set")
	}
	if !strings.Contains(err.Error(), "no CodeBuddy usage found") {
		t.Errorf("error should name the empty-transcript cause, got %v", err)
	}
}

func TestRunCLI_HappyPathDryRun(t *testing.T) {
	logs := t.TempDir()
	writeFile(t, logs, "proj/ses.jsonl", usageLine("rid", 100, 10, 1)+"\n")
	dbPath := openTestDB(t)
	insertRow(t, 1, "codebuddy", "rid", 100, 10, 1)

	if err := runCLI([]string{"-db", dbPath, "-logs", logs}); err != nil {
		t.Fatalf("runCLI: %v", err)
	}
}

func TestBuildPlan_QueryError(t *testing.T) {
	openTestDB(t)
	store.Close() // handles stay non-nil but unusable — the contract callers rely on
	if _, _, err := buildPlan(nil); err == nil {
		t.Fatal("expected an error from a closed database")
	}
}

func TestApplyPlan_BeginError(t *testing.T) {
	openTestDB(t)
	store.Close()
	err := applyPlan([]backfillRow{{MessageID: 1, NewInput: 1}})
	if err == nil {
		t.Fatal("expected an error when the write handle is closed")
	}
	// WriteBegin must not leave the global write mutex held on its error path:
	// the next writer would block forever (the test would time out instead of
	// reporting a failure).
	acquired := make(chan struct{})
	go func() {
		store.WriteLock()
		store.WriteUnlock()
		close(acquired)
	}()
	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("write lock was not released after a failed WriteBegin")
	}
}

func TestReadTranscripts_MissingRootIsTolerated(t *testing.T) {
	// A missing root surfaces as a walk error on the callback, which is logged
	// and skipped rather than aborting: the affected rows stay unmatched and
	// show up in the report instead of the whole backfill dying.
	turns := readTranscripts(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(turns) != 0 {
		t.Errorf("want no turns, got %+v", turns)
	}
}

func TestReadTranscripts_SkipsUnreadableTranscript(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions, so the open cannot be made to fail")
	}
	root := t.TempDir()
	// A transcript the process cannot read exercises the per-file warning path:
	// the walk succeeds, one file is skipped, the rest are still counted.
	broken := writeFile(t, root, "proj/broken.jsonl", usageLine("rid-broken", 10, 1, 0)+"\n")
	if err := os.Chmod(broken, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(broken, 0o644) })
	writeFile(t, root, "proj/ok.jsonl", usageLine("rid-ok", 10, 1, 0)+"\n")

	turns := readTranscripts(root)
	if turns["rid-ok"] == nil {
		t.Errorf("the readable transcript must still be counted: %+v", turns)
	}
	if len(turns) != 1 {
		t.Errorf("want exactly 1 request, got %d", len(turns))
	}
	if turns["rid-broken"] != nil {
		t.Error("the unreadable transcript must be skipped")
	}
}

func TestRun_BuildPlanError(t *testing.T) {
	// A database without the ledger table: buildPlan's query fails, and run must
	// report it instead of silently writing nothing.
	dbPath := filepath.Join(t.TempDir(), "empty.db")
	if err := store.Open(dbPath); err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	store.Close()

	err := run(dbPath, map[string]*turnTotals{"rid": {Calls: 1, InputTokens: 1}}, false)
	if err == nil {
		t.Fatal("expected a build-plan error for a database without chat_metadata")
	}
	if !strings.Contains(err.Error(), "build plan") {
		t.Errorf("error should name the failing stage, got %v", err)
	}
}
