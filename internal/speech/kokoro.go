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
)

const (
	// kokoroScriptVenv is the primary path to the Kokoro TTS Python bridge script (in .venv).
	kokoroScriptVenv = ".venv/bin/kokoro_tts.py"
	// kokoroScriptRepo is the fallback path (tracked in git repo).
	kokoroScriptRepo = "scripts/kokoro_tts.py"
)

// KokoroProvider implements SpeechProvider using Kokoro-82M (local, ONNX-based TTS).
// It uses the shared summarizer (configured globally) and the kokoro-onnx Python
// library for synthesis via a bridge script. Kokoro produces high-quality Chinese
// speech and runs locally with ONNX Runtime (no GPU required).
//
// Synthesize is implemented separately because it uses a two-step binary resolution
// (Python interpreter + bridge script) that does not fit the single-binary pattern
// of CLISpeechProvider.
type KokoroProvider struct {
	CLISpeechProvider
	// ModelPath is the path to the Kokoro .onnx model file.
	// If empty, defaults to models/kokoro-models/kokoro-v1.1-zh.onnx (under BinDir).
	ModelPath string
	// VoicesPath is the path to the Kokoro voices .bin file.
	// If empty, defaults to models/kokoro-models/voices-v1.1-zh.bin (under BinDir).
	VoicesPath string
	// Voice is the voice name (e.g. "zf_001", "zm_010" for v1.1; "zf_xiaobei", "zm_yunxi" for v1.0).
	Voice string
	// Lang is the language code for espeak phonemization (default: "cmn" for Mandarin Chinese).
	Lang string
	// Speed is the speech speed multiplier (default: 1.0).
	Speed float64
}

// NewKokoroProvider creates a KokoroProvider with sensible defaults.
func NewKokoroProvider() *KokoroProvider {
	return &KokoroProvider{
		Voice: "zf_001",
		Lang:  "cmn",
		Speed: 1.0,
	}
}

// Synthesize generates an audio file at outputPath using Kokoro via Python bridge.
// Text is piped via stdin to the bridge script.
func (p *KokoroProvider) Synthesize(ctx context.Context, text string, outputPath string, _ string) error {
	dir := filepath.Dir(outputPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory %s: %w", dir, err)
	}

	// Validate model files exist
	if p.ModelPath == "" {
		return fmt.Errorf("kokoro model path not configured")
	}
	if _, err := os.Stat(p.ModelPath); err != nil {
		return fmt.Errorf("kokoro model file not found: %s", p.ModelPath)
	}
	if p.VoicesPath == "" {
		return fmt.Errorf("kokoro voices path not configured")
	}
	if _, err := os.Stat(p.VoicesPath); err != nil {
		return fmt.Errorf("kokoro voices file not found: %s", p.VoicesPath)
	}

	// Resolve Python interpreter and bridge script path
	pythonPath := ".venv/bin/python3"
	scriptPath := kokoroScriptRepo // default to repo copy
	if exePath, err := os.Executable(); err == nil {
		binDir := filepath.Dir(exePath)
		candidatePython := filepath.Join(binDir, ".venv/bin/python3")
		if _, err := os.Stat(candidatePython); err == nil {
			pythonPath = candidatePython
		}
		// Prefer .venv copy (closer to Python env), fall back to repo copy
		for _, candidate := range []string{
			filepath.Join(binDir, kokoroScriptVenv),
			filepath.Join(binDir, kokoroScriptRepo),
		} {
			if _, err := os.Stat(candidate); err == nil {
				scriptPath = candidate
				break
			}
		}
	}

	args := []string{
		scriptPath,
		"--model", p.ModelPath,
		"--voices", p.VoicesPath,
		"--voice", p.Voice,
		"--lang", p.Lang,
		"--speed", fmt.Sprintf("%g", p.Speed),
		"--output", outputPath,
	}

	slog.Info(
		"kokoro synthesize",
		slog.String("output", outputPath),
		slog.String("model", p.ModelPath),
		slog.String("voice", p.Voice),
		slog.String("lang", p.Lang),
		slog.Float64("speed", p.Speed),
		slog.Int("text_len", len(text)),
	)

	cmd := exec.CommandContext(ctx, pythonPath, args...)
	cmd.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kokoro failed: %w (stderr: %s)", err, stderr.String())
	}

	if _, err := os.Stat(outputPath); err != nil {
		return fmt.Errorf("output file not created: %s", outputPath)
	}

	return nil
}

// ResolveKokoroPaths resolves the Kokoro model and voices paths from defaults.
// If a path is explicitly set, it is returned as-is.
// Otherwise, the path is resolved under {BinDir}/models/kokoro-models/,
// with fallback to {DataDir}/kokoro-models/ (deprecated).
func ResolveKokoroPaths(modelPath, voicesPath string) (string, string) {
	if modelPath == "" {
		modelPath = resolveModelFile("kokoro-models", "kokoro-v1.1-zh.onnx")
	}
	if voicesPath == "" {
		voicesPath = resolveModelFile("kokoro-models", "voices-v1.1-zh.bin")
	}
	return modelPath, voicesPath
}
