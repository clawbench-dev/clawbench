package speech

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	// piperCmd is the path to the piper executable, relative to the binary directory.
	piperCmd = ".venv/bin/piper"
)

// PiperProvider implements SpeechProvider using Piper (local, offline TTS).
// Piper runs entirely locally — no network required.
type PiperProvider struct {
	CLISpeechProvider
	// ModelPath is the path to the Piper .onnx model file.
	// If empty, defaults to models/piper-models/<voice>.onnx (under BinDir).
	ModelPath string
	// NoiseScale controls the randomness of the synthesis (default: 0.667).
	NoiseScale float64
	// LengthScale controls the speech rate (default: 1.0, lower = faster).
	LengthScale float64
	// SentenceSilence is the silence duration between sentences in seconds (default: 0.2).
	SentenceSilence float64
}

// NewPiperProvider creates a PiperProvider with sensible defaults.
func NewPiperProvider() *PiperProvider {
	p := &PiperProvider{
		NoiseScale:      0.667,
		LengthScale:     1.0,
		SentenceSilence: 0.2,
	}

	p.CLISpeechProvider = newCLISpeechProvider(SynthesizeOptions{
		RelativePath: piperCmd,
		BinaryName:   "piper",
		TextSource:   TextViaStdin,
		LogName:      "piper",
		Validate: func(_ any) error {
			if p.ModelPath == "" {
				return fmt.Errorf("piper model path not configured")
			}
			if _, err := os.Stat(p.ModelPath); err != nil {
				return fmt.Errorf("piper model file not found: %s", p.ModelPath)
			}
			return nil
		},
		PostResolve: func(_ any, cliPath string, cmd *exec.Cmd) {
			// Piper needs shared libraries (libespeak-ng, libonnxruntime, libpiper_phonemize)
			// in its own directory. Resolve symlinks to find the actual library directory.
			piperDir := filepath.Dir(cliPath)
			if resolved, err := filepath.EvalSymlinks(cliPath); err == nil {
				piperDir = filepath.Dir(resolved)
			}
			// Preserve any env vars previously set via opts.Env (cmd.Env may already
			// be populated by CLISpeechProvider.Synthesize). Fall back to os.Environ()
			// only if cmd.Env is nil (no prior env configuration).
			if cmd.Env == nil {
				cmd.Env = os.Environ()
			}
			switch runtime.GOOS {
			case "darwin":
				existing := os.Getenv("DYLD_LIBRARY_PATH")
				if existing == "" {
					cmd.Env = append(cmd.Env, "DYLD_LIBRARY_PATH="+piperDir)
				} else {
					cmd.Env = append(cmd.Env, "DYLD_LIBRARY_PATH="+piperDir+":"+existing)
				}
			case "windows":
				existing := os.Getenv("PATH")
				cmd.Env = append(cmd.Env, "PATH="+piperDir+";"+existing)
			default: // linux and other unix-like systems
				existing := os.Getenv("LD_LIBRARY_PATH")
				if existing == "" {
					cmd.Env = append(cmd.Env, "LD_LIBRARY_PATH="+piperDir)
				} else {
					cmd.Env = append(cmd.Env, "LD_LIBRARY_PATH="+piperDir+":"+existing)
				}
			}
		},
		ExtraArgs: func(cliPath string, text string, outputPath string, _ string) []string {
			args := []string{
				"--model", p.ModelPath,
				"--output_file", outputPath,
			}
			if p.NoiseScale > 0 {
				args = append(args, "--noise-scale", fmt.Sprintf("%g", p.NoiseScale))
			}
			if p.LengthScale > 0 {
				args = append(args, "--length-scale", fmt.Sprintf("%g", p.LengthScale))
			}
			if p.SentenceSilence > 0 {
				args = append(args, "--sentence-silence", fmt.Sprintf("%g", p.SentenceSilence))
			}
			return args
		},
	})

	return p
}

// ResolveModelPath resolves the Piper model path from voice name or explicit path.
// If modelPath is explicitly set, it is returned as-is.
// Otherwise, the voice name is used to construct an absolute path under
// {BinDir}/models/piper-models/, with fallback to {DataDir}/piper-models/ (deprecated).
func ResolveModelPath(voice, modelPath string) string {
	if modelPath != "" {
		return modelPath
	}
	if voice == "" {
		return ""
	}
	return resolveModelFile("piper-models", voice+".onnx")
}
