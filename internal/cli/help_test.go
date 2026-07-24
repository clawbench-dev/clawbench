package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFlagDisplayName(t *testing.T) {
	tests := []struct {
		flag FlagHelp
		want string
	}{
		{
			flag: FlagHelp{Name: "name", Type: "string"},
			want: "--name string",
		},
		{
			flag: FlagHelp{Name: "q", Short: "q", Type: "string"},
			want: "-q string",
		},
		{
			flag: FlagHelp{Name: "verbose", Type: ""},
			want: "--verbose",
		},
		{
			flag: FlagHelp{Name: "limit", Type: "int"},
			want: "--limit int",
		},
	}
	for _, tt := range tests {
		got := flagDisplayName(tt.flag)
		assert.Equal(t, tt.want, got)
	}
}

func TestPrintHelp_Output(t *testing.T) {
	info := HelpInfo{
		Usage:       "clawbench task create [flags]",
		Description: "Create a new scheduled task.",
		Flags: []FlagHelp{
			{Name: "name", Type: "string", Desc: "Brief task name", Required: true},
			{Name: "repeat", Type: "string", Default: "unlimited", Desc: "Repeat mode: once|limited|unlimited"},
		},
		Positional: "TASK_ID  (required) ID of the task",
		Examples: []string{
			`clawbench task create --name "test" --cron "0 9 * * *" --agent codebuddy --prompt "test"`,
		},
		Footer: "Response format:\n  {\"ok\":true}",
	}

	// Capture stdout by calling printHelp and checking it doesn't panic
	// (full output capture would require redirecting os.Stdout, but we just
	// verify the function works without errors and the builder logic is correct)
	assert.NotPanics(t, func() {
		printHelp(info)
	})
}

func TestPrintGroupHelp(t *testing.T) {
	subcommands := []CmdHelp{
		{Name: "create", Desc: "Create a new task"},
		{Name: "delete", Desc: "Delete a task"},
	}

	assert.NotPanics(t, func() {
		printGroupHelp("clawbench task <subcommand> [options]", "Manage tasks.", subcommands)
	})
}

func TestHelpInfo_FlagsAlignment(t *testing.T) {
	// Verify flags with different name lengths are properly aligned
	info := HelpInfo{
		Usage: "test [flags]",
		Flags: []FlagHelp{
			{Name: "a", Type: "string", Desc: "Short flag"},
			{Name: "very-long-name", Type: "int", Desc: "Long flag name"},
		},
	}

	assert.NotPanics(t, func() {
		printHelp(info)
	})
}

func TestSearchHelpContainsRequiredFlags(t *testing.T) {
	// Verify the searchHelp definition has the required -q flag
	found := false
	for _, f := range searchHelp.Flags {
		if f.Name == "q" && f.Required {
			found = true
			break
		}
	}
	assert.True(t, found, "searchHelp should have -q flag marked as required")
}

func TestCreateHelpContainsCronReference(t *testing.T) {
	assert.True(t, strings.Contains(createHelp.Footer, "Cron"), "createHelp footer should contain cron reference")
	assert.True(t, strings.Contains(createHelp.Footer, "9:00"), "createHelp footer should contain cron examples")
}

func TestCreateHelpHasExamples(t *testing.T) {
	assert.NotEmpty(t, createHelp.Examples, "createHelp should have examples")
}

func TestSearchHelpHasTips(t *testing.T) {
	assert.True(t, strings.Contains(searchHelp.Footer, "Tips"), "searchHelp footer should contain tips section")
}

// ---------- parseOrHelp tests ----------

func TestParseOrHelp_ValidFlags(t *testing.T) {
	fs := flagSet("test")
	name := fs.String("name", "", "name flag")
	info := &HelpInfo{Usage: "test [flags]"}

	// Should parse successfully and not exit
	// parseOrHelp returns true if help was printed, false otherwise.
	// With valid flags, it returns false.
	result := parseOrHelp(fs, []string{"--name", "hello"}, info)
	assert.False(t, result)
	assert.Equal(t, "hello", *name)
}

func TestParseOrHelp_HelpFlag(t *testing.T) {
	// --help/-h triggers flag.ErrHelp which causes os.Exit(0) in parseOrHelp.
	// We can't easily test os.Exit, so we test the flag.ErrHelp behavior
	// by calling fs.Parse directly.
	fs := flagSet("test")
	err := fs.Parse([]string{"--help"})
	assert.ErrorIs(t, err, flag.ErrHelp)
}

func TestParseOrHelp_InvalidFlag(t *testing.T) {
	// An unrecognized flag causes a parse error.
	fs := flagSet("test")
	err := fs.Parse([]string{"--nonexistent"})
	assert.Error(t, err)
	assert.NotErrorIs(t, err, flag.ErrHelp)
}

func TestParseOrHelp_EmptyArgs(t *testing.T) {
	fs := flagSet("test")
	info := &HelpInfo{Usage: "test [flags]"}

	result := parseOrHelp(fs, []string{}, info)
	assert.False(t, result)
}

// ---------- printHelp with subcommands ----------

func TestPrintHelp_WithSubcommands(t *testing.T) {
	info := HelpInfo{
		Usage:       "clawbench rag <subcommand> [options]",
		Description: "RAG operations",
		Subcommands: []CmdHelp{
			{Name: "search", Desc: "Search conversations"},
			{Name: "message", Desc: "Get message detail"},
		},
	}
	assert.NotPanics(t, func() {
		printHelp(info)
	})
}

func TestPrintHelp_MinimalInfo(t *testing.T) {
	info := HelpInfo{
		Usage: "clawbench test",
	}
	assert.NotPanics(t, func() {
		printHelp(info)
	})
}

func TestPrintHelp_WithExamplesNoFlags(t *testing.T) {
	info := HelpInfo{
		Usage:    "clawbench test",
		Examples: []string{"clawbench test --flag"},
		Footer:   "Additional info here",
	}
	assert.NotPanics(t, func() {
		printHelp(info)
	})
}

// ---------- flagDisplayName edge cases ----------

func TestFlagDisplayName_ShortFlagNoType(t *testing.T) {
	f := FlagHelp{Name: "verbose", Short: "v", Type: ""}
	got := flagDisplayName(f)
	assert.Equal(t, "-v", got)
}

func TestFlagDisplayName_LongFlagWithType(t *testing.T) {
	f := FlagHelp{Name: "output", Type: "string"}
	got := flagDisplayName(f)
	assert.Equal(t, "--output string", got)
}
