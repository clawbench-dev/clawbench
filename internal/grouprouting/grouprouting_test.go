package grouprouting

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSingleSpeaker(t *testing.T) {
	r := Parse(`<clawbench-speaker>B</clawbench-speaker> 请回应 A 的质疑`)
	if !r.Found {
		t.Fatal("expected Found=true")
	}
	if r.End {
		t.Fatal("expected End=false")
	}
	if !reflect.DeepEqual(r.Speakers, []string{"B"}) {
		t.Fatalf("speakers=%v", r.Speakers)
	}
	if r.Instruction != "请回应 A 的质疑" {
		t.Fatalf("instruction=%q", r.Instruction)
	}
}

func TestParseMultipleSpeakers(t *testing.T) {
	r := Parse(`<clawbench-speaker>A, B ,C</clawbench-speaker> 各自表态`)
	if !reflect.DeepEqual(r.Speakers, []string{"A", "B", "C"}) {
		t.Fatalf("speakers=%v", r.Speakers)
	}
}

func TestParseEndSignal(t *testing.T) {
	r := Parse(`讨论已充分。<clawbench-group-end/>`)
	if !r.End {
		t.Fatal("expected End=true")
	}
}

// TestParseBothTags: a message may carry a speaker tag AND an end tag. End must
// win (the orchestrator breaks the loop), and the end tag must not leak into the
// instruction.
func TestParseBothTags(t *testing.T) {
	r := Parse(`<clawbench-speaker>B</clawbench-speaker> 请回应<clawbench-group-end/>`)
	if !r.End {
		t.Fatal("expected End=true (end wins)")
	}
	if strings.Contains(r.Instruction, "clawbench-group-end") {
		t.Fatalf("end tag leaked into instruction: %q", r.Instruction)
	}
	if r.Instruction != "请回应" {
		t.Fatalf("instruction=%q", r.Instruction)
	}
}

func TestParseMalformedKeepsRaw(t *testing.T) {
	in := `<clawbench-speaker></clawbench-speaker>`
	r := Parse(in)
	if r.Found {
		t.Fatal("empty payload must not be Found")
	}
	if r.Raw != in {
		t.Fatalf("Raw must equal source, got %q", r.Raw)
	}
}

func TestParseNoTag(t *testing.T) {
	r := Parse("普通发言，没有标签")
	if r.Found || r.End {
		t.Fatal("expected no tag")
	}
}

// TestParseBeforeEmpty: nothing precedes the tag, so Before is empty.
func TestParseBeforeEmpty(t *testing.T) {
	r := Parse(`<clawbench-speaker>A</clawbench-speaker> 请谈谈`)
	if !r.Found {
		t.Fatal("expected Found=true")
	}
	if r.Before != "" {
		t.Fatalf("before=%q want empty", r.Before)
	}
}

// TestParseBeforeBackground: text before the tag is the host's background
// context, returned trimmed.
func TestParseBeforeBackground(t *testing.T) {
	r := Parse(`A 的观点不错 <clawbench-speaker>B</clawbench-speaker> 请回应`)
	if !r.Found {
		t.Fatal("expected Found=true")
	}
	if r.Before != "A 的观点不错" {
		t.Fatalf("before=%q want %q", r.Before, "A 的观点不错")
	}
}

// TestParseBeforeTrimsSurroundingWhitespace: leading/trailing whitespace around
// the background is trimmed, matching Instruction's treatment.
func TestParseBeforeTrimsSurroundingWhitespace(t *testing.T) {
	r := Parse("\n  A 的观点不错 \n <clawbench-speaker>B</clawbench-speaker> 请回应")
	if r.Before != "A 的观点不错" {
		t.Fatalf("before=%q want %q", r.Before, "A 的观点不错")
	}
}

// TestParseBeforeMalformed: a malformed tag (empty payload) is not Found, but
// Before still slices the text ahead of the tag (the contract is unchanged:
// Parse never strips, so the caller keeps the original text).
func TestParseBeforeMalformed(t *testing.T) {
	r := Parse(`背景在此 <clawbench-speaker></clawbench-speaker>`)
	if r.Found {
		t.Fatal("empty payload must not be Found")
	}
	if r.Before != "背景在此" {
		t.Fatalf("before=%q want %q", r.Before, "背景在此")
	}
}

// StripEndTag removes the end-signal tag and leaves everything else; it is used
// to keep the tag out of a member's injected context when the message has no
// speaker tag (decision #67).
func TestStripEndTag(t *testing.T) {
	cases := []struct{ in, want string }{
		{"no tag here", "no tag here"},
		{"讨论充分。<clawbench-group-end/> 结论：可以发布", "讨论充分。 结论：可以发布"},
		{"<clawbench-group-end/>", ""},
		// Two tags collapse to the surrounding text (the gap between them is
		// preserved as-is; only the outer edges are trimmed).
		{"a <clawbench-group-end/> b <clawbench-group-end/> c", "a  b  c"},
	}
	for _, tc := range cases {
		if got := StripEndTag(tc.in); got != tc.want {
			t.Errorf("StripEndTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── BCC (密送) ────────────────────────────────────────────────────────────
//
// A host may address a private note to individual members:
//
//	<clawbench-speaker>A,B</clawbench-speaker> 公共指令
//	<clawbench-bcc targets="A">只有 A 看得到的话</clawbench-bcc>
//
// The bcc span is extracted AND removed from the surrounding text before the
// speaker/end/before/instruction parsing runs, so its content can never leak
// into Instruction (the public directive) or Before (the shared background).

func TestParseBccSingleTarget(t *testing.T) {
	r := Parse(`<clawbench-speaker>A</clawbench-speaker> 请表态 <clawbench-bcc targets="A">你重点看性能</clawbench-bcc>`)
	if len(r.Bcc) != 1 {
		t.Fatalf("bcc=%v want 1 entry", r.Bcc)
	}
	if !reflect.DeepEqual(r.Bcc[0].Targets, []string{"A"}) {
		t.Fatalf("targets=%v", r.Bcc[0].Targets)
	}
	if r.Bcc[0].Content != "你重点看性能" {
		t.Fatalf("content=%q", r.Bcc[0].Content)
	}
	// The private note must NOT survive in the public directive.
	if r.Instruction != "请表态" {
		t.Fatalf("instruction=%q want %q", r.Instruction, "请表态")
	}
	if strings.Contains(r.Instruction, "性能") {
		t.Fatalf("bcc leaked into instruction: %q", r.Instruction)
	}
}

func TestParseBccMultipleTargetsTrimmed(t *testing.T) {
	r := Parse(`<clawbench-speaker>A,B</clawbench-speaker> 表态 <clawbench-bcc targets="A, B ,C">都注意</clawbench-bcc>`)
	if len(r.Bcc) != 1 {
		t.Fatalf("bcc=%v", r.Bcc)
	}
	if !reflect.DeepEqual(r.Bcc[0].Targets, []string{"A", "B", "C"}) {
		t.Fatalf("targets=%v", r.Bcc[0].Targets)
	}
}

// TestParseBccBeforeTagNotInBefore: a bcc span placed BEFORE the speaker tag
// must not leak into Before (the shared background every member sees).
func TestParseBccBeforeTagNotInBefore(t *testing.T) {
	r := Parse(`<clawbench-bcc targets="A">私下话</clawbench-bcc>背景在此 <clawbench-speaker>A</clawbench-speaker> 请回应`)
	if strings.Contains(r.Before, "私下话") {
		t.Fatalf("bcc leaked into before: %q", r.Before)
	}
	if !strings.Contains(r.Before, "背景在此") {
		t.Fatalf("background lost: %q", r.Before)
	}
	if len(r.Bcc) != 1 || r.Bcc[0].Content != "私下话" {
		t.Fatalf("bcc=%v", r.Bcc)
	}
}

// TestParseBccOrderPreserved: multiple notes keep their order.
func TestParseBccOrderPreserved(t *testing.T) {
	r := Parse(`<clawbench-speaker>A,B</clawbench-speaker> 表态` +
		`<clawbench-bcc targets="A">给A一</clawbench-bcc>` +
		`<clawbench-bcc targets="B">给B二</clawbench-bcc>`)
	if len(r.Bcc) != 2 {
		t.Fatalf("bcc=%v", r.Bcc)
	}
	if r.Bcc[0].Content != "给A一" || r.Bcc[1].Content != "给B二" {
		t.Fatalf("order/content wrong: %v", r.Bcc)
	}
}

// TestParseBccMalformedKept: a malformed note (no targets / empty targets /
// single quotes / unclosed) is NOT recognized as a note (no card) — the display
// contract stays fail-open (never lose content). The INJECTION contract is the
// opposite: no bcc-like text may survive into Instruction, whatever its shape.
func TestParseBccMalformedKept(t *testing.T) {
	cases := []string{
		`<clawbench-bcc>无 targets</clawbench-bcc>`,
		`<clawbench-bcc targets="">空名单</clawbench-bcc>`,
		`<clawbench-bcc targets='A'>单引号</clawbench-bcc>`,
		`<clawbench-bcc targets="A">未闭合`,
	}
	for _, in := range cases {
		r := Parse(in)
		if len(r.Bcc) != 0 {
			t.Errorf("malformed %q must not parse a bcc, got %v", in, r.Bcc)
		}
	}
	// Fail-closed: an unclosed note's tail must NOT reach the instruction.
	r := Parse(`<clawbench-speaker>A</clawbench-speaker> 请谈 <clawbench-bcc targets="A">未闭合`)
	if strings.Contains(r.Instruction, "未闭合") || strings.Contains(r.Instruction, "clawbench-bcc") {
		t.Fatalf("an unclosed note leaked into the instruction (fail-closed): %q", r.Instruction)
	}
	if r.Instruction != "请谈" {
		t.Fatalf("instruction=%q want %q", r.Instruction, "请谈")
	}
}

// TestParseBccEndInsideNotEnd: an end tag INSIDE a well-formed bcc is private
// instruction to the target, not a discussion-ending signal for the group.
func TestParseBccEndInsideNotEnd(t *testing.T) {
	r := Parse(`<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc targets="A">如果没意见就 <clawbench-group-end/> 收尾</clawbench-bcc>`)
	if r.End {
		t.Fatal("an end tag inside a private note must not end the discussion")
	}
	if len(r.Bcc) != 1 || !strings.Contains(r.Bcc[0].Content, "收尾") {
		t.Fatalf("bcc=%v", r.Bcc)
	}
}

// TestParseBccWithEndTag: a real end tag alongside a bcc is still honored.
func TestParseBccWithEndTag(t *testing.T) {
	r := Parse(`<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc targets="A">私</clawbench-bcc><clawbench-group-end/>`)
	if !r.End {
		t.Fatal("expected End=true")
	}
	if len(r.Bcc) != 1 {
		t.Fatalf("bcc=%v", r.Bcc)
	}
	if strings.Contains(r.Instruction, "clawbench-group-end") || strings.Contains(r.Instruction, "私") {
		t.Fatalf("leak into instruction: %q", r.Instruction)
	}
}

// TestStripBccTags mirrors TestStripEndTag: only well-formed spans are removed,
// malformed ones survive verbatim.
func TestStripBccTags(t *testing.T) {
	cases := []struct{ in, want string }{
		{"no tag here", "no tag here"},
		{`前 <clawbench-bcc targets="A">私</clawbench-bcc> 后`, "前  后"},
		{`<clawbench-bcc targets="A,B">多目标</clawbench-bcc>`, ""},
		// Malformed (no targets attr) is kept verbatim.
		{`<clawbench-bcc>畸形</clawbench-bcc>`, `<clawbench-bcc>畸形</clawbench-bcc>`},
		{`<clawbench-bcc targets="">空</clawbench-bcc>`, `<clawbench-bcc targets="">空</clawbench-bcc>`},
	}
	for _, tc := range cases {
		if got := StripBccTags(tc.in); got != tc.want {
			t.Errorf("StripBccTags(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── Fail-closed stripping for injection (review C3 / I1 / I4) ──────────────
//
// The display contract stays fail-open (a malformed note is shown verbatim so
// content is never lost). The INJECTION contract is the opposite: a note's text
// must never reach a member who is not its target, so anything that even looks
// like a bcc tag is removed — malformed, nested, unclosed, or separated by a
// non-ASCII space. Parse's Before/Instruction/End all run on that fail-closed
// text; only Bcc (display) keeps the fail-open view.

func TestParseFailClosed_MalformedNoteNotInInstruction(t *testing.T) {
	cases := []struct{ name, text string }{
		{"single_quotes", `<clawbench-speaker>A</clawbench-speaker> 请表态 <clawbench-bcc targets='A'>秘密一</clawbench-bcc>`},
		{"blank_targets", `<clawbench-speaker>A</clawbench-speaker> 请表态 <clawbench-bcc targets="   ">秘密二</clawbench-bcc>`},
		{"uppercase_attr", `<clawbench-speaker>A</clawbench-speaker> 请表态 <clawbench-bcc Targets="A">秘密三</clawbench-bcc>`},
		{"extra_attr", `<clawbench-speaker>A</clawbench-speaker> 请表态 <clawbench-bcc targets="A" x="1">秘密四</clawbench-bcc>`},
		{"unclosed", `<clawbench-speaker>A</clawbench-speaker> 请表态 <clawbench-bcc targets="A">未闭合秘密`},
		{"nbsp_separator", "<clawbench-speaker>A</clawbench-speaker> 请表态 <clawbench-bcc\u00A0targets=\"A\">秘密五</clawbench-bcc>"},
		{"ideographic_space", "<clawbench-speaker>A</clawbench-speaker> 请表态 <clawbench-bcc\u3000targets=\"A\">秘密六</clawbench-bcc>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Parse(tc.text)
			if strings.Contains(r.Instruction, "秘密") {
				t.Fatalf("a note's text must never survive into Instruction (fail-closed): %q", r.Instruction)
			}
			if strings.Contains(r.Instruction, "clawbench-bcc") {
				t.Fatalf("the bcc tag must not survive into Instruction: %q", r.Instruction)
			}
		})
	}
}

// A well-formed note is stripped fail-closed too (and parsed fail-open for the
// card). Both must hold at once.
func TestParseFailClosed_WellFormedAlsoStrippedFromInstruction(t *testing.T) {
	r := Parse(`<clawbench-speaker>A</clawbench-speaker> 请表态 <clawbench-bcc targets="A">秘密</clawbench-bcc>`)
	if len(r.Bcc) != 1 {
		t.Fatalf("well-formed note must still parse for display: %v", r.Bcc)
	}
	if strings.Contains(r.Instruction, "秘密") || strings.Contains(r.Instruction, "clawbench-bcc") {
		t.Fatalf("Instruction must be free of the note: %q", r.Instruction)
	}
}

// A note placed before the speaker tag must not survive into Before either.
func TestParseFailClosed_MalformedNoteNotInBefore(t *testing.T) {
	r := Parse(`<clawbench-bcc targets='A'>秘密</clawbench-bcc>背景 <clawbench-speaker>A</clawbench-speaker> 请回应`)
	if strings.Contains(r.Before, "秘密") || strings.Contains(r.Before, "clawbench-bcc") {
		t.Fatalf("Before must be free of any bcc form: %q", r.Before)
	}
	if !strings.Contains(r.Before, "背景") {
		t.Fatalf("background lost: %q", r.Before)
	}
}

// Nested / stray closing tags must not leave protocol residue in Instruction.
func TestParseFailClosed_NestedAndStrayTags(t *testing.T) {
	nested := `<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc targets="A">外<clawbench-bcc targets="B">内</clawbench-bcc></clawbench-bcc>`
	r := Parse(nested)
	if strings.Contains(r.Instruction, "clawbench-bcc") || strings.Contains(r.Instruction, "外") || strings.Contains(r.Instruction, "内") {
		t.Fatalf("nested note leaked into Instruction: %q", r.Instruction)
	}
	stray := `<clawbench-speaker>A</clawbench-speaker> 表态 <clawbench-bcc targets="A">笔记</clawbench-bcc></clawbench-bcc>`
	r2 := Parse(stray)
	if strings.Contains(r2.Instruction, "clawbench-bcc") {
		t.Fatalf("stray closing tag leaked into Instruction: %q", r2.Instruction)
	}
}

// StripBccSpans is the fail-closed primitive used by the injection layer.
func TestStripBccSpans(t *testing.T) {
	cases := []struct{ in, want string }{
		{"no tag", "no tag"},
		{`前 <clawbench-bcc targets="A">良构</clawbench-bcc> 后`, "前  后"},
		{`前 <clawbench-bcc targets='A'>畸形</clawbench-bcc> 后`, "前  后"},
		{`前 <clawbench-bcc targets="A">未闭合`, "前"},
		{"前 <clawbench-bcc\u00A0targets=\"A\">NBSP</clawbench-bcc> 后", "前  后"},
	}
	for _, tc := range cases {
		if got := StripBccSpans(tc.in); got != tc.want {
			t.Errorf("StripBccSpans(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The display-side stripper keeps its fail-open contract (malformed stays).
func TestStripBccTags_FailOpenUnchanged(t *testing.T) {
	malformed := `<clawbench-bcc>畸形</clawbench-bcc>`
	if got := StripBccTags(malformed); got != malformed {
		t.Fatalf("display stripper must keep a malformed note: %q", got)
	}
}
