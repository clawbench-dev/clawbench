package handler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/stretchr/testify/require"

	"clawbench/internal/model"
)

// writeTranscript creates a fake CLI transcript under home and returns its path.
func writeTranscript(t *testing.T, home, cwd, sessionID string, lines []string) {
	t.Helper()
	munged := strings.ReplaceAll(cwd, "/", "-")
	dir := filepath.Join(home, ".claude", "projects", munged)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, sessionID+".jsonl")
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))
}

// userTurnJSON builds a minimal claude-cli transcript user line.
func userTurnJSON(t *testing.T, text string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": []map[string]string{{"type": "text", "text": text}},
		},
	})
	require.NoError(t, err)
	return string(b)
}

func TestACPDisplayTitle_HumanTitlePassesThrough(t *testing.T) {
	home := t.TempDir()
	got := acpDisplayTitleFromHome(home, "/Users/x", "sid-1", "怎么导出数据库备份")
	assert.Equal(t, "怎么导出数据库备份", got)
}

func TestACPDisplayTitle_MachineTitleDerivedFromTranscript(t *testing.T) {
	home := t.TempDir()
	writeTranscript(t, home, "/Users/x", "sid-2", []string{
		userTurnJSON(t, "[System Instructions: rules]\n\n如何配置定时备份"),
	})
	got := acpDisplayTitleFromHome(home, "/Users/x", "sid-2",
		"[System Instructions: ## User Interaction (Hi")
	assert.Equal(t, "如何配置定时备份", got)
}

func TestACPDisplayTitle_SkipsSummaryOnlyTurns(t *testing.T) {
	home := t.TempDir()
	writeTranscript(t, home, "/Users/x", "sid-3", []string{
		userTurnJSON(t, "[System Instructions: rules]\n\n[Below is the conversation history from before this session.\n\nSummary:\n    old talk"),
		userTurnJSON(t, "[Below is the conversation history from before this session.\n\nSummary:\n    more old talk"),
		userTurnJSON(t, "[System Instructions: rules]\n\n定位 clawbench 壳类型"),
	})
	got := acpDisplayTitleFromHome(home, "/Users/x", "sid-3", "[Below is the conversation his")
	assert.Equal(t, "定位 clawbench 壳类型", got)
}

func TestACPDisplayTitle_MissingTranscriptFallsBack(t *testing.T) {
	home := t.TempDir()
	// No transcript on disk. A HUMAN agent-reported title is used as the
	// last-resort fallback; a MACHINE one is suppressed (empty) so no
	// [System Instructions:...] noise is ever displayed.
	// 无转录时：人类上报标题作末级兜底；机器上报标题被抑制为空，绝不显示
	// [System Instructions:...] 噪声。
	assert.Equal(t, "怎么导出数据库备份",
		acpDisplayTitleFromHome(home, "/Users/x", "no-such-sid", "怎么导出数据库备份"))
	assert.Equal(t, "",
		acpDisplayTitleFromHome(home, "/Users/x", "no-such-sid", "[System Instructions: ## User Interaction (Hi"))
}

func TestACPDisplayTitle_TruncatesLongQuestions(t *testing.T) {
	home := t.TempDir()
	long := strings.Repeat("问", 80)
	writeTranscript(t, home, "/Users/x", "sid-4", []string{
		userTurnJSON(t, "[System Instructions: rules]\n\n"+long),
	})
	got := acpDisplayTitleFromHome(home, "/Users/x", "sid-4", "[System Instructions: x")
	assert.Equal(t, strings.Repeat("问", 50)+"...", got)
}

func TestACPDisplayTitle_CrossProjectTranscriptFallback(t *testing.T) {
	home := t.TempDir()
	// Transcript lives under a different project dir than the cookie cwd.
	writeTranscript(t, home, "/Users/other", "sid-x", []string{
		userTurnJSON(t, "[System Instructions: rules]\n\n另一个项目的首个问题"),
	})
	got := acpDisplayTitleFromHome(home, "/Users/x", "sid-x",
		"[System Instructions: ## User Interaction (Hi")
	assert.Equal(t, "另一个项目的首个问题", got)
}

func TestACPTranscriptPath_RejectsUnsafeSessionIDs(t *testing.T) {
	for _, sid := range []string{"../../etc/passwd", "a/b", `a\b`, "x..y", "*"} {
		if got := acpTranscriptPath("/home/u", "/Users/x", sid); got != "" {
			t.Errorf("sessionID %q should be rejected, got %q", sid, got)
		}
	}
}

func TestDeriveSessionTitleForAgent(t *testing.T) {
	t.Run("claude transcript wins over post-compaction replay", func(t *testing.T) {
		home := t.TempDir()
		writeTranscript(t, home, "/Users/x", "sid-c", []string{
			userTurnJSON(t, "怎么导出数据库备份"),
			userTurnJSON(t, "This session is being continued from a previous conversation that ran out of context. …"),
			userTurnJSON(t, "后续的压缩后消息"),
		})
		// Replay mirrors a heavily compacted session's CURRENT context: the
		// original question is gone, a later message comes first — the
		// replay path alone would title it after that later message.
		userMsg := func(text string) replayMessage {
			return replayMessage{role: strUser, content: fmt.Sprintf(`{"blocks":[{"type":"text","text":%q}]}`, text)}
		}
		replay := []replayMessage{
			userMsg("后续的压缩后消息"),
			{role: strAssistant, content: `{"blocks":[{"type":"text","text":"answer"}]}`},
		}
		assert.Equal(t, "后续的压缩后消息", deriveSessionTitleFromReplay(replay))
		// Transcript-first lookup returns the ORIGINAL first question.
		assert.Equal(t, "怎么导出数据库备份", acpDisplayTitleFromHome(home, "/Users/x", "sid-c", ""))
	})

	t.Run("no transcript yields empty", func(t *testing.T) {
		assert.Empty(t, acpDisplayTitleFromHome(t.TempDir(), "/Users/x", "no-sid", ""))
	})

	t.Run("machine-only transcript yields empty", func(t *testing.T) {
		home := t.TempDir()
		writeTranscript(t, home, "/Users/x", "sid-m", []string{
			userTurnJSON(t, "This session is being continued from a previous conversation that ran out of context. …"),
		})
		assert.Empty(t, acpDisplayTitleFromHome(home, "/Users/x", "sid-m", ""))
	})

	t.Run("transcript in another project dir found via global lookup", func(t *testing.T) {
		home := t.TempDir()
		writeTranscript(t, home, "/Users/other", "sid-g", []string{
			userTurnJSON(t, "另一个项目的首个问题"),
		})
		assert.Equal(t, "另一个项目的首个问题", acpDisplayTitleFromHome(home, "/Users/x", "sid-g", ""))
	})

	t.Run("non-claude backend falls through to replay", func(t *testing.T) {
		// Non-claude agents skip the transcript path entirely and
		// fall through to deriveSessionTitleFromReplay.
		agent := &model.Agent{ID: "opencode", Name: "OpenCode", Backend: "opencode", Transport: "acp-stdio"}
		userMsg := func(text string) replayMessage {
			return replayMessage{role: strUser, content: fmt.Sprintf(`{"blocks":[{"type":"text","text":%q}]}`, text)}
		}
		replay := []replayMessage{
			userMsg("[System Instructions: rules]\n\n非 claude 后端的首问"),
			{role: strAssistant, content: `{"blocks":[{"type":"text","text":"answer"}]}`},
		}
		got := deriveSessionTitleForAgent(agent, "/Users/x", "sid-nc", replay)
		assert.Equal(t, "非 claude 后端的首问", got)
	})

	t.Run("nil agent falls through to replay", func(t *testing.T) {
		userMsg := func(text string) replayMessage {
			return replayMessage{role: strUser, content: fmt.Sprintf(`{"blocks":[{"type":"text","text":%q}]}`, text)}
		}
		replay := []replayMessage{
			userMsg("nil agent 的首问"),
		}
		got := deriveSessionTitleForAgent(nil, "/Users/x", "sid-nil", replay)
		assert.Equal(t, "nil agent 的首问", got)
	})
}

// customTitleJSON builds a minimal claude-cli custom-title transcript line.
func customTitleJSON(t *testing.T, name string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"type": "custom-title", "customTitle": name})
	require.NoError(t, err)
	return string(b)
}

func TestCustomTitleWinsOverEverything(t *testing.T) {
	t.Run("custom name replaces all derived candidates", func(t *testing.T) {
		home := t.TempDir()
		writeTranscript(t, home, "/Users/x", "sid-n", []string{
			customTitleJSON(t, "my-important-session"),
			userTurnJSON(t, "怎么导出数据库备份"),
		})
		assert.Equal(t, "my-important-session", acpDisplayTitleFromHome(home, "/Users/x", "sid-n", ""))
		// Also wins over a non-machine agent-reported title in the display path.
		assert.Equal(t, "my-important-session", acpDisplayTitleFromHome(home, "/Users/x", "sid-n", "普通标题"))
		// And over a machine one.
		assert.Equal(t, "my-important-session", acpDisplayTitleFromHome(home, "/Users/x", "sid-n", "[System Instructions: x"))
	})

	t.Run("latest custom name wins", func(t *testing.T) {
		home := t.TempDir()
		writeTranscript(t, home, "/Users/x", "sid-n2", []string{
			customTitleJSON(t, "old-name"),
			userTurnJSON(t, "怎么导出数据库备份"),
			customTitleJSON(t, "new-name"),
		})
		assert.Equal(t, "new-name", acpDisplayTitleFromHome(home, "/Users/x", "sid-n2", ""))
	})

	t.Run("no custom name falls through to first question", func(t *testing.T) {
		home := t.TempDir()
		writeTranscript(t, home, "/Users/x", "sid-n3", []string{
			userTurnJSON(t, "怎么导出数据库备份"),
		})
		assert.Equal(t, "怎么导出数据库备份", acpDisplayTitleFromHome(home, "/Users/x", "sid-n3", ""))
	})
}

func TestIsMachineGeneratedTitle_AllPrefixes(t *testing.T) {
	// Every prefix in machineGeneratedUserPrefixes must be detected as
	// machine-generated, both in full and truncated form (the CLI truncates
	// titles to ~200 chars).
	// machineGeneratedUserPrefixes 中的每个前缀都必须被判定为机器文本，
	// 无论完整还是截断形式（CLI 会把标题截断到约 200 字符）。
	for _, marker := range machineGeneratedUserPrefixes {
		if !isMachineGeneratedTitle(marker + "rest of title") {
			t.Errorf("full title starting with %q not detected as machine", marker)
		}
		if !isMachineGeneratedTitle(marker) {
			t.Errorf("bare marker %q not detected as machine", marker)
		}
		// Truncated to half the marker length — still a prefix of the marker.
		half := marker[:len(marker)/2+1]
		if !isMachineGeneratedTitle(half) {
			t.Errorf("truncated prefix %q not detected as machine", half)
		}
	}
}

func TestIsMachineGeneratedTitle_HumanTitles(t *testing.T) {
	for _, title := range []string{"怎么导出数据库备份", "list", "你是什么模型", "palminput-demo-pcb-design"} {
		if isMachineGeneratedTitle(title) {
			t.Errorf("human title %q wrongly flagged as machine", title)
		}
	}
	if isMachineGeneratedTitle("") {
		t.Error("empty title should not be machine-generated")
	}
}

func TestMachinePrefixesMatchStrip(t *testing.T) {
	// Sync contract: every prefix in machineGeneratedUserPrefixes must be
	// recognized by stripMachineGeneratedUserText (so a title detected as
	// machine is one whose source turn would actually be stripped).
	//
	// There are two categories of prefixes:
	//   - whole-turn markers: the entire turn is machine text, ok=false.
	//     Test with just the marker (no user text).
	//   - strip-with-delimiter markers: the header is stripped and user
	//     text after it may be kept. Test with the marker AND its proper
	//     delimiter, verifying that the marker portion is consumed and
	//     only the trailing user text (if any) remains.
	//
	// 同步契约：machineGeneratedUserPrefixes 中的每个前缀都必须能被
	// stripMachineGeneratedUserText 识别。
	//
	// 前缀分两类：
	//   - 整轮标记：整轮都是机器文本，ok=false。仅用标记本身测试。
	//   - 剥离式标记：头部被剥离，后面的用户文本可能保留。需要带正确的分隔符
	//     测试，验证标记部分被消耗、仅剩尾部用户文本。
	wholeTurnMarkers := []string{
		"[Request interrupted",
		"This session is being continued from a previous conversation",
		"<command-name>/",
		"<local-command",
	}
	stripWithDelimiterCases := []struct {
		marker  string
		input   string // marker + delimiter + optional user text
		wantOK  bool
		wantRem string // expected remaining text
	}{
		{
			marker:  "[System Instructions:",
			input:   "[System Instructions: rules]\n\n",
			wantOK:  true, // header stripped, remainder is whitespace-only
			wantRem: "",
		},
		{
			marker:  "[System Instructions:",
			input:   "[System Instructions: rules]\n\n用户问题",
			wantOK:  true,
			wantRem: "用户问题",
		},
		{
			marker: "[Below is the conversation history",
			input:  "[Below is the conversation history from before this session.\n\nSummary:\n    old talk]",
			wantOK: false, // no compactEnd delimiter → whole turn
		},
		{
			marker:  "[Below is the conversation history",
			input:   "[Below is the conversation history]\n[End of conversation history. Now answer the user's new question.]\n\n用户问题",
			wantOK:  true,
			wantRem: "用户问题",
		},
		{
			marker: "Caveat: The messages below were generated by the user",
			input:  "Caveat: The messages below were generated by the user while running local commands.",
			wantOK: false, // no \n\n delimiter → no user text
		},
		{
			marker:  "Caveat: The messages below were generated by the user",
			input:   "Caveat: The messages below were generated by the user.\n\n用户问题",
			wantOK:  true,
			wantRem: "用户问题",
		},
		{
			marker: "[Current file: ",
			input:  "[Current file: /tmp/a.png]",
			wantOK: false, // no newline → no user text
		},
		{
			marker:  "[Current file: ",
			input:   "[Current file: /tmp/a.png]\n用户问题",
			wantOK:  true,
			wantRem: "用户问题",
		},
		{
			marker:  "[Current directory: ",
			input:   "[Current directory: /tmp]\n用户问题",
			wantOK:  true,
			wantRem: "用户问题",
		},
		{
			marker:  "[User uploaded ",
			input:   "[User uploaded file.png]\n用户问题",
			wantOK:  true,
			wantRem: "用户问题",
		},
	}

	// Test whole-turn markers.
	for _, marker := range wholeTurnMarkers {
		_, ok := stripMachineGeneratedUserText(marker + " padding that is still part of the machine turn")
		if ok {
			t.Errorf("whole-turn marker %q not stripped by stripMachineGeneratedUserText", marker)
		}
	}

	// Test strip-with-delimiter markers.
	for _, tc := range stripWithDelimiterCases {
		got, ok := stripMachineGeneratedUserText(tc.input)
		if ok != tc.wantOK {
			t.Errorf("stripWithDelimiter %q: ok=%v, want %v (input=%q)", tc.marker, ok, tc.wantOK, tc.input)
		}
		if tc.wantOK && got != tc.wantRem {
			t.Errorf("stripWithDelimiter %q: got %q, want %q", tc.marker, got, tc.wantRem)
		}
	}

	// Verify every prefix in machineGeneratedUserPrefixes is covered by
	// either the whole-turn list or the strip-with-delimiter list.
	allTested := map[string]bool{}
	for _, m := range wholeTurnMarkers {
		allTested[m] = true
	}
	for _, tc := range stripWithDelimiterCases {
		allTested[tc.marker] = true
	}
	for _, marker := range machineGeneratedUserPrefixes {
		if !allTested[marker] {
			t.Errorf("prefix %q in machineGeneratedUserPrefixes but not tested in TestMachinePrefixesMatchStrip", marker)
		}
	}
}

// userTurnStringJSON builds a transcript user line whose content is a plain
// string (claude-code writes this form in some contexts), not a block list.
// 构造 content 为纯字符串（claude-code 部分场景用此形式）的转录用户行。
func userTurnStringJSON(t *testing.T, text string) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"type": "user",
		"message": map[string]any{
			"role":    "user",
			"content": text,
		},
	})
	require.NoError(t, err)
	return string(b)
}

func TestFirstRealQuestion_StringContent(t *testing.T) {
	home := t.TempDir()
	// First user turn has string content (not a block list). The parser must
	// still extract it; a machine prefix on a string turn is also stripped.
	// 首条用户轮次的 content 为字符串（非块列表）。解析器须照样提取；
	// 字符串轮次上的机器前缀也要剥离。
	writeTranscript(t, home, "/Users/x", "sid-s", []string{
		userTurnStringJSON(t, "[System Instructions: rules]\n\n如何配置自动备份"),
	})
	assert.Equal(t, "如何配置自动备份", acpDisplayTitleFromHome(home, "/Users/x", "sid-s", "给出完整ID"))
}
