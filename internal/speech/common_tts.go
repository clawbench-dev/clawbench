package speech

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"clawbench/internal/model"
)

// TextSource describes how text is supplied to the CLI process.
type TextSource int

const (
	// TextViaStdin means text is piped to the CLI via stdin.
	TextViaStdin TextSource = iota
	// TextViaTempFile means text is written to a temp file and passed via --text-file / --file.
	TextViaTempFile TextSource = iota
)

// SynthesizeOptions defines the behavior of CLISpeechProvider.Synthesize.
type SynthesizeOptions struct {
	// BinaryName is the CLI binary name as found via $PATH (e.g. "mmx", "moss-tts-nano").
	BinaryName string

	// RelativePath is the path to the CLI binary relative to os.Executable()
	// directory (e.g. ".venv/bin/edge-tts"). When non-empty, this is checked first
	// before the default .venv/bin/<BinaryName> lookup and $PATH fallback.
	RelativePath string

	// ExtraArgs builds the full argument list for the sub-command.
	// It receives the resolved CLI binary path, the text (or temp file path when
	// TextSource is TextViaTempFile), the output path, and the language.
	ExtraArgs func(cliPath string, text string, outputPath string, language string) []string

	// TextSource describes how text is supplied to the CLI (stdin, temp file).
	TextSource TextSource

	// Env is an optional list of environment variables to add to cmd.Env.
	// Each element is of the form "KEY=value". nil means no extra env vars.
	Env []string

	// Validate is an optional pre-flight check run before building the command.
	// p is the concrete provider (e.g. *PiperProvider, *MossNanoProvider).
	// Return nil to proceed; return an error to abort synthesis.
	Validate func(p any) error

	// PostResolve is an optional hook called after the CLI binary path is resolved
	// but before the command is executed. It receives the provider and the resolved
	// cliPath, and may modify cmd.Env to add runtime-dependent environment variables
	// (e.g. LD_LIBRARY_PATH for shared library loading). The caller must copy
	// os.Environ() into cmd.Env before appending PostResolve additions.
	PostResolve func(p any, cliPath string, cmd *exec.Cmd)

	// LogName is the string used as the slog.Info "provider" field (e.g. "edge-tts").
	LogName string
}

// CLISpeechProvider is the base for all CLI-based SpeechProvider implementations.
// Each concrete provider embeds CLISpeechProvider and provides its own BinaryName
// and ExtraArgs via SynthesizeOptions.
type CLISpeechProvider struct {
	opts SynthesizeOptions
}

// newCLISpeechProvider constructs a CLISpeechProvider with the given options.
func newCLISpeechProvider(opts SynthesizeOptions) CLISpeechProvider {
	return CLISpeechProvider{opts: opts}
}

// Synthesize is the shared implementation for all CLI-based providers.
// It handles directory creation, binary resolution, temp-file / stdin
// text delivery, command execution, and output validation.
func (p CLISpeechProvider) Synthesize(ctx context.Context, text string, outputPath string, language string) error { //nolint:gocyclo // multi-provider CLI synthesis
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", dir, err)
	}

	if p.opts.Validate != nil {
		if err := p.opts.Validate(p); err != nil {
			return err
		}
	}

	// Resolve CLI binary path.
	cliPath := p.opts.BinaryName
	if p.opts.RelativePath != "" {
		if exePath, err := os.Executable(); err == nil {
			candidatePath := filepath.Join(filepath.Dir(exePath), p.opts.RelativePath)
			if _, err := os.Stat(candidatePath); err == nil {
				cliPath = candidatePath
			}
		}
	} else if exePath, err := os.Executable(); err == nil {
		candidatePath := filepath.Join(filepath.Dir(exePath), ".venv/bin", p.opts.BinaryName)
		if _, err := os.Stat(candidatePath); err == nil {
			cliPath = candidatePath
		}
	}

	// Fall back to $PATH lookup if not found relative to binary.
	if cliPath == p.opts.BinaryName || cliPath == p.opts.RelativePath {
		if absPath, err := exec.LookPath(p.opts.BinaryName); err == nil {
			cliPath = absPath
		}
	}

	// For TextViaTempFile, write text to a temp file first.
	textForArgs := text
	if p.opts.TextSource == TextViaTempFile {
		tmpFile, err := os.CreateTemp("", p.opts.LogName+"-text-*.txt")
		if err != nil {
			return fmt.Errorf("failed to create temp file: %w", err)
		}
		tmpPath := tmpFile.Name()
		defer func() { _ = os.Remove(tmpPath) }() // clean up after synthesis
		if _, err := tmpFile.WriteString(text); err != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("failed to write temp file: %w", err)
		}
		_ = tmpFile.Close()
		textForArgs = tmpPath
	}

	args := p.opts.ExtraArgs(cliPath, textForArgs, outputPath, language)

	slog.Info(
		p.opts.LogName+" synthesize",
		slog.String("output", outputPath),
		slog.Int("text_len", len(text)),
	)

	cmd := exec.CommandContext(ctx, cliPath, args...)
	cmd.Stderr = &bytes.Buffer{}

	if p.opts.TextSource == TextViaStdin {
		cmd.Stdin = strings.NewReader(text)
	}

	if len(p.opts.Env) > 0 {
		cmd.Env = append(os.Environ(), p.opts.Env...)
	}

	if p.opts.PostResolve != nil {
		p.opts.PostResolve(p, cliPath, cmd)
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w (stderr: %s)", p.opts.LogName, err, func() string { b, _ := cmd.Stderr.(*bytes.Buffer); return b.String() }())
	}

	if _, err := os.Stat(outputPath); err != nil {
		return fmt.Errorf("output file not created: %s", outputPath)
	}

	return nil
}

// resolveModelFile resolves a TTS model file path under {BinDir}/models/{subDir}/.
func resolveModelFile(subDir, filename string) string {
	if model.BinDir != "" {
		p := filepath.Join(model.BinDir, "models", subDir, filename)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Last resort: return relative path under models/ (will resolve from CWD)
	return filepath.Join("models", subDir, filename)
}
