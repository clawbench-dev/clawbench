package model

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ParseProviderModel — "provider/model" one per line (opencode)
// ---------------------------------------------------------------------------

func TestParseProviderModel_BasicLines(t *testing.T) {
	out := "opencode/minimax-m2.5-free\nminimax/MiniMax-M2.5\nanthropic/claude-sonnet-4-6\n"

	got := ParseProviderModel(out)

	require.Len(t, got, 3)
	assert.Equal(t, "opencode/minimax-m2.5-free", got[0].ID)
	assert.Equal(t, "opencode/minimax-m2.5-free", got[0].Name, "provider-prefixed name disambiguates across providers")
	assert.True(t, got[0].Default, "first entry is the default")
	assert.False(t, got[1].Default)
}

func TestParseProviderModel_SkipsBlankAndUnpairedLines(t *testing.T) {
	out := "\n  \nno-slash-here\nok/model\n"

	got := ParseProviderModel(out)

	require.Len(t, got, 1)
	assert.Equal(t, "ok/model", got[0].ID)
}

func TestParseProviderModel_Deduplicates(t *testing.T) {
	got := ParseProviderModel("a/b\na/b\n")

	require.Len(t, got, 1)
}

func TestParseProviderModel_EmptyInput(t *testing.T) {
	assert.Nil(t, ParseProviderModel(""))
	assert.Nil(t, ParseProviderModel("\n\n"))
}

// ---------------------------------------------------------------------------
// ParseTabular — first two whitespace-separated fields (pi)
// ---------------------------------------------------------------------------

func TestParseTabular_SkipsHeaderRow(t *testing.T) {
	out := "provider        model                       context  max-out\n" +
		"anthropic       claude-sonnet-4-6           1M       64K\n" +
		"openai          gpt-4o                      128K     4.1K\n"

	got := ParseTabular(out)

	require.Len(t, got, 2, "the 'provider model' header row must be dropped")
	assert.Equal(t, "anthropic/claude-sonnet-4-6", got[0].ID)
	assert.Equal(t, "openai/gpt-4o", got[1].ID)
	assert.True(t, got[0].Default)
}

func TestParseTabular_SkipsSingleFieldLines(t *testing.T) {
	got := ParseTabular("lonely\nanthropic  claude-sonnet-4-6\n")

	require.Len(t, got, 1)
	assert.Equal(t, "anthropic/claude-sonnet-4-6", got[0].ID)
}

func TestParseTabular_EmptyInput(t *testing.T) {
	assert.Nil(t, ParseTabular(""))
}

// ---------------------------------------------------------------------------
// ParsePlainLines — one ID per line with a status-line filter (antigravity)
// ---------------------------------------------------------------------------

func TestParsePlainLines_AppliesNameMap(t *testing.T) {
	names := map[string]string{"gemini-3-pro": "Gemini 3 Pro"}
	out := "gemini-3-pro\ngemini-3-flash\n"

	got := ParsePlainLines(out, PlainLineOptions{Names: names})

	require.Len(t, got, 2)
	assert.Equal(t, "Gemini 3 Pro", got[0].Name)
	assert.Equal(t, "gemini-3-flash", got[1].Name, "unknown IDs fall back to the raw ID as the name")
}

func TestParsePlainLines_StatusFilterDropsDiagnostics(t *testing.T) {
	out := "Fetching available models...\n" +
		"You are not logged into Antigravity\n" +
		"error something broke\n" +
		"Failed to fetch\n" +
		"I0428 10:00:00.000000   1234 log noise\n" +
		"gemini-3-pro\n"

	// A plain-line parser cannot tell prose from an ID on its own, so the
	// caller supplies the diagnostic filter — mirroring the agy status-line
	// check the antigravity backend passes in.
	logPrefix := regexp.MustCompile(`^[IWEF]\d{4}\s`)
	got := ParsePlainLines(out, PlainLineOptions{
		Skip: func(line string) bool {
			switch {
			case line == "Fetching available models...":
				return true
			case line == "You are not logged into Antigravity":
				return true
			case strings.HasPrefix(line, "error "):
				return true
			case strings.Contains(line, "Failed to"):
				return true
			case logPrefix.MatchString(line):
				return true
			}
			return false
		},
	})

	require.Len(t, got, 1)
	assert.Equal(t, "gemini-3-pro", got[0].ID)
}

func TestParsePlainLines_SkipExactPrefixAndContains(t *testing.T) {
	out := "Fetching available models...\nerror broke\nFailed to fetch\nI0428 noise\ngemini-3-pro\n"

	got := ParsePlainLines(out, PlainLineOptions{
		SkipExact:    []string{"Fetching available models..."},
		SkipPrefix:   []string{"error "},
		SkipContains: []string{"Failed to"},
		Skip: func(line string) bool {
			return strings.HasPrefix(line, "I0428")
		},
	})

	require.Len(t, got, 1)
	assert.Equal(t, "gemini-3-pro", got[0].ID)
}

func TestParsePlainLines_TrimsAndDeduplicates(t *testing.T) {
	got := ParsePlainLines("  a  \na\n\nb\n", PlainLineOptions{})

	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].ID)
}

func TestParsePlainLines_EmptyInput(t *testing.T) {
	assert.Nil(t, ParsePlainLines("", PlainLineOptions{}))
}

// ---------------------------------------------------------------------------
// ParseRegexCapture — regex with named/positional capture (deepseek, grok)
// ---------------------------------------------------------------------------

func TestParseRegexCapture_UsesCaptureAsID(t *testing.T) {
	out := "  deepseek-v4-flash (deepseek)\n* deepseek-v4-pro (deepseek)\n"

	got := ParseRegexCapture(out, RegexOptions{
		Pattern: `^(\*?)\s*(\S+)\s+\((\S+)\)`,
		IDGroup: 2,
	})

	require.Len(t, got, 2)
	assert.Equal(t, "deepseek-v4-flash", got[0].ID)
	assert.Equal(t, "deepseek-v4-pro", got[1].ID)
}

func TestParseRegexCapture_DefaultMarkedByStarGroup(t *testing.T) {
	out := "  deepseek-v4-flash (deepseek)\n* deepseek-v4-pro (deepseek)\n"

	got := ParseRegexCapture(out, RegexOptions{
		Pattern:      `^(\*?)\s*(\S+)\s+\((\S+)\)`,
		IDGroup:      2,
		DefaultGroup: 1,
		DefaultValue: "*",
	})

	require.Len(t, got, 2)
	assert.False(t, got[0].Default)
	assert.True(t, got[1].Default, "the '*' bullet marks the default")
}

func TestParseRegexCapture_TransformsID(t *testing.T) {
	out := "  deepseek-v4-flash (deepseek)\n  other-model (other)\n"

	got := ParseRegexCapture(out, RegexOptions{
		Pattern: `^\s*(\S+)\s+\((\S+)\)`,
		IDGroup: 1,
		Transform: func(m []string) string {
			return m[2] + "/" + m[1] // provider/id
		},
	})

	require.Len(t, got, 2)
	assert.Equal(t, "deepseek/deepseek-v4-flash", got[0].ID)
	assert.Equal(t, "other/other-model", got[1].ID)
}

func TestParseRegexCapture_FilterRejectsLines(t *testing.T) {
	out := "  deepseek-v4-flash (deepseek)\n  other-model (other)\n"

	got := ParseRegexCapture(out, RegexOptions{
		Pattern: `^\s*(\S+)\s+\((\S+)\)`,
		IDGroup: 1,
		Filter: func(m []string) bool {
			return m[2] == "deepseek"
		},
	})

	require.Len(t, got, 1)
	assert.Equal(t, "deepseek-v4-flash", got[0].ID)
}

func TestParseRegexCapture_CaptureIDGroupZeroUsesWholeMatch(t *testing.T) {
	got := ParseRegexCapture("abc-123\n", RegexOptions{Pattern: `^[a-z]+-\d+$`})

	require.Len(t, got, 1)
	assert.Equal(t, "abc-123", got[0].ID)
}

func TestParseRegexCapture_InvalidPatternYieldsNothing(t *testing.T) {
	assert.Nil(t, ParseRegexCapture("anything", RegexOptions{Pattern: `([`}))
}

func TestParseRegexCapture_EmptyInput(t *testing.T) {
	assert.Nil(t, ParseRegexCapture("", RegexOptions{Pattern: `^(\S+)$`}))
}

// ---------------------------------------------------------------------------
// ParseBulletList — bullets, headers and an explicit default line (grok)
// ---------------------------------------------------------------------------

func TestParseBulletList_BothBulletStyles(t *testing.T) {
	out := "Available models:\n- grok-4.5 (default)\n* grok-code\n"

	got := ParseBulletList(out, BulletOptions{})

	require.Len(t, got, 2)
	assert.Equal(t, "grok-4.5", got[0].ID)
	assert.True(t, got[0].Default, "'(default)' suffix marks the default")
	assert.Equal(t, "grok-code", got[1].ID)
	assert.False(t, got[1].Default)
}

func TestParseBulletList_SkipsSectionHeaders(t *testing.T) {
	out := "Available models\nDefault model: grok-3\n- grok-4.5\n"

	got := ParseBulletList(out, BulletOptions{})

	require.Len(t, got, 1, "'Available models' must not be read as a model")
	assert.Equal(t, "grok-4.5", got[0].ID)
}

func TestParseBulletList_ExplicitDefaultLineWins(t *testing.T) {
	out := "Default model: grok-3\n- grok-4.5\n- grok-3\n"

	got := ParseBulletList(out, BulletOptions{})

	require.Len(t, got, 2)
	assert.False(t, got[0].Default)
	assert.True(t, got[1].Default, "'Default model:' line selects the default")
}

func TestParseBulletList_FirstModelDefaultWhenNothingMarked(t *testing.T) {
	got := ParseBulletList("- a\n- b\n", BulletOptions{})

	require.Len(t, got, 2)
	assert.True(t, got[0].Default)
}

func TestParseBulletList_NameMapApplied(t *testing.T) {
	got := ParseBulletList("- grok-4.5\n", BulletOptions{Names: map[string]string{"grok-4.5": "Grok 4.5"}})

	require.Len(t, got, 1)
	assert.Equal(t, "Grok 4.5", got[0].Name)
}

func TestParseBulletList_EmptyInput(t *testing.T) {
	assert.Nil(t, ParseBulletList("", BulletOptions{}))
}

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

func TestMarkFirstDefault_NoOpWhenAlreadyMarked(t *testing.T) {
	models := []AgentModel{{ID: "a"}, {ID: "b", Default: true}}

	markFirstDefault(models)

	assert.False(t, models[0].Default)
	assert.True(t, models[1].Default)
}

func TestMarkFirstDefault_EmptySlice(t *testing.T) {
	markFirstDefault(nil) // must not panic
}

func TestDedupeModels_KeepsFirstOccurrence(t *testing.T) {
	got := dedupeModels([]AgentModel{
		{ID: "a", Name: "first"},
		{ID: "b", Name: "B"},
		{ID: "a", Name: "second"},
	})

	require.Len(t, got, 2)
	assert.Equal(t, "first", got[0].Name)
}

func TestDedupeModels_DropsBlankIDs(t *testing.T) {
	got := dedupeModels([]AgentModel{{ID: ""}, {ID: " "}, {ID: "a"}})

	require.Len(t, got, 1)
	assert.Equal(t, "a", got[0].ID)
}

func TestDedupeModels_TrimsIDs(t *testing.T) {
	got := dedupeModels([]AgentModel{{ID: "  a  "}})

	require.Len(t, got, 1)
	assert.Equal(t, "a", got[0].ID)
}

// ---------------------------------------------------------------------------
// Additional parser coverage
// ---------------------------------------------------------------------------

func TestParseRegexCapture_HeaderDefaultLineNotTreatedAsEntry(t *testing.T) {
	out := "Available models (default: a)\n  a (p)\n  b (p)\n"

	got := ParseRegexCapture(out, RegexOptions{
		Pattern:            `^\s*(\S+)\s+\((\S+)\)`,
		IDGroup:            1,
		DefaultLinePattern: `Available models \(default:\s*(\S+)\)`,
		DefaultLineGroup:   1,
	})

	require.Len(t, got, 2, "the header line must not become a model")
	assert.True(t, got[0].Default)
	assert.False(t, got[1].Default)
}

func TestParseRegexCapture_InvalidDefaultLinePattern(t *testing.T) {
	assert.Nil(t, ParseRegexCapture("x", RegexOptions{
		Pattern:            `^(\S+)$`,
		DefaultLinePattern: `([`,
	}))
}

func TestParseRegexCapture_NameGroup(t *testing.T) {
	got := ParseRegexCapture("id-a|Pretty Name A\n", RegexOptions{
		Pattern:   `^(\S+)\|(.+)$`,
		IDGroup:   1,
		NameGroup: 2,
	})

	require.Len(t, got, 1)
	assert.Equal(t, "id-a", got[0].ID)
	assert.Equal(t, "Pretty Name A", got[0].Name)
}

func TestParseBulletList_SkipCallback(t *testing.T) {
	got := ParseBulletList("- keep\n- dropme\n", BulletOptions{
		Skip: func(line string) bool { return strings.Contains(line, "dropme") },
	})

	require.Len(t, got, 1)
	assert.Equal(t, "keep", got[0].ID)
}

func TestParseBulletList_PlusBullet(t *testing.T) {
	got := ParseBulletList("+ plus-model\n", BulletOptions{})

	require.Len(t, got, 1)
	assert.Equal(t, "plus-model", got[0].ID)
}

func TestParsePlainLines_SkipPrefixAndContains(t *testing.T) {
	got := ParsePlainLines("noise: x\nbad line\nreal-model\n", PlainLineOptions{
		SkipPrefix:   []string{"noise:"},
		SkipContains: []string{"bad"},
	})

	require.Len(t, got, 1)
	assert.Equal(t, "real-model", got[0].ID)
}

// ---------------------------------------------------------------------------
// Bullet requirement (grok regression)
// ---------------------------------------------------------------------------

// A single-token line with no bullet marker is prose ("Options:", "Usage:"), not
// a model. The bespoke grok parser required a "-" or "*" marker; the declarative
// one must too, or section headers leak into the model list.
func TestParseBulletList_RejectsUnbulletedSingleTokenProse(t *testing.T) {
	out := "Options:\n  * grok-4.5\n  * grok-build\n"

	got := ParseBulletList(out, BulletOptions{})

	require.Len(t, got, 2, "the section header must not become a model")
	assert.Equal(t, "grok-4.5", got[0].ID)
	assert.Equal(t, "grok-build", got[1].ID)
}

func TestParseBulletList_RejectsVariousHeaders(t *testing.T) {
	for _, header := range []string{"Options:", "Usage:", "Tips:", "authenticated"} {
		got := ParseBulletList(header+"\n  * real-model\n", BulletOptions{})
		require.Len(t, got, 1, "header %q must not become a model", header)
		assert.Equal(t, "real-model", got[0].ID)
	}
}

// Every recognized bullet marker still works.
func TestParseBulletList_AllBulletMarkers(t *testing.T) {
	got := ParseBulletList("- dash\n* star\n+ plus\n", BulletOptions{})

	require.Len(t, got, 3)
	assert.Equal(t, "dash", got[0].ID)
	assert.Equal(t, "star", got[1].ID)
	assert.Equal(t, "plus", got[2].ID)
}

// A bare hyphenated ID on its own line is still accepted (the bespoke grok parser
// did), while single-token prose without a hyphen or bullet is not.
func TestParseBulletList_BareHyphenatedIDStillAccepted(t *testing.T) {
	got := ParseBulletList("grok-4.5\ngrok-build\n", BulletOptions{})

	require.Len(t, got, 2)
	assert.Equal(t, "grok-4.5", got[0].ID)
	assert.Equal(t, "grok-build", got[1].ID)
}
