package terminal

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// builtinCommands are shell builtins that never appear on disk, so LookPath
// cannot resolve them. Kept deliberately short: a command missing from this
// list is merely not remembered, while a wrong entry would let noise into the
// history panel.
var builtinCommands = map[string]bool{
	"cd": true, "export": true, "unset": true, "alias": true, "unalias": true,
	"source": true, ".": true, "set": true, "shopt": true, "declare": true,
	"readonly": true, "local": true, "return": true, "shift": true, "trap": true,
	"exit": true, "logout": true, "eval": true, "exec": true, "type": true,
	"history": true, "jobs": true, "fg": true, "bg": true, "wait": true,
	"pushd": true, "popd": true, "dirs": true, "umask": true, "ulimit": true,
	"command": true, "builtin": true, "hash": true, "help": true, "let": true,
	"read": true, "printf": true, "echo": true, "pwd": true, "test": true,
	"[": true, "true": true, "false": true, ":": true,
}

// maxCommandLength bounds what is stored per entry. Longer "commands" are
// pastes of files or generated blobs, not something worth recalling.
const maxCommandLength = 4096

// credentialAssignmentRe matches `NAME=value` / `NAME: value` where NAME looks
// like it names a credential. It keys on the variable name rather than the
// value, because the value is exactly what we must never have to recognize:
// `export OPENAI_KEY=sk-...` and `GH_TOKEN=ghp_...` share no literal.
var credentialAssignmentRe = regexp.MustCompile(
	`(?i)[A-Za-z0-9_]*(?:key|token|secret|password|passwd|credential)[A-Za-z0-9_]*\s*[=:]\s*\S+`)

// IsSensitiveCommand reports whether a command looks like it carries a
// credential. Such commands are never recorded: the history panel is for
// recalling commands, and `export OPENAI_KEY=sk-...` has no recall value while
// having every reason not to sit in a list that gets shown on screen.
//
// The match is intentionally narrow — a credential-named assignment, or an
// explicit --password flag. `-p` style flags are NOT matched because
// `mkdir -p`, `ssh -p 22` and `docker run -p` are far more common than
// `mysql -p<secret>`.
func IsSensitiveCommand(command string) bool {
	if credentialAssignmentRe.MatchString(command) {
		return true
	}
	lower := strings.ToLower(command)
	for _, marker := range []string{"--password", "-password", "authorization: bearer"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// LooksLikeCommand reports whether a captured line is plausibly a command typed
// at a shell prompt, as opposed to keystrokes consumed by a full-screen program
// (vim's `:wq`, less's `/needle`, an ssh session's remote command).
//
// The frontend accumulates the current input line from the raw byte stream, so
// this is the second line of defense: on Linux the caller also checks that the
// PTY's foreground process is still the shell, which rejects program input
// outright. Platforms without that probe rely on this heuristic alone, which is
// why it errs toward false negatives.
func LooksLikeCommand(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || len(line) > maxCommandLength {
		return false
	}
	for _, r := range line {
		if r < 0x20 && r != '\t' {
			return false
		}
	}

	first := firstCommandWord(line)
	if first == "" {
		return false
	}
	// vim/ex commands and less/more searches start with these; no real command
	// does. (`!` is deliberately absent — bash uses it for history expansion.)
	if strings.HasPrefix(first, ":") || strings.HasPrefix(first, "/") ||
		strings.HasPrefix(first, "?") {
		return false
	}
	if builtinCommands[first] {
		return true
	}
	if strings.Contains(first, "/") {
		info, err := os.Stat(first)
		return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
	}
	_, err := exec.LookPath(first)
	return err == nil
}

// firstCommandWord returns the command name of a shell line, skipping leading
// `VAR=value` assignments so `LC_ALL=C grep x` resolves to "grep".
func firstCommandWord(line string) string {
	for _, tok := range splitShellWords(line) {
		if isAssignment(tok) {
			continue
		}
		return tok
	}
	return ""
}

// isAssignment reports whether tok is a leading `NAME=value` environment
// assignment (as opposed to a command that merely contains '=').
func isAssignment(tok string) bool {
	eq := strings.IndexByte(tok, '=')
	if eq <= 0 {
		return false
	}
	for i := range eq {
		c := tok[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// splitShellWords splits on unquoted whitespace and strips surrounding quotes
// from each token. It is not a full shell parser — only enough to find the
// first word of a line for the checks above.
func splitShellWords(line string) []string {
	var words []string
	var cur strings.Builder
	var quote byte
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for i := range len(line) {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
				continue
			}
			cur.WriteByte(c)
		case c == '\'' || c == '"':
			quote = c
		case c == ' ' || c == '\t':
			flush()
		case c == ';' || c == '|' || c == '&':
			flush()
			if len(words) > 0 {
				return words
			}
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return words
}
