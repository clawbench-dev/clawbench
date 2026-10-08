package grouprouting

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSingleMention(t *testing.T) {
	r := Parse(`<clawbench-mention targets="B">请回应 A 的质疑</clawbench-mention>`)
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

func TestParseMultipleTargets(t *testing.T) {
	r := Parse(`<clawbench-mention targets="A, B ,C">各自表态</clawbench-mention>`)
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

// TestParseMentionAndEnd: a message may carry a mention AND an end tag.
func TestParseMentionAndEnd(t *testing.T) {
	r := Parse(`<clawbench-mention targets="B">请回应</clawbench-mention><clawbench-group-end/>`)
	if !r.End {
		t.Fatal("expected End=true")
	}
	if r.Instruction != "请回应" {
		t.Fatalf("instruction=%q", r.Instruction)
	}
}

func TestParseMalformedKeepsRaw(t *testing.T) {
	in := `<clawbench-mention targets="">空名单</clawbench-mention>`
	r := Parse(in)
	if r.Found {
		t.Fatal("empty targets must not be Found")
	}
	if r.Raw != "" && r.Raw != in {
		t.Fatalf("Raw must equal source or be empty, got %q", r.Raw)
	}
}

func TestParseNoTag(t *testing.T) {
	r := Parse("普通发言，没有标签")
	if r.Found || r.End {
		t.Fatal("expected no tag")
	}
}

// TestParseBeforeAfter: prose outside the tag is the speaker's background
// (Before) and trailing prose (After).
func TestParseBeforeAfter(t *testing.T) {
	r := Parse(`背景在此 <clawbench-mention targets="B">请回应</clawbench-mention> 收尾语`)
	if r.Before != "背景在此" {
		t.Fatalf("before=%q want %q", r.Before, "背景在此")
	}
	if r.After != "收尾语" {
		t.Fatalf("after=%q want %q", r.After, "收尾语")
	}
}

// TestParseBeforeMalformed: a malformed tag is not Found, but Before still
// slices the prose ahead of it (Parse never strips).
func TestParseBeforeMalformed(t *testing.T) {
	r := Parse(`背景在此 <clawbench-mention targets="">空</clawbench-mention>`)
	if r.Found {
		t.Fatal("empty targets must not be Found")
	}
	if r.Before != "背景在此" {
		t.Fatalf("before=%q want %q", r.Before, "背景在此")
	}
}

// StripEndTag removes the end-signal tag and leaves everything else.
func TestStripEndTag(t *testing.T) {
	cases := []struct{ in, want string }{
		{"no tag here", "no tag here"},
		{"讨论充分。<clawbench-group-end/> 结论：可以发布", "讨论充分。 结论：可以发布"},
		{"<clawbench-group-end/>", ""},
		{"a <clawbench-group-end/> b <clawbench-group-end/> c", "a  b  c"},
	}
	for _, tc := range cases {
		if got := StripEndTag(tc.in); got != tc.want {
			t.Errorf("StripEndTag(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ── Private notes (密送) ──────────────────────────────────────────────────
//
// A private note is a mention carrying the `private` attribute:
//
//	<clawbench-mention targets="A" private>只有 A 看得到的话</clawbench-mention>
//
// It is parsed for the DISPLAY card and, on the injection boundary, removed
// entirely (fail-closed).

func TestParsePrivateSingleTarget(t *testing.T) {
	r := Parse(`<clawbench-mention targets="A">请表态</clawbench-mention><clawbench-mention targets="A" private>你重点看性能</clawbench-mention>`)
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
		t.Fatalf("private note leaked into instruction: %q", r.Instruction)
	}
}

func TestParsePrivateMultipleTargetsTrimmed(t *testing.T) {
	r := Parse(`<clawbench-mention targets="A,B" private>都注意</clawbench-mention>`)
	if len(r.Bcc) != 1 {
		t.Fatalf("bcc=%v", r.Bcc)
	}
	if !reflect.DeepEqual(r.Bcc[0].Targets, []string{"A", "B"}) {
		t.Fatalf("targets=%v", r.Bcc[0].Targets)
	}
}

// TestParsePrivateBeforeTagNotInBefore: a private span placed BEFORE a public
// mention must not leak into Before (the shared background).
func TestParsePrivateBeforeTagNotInBefore(t *testing.T) {
	r := Parse(`<clawbench-mention targets="A" private>私下话</clawbench-mention>背景在此 <clawbench-mention targets="A">请回应</clawbench-mention>`)
	if strings.Contains(r.Before, "私下话") {
		t.Fatalf("private note leaked into before: %q", r.Before)
	}
	if !strings.Contains(r.Before, "背景在此") {
		t.Fatalf("background lost: %q", r.Before)
	}
	if len(r.Bcc) != 1 || r.Bcc[0].Content != "私下话" {
		t.Fatalf("bcc=%v", r.Bcc)
	}
}

// TestParsePrivateOrderPreserved: multiple notes keep their order.
func TestParsePrivateOrderPreserved(t *testing.T) {
	r := Parse(`<clawbench-mention targets="A,B">表态</clawbench-mention>` +
		`<clawbench-mention targets="A" private>给A一</clawbench-mention>` +
		`<clawbench-mention targets="B" private>给B二</clawbench-mention>`)
	if len(r.Bcc) != 2 {
		t.Fatalf("bcc=%v", r.Bcc)
	}
	if r.Bcc[0].Content != "给A一" || r.Bcc[1].Content != "给B二" {
		t.Fatalf("order/content wrong: %v", r.Bcc)
	}
}

// TestParsePrivateMalformedKept: a malformed private-ish span (single quotes /
// no targets / empty targets / uppercase attr) is NOT recognized as a note (no
// card) — the display contract stays fail-open. The INJECTION contract is the
// opposite (see StripProtocolTags).
func TestParsePrivateMalformedKept(t *testing.T) {
	cases := []string{
		`<clawbench-mention private>无 targets</clawbench-mention>`,
		`<clawbench-mention targets="" private>空名单</clawbench-mention>`,
		`<clawbench-mention targets='A' private>单引号</clawbench-mention>`,
		`<clawbench-mention Targets="A" private>大写</clawbench-mention>`,
	}
	for _, in := range cases {
		r := Parse(in)
		if len(r.Bcc) != 0 {
			t.Errorf("malformed %q must not parse a private note, got %v", in, r.Bcc)
		}
	}
	// Fail-closed: an unclosed note's tail must NOT reach the instruction.
	r := Parse(`<clawbench-mention targets="A">请谈</clawbench-mention> <clawbench-mention targets="A" private>未闭合`)
	if strings.Contains(r.Instruction, "未闭合") || strings.Contains(r.Instruction, "clawbench-mention") {
		t.Fatalf("an unclosed note leaked into the instruction (fail-closed): %q", r.Instruction)
	}
	if r.Instruction != "请谈" {
		t.Fatalf("instruction=%q want %q", r.Instruction, "请谈")
	}
}

// TestParseEndInsidePrivateNotEnd: an end tag INSIDE a private note is private
// instruction to the target, not a discussion-ending signal for the group.
func TestParseEndInsidePrivateNotEnd(t *testing.T) {
	r := Parse(`<clawbench-mention targets="A">表态</clawbench-mention> <clawbench-mention targets="A" private>如果没意见就 <clawbench-group-end/> 收尾</clawbench-mention>`)
	if r.End {
		t.Fatal("an end tag inside a private note must not end the discussion")
	}
	if len(r.Bcc) != 1 || !strings.Contains(r.Bcc[0].Content, "收尾") {
		t.Fatalf("bcc=%v", r.Bcc)
	}
}

// TestParsePrivateWithEndTag: a real end tag alongside a private note is honored.
func TestParsePrivateWithEndTag(t *testing.T) {
	r := Parse(`<clawbench-mention targets="A">表态</clawbench-mention> <clawbench-mention targets="A" private>私</clawbench-mention><clawbench-group-end/>`)
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

// ── Fail-closed stripping for injection ───────────────────────────────────
//
// StripProtocolTags unwraps public mentions (their body survives), removes
// private mentions entirely, and drops the end signal. It is the security
// boundary: a private note must never reach a non-target member.

func TestStripProtocolTags(t *testing.T) {
	cases := []struct{ in, want string }{
		{"no tag here", "no tag here"},
		{`<clawbench-mention targets="A">请回应</clawbench-mention>`, "请回应"},
		{`前 <clawbench-mention targets="A">中</clawbench-mention> 后`, "前 中 后"},
		{`<clawbench-mention targets="A" private>秘密</clawbench-mention>`, ""},
		{`公开<clawbench-mention targets="A" private>秘密</clawbench-mention>尾巴`, "公开尾巴"},
		{`收尾 <clawbench-group-end/>`, "收尾"},
	}
	for _, tc := range cases {
		if got := StripProtocolTags(tc.in); got != tc.want {
			t.Errorf("StripProtocolTags(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A malformed private-like span is removed entirely by the injection boundary
// (fail-closed), whatever its shape.
func TestStripProtocolTags_FailClosedMalformed(t *testing.T) {
	cases := []string{
		`<clawbench-mention targets='A' private>秘密一</clawbench-mention>`,
		`<clawbench-mention private>秘密二</clawbench-mention>`,
		`<clawbench-mention Targets="A" private>秘密三</clawbench-mention>`,
		`<clawbench-mention targets="A" private>未闭合秘密`,
	}
	for _, in := range cases {
		got := StripProtocolTags(in)
		if strings.Contains(got, "秘密") || strings.Contains(got, "clawbench-mention") {
			t.Errorf("StripProtocolTags(%q) = %q, must drop the whole span", in, got)
		}
	}
}

// Nested / stray closing tags must not leave protocol residue.
func TestStripProtocolTags_NestedAndStray(t *testing.T) {
	nested := `<clawbench-mention targets="A">外<clawbench-mention targets="B">内</clawbench-mention></clawbench-mention>`
	got := StripProtocolTags(nested)
	if strings.Contains(got, "clawbench-mention") {
		t.Fatalf("nested tags left residue: %q", got)
	}
	stray := `<clawbench-mention targets="A">笔记</clawbench-mention></clawbench-mention>`
	if got := StripProtocolTags(stray); strings.Contains(got, "clawbench-mention") {
		t.Fatalf("stray closing tag left residue: %q", got)
	}
}

// ── Public-mention joining ────────────────────────────────────────────────

// TestParseDedupSpeakersAcrossMentions: the flat Speakers view de-duplicates
// targets across several public mentions while Instruction joins every body.
func TestParseDedupSpeakersAcrossMentions(t *testing.T) {
	r := Parse(`<clawbench-mention targets="A">先说</clawbench-mention><clawbench-mention targets="A,B">再说</clawbench-mention>`)
	if !reflect.DeepEqual(r.Speakers, []string{"A", "B"}) {
		t.Fatalf("speakers=%v", r.Speakers)
	}
	if r.Instruction != "先说\n\n再说" {
		t.Fatalf("instruction=%q", r.Instruction)
	}
}
