package service

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// --- finalizeTimer ---

// A fast Finalize must stay silent: it runs on every turn's completion path,
// so logging it unconditionally would drown the log in normal traffic.
func TestFinalizeTimer_FastFinalizeIsSilent(t *testing.T) {
	read := captureLogs(t)
	ft := newFinalizeTimer("sess-fast")
	done := ft.phase("drain_events")
	done()
	ft.done("user", 10)

	if out := read(); strings.Contains(out, "finalize: slow") {
		t.Fatalf("expected no log for a fast finalize, got: %s", out)
	}
}

// A slow Finalize must log its phase breakdown, so the slow step is
// identifiable from the log alone.
func TestFinalizeTimer_SlowFinalizeLogsPhases(t *testing.T) {
	read := captureLogs(t)
	ft := newFinalizeTimer("sess-slow")
	// Backdate the start so the total exceeds the threshold without sleeping.
	ft.start = time.Now().Add(-2 * time.Second)

	doneDrain := ft.phase("drain_events")
	doneDrain()
	doneFinalize := ft.phase("finalize_row")
	doneFinalize()

	ft.done("user", 42)

	out := read()
	if !strings.Contains(out, "finalize: slow") {
		t.Fatalf("expected a slow-finalize log, got: %s", out)
	}
	for _, sub := range []string{"session=sess-slow", "cancel_reason=user", "blocks=42", "drain_events=", "finalize_row=", "total="} {
		if !strings.Contains(out, sub) {
			t.Fatalf("expected log to contain %q, got: %s", sub, out)
		}
	}
}

// Phases must be reported in execution order: the point of the breakdown is to
// see where the time went, which requires a stable ordering.
func TestFinalizeTimer_PhasesKeepExecutionOrder(t *testing.T) {
	ft := newFinalizeTimer("sess-order")
	ft.start = time.Now().Add(-2 * time.Second)
	for _, name := range []string{"drain_events", "flush_pending", "finalize_row"} {
		done := ft.phase(name)
		done()
	}

	if len(ft.phases) != 3 {
		t.Fatalf("expected 3 phases, got %d", len(ft.phases))
	}
	// Each phase is stored as an slog.Attr whose Key is the phase name.
	want := []string{"drain_events", "flush_pending", "finalize_row"}
	for i, attr := range ft.phases {
		a, ok := attr.(slog.Attr)
		if !ok {
			t.Fatalf("phase %d is not an slog.Attr: %T", i, attr)
		}
		if a.Key != want[i] {
			t.Fatalf("phase %d = %q, want %q", i, a.Key, want[i])
		}
	}
}
