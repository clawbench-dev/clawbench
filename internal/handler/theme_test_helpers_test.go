package handler

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

// Shared helpers for the wallpaper/theme handler tests.

// setupThemeTestEnv configures model.DataDir to a fresh temp dir and gives
// ConfigInstance a clean Appearance section so handlers read empty defaults.
// The theme dir lives under the test env watch dir so absolute-path source
// files resolve against model.RootPaths (set to the watch dir).
func setupThemeTestEnv(t *testing.T) (string, func()) {
	themeDir, _, teardown := setupThemeTestEnvFull(t)
	return themeDir, teardown
}

// setupThemeTestEnvFull is setupThemeTestEnv plus the underlying test env, so
// tests can build project-relative source paths (robust across macOS and
// Windows where absolute temp paths go through symlinks/8.3 short names).
func setupThemeTestEnvFull(t *testing.T) (string, string, func()) {
	env, teardown := setupTestEnv(t)

	origConfig := model.ConfigInstance
	origDataDir := model.DataDir

	themeDir := filepath.Join(env.WatchDir, "data", "theme")
	model.DataDir = filepath.Dir(themeDir)
	model.ConfigInstance = model.Config{}
	require.NoError(t, os.MkdirAll(themeDir, 0o755))

	cleanup := func() {
		model.ConfigInstance = origConfig
		model.DataDir = origDataDir
		teardown()
	}
	return themeDir, env.ProjectDir, cleanup
}

// makePNG renders an RGBA image and PNG-encodes it.
func makePNG(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
