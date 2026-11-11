package handler

import (
	"fmt"
	"strings"
)

// Windows command-line quoting for the restart sentinel.
//
// The sentinel is `cmd /c "<script>"`, so an argument has to survive TWO
// parsers: cmd.exe's own metacharacter pass, then the CreateProcess/
// CommandLineToArgvW rules the child actually sees. The POSIX shellQuote used
// by the Unix sentinel satisfies neither — cmd.exe does not treat single
// quotes as quotes at all, so `'--port'` arrived as the literal token
// `'--port'`, every flag comparison in cmd/server/main.go missed, and the new
// process silently came up on the DEFAULT port and the DEFAULT data directory.
//
// These helpers implement the second layer only (the CommandLineToArgvW rules,
// byte-for-byte the algorithm in $GOROOT/src/syscall/exec_windows.go). They
// live in a build-tag-free file so the quoting can be unit-tested on any OS:
// the Windows path is otherwise compiled but never executed by CI, which is
// how the single-quote bug shipped in the first place.
//
// Residual caveat, deliberately not "fixed": cmd.exe expands %VAR% even inside
// double quotes. Escaping it is not portable (%% is literal %% on the command
// line but a single % inside a batch file), so a data directory containing a
// percent sign that happens to match an environment variable name would still
// be rewritten. Every other metacharacter (& | < > ^ ( )) is protected by the
// quoting below.

// escapeWindowsArg quotes one argument so CreateProcess hands the child exactly
// s. Mirrors syscall.EscapeArg: plain tokens are returned untouched, anything
// containing a quote, backslash, space or tab gets quoted with the MSVC
// backslash-doubling rules.
func escapeWindowsArg(s string) string {
	if s == "" {
		return `""`
	}
	needsBackslash := false
	hasSpace := false
	for i := range len(s) {
		switch s[i] {
		case '"', '\\':
			needsBackslash = true
		case ' ', '\t':
			hasSpace = true
		}
	}
	if !needsBackslash && !hasSpace {
		return s
	}

	var b strings.Builder
	if hasSpace {
		b.WriteByte('"')
	}
	if !needsBackslash {
		// Spaces only: the surrounding quotes are the whole story.
		b.WriteString(s)
		b.WriteByte('"')
		return b.String()
	}

	slashes := 0
	for i := range len(s) {
		c := s[i]
		switch c {
		default:
			slashes = 0
		case '\\':
			slashes++
		case '"':
			// Backslashes before a quote must be doubled, plus one to escape it.
			b.WriteString(strings.Repeat(`\`, slashes))
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	if hasSpace {
		// Trailing backslashes would otherwise escape the closing quote.
		b.WriteString(strings.Repeat(`\`, slashes))
		b.WriteByte('"')
	}
	return b.String()
}

// escapeWindowsArgs joins args with the same quoting joinArgs applies for
// POSIX, but using the Windows rules above.
func escapeWindowsArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = escapeWindowsArg(arg)
	}
	return strings.Join(quoted, " ")
}

// buildWindowsSentinelScript renders the cmd.exe script the sentinel runs: wait
// for the outgoing process to EXIT — that is when the data-directory lock is
// released — then re-exec this binary with the SAME arguments.
//
// The wait used to be `timeout /t 2`, a guess at how long shutdown takes. It is
// the wrong signal: the data-directory lock is held until the process exits,
// which happens well after the port is released (the outgoing server closes ten
// terminals at 3s each). A replacement launched after 2s then burned its own
// lock wait and exited — the 2026-11-11 outage, on Windows the same way.
//
// Wait-Process blocks on the PID instead, and the wait is bounded: if it
// expires the process is force-killed rather than waited on forever, because a
// drain that never finishes would leave the service down with nobody to start
// it. The kill is safe — shutdown was requested before the sentinel was
// launched, so the target is never a healthy server. See sentinelWaitSeconds.
//
// `&` (not `&&`) is deliberate: cmd runs the next command whatever the exit
// code, so a machine without PowerShell degrades to the old "start
// immediately" behavior — which the new process's lock wait still absorbs —
// instead of never restarting at all.
//
// Passing the args through unchanged is the point: --port and --data-dir must
// reach the new process, or it comes up as a second instance on the default
// port writing to ~/.clawbench instead of the configured directory.
func buildWindowsSentinelScript(exe string, args []string, pid int) string {
	// The -Command payload is wrapped in double quotes so cmd.exe passes it
	// through untouched: it contains `;`, `(`, `)`, `{` and `}`, all of which
	// cmd treats as syntax when they are bare. Double quotes INSIDE the payload
	// are still forbidden — those are what cmd mangles — which is why the exe
	// path is quoted outside it, after the `&`.
	//
	// The wait sits in try/catch because Wait-Process reports BOTH "the process
	// is already gone" and "the timeout expired" as errors; `-ErrorAction Stop`
	// makes either one catchable, and swallowing it lets the script fall
	// through to the "is it still there? then kill it" check. Without this the
	// error would abort the rest of the payload and the kill would never run.
	script := fmt.Sprintf(
		`powershell -NoProfile -NonInteractive -Command "`+
			`try { Wait-Process -Id %d -Timeout %d -ErrorAction Stop } catch {}; `+
			`if (Get-Process -Id %d -ErrorAction SilentlyContinue) { `+
			`Stop-Process -Id %d -Force -ErrorAction SilentlyContinue; `+
			`Start-Sleep -Milliseconds 800 }" & %s`,
		pid, sentinelWaitSeconds, pid, pid, escapeWindowsArg(exe),
	)
	if len(args) > 0 {
		script += " " + escapeWindowsArgs(args)
	}
	return script
}
