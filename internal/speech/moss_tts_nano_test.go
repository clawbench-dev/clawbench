package speech

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- NewMossNanoProvider defaults ---

func TestNewMossNanoProvider_Defaults(t *testing.T) {
	p := NewMossNanoProvider()
	assert.Equal(t, "", p.ModelDir)
	assert.Equal(t, "onnx", p.Backend)
	assert.Equal(t, "Junhao", p.Voice)
}

// --- ResolveMossNanoModelDir ---

func TestResolveMossNanoModelDir_DefaultNoModels(t *testing.T) {
	dir := ResolveMossNanoModelDir("")
	assert.Equal(t, "", dir, "should return empty when default dir has no models, letting CLI auto-download")
}

func TestResolveMossNanoModelDir_DefaultWithModels(t *testing.T) {
	// Create a temp directory mimicking the default model structure under models/
	tmpDir := t.TempDir()
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir = origBinDir; model.DataDir = origDataDir }()

	// Create models/moss-nano-models/MOSS-TTS-Nano-100M-ONNX/browser_poc_manifest.json
	modelDir := filepath.Join(tmpDir, "models", "moss-nano-models", "MOSS-TTS-Nano-100M-ONNX")
	require.NoError(t, os.MkdirAll(modelDir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(modelDir, "browser_poc_manifest.json"), []byte("{}"), 0o600))

	dir := ResolveMossNanoModelDir("")
	assert.Equal(t, filepath.Join(tmpDir, "models", "moss-nano-models"), dir, "should return new models/ dir when models exist")
}

func TestResolveMossNanoModelDir_Explicit(t *testing.T) {
	dir := ResolveMossNanoModelDir("/custom/models")
	assert.Equal(t, "/custom/models", dir)
}

// --- Synthesize context cancellation ---

func TestMossNanoSynthesize_CancelledContext(t *testing.T) {
	p := NewMossNanoProvider()
	outputPath := filepath.Join(t.TempDir(), "cancelled.wav")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.Synthesize(ctx, "test", outputPath, "")
	assert.Error(t, err)
}

// --- Synthesize with CLI unavailable ---

func TestMossNanoSynthesize_CLIUnavailable(t *testing.T) {
	p := NewMossNanoProvider()
	outputPath := filepath.Join(t.TempDir(), "deep", "nested", "output.wav")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := p.Synthesize(ctx, "测试文本", outputPath, "")
	assert.Error(t, err)
}

// --- Synthesize integration test (requires moss-tts-nano CLI) ---

func TestMossNanoSynthesize_WithCLI(t *testing.T) {
	if _, err := exec.LookPath("moss-tts-nano"); err != nil {
		t.Skip("moss-tts-nano CLI not available, skipping integration test")
	}

	p := NewMossNanoProvider()
	outputPath := filepath.Join(t.TempDir(), "moss_nano_output.wav")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := p.Synthesize(ctx, "你好，这是一个测试。", outputPath, "")
	assert.NoError(t, err)

	// Verify the output file was created and has content
	fi, err := os.Stat(outputPath)
	assert.NoError(t, err)
	assert.Greater(t, fi.Size(), int64(0))
}
