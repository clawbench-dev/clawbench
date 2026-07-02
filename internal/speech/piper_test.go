package speech

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawbench/internal/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- NewPiperProvider defaults ---

func TestNewPiperProvider_Defaults(t *testing.T) {
	p := NewPiperProvider()
	assert.Equal(t, "", p.ModelPath) // Must be configured explicitly
	assert.Equal(t, 0.667, p.NoiseScale)
	assert.Equal(t, 1.0, p.LengthScale)
	assert.Equal(t, 0.2, p.SentenceSilence)
}

// --- ResolveModelPath ---

func TestResolveModelPath_ExplicitPath(t *testing.T) {
	result := ResolveModelPath("some-voice", "/custom/path/model.onnx")
	assert.Equal(t, "/custom/path/model.onnx", result)
}

func TestResolveModelPath_VoiceName(t *testing.T) {
	// Without BinDir set, falls back to relative path
	result := ResolveModelPath("zh_CN-huayan-medium", "")
	assert.Equal(t, filepath.Join("models", "piper-models", "zh_CN-huayan-medium.onnx"), result)
}

func TestResolveModelPath_NewLocation(t *testing.T) {
	tmpDir := t.TempDir()
	origBinDir := model.BinDir
	origDataDir := model.DataDir
	model.BinDir = tmpDir
	model.DataDir = filepath.Join(tmpDir, ".clawbench")
	defer func() { model.BinDir = origBinDir; model.DataDir = origDataDir }()

	// Create models/piper-models/zh_CN-huayan-medium.onnx
	modelDir := filepath.Join(tmpDir, "models", "piper-models")
	require.NoError(t, os.MkdirAll(modelDir, 0o755))
	modelFile := filepath.Join(modelDir, "zh_CN-huayan-medium.onnx")
	require.NoError(t, os.WriteFile(modelFile, []byte("fake"), 0o644))

	result := ResolveModelPath("zh_CN-huayan-medium", "")
	assert.Equal(t, modelFile, result)
}

func TestResolveModelPath_Empty(t *testing.T) {
	result := ResolveModelPath("", "")
	assert.Equal(t, "", result)
}

// --- Synthesize integration test (requires piper binary) ---

func TestPiperSynthesize_WithCLI(t *testing.T) {
	piperPath, err := exec.LookPath("piper")
	if err != nil {
		// Try .venv/bin/piper relative to binary
		if exePath, exeErr := os.Executable(); exeErr == nil {
			candidatePath := filepath.Join(filepath.Dir(exePath), ".venv/bin/piper")
			if _, statErr := os.Stat(candidatePath); statErr == nil {
				piperPath = candidatePath
			}
		}
	}
	if piperPath == "" {
		t.Skip("piper binary not available, skipping integration test")
	}

	// Find a model file
	modelPath := findPiperModel(t)
	if modelPath == "" {
		t.Skip("no Piper model file found, skipping integration test")
	}

	p := NewPiperProvider()
	p.ModelPath = modelPath
	outputPath := filepath.Join(t.TempDir(), "test_output.wav")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = p.Synthesize(ctx, "这是一个测试语音。", outputPath, "")
	assert.NoError(t, err)

	info, statErr := os.Stat(outputPath)
	assert.NoError(t, statErr)
	assert.Greater(t, info.Size(), int64(0))
}

// --- Synthesize creates output directory ---

func TestPiperSynthesize_CreatesDirectory(t *testing.T) {
	if _, err := exec.LookPath("piper"); err != nil {
		t.Skip("piper binary not available, skipping integration test")
	}

	modelPath := findPiperModel(t)
	if modelPath == "" {
		t.Skip("no Piper model file found, skipping integration test")
	}

	p := NewPiperProvider()
	p.ModelPath = modelPath
	nestedDir := filepath.Join(t.TempDir(), "deep", "nested", "dir")
	outputPath := filepath.Join(nestedDir, "output.wav")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := p.Synthesize(ctx, "测试目录创建。", outputPath, "")
	assert.NoError(t, err)

	_, err = os.Stat(nestedDir)
	assert.NoError(t, err)
}

// --- Synthesize missing model file ---

func TestPiperSynthesize_MissingModel(t *testing.T) {
	p := NewPiperProvider()
	p.ModelPath = "/nonexistent/model.onnx"
	outputPath := filepath.Join(t.TempDir(), "output.wav")

	err := p.Synthesize(context.Background(), "test", outputPath, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "model file not found")
}

// --- Synthesize no model configured ---

func TestPiperSynthesize_NoModelConfigured(t *testing.T) {
	p := NewPiperProvider()
	// ModelPath is empty by default
	outputPath := filepath.Join(t.TempDir(), "output.wav")

	err := p.Synthesize(context.Background(), "test", outputPath, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "model path not configured")
}

// --- Synthesize context cancellation ---

func TestPiperSynthesize_CancelledContext(t *testing.T) {
	p := NewPiperProvider()
	p.ModelPath = "/fake/model.onnx"
	outputPath := filepath.Join(t.TempDir(), "cancelled.wav")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.Synthesize(ctx, "test", outputPath, "")
	assert.Error(t, err)
}

// findPiperModel searches for a .onnx model file in common locations.
func findPiperModel(t *testing.T) string {
	t.Helper()

	searchDirs := []string{
		filepath.Join("models", "piper-models"),
	}

	// Also check relative to binary
	if exePath, err := os.Executable(); err == nil {
		binDir := filepath.Dir(exePath)
		searchDirs = append(
			searchDirs,
			filepath.Join(binDir, "models", "piper-models"),
		)
	}

	// Also check project root (go test runs from package dir)
	if cwd, err := os.Getwd(); err == nil {
		projectRoot := filepath.Join(cwd, "..", "..")
		searchDirs = append(
			searchDirs,
			filepath.Join(projectRoot, "models", "piper-models"),
		)
	}

	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".onnx") {
				return filepath.Join(dir, entry.Name())
			}
		}
	}

	return ""
}
