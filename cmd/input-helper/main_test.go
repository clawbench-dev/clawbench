package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"clawbench/internal/inputinject"
)

// decodeReplies parses the helper's output back into replies. Asserting on the
// decoded values rather than on the raw bytes keeps the test about the protocol
// contract (ok + error text) instead of about JSON key ordering.
func decodeReplies(t *testing.T, out string) []reply {
	t.Helper()
	if out == "" {
		return nil
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("every reply must be newline-terminated, got %q", out)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	replies := make([]reply, 0, len(lines))
	for i, line := range lines {
		var r reply
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("reply %d is not JSON (%q): %v", i, line, err)
		}
		replies = append(replies, r)
	}
	return replies
}

// appliedReply is what a valid event produces on this build: success where an
// injector exists, the stub's error where it does not. Keeping it derived from
// the build tag means the same test runs on every platform.
func appliedReply() reply {
	if inputinject.Supported() {
		return reply{OK: true}
	}
	return reply{Error: inputinject.ErrUnsupported.Error()}
}

// TestRun_OneReplyPerLine covers the line protocol: every non-empty line yields
// exactly one reply, blank lines are skipped, and a final line without a
// trailing newline is still processed.
func TestRun_OneReplyPerLine(t *testing.T) {
	want := appliedReply()

	cases := []struct {
		name  string
		input string
		want  []reply
	}{
		{
			name:  "blank lines are skipped",
			input: "\n\n\n",
			want:  nil,
		},
		{
			name:  "one event",
			input: `{"kind":"move","x":0.5,"y":0.5}` + "\n",
			want:  []reply{want},
		},
		{
			name:  "blank lines between events do not shift the replies",
			input: "\n" + `{"kind":"move","x":0.1,"y":0.1}` + "\n\n" + `{"kind":"down","x":0.2,"y":0.2}` + "\n",
			want:  []reply{want, want},
		},
		{
			name:  "the last line needs no newline",
			input: `{"kind":"key","key":"Enter"}`,
			want:  []reply{want},
		},
		{
			name:  "empty input produces nothing",
			input: "",
			want:  nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := run(strings.NewReader(tc.input), &out); err != nil {
				t.Fatalf("run: %v", err)
			}
			got := decodeReplies(t, out.String())
			if len(got) != len(tc.want) {
				t.Fatalf("got %d replies (%+v), want %d", len(got), got, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("reply %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestRun_ReportsMalformedLines covers the failure replies: the renderer has to
// learn which line was rejected and why, so the error text is part of the
// protocol, not decoration.
func TestRun_ReportsMalformedLines(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		wantSub string
	}{
		{
			name:    "whitespace is not an empty line",
			line:    "   ",
			wantSub: "empty event",
		},
		{
			name:    "unknown kind",
			line:    `{"kind":"bogus"}`,
			wantSub: `unknown kind "bogus"`,
		},
		{
			name:    "unknown field",
			line:    `{"kind":"move","x":0.1,"y":0.1,"extra":1}`,
			wantSub: "invalid event:",
		},
		{
			name:    "out of range coordinates",
			line:    `{"kind":"move","x":1.5,"y":0.5}`,
			wantSub: "coordinates out of range",
		},
		{
			name:    "not JSON",
			line:    `{oops`,
			wantSub: "invalid event:",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := run(strings.NewReader(tc.line+"\n"), &out); err != nil {
				t.Fatalf("run: %v", err)
			}
			replies := decodeReplies(t, out.String())
			if len(replies) != 1 {
				t.Fatalf("got %d replies (%+v), want exactly one", len(replies), replies)
			}
			if replies[0].OK {
				t.Fatalf("a rejected line must report ok=false, got %+v", replies[0])
			}
			if !strings.Contains(replies[0].Error, tc.wantSub) {
				t.Errorf("error = %q, want it to mention %q", replies[0].Error, tc.wantSub)
			}
		})
	}
}

// TestRun_AcceptsLargeLinesAndStopsOnOverlongOnes pins the scanner limits. The
// renderer types text events up to maxTextRunes, and a single line carrying a
// few hundred KiB must still be delivered; only a line past the hard cap ends
// the helper (the parent then respawns it).
func TestRun_AcceptsLargeLinesAndStopsOnOverlongOnes(t *testing.T) {
	// 200 KiB fits the 256 KiB buffer.
	large := `{"kind":"text","text":"` + strings.Repeat("a", 200*1024) + `"}` + "\n"
	var out bytes.Buffer
	if err := run(strings.NewReader(large), &out); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := decodeReplies(t, out.String()); len(got) != 1 {
		t.Fatalf("got %d replies for a 200 KiB line, want 1", len(got))
	}

	// 300 KiB does not, and the failure has to surface rather than be swallowed.
	tooLong := `{"kind":"text","text":"` + strings.Repeat("a", 300*1024) + `"}` + "\n"
	err := run(strings.NewReader(tooLong), io.Discard)
	if err == nil {
		t.Fatal("a line past the scanner limit should end run with an error")
	}
}

// TestServe_DispatchesOnArgs covers the wiring main hands to serve: only a
// leading --check is special, and every other invocation runs the line loop.
func TestServe_DispatchesOnArgs(t *testing.T) {
	var out bytes.Buffer
	code := serve([]string{"--check"}, strings.NewReader(""), &out)
	if inputinject.Supported() {
		if code != 0 {
			t.Errorf("--check exit code = %d, want 0", code)
		}
	} else if code != 2 {
		t.Errorf("--check exit code = %d, want 2", code)
	}
	if !strings.Contains(out.String(), `"ok"`) {
		t.Errorf("--check output = %q, want a JSON reply", out.String())
	}

	// Anything that is not a leading --check falls through to the line loop.
	for _, args := range [][]string{nil, {"--verbose"}, {"-check"}} {
		out.Reset()
		code = serve(args, strings.NewReader(`{"kind":"bogus"}`+"\n"), &out)
		if code != 0 {
			t.Errorf("args %v: exit code = %d, want 0", args, code)
		}
		if !strings.Contains(out.String(), `"error"`) {
			t.Errorf("args %v: output = %q, want the event to be processed", args, out.String())
		}
	}
}

// TestCheck_ReportsSupport pins the --check contract: the exit code and the
// single line of JSON the desktop app parses to decide whether to spawn the
// helper at all.
func TestCheck_ReportsSupport(t *testing.T) {
	var out bytes.Buffer
	code := check(&out)

	if inputinject.Supported() {
		if code != 0 {
			t.Errorf("exit code = %d, want 0 when an injector is available", code)
		}
		if got := out.String(); got != "{\"ok\":true}\n" {
			t.Errorf("output = %q", got)
		}
		return
	}

	if code != 2 {
		t.Errorf("exit code = %d, want 2 when the platform has no injector", code)
	}
	if got := out.String(); got != "{\"ok\":false,\"error\":\"unsupported\"}\n" {
		t.Errorf("output = %q", got)
	}
}
