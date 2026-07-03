// Package startup renders a structured banner on server ready.
// All content lines are aligned using rune-width awareness
// so that CJK characters and emojis do not break the box border.
package startup

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// AgentInfo describes one discovered AI agent for the banner.
type AgentInfo struct {
	Name   string // human-readable agent name (e.g. "codebuddy")
	Models int    // number of available models
}

// BannerConfig holds all data needed to render the startup banner.
type BannerConfig struct {
	Version         string
	Scheme          string // "http" or "https"
	Port            int
	LocalIP         string // non-loopback LAN IP; empty = not available
	AutoPassword    string // plaintext auto-generated password; empty = user configured
	DataDir         string // .clawbench/ absolute path
	Agents          []AgentInfo
	SSHEnabled      bool
	SSHPort         int
	TTSEngine       string
	RAGAvailable    bool
	TerminalOn      bool
	TaskCount       int
	StartupDuration time.Duration
}

// ---------------------------------------------------------------------------
// rune-width helpers (CJK / emoji aware)
// ---------------------------------------------------------------------------

// wideRange defines a Unicode range where all codepoints have display width 2.
type wideRange struct {
	lo, hi rune // inclusive range
}

// wideRanges lists Unicode ranges where characters occupy 2 terminal cells.
// Ordered by lo for binary search. Covers CJK, Hangul, emoji, fullwidth, etc.
var wideRanges = []wideRange{
	{0x1100, 0x115F},   // Hangul Jamo
	{0x2329, 0x232A},   // Angle brackets
	{0x2E80, 0xA4CF},   // CJK Radicals..Yi
	{0xAC00, 0xD7A3},   // Hangul Syllables
	{0xF900, 0xFAFF},   // CJK Compatibility Ideographs
	{0xFE10, 0xFE19},   // Vertical forms
	{0xFE30, 0xFE6F},   // CJK Compatibility Forms
	{0xFF01, 0xFF60},   // Fullwidth forms
	{0xFFE0, 0xFFE6},   // Fullwidth signs
	{0x1F300, 0x1FAFF}, // Emoji & symbols
	{0x20000, 0x2FFFD}, // CJK Extension B..I
	{0x30000, 0x3FFFD}, // CJK Extension G..
}

// excludedCodepoints are codepoints within a wideRange that are actually narrow.
var excludedCodepoints = map[rune]bool{
	0x303F: true, // within 0x2E80..0xA4CF but narrow
}

// charWidth returns the display width of a single rune in terminal cells.
func charWidth(r rune) int {
	if r < 0x1100 {
		return 1
	}
	if excludedCodepoints[r] {
		return 1
	}
	for _, wr := range wideRanges {
		if r >= wr.lo && r <= wr.hi {
			return 2
		}
	}
	return 1
}

// runeWidth returns the display width of a string in terminal cells.
// ASCII = 1 cell, CJK ideographs / Hangul / Kana / emoji = 2 cells.
func runeWidth(s string) int {
	w := 0
	for _, r := range s {
		w += charWidth(r)
	}
	return w
}

// padRight pads s with spaces on the right so that its display width equals width.
func padRight(s string, width int) string {
	gap := width - runeWidth(s)
	if gap <= 0 {
		return s
	}
	return s + strings.Repeat(" ", gap)
}

// ---------------------------------------------------------------------------
// banner line builder
// ---------------------------------------------------------------------------

// buildLines constructs the content lines (without borders).
// Each line is a plain string; the caller pads them to equal rune-width.
func buildLines(cfg BannerConfig) []string {
	labelW := 14 // display width for label alignment
	label := func(prefix, text string) string {
		return padRight(prefix, labelW) + text
	}

	lines := []string{
		fmt.Sprintf("🐾 ClawBench  %s", cfg.Version),
		"",
	}

	// --- URLs ---
	localURL := fmt.Sprintf("%s://localhost:%d", cfg.Scheme, cfg.Port)
	lines = append(lines, label("💻 Local:", localURL))
	if cfg.LocalIP != "" {
		networkURL := fmt.Sprintf("%s://%s:%d", cfg.Scheme, cfg.LocalIP, cfg.Port)
		lines = append(lines, label("🌐 Network:", networkURL))
	}

	// --- Password ---
	if cfg.AutoPassword != "" {
		lines = append(lines, label("🔑 Password:", cfg.AutoPassword))
	} else {
		lines = append(lines, label("🔐 Auth:", "password configured"))
	}
	lines = append(lines, "")

	// --- Agents ---
	if len(cfg.Agents) == 0 {
		lines = append(lines, label("🤖 Agents:", "(none — install agents from welcome screen)"))
	} else {
		lines = append(lines, "🤖 Agents:")
		maxNameW := 0
		for _, a := range cfg.Agents {
			if rw := runeWidth(a.Name); rw > maxNameW {
				maxNameW = rw
			}
		}
		for _, a := range cfg.Agents {
			modelsStr := "1 model"
			if a.Models != 1 {
				modelsStr = fmt.Sprintf("%d models", a.Models)
			}
			lines = append(lines, fmt.Sprintf("  ● %s  %s", padRight(a.Name, maxNameW), modelsStr))
		}
	}
	lines = append(lines, "")

	// --- SSH (conditional) ---
	if cfg.SSHEnabled {
		sshCmd := fmt.Sprintf("ssh -p %d clawbench@%s", cfg.SSHPort, firstNonEmpty(cfg.LocalIP, "localhost"))
		lines = append(lines, label("🔒 SSH:", sshCmd), "")
	}

	// --- Data directory ---
	lines = append(lines, label("📁 Data:", cfg.DataDir), "")

	// --- Service status line ---
	// Note: use single-codepoint emoji only to avoid combining-sequence width issues.
	parts := []string{}
	if cfg.TTSEngine != "" {
		parts = append(parts, fmt.Sprintf("🔊 TTS: %s", cfg.TTSEngine))
	}
	if cfg.RAGAvailable {
		parts = append(parts, "🔍 RAG ✦")
	} else {
		parts = append(parts, "🔍 RAG —")
	}
	if cfg.TerminalOn {
		parts = append(parts, "⌨️ Terminal ✦")
	} else {
		parts = append(parts, "⌨️ Terminal —")
	}
	parts = append(parts, fmt.Sprintf("📋 Tasks: %d", cfg.TaskCount))
	lines = append(lines, strings.Join(parts, "  "), "")

	// --- Startup duration ---
	lines = append(lines, fmt.Sprintf("⚡ Ready in %s", formatDuration(cfg.StartupDuration)))

	return lines
}

// formatDuration returns a human-friendly duration string.
func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000.0)
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// firstNonEmpty returns the first non-empty string argument, or "".
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// PrintBanner renders the startup banner to stdout.
// padRight ensures each line has exactly maxW display cells,
// so we use plain %s — not fmt's %-*s which pads by byte count
// and would misalign CJK/emoji content.
func PrintBanner(cfg BannerConfig) {
	FprintBanner(os.Stdout, cfg)
}

// FprintBanner renders the startup banner to w.
func FprintBanner(w io.Writer, cfg BannerConfig) {
	lines := buildLines(cfg)

	// Compute max display width across all lines
	maxW := 0
	for _, l := range lines {
		if rw := runeWidth(l); rw > maxW {
			maxW = rw
		}
	}

	horiz := strings.Repeat("─", maxW)
	_, _ = fmt.Fprintln(w)
	_, _ = fmt.Fprintf(w, " ┌%s┐\n", horiz)
	for _, l := range lines {
		_, _ = fmt.Fprintf(w, " │ %s │\n", padRight(l, maxW))
	}
	_, _ = fmt.Fprintf(w, " └%s┘\n", horiz)
	_, _ = fmt.Fprintln(w)
}
