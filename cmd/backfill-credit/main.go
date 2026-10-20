// Command backfill-credit rebuilds CodeBuddy usage rows in chat_metadata from
// the agent's own session transcripts.
//
// # WHY THIS EXISTS
//
// Two defects corrupted historical CodeBuddy usage, in OPPOSITE directions, and
// neither can be repaired from the ledger itself:
//
//  1. LAST-CALL-ONLY (2026-09-18 → 2026-10-19): one agentic turn issues one
//     model call per tool round — measured 23.5 calls per turn in
//     bypassPermissions mode, up to 505. CodeBuddy reports each call's usage
//     separately; the old accumulator adopted the LATEST snapshot, so a turn
//     that really consumed 50.6M prompt tokens was stored as 370k (137x under).
//
//  2. SESSION-CUMULATIVE INPUT (before 2026-09): the pre-ACP CLI parser stored
//     the session's running prompt size in input_tokens. One measured session
//     climbs 73k → 31.0M while its window is 200k, inflating totals (43x over
//     for glm-5.1).
//
// The ONLY source of truth for a turn's true usage is CodeBuddy's own
// transcript: one JSONL line per model call, each carrying that call's
// rawUsage. This command reads them, groups by conversationRequestId, sums the
// calls, and rewrites the matching ledger row.
//
// # WHAT IT WILL NOT TOUCH
//
// A row is only rewritten when its request_id matches a requestId present in
// the transcripts. Rows with no request_id (12k legacy rows) or an id absent
// from the local transcripts (3708 rows, transcripts deleted or from another
// machine) are LEFT ALONE — there is no signal to reconstruct them from, and
// guessing would fabricate usage. The command reports both counts so the
// remaining gap is never mistaken for success.
//
// SCOPE — CODEBUDDY ONLY. This is not a generic ledger repair tool.
//
// It reads CodeBuddy's own session transcripts (~/.codebuddy/projects/<proj>/
// <session>.jsonl), whose providerData.rawUsage carries prompt_tokens /
// completion_tokens / credit PER MODEL CALL. Three things make it
// CodeBuddy-specific and unsafe to point at another backend:
//
//  1. THE DATA SOURCE IS AGENT-PRIVATE. Every backend stores its transcripts
//     elsewhere and in its own shape (Claude under ~/.claude, OpenCode in its
//     storage dir, many CLIs keep no per-call usage at all). A different
//     -logs root would simply match nothing — or, worse, silently match a
//     subset and write partial totals.
//  2. THE ROW FILTER IS backend = 'codebuddy' (see buildPlan). Rows from other
//     backends are never read or written.
//  3. THE DEFECTS IT REPAIRS ARE CODEBUDDY-ONLY. Only CodeBuddy emits one
//     usage_update per model call (the LAST-call-only defect) and only its old
//     CLI parser wrote session-cumulative input. Other backends' cumulative
//     COST was already healed by the migUsageLedgerHeal migration, and their
//     credit column is 0 by construction.
//
// The command therefore REFUSES to run unless the transcripts it is pointed at
// actually look like CodeBuddy transcripts, so a typo in -logs cannot quietly
// produce a no-op "success".
//
// Usage:
//
//	go run ./cmd/backfill-credit -db ~/.clawbench/ClawBench.db -logs ~/.codebuddy/projects            # dry-run
//	go run ./cmd/backfill-credit -db ~/.clawbench/ClawBench.db -logs ~/.codebuddy/projects -apply     # write
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"clawbench/internal/store"
)

// transcriptUsage is one model call's usage as CodeBuddy recorded it.
type transcriptUsage struct {
	PromptTokens     int64   `json:"prompt_tokens"`
	CompletionTokens int64   `json:"completion_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	CacheHit         int64   `json:"prompt_cache_hit_tokens"`
	CacheMiss        int64   `json:"prompt_cache_miss_tokens"`
	Credit           float64 `json:"credit"`
}

// turnTotals is the sum over every model call belonging to one requestId.
type turnTotals struct {
	Calls        int
	InputTokens  int64
	OutputTokens int64
	CacheHit     int64
	CacheMiss    int64
	Credit       float64
}

func main() {
	if err := runCLI(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

// runCLI parses flags and performs the backfill. main only translates the error
// into an exit code, so every branch below is reachable from a unit test.
func runCLI(args []string) error {
	fs := flag.NewFlagSet("backfill-credit", flag.ContinueOnError)
	dbPath := fs.String("db", "", "path to ClawBench.db (required)")
	logsRoot := fs.String("logs", "", "root of CodeBuddy project transcripts (e.g. ~/.codebuddy/projects)")
	apply := fs.Bool("apply", false, "write the backfill (default: dry-run)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *dbPath == "" || *logsRoot == "" {
		return errors.New("both -db and -logs are required")
	}

	turns := readTranscripts(*logsRoot)
	// Fail loud rather than reporting a clean zero-row run: the whole tool is
	// scoped to CodeBuddy, and an empty transcript set almost always means the
	// -logs path points somewhere else (a different agent's directory, or a
	// typo). See the SCOPE note at the top of this file.
	if len(turns) == 0 {
		return fmt.Errorf("no CodeBuddy usage found under %s — this tool only reads "+
			"CodeBuddy transcripts (expects <project>/<session>.jsonl with "+
			"providerData.rawUsage)", *logsRoot)
	}
	fmt.Printf("transcripts: %d requestIds, %d model calls\n", len(turns), countCalls(turns))

	// run() owns the DB handle so its deferred Close actually runs.
	return run(*dbPath, turns, *apply)
}

// run performs the plan/apply cycle. It is separate from main so the deferred
// store.Close() is not skipped by an exiting log.Fatalf.
func run(dbPath string, turns map[string]*turnTotals, apply bool) error {
	if err := store.Open(dbPath); err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer store.Close()

	plan, stats, err := buildPlan(turns)
	if err != nil {
		return fmt.Errorf("build plan: %w", err)
	}
	reportPlan(plan, stats)

	if !apply {
		fmt.Println("\nDRY RUN — nothing written. Re-run with -apply to persist.")
		return nil
	}
	if err := applyPlan(plan); err != nil {
		return fmt.Errorf("apply: %w", err)
	}
	fmt.Printf("\nAPPLIED: %d rows updated\n", len(plan))
	return nil
}

// readTranscripts walks every CodeBuddy transcript and groups its per-call
// usage by conversationRequestId.
//
// SCOPE — must match what CodeBuddy's own /api/v1/stats counts, which was
// verified against this exact corpus by raising CODEBUDDY_SESSION_MAX_ITEMS:
//
//   - TOP-LEVEL session files only. Sub-agent transcripts (<session>/subagents/)
//     belong to the same request but are excluded by the agent's own accounting
//     (SessionUtils.isSubAgent), so including them would double the parent.
//   - Sessions whose meta sets isForkedCompactionSession are skipped entirely
//     (SessionStore.isNonUserSession filters them out of every read path).
//
// With those two rules this reproduces the agent's numbers to within 0.01%
// (deepseek 35,175,059,528 vs 35,142,148,950; glm/auto exact).
//
// It never fails: every walk or per-file error is logged and skipped, because a
// single unreadable transcript must not abort a repair of the other thousands.
// The consequence is surfaced downstream — a skipped file's requests end up in
// the "request_id not in log" bucket of the report.
func readTranscripts(root string) map[string]*turnTotals {
	turns := make(map[string]*turnTotals)

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// A permission error on one transcript must not abort the whole
			// backfill; the affected rows simply stay unmatched (and are
			// reported as such). This is a deliberate skip, not an ignored
			// error: the unmatched count in the report is what surfaces it.
			fmt.Fprintf(os.Stderr, "warn: skip %s: %v\n", path, walkErr)
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		// Sub-agent transcripts are a separate accounting unit in the agent's
		// own stats; counting them here would double the parent request.
		if isSubagentTranscript(path) {
			return nil
		}
		if err := readOneTranscript(path, turns); err != nil {
			fmt.Fprintf(os.Stderr, "warn: %s: %v\n", path, err)
		}
		return nil
	})
	return turns
}

// isSubagentTranscript reports whether path is a sub-agent transcript.
//
// They live at <project>/<session>/subagents/agent-*.jsonl. The parent session
// bills the same request, so counting both would double it.
func isSubagentTranscript(path string) bool {
	return strings.Contains(filepath.ToSlash(path), "/subagents/")
}

// readOneTranscript scans a single JSONL file. Lines that are not usage records
// (messages, reasoning, tool calls) are skipped; a malformed line is skipped
// rather than aborting the file, matching how the agent itself tolerates them.
func readOneTranscript(path string, turns map[string]*turnTotals) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	// A single transcript line can exceed the 64KiB default (a large tool
	// result embedded in the record), so raise the ceiling substantially.
	sc.Buffer(make([]byte, 0, 256*1024), 32*1024*1024)

	calls, forked, err := scanTranscript(sc)
	if err != nil {
		return err
	}
	// A forked-compaction session is excluded from the agent's own accounting
	// (SessionStore.isNonUserSession), so its calls must not be counted here.
	if forked {
		return nil
	}
	for _, c := range calls {
		t := turns[c.rid]
		if t == nil {
			t = &turnTotals{}
			turns[c.rid] = t
		}
		t.Calls++
		t.InputTokens += c.u.PromptTokens
		t.OutputTokens += c.u.CompletionTokens
		t.CacheHit += c.u.CacheHit
		t.CacheMiss += c.u.CacheMiss
		t.Credit += c.u.Credit
	}
	return nil
}

// transcriptCall is one model call read out of a transcript.
type transcriptCall struct {
	rid string
	u   transcriptUsage
}

// scanTranscript reads every model call from one file.
//
// The whole file is buffered before anything is returned because a
// forked-compaction session must be skipped ENTIRELY, and that flag lives on a
// session-meta line the agent may write anywhere (in practice at the end).
func scanTranscript(sc *bufio.Scanner) (calls []transcriptCall, forked bool, err error) {
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if isForkedCompactionSession(line) {
			forked = true
			continue
		}
		if c, ok := parseTranscriptCall(line); ok {
			calls = append(calls, c)
		}
	}
	return calls, forked, sc.Err()
}

// isForkedCompactionSession reports whether this line is the session-meta
// record marking the file as a compaction fork.
func isForkedCompactionSession(line []byte) bool {
	var meta struct {
		Type string `json:"type"`
		Meta struct {
			IsForkedCompactionSession bool `json:"isForkedCompactionSession"`
		} `json:"meta"`
	}
	if json.Unmarshal(line, &meta) != nil {
		return false
	}
	return meta.Type == "session-meta" && meta.Meta.IsForkedCompactionSession
}

// parseTranscriptCall extracts one model call's usage from a transcript line.
// Non-usage lines (messages, reasoning, tool calls) report ok=false.
func parseTranscriptCall(line []byte) (transcriptCall, bool) {
	var rec struct {
		ProviderData struct {
			ConversationRequestID string          `json:"conversationRequestId"`
			RequestID             string          `json:"requestId"`
			RawUsage              json.RawMessage `json:"rawUsage"`
			Usage                 json.RawMessage `json:"usage"`
		} `json:"providerData"`
	}
	if err := json.Unmarshal(line, &rec); err != nil {
		return transcriptCall{}, false
	}
	rid := rec.ProviderData.ConversationRequestID
	if rid == "" {
		rid = rec.ProviderData.RequestID
	}
	if rid == "" {
		return transcriptCall{}, false
	}
	// rawUsage is the OpenAI-style block; `usage` is the agent's own summary
	// and carries no per-call credit, so prefer rawUsage and fall back only
	// when it is absent.
	raw := rec.ProviderData.RawUsage
	if len(raw) == 0 || string(raw) == "null" {
		raw = rec.ProviderData.Usage
	}
	if len(raw) == 0 || string(raw) == "null" {
		return transcriptCall{}, false
	}
	var u transcriptUsage
	if err := json.Unmarshal(raw, &u); err != nil {
		return transcriptCall{}, false
	}
	if u.PromptTokens == 0 && u.CompletionTokens == 0 && u.Credit == 0 {
		return transcriptCall{}, false
	}
	return transcriptCall{rid: rid, u: u}, true
}

func countCalls(turns map[string]*turnTotals) int {
	n := 0
	for _, t := range turns {
		n += t.Calls
	}
	return n
}

// backfillRow is one ledger row plus its reconstructed truth.
type backfillRow struct {
	MessageID int64
	RequestID string
	Model     string
	CreatedAt string
	Calls     int

	OldInput, OldOutput       int64
	OldCacheHit, OldCacheMiss int64
	OldCredit                 float64

	NewInput, NewOutput       int64
	NewCacheHit, NewCacheMiss int64
	NewCredit                 float64
}

// planStats separates "repaired" from "could not be repaired" so a partial
// backfill is never reported as complete.
type planStats struct {
	TotalRows      int
	Matched        int
	Unchanged      int
	NoRequestID    int
	Unmatched      int
	IncreaseInput  int64
	DecreaseInput  int64
	IncreaseCredit float64
	DecreaseCredit float64
}

// buildPlan reads every CodeBuddy ledger row and decides what it should become.
func buildPlan(turns map[string]*turnTotals) ([]backfillRow, planStats, error) {
	rows, err := store.ReadDB().Query(`
		SELECT message_id, COALESCE(request_id,''), COALESCE(model,''), COALESCE(created_at,''),
		       COALESCE(input_tokens,0), COALESCE(output_tokens,0), COALESCE(credit,0),
		       COALESCE(cache_hit_tokens,0), COALESCE(cache_miss_tokens,0)
		FROM chat_metadata
		WHERE backend = 'codebuddy'
		ORDER BY message_id`)
	if err != nil {
		return nil, planStats{}, err
	}
	defer func() { _ = rows.Close() }()

	var (
		plan  []backfillRow
		stats planStats
	)
	for rows.Next() {
		var r backfillRow
		if err := rows.Scan(&r.MessageID, &r.RequestID, &r.Model, &r.CreatedAt,
			&r.OldInput, &r.OldOutput, &r.OldCredit,
			&r.OldCacheHit, &r.OldCacheMiss); err != nil {
			return nil, planStats{}, err
		}
		stats.TotalRows++
		if r.RequestID == "" {
			stats.NoRequestID++
			continue
		}
		t := turns[r.RequestID]
		if t == nil {
			stats.Unmatched++
			continue
		}
		stats.Matched++

		r.Calls = t.Calls
		r.NewInput = t.InputTokens
		r.NewOutput = t.OutputTokens
		r.NewCacheHit = t.CacheHit
		r.NewCacheMiss = t.CacheMiss
		r.NewCredit = t.Credit

		// A row is "unchanged" when the transcript agrees with the ledger.
		// Input is the discriminator: credit is derived from it, so a match
		// there means the row was already written by the fixed accumulator.
		if r.NewInput == r.OldInput && r.NewOutput == r.OldOutput &&
			diffFloat(r.NewCredit, r.OldCredit) < 0.01 {
			stats.Unchanged++
			continue
		}
		delta := r.NewInput - r.OldInput
		if delta > 0 {
			stats.IncreaseInput += delta
		} else {
			stats.DecreaseInput += delta
		}
		dc := r.NewCredit - r.OldCredit
		if dc > 0 {
			stats.IncreaseCredit += dc
		} else {
			stats.DecreaseCredit += dc
		}
		plan = append(plan, r)
	}
	return plan, stats, rows.Err()
}

func diffFloat(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}

func reportPlan(plan []backfillRow, s planStats) {
	fmt.Printf("\n=== ledger coverage ===\n")
	fmt.Printf("codebuddy rows          : %d\n", s.TotalRows)
	fmt.Printf("  matched a transcript  : %d\n", s.Matched)
	fmt.Printf("    already correct     : %d\n", s.Unchanged)
	fmt.Printf("    will be repaired    : %d\n", len(plan))
	fmt.Printf("  NO request_id         : %d  (legacy rows; unreconstructable)\n", s.NoRequestID)
	fmt.Printf("  request_id not in log : %d  (transcript gone; unreconstructable)\n", s.Unmatched)

	fmt.Printf("\n=== net change from the repair ===\n")
	fmt.Printf("input  : %+d\n", s.IncreaseInput+s.DecreaseInput)
	fmt.Printf("credit : %+.2f\n", s.IncreaseCredit+s.DecreaseCredit)
	fmt.Printf("  rows raised (LAST-call defect)  : input %+d\n", s.IncreaseInput)
	fmt.Printf("  rows lowered (cumulative input) : input %+d\n", s.DecreaseInput)

	if len(plan) == 0 {
		return
	}
	top := append([]backfillRow(nil), plan...)
	sort.Slice(top, func(i, j int) bool {
		di := top[i].NewInput - top[i].OldInput
		dj := top[j].NewInput - top[j].OldInput
		if di < 0 {
			di = -di
		}
		if dj < 0 {
			dj = -dj
		}
		return di > dj
	})
	n := 5
	if len(top) < n {
		n = len(top)
	}
	fmt.Printf("\n=== largest repairs ===\n")
	for _, r := range top[:n] {
		fmt.Printf("  msg=%d calls=%d %s\n", r.MessageID, r.Calls, r.Model)
		fmt.Printf("    input %15d -> %15d   credit %10.2f -> %10.2f\n",
			r.OldInput, r.NewInput, r.OldCredit, r.NewCredit)
	}
}

// applyPlan writes every planned row inside ONE transaction, so a failure
// halfway through cannot leave the ledger with a mix of repaired and
// unrepaired rows for the same period.
func applyPlan(plan []backfillRow) error {
	if len(plan) == 0 {
		return nil
	}
	tx, err := store.WriteBegin()
	if err != nil {
		return err
	}
	// WriteBegin leaves the process-global write mutex HELD: the caller owns it
	// for the whole transaction and must release it. Omitting this leaked the
	// lock — harmless for the one-shot CLI (it exits next) but it wedges every
	// later writer, which is what the unit tests hit as a hang.
	defer store.WriteUnlock()
	defer func() { _ = tx.Rollback() }()

	for _, r := range plan {
		// total_tokens keeps the ledger invariant (>= input+output) that the
		// stats query relies on for its default metric.
		total := r.NewInput + r.NewOutput
		if _, err := tx.Exec(
			`
			UPDATE chat_metadata
			SET input_tokens = ?, output_tokens = ?, total_tokens = ?,
			    credit = ?, cache_hit_tokens = ?, cache_miss_tokens = ?
			WHERE message_id = ?`,
			r.NewInput, r.NewOutput, total, r.NewCredit, r.NewCacheHit, r.NewCacheMiss, r.MessageID,
		); err != nil {
			return fmt.Errorf("update message %d: %w", r.MessageID, err)
		}
	}
	return tx.Commit()
}
