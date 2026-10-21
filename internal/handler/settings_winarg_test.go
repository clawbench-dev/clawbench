package handler

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The Windows sentinel is compiled but NEVER executed by CI (every test that
// touches it carries `//go:build !windows`), so the only thing standing between
// a regression and a silent misconfiguration is these string assertions. The
// bug this guards against shipped once already: args were quoted with the
// POSIX shellQuote, cmd.exe treated the single quotes as literal characters,
// and the restarted process came up on the default port and default data dir.

func TestEscapeWindowsArg(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty becomes explicit empty quotes", "", `""`},
		{"plain token is untouched", "--port", "--port"},
		{"path with backslashes and no space", `C:\clawbench\clawbench.exe`, `C:\clawbench\clawbench.exe`},
		{"space forces quotes", `C:\Program Files\clawbench.exe`, `"C:\Program Files\clawbench.exe"`},
		{"tab forces quotes", "a\tb", "\"a\tb\""},
		{"embedded quote is escaped", `a"b`, `a\"b`},
		{"quote plus space", `a "b"`, `"a \"b\""`},
		// Backslashes are only doubled when they precede a quote (or the
		// closing quote), which is what makes the C:\ path above pass through
		// unchanged.
		{"backslash before quote", `a\"b`, `a\\\"b`},
		{"trailing backslash inside quotes", `C:\dir \`, `"C:\dir \\"`},
		{"percent is left alone", `%APPDATA%`, `%APPDATA%`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, escapeWindowsArg(tc.in))
		})
	}
}

func TestEscapeWindowsArgs(t *testing.T) {
	assert.Equal(t, "", escapeWindowsArgs(nil))
	assert.Equal(t, "--port 20000", escapeWindowsArgs([]string{"--port", "20000"}))
	// The regression itself: no single quotes may appear anywhere.
	got := escapeWindowsArgs([]string{"--data-dir", `C:\Users\me\.clawbench`})
	assert.NotContains(t, got, "'", "cmd.exe does not treat single quotes as quotes")
	assert.Equal(t, `--data-dir C:\Users\me\.clawbench`, got)
	assert.Equal(t,
		`--data-dir "C:\Program Files\cb"`,
		escapeWindowsArgs([]string{"--data-dir", `C:\Program Files\cb`}),
	)
}

func TestBuildWindowsSentinelScript(t *testing.T) {
	t.Run("quotes the exe and passes args through", func(t *testing.T) {
		script := buildWindowsSentinelScript(`C:\Program Files\clawbench.exe`, []string{"--port", "20000"})
		assert.Contains(t, script, `timeout /t 2 /nobreak >nul & `)
		assert.Contains(t, script, `"C:\Program Files\clawbench.exe"`)
		assert.Contains(t, script, ` --port 20000`)
		assert.NotContains(t, script, "'")
	})

	// The flag values must reach the child verbatim. cmd/server/main.go compares
	// os.Args[i] with ==, so any quoting that survives into argv is a miss.
	t.Run("flags survive as bare tokens", func(t *testing.T) {
		script := buildWindowsSentinelScript(
			`C:\cb\clawbench.exe`,
			[]string{"--port", "28000", "--data-dir", `D:\cbdata`},
		)
		assert.Contains(t, script, `--port 28000 --data-dir D:\cbdata`)
	})

	t.Run("no trailing space when there are no args", func(t *testing.T) {
		script := buildWindowsSentinelScript(`C:\cb\clawbench.exe`, nil)
		assert.Equal(t, `timeout /t 2 /nobreak >nul & C:\cb\clawbench.exe`, script)
		assert.False(t, strings.HasSuffix(script, " "))
	})

	// A data directory containing a space is the case the old code mangled:
	// joinArgs turned it into 'C:\Program Files\cb' — one token starting with a
	// literal apostrophe.
	t.Run("data dir with spaces stays one argument", func(t *testing.T) {
		script := buildWindowsSentinelScript(
			`C:\cb\clawbench.exe`,
			[]string{"--data-dir", `C:\Program Files\cb`},
		)
		assert.Contains(t, script, `--data-dir "C:\Program Files\cb"`)
		assert.NotContains(t, script, "'")
	})
}

// Guard the pairing: the Windows script must not go back to joinArgs. A future
// refactor that "unifies" the two sentinels would reintroduce the exact bug
// this file exists for, and no runtime test would notice.
func TestWindowsSentinelDoesNotUsePOSIXQuoting(t *testing.T) {
	src, err := os.ReadFile("settings_sentinel_windows.go")
	require.NoError(t, err)
	text := string(src)
	assert.NotContains(t, text, "joinArgs(", "Windows sentinel must not use POSIX quoting")
	assert.NotContains(t, text, "shellQuote(", "Windows sentinel must not use POSIX quoting")
	assert.Contains(t, text, "buildWindowsSentinelScript(")
}
