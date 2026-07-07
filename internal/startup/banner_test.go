package startup

import (
	"strings"
	"testing"
	"time"
)

// ---------- runeWidth tests ----------

func TestRuneWidth_ASCII(t *testing.T) {
	got := runeWidth("hello")
	if got != 5 {
		t.Errorf("runeWidth(%q) = %d, want 5", "hello", got)
	}
}

func TestRuneWidth_CJK(t *testing.T) {
	// Each CJK character is 2 cells wide
	got := runeWidth("中文")
	if got != 4 {
		t.Errorf("runeWidth(%q) = %d, want 4", "中文", got)
	}
}

func TestRuneWidth_Mixed(t *testing.T) {
	// "🐾" = 2, " hello" = 6
	got := runeWidth("🐾 hello")
	if got != 8 {
		t.Errorf("runeWidth(%q) = %d, want 8", "🐾 hello", got)
	}
}

func TestRuneWidth_Emoji(t *testing.T) {
	got := runeWidth("🎉")
	if got != 2 {
		t.Errorf("runeWidth(%q) = %d, want 2", "🎉", got)
	}
}

func TestRuneWidth_Fullwidth(t *testing.T) {
	// Fullwidth Latin letter Ａ = U+FF21, width 2
	got := runeWidth("Ａ")
	if got != 2 {
		t.Errorf("runeWidth(%q) = %d, want 2", "Ａ", got)
	}
}

// ---------- padRight tests ----------

func TestPadRight_ASCII(t *testing.T) {
	got := padRight("hi", 5)
	if got != "hi   " {
		t.Errorf("padRight(%q, 5) = %q, want %q", "hi", got, "hi   ")
	}
}

func TestPadRight_CJK(t *testing.T) {
	// "中文" has runeWidth 4, pad to 6 => 2 spaces
	got := padRight("中文", 6)
	if got != "中文  " {
		t.Errorf("padRight(%q, 6) = %q, want %q", "中文", got, "中文  ")
	}
}

func TestPadRight_NoPadNeeded(t *testing.T) {
	got := padRight("hello", 3)
	if got != "hello" {
		t.Errorf("padRight(%q, 3) = %q, want %q", "hello", got, "hello")
	}
}

// ---------- buildLines border alignment tests ----------

func TestBannerBasic(t *testing.T) {
	cfg := BannerConfig{
		Version:      "v1.0.0",
		Scheme:       "http",
		Port:         20000,
		LocalIPs:     []string{"192.168.1.100"},
		AutoPassword: "a1b2c3d4",
		DataDir:      "/home/user/clawbench/.clawbench",
		Agents: []AgentInfo{
			{Name: "codebuddy", Models: 21},
			{Name: "pi", Models: 8},
			{Name: "kimi", Models: 5},
		},
		SSHEnabled:      true,
		SSHPort:         20001,
		TTSEngine:       "edge",
		RAGAvailable:    true,
		TerminalOn:      true,
		TaskCount:       3,
		StartupDuration: 1200 * time.Millisecond,
	}
	lines := buildLines(cfg)

	// Verify all lines have equal display width
	padded := make([]string, len(lines))
	maxW := 0
	for _, l := range lines {
		if rw := runeWidth(l); rw > maxW {
			maxW = rw
		}
	}
	for i, l := range lines {
		padded[i] = padRight(l, maxW)
		if rw := runeWidth(padded[i]); rw != maxW {
			t.Errorf("padded line %d runeWidth=%d, want %d: %q", i, rw, maxW, padded[i])
		}
	}

	// Verify key content
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "v1.0.0") {
		t.Error("banner should contain version")
	}
	if !strings.Contains(joined, "http://localhost:20000") {
		t.Error("banner should contain local URL")
	}
	if !strings.Contains(joined, "http://192.168.1.100:20000") {
		t.Error("banner should contain network URL")
	}
	if !strings.Contains(joined, "a1b2c3d4") {
		t.Error("banner should contain auto-password")
	}
	if !strings.Contains(joined, "codebuddy") {
		t.Error("banner should contain agent name")
	}
	if !strings.Contains(joined, "21 models") {
		t.Error("banner should contain model count")
	}
	if !strings.Contains(joined, "ssh -p 20001") {
		t.Error("banner should contain SSH command")
	}
	if !strings.Contains(joined, "RAG ✦") {
		t.Error("banner should show RAG available")
	}
	if !strings.Contains(joined, "Tasks: 3") {
		t.Error("banner should show task count")
	}
	if !strings.Contains(joined, "1.2s") {
		t.Error("banner should show startup duration")
	}
}

func TestBannerNoAgents(t *testing.T) {
	cfg := BannerConfig{
		Version:         "dev",
		Scheme:          "http",
		Port:            20000,
		AutoPassword:    "abcdefgh",
		DataDir:         "/tmp/.clawbench",
		Agents:          nil,
		TTSEngine:       "edge",
		TaskCount:       0,
		StartupDuration: 300 * time.Millisecond,
	}
	lines := buildLines(cfg)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "welcome screen") {
		t.Error("banner should mention welcome screen when no agents")
	}

	// Verify alignment
	maxW := 0
	for _, l := range lines {
		if rw := runeWidth(l); rw > maxW {
			maxW = rw
		}
	}
	for i, l := range lines {
		padded := padRight(l, maxW)
		if rw := runeWidth(padded); rw != maxW {
			t.Errorf("padded line %d runeWidth=%d, want %d: %q", i, rw, maxW, padded)
		}
	}
}

func TestBannerCJKWidth(t *testing.T) {
	cfg := BannerConfig{
		Version:      "v1.0.0",
		Scheme:       "http",
		Port:         20000,
		LocalIPs:     []string{"192.168.1.100"},
		AutoPassword: "测试密码",                // CJK password (unlikely but tests alignment)
		DataDir:      "/home/用户/.clawbench", // CJK path
		Agents: []AgentInfo{
			{Name: "智能体", Models: 5}, // CJK name
		},
		TTSEngine:       "edge",
		RAGAvailable:    true,
		TerminalOn:      true,
		TaskCount:       0,
		StartupDuration: 500 * time.Millisecond,
	}
	lines := buildLines(cfg)

	// All padded lines must have equal rune width
	maxW := 0
	for _, l := range lines {
		if rw := runeWidth(l); rw > maxW {
			maxW = rw
		}
	}
	for i, l := range lines {
		padded := padRight(l, maxW)
		if rw := runeWidth(padded); rw != maxW {
			t.Errorf("CJK: padded line %d runeWidth=%d, want %d: %q", i, rw, maxW, padded)
		}
	}
}

func TestBannerNoSSH(t *testing.T) {
	cfg := BannerConfig{
		Version:      "v1.0.0",
		Scheme:       "https",
		Port:         20000,
		AutoPassword: "",
		DataDir:      "/opt/clawbench/.clawbench",
		Agents: []AgentInfo{
			{Name: "claude", Models: 12},
		},
		SSHEnabled:      false,
		TTSEngine:       "edge",
		RAGAvailable:    false,
		TerminalOn:      true,
		TaskCount:       0,
		StartupDuration: 800 * time.Millisecond,
	}
	lines := buildLines(cfg)
	joined := strings.Join(lines, "\n")

	if strings.Contains(joined, "ssh") {
		t.Error("banner should NOT contain SSH info when disabled")
	}
	if !strings.Contains(joined, "https://localhost:20000") {
		t.Error("banner should use https scheme")
	}
	if !strings.Contains(joined, "password configured") {
		t.Error("banner should show 'password configured' when no auto-password")
	}
	if !strings.Contains(joined, "RAG —") {
		t.Error("banner should show RAG unavailable")
	}
}

func TestBannerAgentNameAlignment(t *testing.T) {
	cfg := BannerConfig{
		Version:      "v1.0.0",
		Scheme:       "http",
		Port:         20000,
		AutoPassword: "test",
		DataDir:      "/tmp/.clawbench",
		Agents: []AgentInfo{
			{Name: "pi", Models: 8},
			{Name: "codebuddy", Models: 21},
			{Name: "codewhale", Models: 3},
		},
		TTSEngine:       "edge",
		RAGAvailable:    true,
		TerminalOn:      true,
		TaskCount:       0,
		StartupDuration: 100 * time.Millisecond,
	}
	lines := buildLines(cfg)

	// Find agent lines and verify the "● name" part has the same display width
	var agentLines []string
	for _, l := range lines {
		if strings.HasPrefix(l, "  ● ") {
			agentLines = append(agentLines, l)
		}
	}
	if len(agentLines) != 3 {
		t.Fatalf("expected 3 agent lines, got %d", len(agentLines))
	}

	// Extract the "● name" portion (up to the double-space before model count)
	widths := make([]int, len(agentLines))
	for i, l := range agentLines {
		// The format is "  ● name  N models" — double space separates name from count
		// Find the double-space gap after the padded name
		doubleSpIdx := strings.Index(l, "  ")
		if doubleSpIdx < 0 {
			// Single-word agent with no padding? The "  " separates name from "N models"
			t.Fatalf("agent line %d has no double-space separator: %q", i, l)
		}
		prefix := l[:doubleSpIdx]
		widths[i] = runeWidth(prefix)
	}

	for i, w := range widths {
		if w != widths[0] {
			t.Errorf("agent line %d prefix width=%d, want %d: %q", i, w, widths[0], agentLines[i])
		}
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{100 * time.Nanosecond, "100ns"},
		{500 * time.Microsecond, "0.5ms"},
		{1500 * time.Millisecond, "1.5s"},
		{2 * time.Second, "2.0s"},
	}
	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestPrintBannerDoesNotPanic(t *testing.T) {
	// Just verify PrintBanner doesn't panic with various configs
	cfg := BannerConfig{
		Version:         "dev",
		Scheme:          "http",
		Port:            20000,
		AutoPassword:    "test1234",
		DataDir:         "/tmp/.clawbench",
		Agents:          []AgentInfo{{Name: "codebuddy", Models: 5}},
		TTSEngine:       "edge",
		TaskCount:       1,
		StartupDuration: 100 * time.Millisecond,
	}
	// Output goes to stdout; we just verify no panic
	PrintBanner(cfg)
}

func TestPrintBannerEmptyConfig(t *testing.T) {
	// Minimal config — no agents, no SSH, no password
	cfg := BannerConfig{
		Version:         "dev",
		Scheme:          "http",
		Port:            20000,
		DataDir:         "/tmp/.clawbench",
		TTSEngine:       "edge",
		TaskCount:       0,
		StartupDuration: 50 * time.Millisecond,
	}
	PrintBanner(cfg)
}

func TestBannerBorderAlignmentWithCJK(t *testing.T) {
	// Verify that the actual rendered output has consistent border alignment
	// even with CJK/emoji content. We simulate the PrintBanner rendering
	// and verify all lines have equal display width.
	cfg := BannerConfig{
		Version:      "v1.0.0",
		Scheme:       "http",
		Port:         20000,
		LocalIPs:     []string{"192.168.1.100"},
		AutoPassword: "测试密码",                // CJK
		DataDir:      "/home/用户/.clawbench", // CJK
		Agents: []AgentInfo{
			{Name: "智能体", Models: 5}, // CJK
			{Name: "codebuddy", Models: 21},
		},
		SSHEnabled:      true,
		SSHPort:         20001,
		TTSEngine:       "edge",
		RAGAvailable:    true,
		TerminalOn:      true,
		TaskCount:       3,
		StartupDuration: 1 * time.Second,
	}

	lines := buildLines(cfg)

	// Compute max display width
	maxW := 0
	for _, l := range lines {
		if rw := runeWidth(l); rw > maxW {
			maxW = rw
		}
	}

	// Verify every padded line has exactly maxW display width
	for i, l := range lines {
		padded := padRight(l, maxW)
		if rw := runeWidth(padded); rw != maxW {
			t.Errorf("CJK border: line %d padded runeWidth=%d, want %d: %q", i, rw, maxW, padded)
		}
	}

	// Also verify the border horizontal line is correct width
	horiz := strings.Repeat("─", maxW)
	if runeWidth(horiz) != maxW {
		t.Errorf("horizontal border runeWidth=%d, want %d", runeWidth(horiz), maxW)
	}
}

// ---------- charWidth tests ----------

func TestCharWidth_ASCII(t *testing.T) {
	for _, r := range "hello" {
		if w := charWidth(r); w != 1 {
			t.Errorf("charWidth(%q) = %d, want 1", r, w)
		}
	}
}

func TestCharWidth_CJK(t *testing.T) {
	for _, r := range "中文" {
		if w := charWidth(r); w != 2 {
			t.Errorf("charWidth(%q) = %d, want 2", r, w)
		}
	}
}

func TestCharWidth_Hangul(t *testing.T) {
	// Hangul syllable 가 (U+AC00)
	if w := charWidth('가'); w != 2 {
		t.Errorf("charWidth('가') = %d, want 2", w)
	}
}

func TestCharWidth_Fullwidth(t *testing.T) {
	// Fullwidth A (U+FF21)
	if w := charWidth('Ａ'); w != 2 {
		t.Errorf("charWidth('Ａ') = %d, want 2", w)
	}
}

func TestCharWidth_Emoji(t *testing.T) {
	// Emoji 🐾 (U+1F43E)
	if w := charWidth('🐾'); w != 2 {
		t.Errorf("charWidth('🐾') = %d, want 2", w)
	}
}

func TestCharWidth_ExcludedCodepoint(t *testing.T) {
	// U+303F is in the wide range (0x2E80..0xA4CF) but explicitly excluded (narrow)
	if w := charWidth(0x303F); w != 1 {
		t.Errorf("charWidth(U+303F) = %d, want 1 (excluded codepoint)", w)
	}
}

func TestCharWidth_LowCodepoint(t *testing.T) {
	// Codepoints below 0x1100 are narrow
	if w := charWidth('A'); w != 1 {
		t.Errorf("charWidth('A') = %d, want 1", w)
	}
}

// ---------- firstNonEmpty tests ----------

func TestFirstNonEmpty_First(t *testing.T) {
	if got := firstNonEmpty("a", "b", "c"); got != "a" {
		t.Errorf("firstNonEmpty(a,b,c) = %q, want %q", got, "a")
	}
}

func TestFirstNonEmpty_SkipEmpty(t *testing.T) {
	if got := firstNonEmpty("", "b", "c"); got != "b" {
		t.Errorf("firstNonEmpty('',b,c) = %q, want %q", got, "b")
	}
}

func TestFirstNonEmpty_AllEmpty(t *testing.T) {
	if got := firstNonEmpty("", "", ""); got != "" {
		t.Errorf("firstNonEmpty('','','') = %q, want empty", got)
	}
}

func TestFirstNonEmpty_NoArgs(t *testing.T) {
	if got := firstNonEmpty(); got != "" {
		t.Errorf("firstNonEmpty() = %q, want empty", got)
	}
}

// ---------- FprintBanner tests ----------

func TestFprintBanner_WritesBox(t *testing.T) {
	var buf strings.Builder
	cfg := BannerConfig{
		Version:         "v1.0.0",
		Scheme:          "http",
		Port:            20000,
		AutoPassword:    "test1234",
		DataDir:         "/tmp/.clawbench",
		Agents:          []AgentInfo{{Name: "test", Models: 1}},
		TTSEngine:       "edge",
		TaskCount:       0,
		StartupDuration: 100 * time.Millisecond,
	}
	FprintBanner(&buf, cfg)

	output := buf.String()
	// Should contain box borders
	if !strings.Contains(output, "┌") {
		t.Error("banner should contain top-left border")
	}
	if !strings.Contains(output, "┐") {
		t.Error("banner should contain top-right border")
	}
	if !strings.Contains(output, "└") {
		t.Error("banner should contain bottom-left border")
	}
	if !strings.Contains(output, "┘") {
		t.Error("banner should contain bottom-right border")
	}
	// Should contain version
	if !strings.Contains(output, "v1.0.0") {
		t.Error("banner should contain version")
	}
}

// ---------- formatDuration edge cases ----------

func TestFormatDuration_Zero(t *testing.T) {
	if got := formatDuration(0); got != "0ns" {
		t.Errorf("formatDuration(0) = %q, want %q", got, "0ns")
	}
}

func TestFormatDuration_SubMicrosecond(t *testing.T) {
	if got := formatDuration(50 * time.Nanosecond); got != "50ns" {
		t.Errorf("formatDuration(50ns) = %q, want %q", got, "50ns")
	}
}

func TestFormatDuration_ExactlyOneSecond(t *testing.T) {
	if got := formatDuration(1 * time.Second); got != "1.0s" {
		t.Errorf("formatDuration(1s) = %q, want %q", got, "1.0s")
	}
}

// ---------- FRP banner section ----------

func TestBannerMultipleIPs(t *testing.T) {
	cfg := BannerConfig{
		Version:      "v1.0.0",
		Scheme:       "http",
		Port:         20000,
		LocalIPs:     []string{"192.168.1.100", "10.0.0.5", "fd00::1"},
		AutoPassword: "test1234",
		DataDir:      "/tmp/.clawbench",
		Agents: []AgentInfo{
			{Name: "codebuddy", Models: 5},
		},
		TTSEngine:       "edge",
		TaskCount:       0,
		StartupDuration: 100 * time.Millisecond,
	}
	lines := buildLines(cfg)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "http://localhost:20000") {
		t.Error("banner should contain local URL")
	}
	if !strings.Contains(joined, "http://192.168.1.100:20000") {
		t.Error("banner should contain first network URL")
	}
	if !strings.Contains(joined, "http://10.0.0.5:20000") {
		t.Error("banner should contain second network URL")
	}
	if !strings.Contains(joined, "http://[fd00::1]:20000") {
		t.Error("banner should contain IPv6 network URL with brackets")
	}

	// Verify alignment
	maxW := 0
	for _, l := range lines {
		if rw := runeWidth(l); rw > maxW {
			maxW = rw
		}
	}
	for i, l := range lines {
		padded := padRight(l, maxW)
		if rw := runeWidth(padded); rw != maxW {
			t.Errorf("padded line %d runeWidth=%d, want %d: %q", i, rw, maxW, padded)
		}
	}
}

func TestBannerNoIPs(t *testing.T) {
	cfg := BannerConfig{
		Version:         "v1.0.0",
		Scheme:          "http",
		Port:            20000,
		LocalIPs:        nil,
		AutoPassword:    "test1234",
		DataDir:         "/tmp/.clawbench",
		TTSEngine:       "edge",
		TaskCount:       0,
		StartupDuration: 100 * time.Millisecond,
	}
	lines := buildLines(cfg)
	joined := strings.Join(lines, "\n")

	if !strings.Contains(joined, "http://localhost:20000") {
		t.Error("banner should contain local URL")
	}
	if strings.Contains(joined, "Network:") {
		t.Error("banner should NOT contain Network line when no IPs")
	}
}

func TestBanner_FRPEnabledWithRemoteURL(t *testing.T) {
	cfg := BannerConfig{
		Version:         "v1.0.0",
		Scheme:          "http",
		Port:            20000,
		DataDir:         "/tmp/.clawbench",
		TTSEngine:       "edge",
		FRPEnabled:      true,
		FRPRemoteURL:    "http://120.26.168.245:20050",
		FRPServerAddr:   "120.26.168.245",
		FRPRemotePort:   20050,
		TaskCount:       0,
		StartupDuration: 100 * time.Millisecond,
	}
	lines := buildLines(cfg)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "http://120.26.168.245:20050") {
		t.Error("banner should contain FRP remote URL when provided")
	}
}

func TestBanner_FRPEnabledWaitingForPort(t *testing.T) {
	cfg := BannerConfig{
		Version:         "v1.0.0",
		Scheme:          "http",
		Port:            20000,
		DataDir:         "/tmp/.clawbench",
		TTSEngine:       "edge",
		FRPEnabled:      true,
		FRPRemoteURL:    "",
		FRPServerAddr:   "frp-server",
		FRPRemotePort:   0,
		TaskCount:       0,
		StartupDuration: 100 * time.Millisecond,
	}
	lines := buildLines(cfg)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "waiting for port") {
		t.Error("banner should show 'waiting for port' when FRP enabled but no remote URL")
	}
}

func TestBanner_FRPEnabledNoServerAddr(t *testing.T) {
	cfg := BannerConfig{
		Version:         "v1.0.0",
		Scheme:          "http",
		Port:            20000,
		DataDir:         "/tmp/.clawbench",
		TTSEngine:       "edge",
		FRPEnabled:      true,
		FRPRemoteURL:    "",
		FRPServerAddr:   "",
		FRPRemotePort:   0,
		TaskCount:       0,
		StartupDuration: 100 * time.Millisecond,
	}
	lines := buildLines(cfg)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "enabled") {
		t.Error("banner should show 'enabled' for FRP when no server addr or URL")
	}
}

func TestBanner_FRPDisabled(t *testing.T) {
	cfg := BannerConfig{
		Version:         "v1.0.0",
		Scheme:          "http",
		Port:            20000,
		DataDir:         "/tmp/.clawbench",
		TTSEngine:       "edge",
		FRPEnabled:      false,
		TaskCount:       0,
		StartupDuration: 100 * time.Millisecond,
	}
	lines := buildLines(cfg)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "FRP:") {
		t.Error("banner should NOT contain FRP section when disabled")
	}
}

// ---------- FrontendMode banner ----------

func TestBanner_FrontendMode(t *testing.T) {
	cfg := BannerConfig{
		Version:         "v1.0.0",
		Scheme:          "http",
		Port:            20000,
		DataDir:         "/tmp/.clawbench",
		TTSEngine:       "edge",
		FrontendMode:    "embedded",
		TaskCount:       0,
		StartupDuration: 100 * time.Millisecond,
	}
	lines := buildLines(cfg)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "embedded") {
		t.Error("banner should contain frontend mode")
	}
}
