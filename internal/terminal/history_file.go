package terminal

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// maxHistoryFileBytes caps how much of a history file is read. Users with an
// unlimited history setting can accumulate hundreds of megabytes, while the
// panel only ever shows the most recent few hundred entries — reading the tail
// is enough and keeps a history sync cheap.
const maxHistoryFileBytes = 4 << 20

// shellHistoryKind identifies the on-disk format of a history file.
type shellHistoryKind string

const (
	historyBash shellHistoryKind = "bash"
	historyZsh  shellHistoryKind = "zsh"
	historyFish shellHistoryKind = "fish"
)

// shellHistoryFile is one candidate history file with its format.
type shellHistoryFile struct {
	kind shellHistoryKind
	path string
}

// zshExtendedPrefix matches zsh's EXTENDED_HISTORY prefix: `: <start>:<elapsed>;`.
var zshExtendedPrefix = regexp.MustCompile(`^: \d+:\d+;`)

// bashTimestampPrefix matches the `#<epoch>` lines bash writes when
// HISTTIMEFORMAT is set. They are metadata, not commands.
var bashTimestampPrefix = regexp.MustCompile(`^#\d{9,}$`)

// ShellHistoryFiles returns the history files that exist on this machine,
// most-likely shell first. It never writes to them.
//
// Deliberately reads every shell's file rather than only $SHELL's: a user who
// switched shells still wants the commands they ran under the old one. The
// formats are told apart per file, so mixing them is safe.
func ShellHistoryFiles() []shellHistoryFile {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil
	}

	candidates := []shellHistoryFile{
		{kind: historyBash, path: filepath.Join(home, ".bash_history")},
		{kind: historyZsh, path: filepath.Join(home, ".zsh_history")},
		{kind: historyFish, path: fishHistoryPath(home)},
	}

	// $HISTFILE overrides the default for the shell that set it. Only honor it
	// for bash/zsh — fish ignores it and uses its own XDG location.
	if histfile := os.Getenv("HISTFILE"); histfile != "" {
		kind := historyBash
		if strings.Contains(filepath.Base(os.Getenv("SHELL")), "zsh") {
			kind = historyZsh
		}
		candidates = append([]shellHistoryFile{{kind: kind, path: histfile}}, candidates...)
	}

	seen := make(map[string]bool, len(candidates))
	out := make([]shellHistoryFile, 0, len(candidates))
	for _, c := range candidates {
		if c.path == "" || seen[c.path] {
			continue
		}
		seen[c.path] = true
		if info, err := os.Stat(c.path); err == nil && info.Mode().IsRegular() {
			out = append(out, c)
		}
	}
	return out
}

func fishHistoryPath(home string) string {
	if data := os.Getenv("XDG_DATA_HOME"); data != "" {
		return filepath.Join(data, "fish", "fish_history")
	}
	return filepath.Join(home, ".local", "share", "fish", "fish_history")
}

// ReadShellHistory returns the commands recorded by the local shells, oldest
// first. Unreadable files are skipped silently: a container deployment may have
// no history at all, which is an empty list, not an error.
func ReadShellHistory() []string {
	var all []string
	for _, f := range ShellHistoryFiles() {
		data, err := readFileTail(f.path, maxHistoryFileBytes)
		if err != nil {
			continue
		}
		all = append(all, parseShellHistory(f.kind, data)...)
	}
	return all
}

// readFileTail reads at most limit bytes from the end of path. When the file is
// truncated, everything before the first newline is dropped so no partial line
// reaches a parser.
func readFileTail(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	truncated := info.Size() > limit
	if truncated {
		if _, seekErr := f.Seek(-limit, io.SeekEnd); seekErr != nil {
			return nil, seekErr
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	if truncated {
		if nl := bytes.IndexByte(data, '\n'); nl >= 0 {
			data = data[nl+1:]
		}
	}
	return data, nil
}

// parseShellHistory dispatches on the file format. All parsers return entries
// oldest first and skip the leading line when the file was truncated.
func parseShellHistory(kind shellHistoryKind, data []byte) []string {
	switch kind {
	case historyZsh:
		return parseZshHistory(data)
	case historyFish:
		return parseFishHistory(data)
	default:
		return parseBashHistory(data)
	}
}

// parseBashHistory reads one command per line, joining backslash continuations.
// Timestamp comment lines are dropped.
func parseBashHistory(data []byte) []string {
	var out []string
	var pending strings.Builder

	flush := func() {
		if line := strings.TrimSpace(pending.String()); line != "" {
			out = append(out, line)
		}
		pending.Reset()
	}

	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), maxHistoryFileBytes)
	for sc.Scan() {
		line := sc.Text()
		if pending.Len() == 0 && bashTimestampPrefix.MatchString(line) {
			continue
		}
		if strings.HasSuffix(line, `\`) {
			pending.WriteString(strings.TrimRight(strings.TrimSuffix(line, `\`), " \t"))
			pending.WriteByte('\n')
			continue
		}
		pending.WriteString(line)
		flush()
	}
	flush()
	return out
}

// parseZshHistory strips the EXTENDED_HISTORY prefix (`: <start>:<elapsed>;`)
// when present and keeps plain lines as-is, so it works with or without
// `setopt EXTENDED_HISTORY`.
func parseZshHistory(data []byte) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), maxHistoryFileBytes)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if loc := zshExtendedPrefix.FindStringIndex(line); loc != nil {
			line = strings.TrimSpace(line[loc[1]:])
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// parseFishHistory reads fish's YAML-ish format: `- cmd: <command>` with an
// indented `when:` line following. Newlines inside a command are stored
// escaped as `\n`.
func parseFishHistory(data []byte) []string {
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), maxHistoryFileBytes)
	for sc.Scan() {
		line := sc.Text()
		const marker = "- cmd: "
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		cmd := strings.TrimSpace(line[idx+len(marker):])
		cmd = strings.ReplaceAll(cmd, `\n`, "\n")
		if cmd != "" {
			out = append(out, cmd)
		}
	}
	return out
}
