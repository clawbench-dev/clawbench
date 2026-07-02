package speech

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// --- NewKokoroProvider defaults ---

func TestNewKokoroProvider_Defaults(t *testing.T) {
	p := NewKokoroProvider()
	assert.Equal(t, "", p.ModelPath)  // Must be configured or resolved
	assert.Equal(t, "", p.VoicesPath) // Must be configured or resolved
	assert.Equal(t, "zf_001", p.Voice)
	assert.Equal(t, "cmn", p.Lang)
	assert.Equal(t, 1.0, p.Speed)
}

// --- ResolveKokoroPaths ---

func TestResolveKokoroPaths_Defaults(t *testing.T) {
	// Without BinDir set, falls back to relative path
	mdl, voices := ResolveKokoroPaths("", "")
	assert.Equal(t, filepath.Join("models", "kokoro-models", "kokoro-v1.1-zh.onnx"), mdl)
	assert.Equal(t, filepath.Join("models", "kokoro-models", "voices-v1.1-zh.bin"), voices)
}

func TestResolveKokoroPaths_Explicit(t *testing.T) {
	model, voices := ResolveKokoroPaths("/custom/model.onnx", "/custom/voices.bin")
	assert.Equal(t, "/custom/model.onnx", model)
	assert.Equal(t, "/custom/voices.bin", voices)
}

// --- Synthesize missing model ---

func TestKokoroSynthesize_MissingModel(t *testing.T) {
	p := NewKokoroProvider()
	p.ModelPath = "/nonexistent/kokoro-v1.0.onnx"
	p.VoicesPath = "/nonexistent/voices-v1.0.bin"
	outputPath := filepath.Join(t.TempDir(), "output.wav")

	err := p.Synthesize(context.Background(), "test", outputPath, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "model file not found")
}

// --- Synthesize no model configured ---

func TestKokoroSynthesize_NoModelConfigured(t *testing.T) {
	p := NewKokoroProvider()
	outputPath := filepath.Join(t.TempDir(), "output.wav")

	err := p.Synthesize(context.Background(), "test", outputPath, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "model path not configured")
}

// --- Synthesize context cancellation ---

func TestKokoroSynthesize_CancelledContext(t *testing.T) {
	p := NewKokoroProvider()
	p.ModelPath = "/fake/model.onnx"
	p.VoicesPath = "/fake/voices.bin"
	outputPath := filepath.Join(t.TempDir(), "cancelled.wav")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.Synthesize(ctx, "test", outputPath, "")
	assert.Error(t, err)
}

// --- Synthesize MkdirAll error path ---

func TestKokoroSynthesize_MkdirAllError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("MkdirAll error path not reliably testable on Windows")
	}
	p := NewKokoroProvider()
	p.ModelPath = "/proc/self/exe" // exists on Linux, so we get past model check
	p.VoicesPath = "/fake/voices.bin"
	// Impossible output path triggers MkdirAll error
	outputPath := "/proc/nonexistent-dir-for-test/output.wav"

	err := p.Synthesize(context.Background(), "test", outputPath, "")
	assert.Error(t, err)
	// On Linux, the error could be either MkdirAll or model check depending on order
}

// --- Synthesize voices not configured ---

func TestKokoroSynthesize_NoVoicesConfigured(t *testing.T) {
	p := NewKokoroProvider()
	// Use a real file for ModelPath so we get past the model check
	p.ModelPath = os.Args[0] // the test binary itself exists
	outputPath := filepath.Join(t.TempDir(), "output.wav")

	err := p.Synthesize(context.Background(), "test", outputPath, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "voices path not configured")
}

// --- Synthesize exercises slog.Info path (L110-111) ---

func TestKokoroSynthesize_SlogInfoPath(t *testing.T) {
	p := NewKokoroProvider()
	// Use real files so we get past model/voices checks
	p.ModelPath = os.Args[0]
	p.VoicesPath = os.Args[0]
	outputPath := filepath.Join(t.TempDir(), "output.wav")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This will fail because python3/bridge script won't be found,
	// but it exercises the slog.Info and MkdirAll success paths
	err := p.Synthesize(ctx, "test", outputPath, "")
	assert.Error(t, err)
}
