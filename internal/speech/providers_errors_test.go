package speech

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are the failure paths every Synthesize implementation shares: an output
// directory that cannot be created, an output file that cannot be opened, and a
// server that refuses to answer. Each must fail with ITS OWN error text and
// leave no output file behind — a partially written .mp3 would be served to the
// user as a broken clip.

// outputUnderAFile returns an output path whose parent is a regular file, so
// MkdirAll must fail exactly as it would on a read-only disk.
func outputUnderAFile(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed blocker file: %v", err)
	}
	return filepath.Join(blocker, "sub", "out.mp3")
}

func TestSynthesize_FailsWhenTheOutputDirectoryCannotBeCreated(t *testing.T) {
	cases := []struct {
		name string
		p    SpeechProvider
		want string
	}{
		{"alibaba", newTestAlibabaProvider("http://127.0.0.1:1"), "alibaba-tts: failed to create output directory"},
		{"minimax", newTestMinimaxProvider("http://127.0.0.1:1"), "minimax-tts: failed to create output directory"},
		{"tencent", newTestTencentProvider("http://127.0.0.1:1"), "tencent-tts: failed to create output directory"},
		{"volcano", newTestVolcanoProvider("http://127.0.0.1:1"), "volcano-tts: failed to create output directory"},
	}
	for _, tc := range cases {
		err := tc.p.Synthesize(context.Background(), "你好", outputUnderAFile(t), "zh")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to say %q", tc.name, err, tc.want)
		}
	}
}

func TestSynthesize_FailsWhenTheOutputFileCannotBeCreated(t *testing.T) {
	// A directory in the file's place: MkdirAll succeeds (it exists), os.Create
	// fails.
	outDirIsADir := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		return dir // create over a directory path fails
	}
	cases := []struct {
		name string
		p    SpeechProvider
		want string
	}{
		{"alibaba", newTestAlibabaProvider("http://127.0.0.1:1"), "alibaba-tts: failed to create output file"},
		{"minimax", newTestMinimaxProvider("http://127.0.0.1:1"), "minimax-tts: failed to create output file"},
		{"tencent", newTestTencentProvider("http://127.0.0.1:1"), "tencent-tts: failed to create output file"},
		{"volcano", newTestVolcanoProvider("http://127.0.0.1:1"), "volcano-tts: failed to create output file"},
	}
	for _, tc := range cases {
		err := tc.p.Synthesize(context.Background(), "你好", outDirIsADir(t), "zh")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to say %q", tc.name, err, tc.want)
		}
	}
}

func TestSynthesize_FailsWhenTheServerCannotBeReached(t *testing.T) {
	// 127.0.0.1:1 is always closed: the dial fails immediately, which exercises
	// the request-failure branch without any network access.
	cases := []struct {
		name string
		p    SpeechProvider
		want string
	}{
		{"alibaba", newTestAlibabaProvider("http://127.0.0.1:1"), "alibaba-tts: request failed"},
		{"minimax", newTestMinimaxProvider("http://127.0.0.1:1"), "minimax-tts: request failed"},
		{"tencent", newTestTencentProvider("http://127.0.0.1:1"), "tencent-tts: request failed"},
		{"volcano", newTestVolcanoProvider("http://127.0.0.1:1"), "volcano-tts: request failed"},
	}
	for _, tc := range cases {
		out := filepath.Join(t.TempDir(), "out.mp3")
		err := tc.p.Synthesize(context.Background(), "你好", out, "zh")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to say %q", tc.name, err, tc.want)
		}
		if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
			t.Errorf("%s: a failed synthesis left %s behind", tc.name, out)
		}
	}
}

// TestSynthesize_FailsWhenTheRequestCannotBeBuilt: a base URL carrying a control
// character makes http.NewRequest reject the target. This is the branch between
// "encode the body" and "dial": it must name the provider rather than panic.
func TestSynthesize_FailsWhenTheRequestCannotBeBuilt(t *testing.T) {
	const badBase = "http://bad\nhost"
	cases := []struct {
		name string
		p    SpeechProvider
		want string
	}{
		{"alibaba", newTestAlibabaProvider(badBase), "alibaba-tts: failed to create request"},
		{"minimax", newTestMinimaxProvider(badBase), "minimax-tts: failed to create request"},
		{"tencent", newTestTencentProvider(badBase), "tencent-tts: failed to create request"},
		{"volcano", newTestVolcanoProvider(badBase), "volcano-tts: failed to create request"},
	}
	for _, tc := range cases {
		out := filepath.Join(t.TempDir(), "out.mp3")
		err := tc.p.Synthesize(context.Background(), "你好", out, "zh")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to say %q", tc.name, err, tc.want)
		}
		if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
			t.Errorf("%s: a failed synthesis left %s behind", tc.name, out)
		}
	}
}
