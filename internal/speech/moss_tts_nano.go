package speech

import (
	"path/filepath"

	"clawbench/internal/model"
)

// MossNanoProvider implements SpeechProvider using MOSS-TTS-Nano (local, ONNX-based TTS).
//
// MOSS-TTS-Nano is a 0.1B-parameter multilingual speech generation model from MOSI.AI
// and the OpenMOSS team. It supports real-time streaming on CPU via ONNX Runtime,
// produces 48kHz stereo WAV output, and supports ~20 languages including Chinese,
// English, Japanese, Korean, and more.
type MossNanoProvider struct {
	CLISpeechProvider
	// ModelDir is the directory containing MOSS-TTS-Nano ONNX model files.
	// If empty, models are auto-detected under {BinDir}/models/moss-nano-models/,
	// or CLI auto-downloads on first run.
	ModelDir string
	// Backend selects the inference backend: "onnx" (default, CPU-friendly) or "pytorch" (requires GPU).
	Backend string
	// Voice is the built-in voice preset name for ONNX backend.
	// Default: "Junhao".
	Voice string
}

// NewMossNanoProvider creates a MossNanoProvider with sensible defaults.
func NewMossNanoProvider() *MossNanoProvider {
	p := &MossNanoProvider{
		Backend: "onnx",
		Voice:   "Junhao",
	}

	p.CLISpeechProvider = newCLISpeechProvider(SynthesizeOptions{
		BinaryName: "moss-tts-nano",
		TextSource: TextViaTempFile,
		LogName:    "moss-nano",
		ExtraArgs: func(cliPath string, text string, outputPath string, _ string) []string {
			args := []string{
				"generate",
				"--backend", p.Backend,
				"--text-file", text,
				"--output", outputPath,
			}
			if p.ModelDir != "" {
				args = append(args, "--onnx-model-dir", p.ModelDir)
			}
			if p.Voice != "" {
				args = append(args, "--voice", p.Voice)
			}
			return args
		},
	})

	return p
}

// ResolveMossNanoModelDir resolves the MOSS-TTS-Nano model directory.
// If modelDir is explicitly set, it is returned as-is.
// Otherwise, it checks {BinDir}/models/moss-nano-models/.
// If no models found, returns "" to let the CLI auto-download.
func ResolveMossNanoModelDir(modelDir string) string {
	if modelDir != "" {
		return modelDir
	}

	// Check {BinDir}/models/moss-nano-models/
	if model.BinDir != "" {
		dir := filepath.Join(model.BinDir, "models", "moss-nano-models")
		matches, _ := filepath.Glob(filepath.Join(dir, "*", "browser_poc_manifest.json"))
		if len(matches) > 0 {
			return dir
		}
	}

	return "" // let CLI auto-download
}
