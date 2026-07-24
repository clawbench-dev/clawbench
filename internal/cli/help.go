package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

// FlagHelp describes a single flag for help output.
type FlagHelp struct {
	Name     string // e.g. "name", "q" (without dashes)
	Short    string // short flag name, e.g. "q" for "-q"
	Type     string // "string", "int", "" for bool
	Default  string // default value description; empty means required
	Desc     string // human-readable description
	Required bool   // if true, mark as (required)
}

// HelpInfo describes a command or subcommand's help output.
type HelpInfo struct {
	Usage       string     // e.g. "clawbench task create [flags]"
	Description string     // one-line description
	Subcommands []CmdHelp  // child subcommands (for group commands)
	Flags       []FlagHelp // flag definitions
	Positional  string     // positional arg description, e.g. "TASK_ID"
	Examples    []string   // example commands
	Footer      string     // additional info (cron reference, response format, tips)
}

// CmdHelp describes a subcommand entry in a group help listing.
type CmdHelp struct {
	Name string // subcommand name
	Desc string // one-line description
}

// printHelp formats and prints help info to stdout.
func printHelp(info HelpInfo) { //nolint:gocyclo // multi-command help text generation
	var b strings.Builder

	if info.Description != "" {
		b.WriteString(info.Description)
		b.WriteString("\n\n")
	}

	b.WriteString("Usage: ")
	b.WriteString(info.Usage)
	b.WriteString("\n")

	// Subcommands
	if len(info.Subcommands) > 0 {
		b.WriteString("\nSubcommands:\n")
		maxName := 0
		for _, cmd := range info.Subcommands {
			if len(cmd.Name) > maxName {
				maxName = len(cmd.Name)
			}
		}
		for _, cmd := range info.Subcommands {
			b.WriteString("  ")
			b.WriteString(cmd.Name)
			b.WriteString(strings.Repeat(" ", maxName-len(cmd.Name)+2))
			b.WriteString(cmd.Desc)
			b.WriteString("\n")
		}
		b.WriteString("\nRun \"")
		// Use first two words of Usage for group help prefix (e.g. "clawbench task")
		usageWords := strings.Fields(info.Usage)
		groupPrefix := usageWords[0]
		if len(usageWords) > 1 && !strings.HasPrefix(usageWords[1], "<") && !strings.HasPrefix(usageWords[1], "[") {
			groupPrefix = usageWords[0] + " " + usageWords[1]
		}
		b.WriteString(groupPrefix)
		b.WriteString(" <subcommand> --help\" for details.\n")
	}

	// Flags
	if len(info.Flags) > 0 {
		b.WriteString("\nFlags:\n")
		maxFlag := 0
		for _, f := range info.Flags {
			flagStr := flagDisplayName(f)
			if len(flagStr) > maxFlag {
				maxFlag = len(flagStr)
			}
		}
		for _, f := range info.Flags {
			b.WriteString("  ")
			name := flagDisplayName(f)
			b.WriteString(name)
			b.WriteString(strings.Repeat(" ", maxFlag-len(name)+2))
			b.WriteString(f.Desc)
			if f.Required {
				b.WriteString(" (required)")
			} else if f.Default != "" && f.Default != "0" && f.Default != "\"\"" {
				b.WriteString(" (default: ")
				b.WriteString(f.Default)
				b.WriteString(")")
			}
			b.WriteString("\n")
		}
	}

	// Positional args
	if info.Positional != "" {
		b.WriteString("\nPositional:\n  ")
		b.WriteString(info.Positional)
		b.WriteString("\n")
	}

	// Examples
	if len(info.Examples) > 0 {
		b.WriteString("\nExamples:\n")
		for _, ex := range info.Examples {
			b.WriteString("  ")
			b.WriteString(ex)
			b.WriteString("\n")
		}
	}

	// Footer
	if info.Footer != "" {
		b.WriteString("\n")
		b.WriteString(info.Footer)
		b.WriteString("\n")
	}

	fmt.Print(b.String())
}

// flagDisplayName returns the display form of a flag, e.g. "--name string" or "-q string".
func flagDisplayName(f FlagHelp) string {
	typ := f.Type
	if typ != "" {
		typ = " " + typ
	}
	if f.Short != "" {
		return "-" + f.Short + typ
	}
	return "--" + f.Name + typ
}

// parseOrHelp wraps flag.FlagSet.Parse() to handle --help and usage errors.
// Returns true if help was printed (caller should os.Exit).
func parseOrHelp(fs *flag.FlagSet, args []string, info *HelpInfo) bool {
	err := fs.Parse(args)
	if errors.Is(err, flag.ErrHelp) {
		printHelp(*info)
		os.Exit(0)
	}
	if err != nil {
		// Bad flag (e.g. --nonexistent) — show help and exit 1
		fmt.Fprintf(os.Stderr, "Error: %v\n\n", err)
		printHelp(*info)
		os.Exit(1)
	}
	return false
}

// printGroupHelp prints help for a command group that has subcommands but no flags.
// Used when a user runs "clawbench task" without a subcommand.
func printGroupHelp(usage string, description string, subcommands []CmdHelp) {
	info := HelpInfo{
		Usage:       usage,
		Description: description,
		Subcommands: subcommands,
	}
	printHelp(info)
}
