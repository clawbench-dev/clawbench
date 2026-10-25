package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- canDiscoverModels internal tests ---

func TestCanDiscoverModels(t *testing.T) {
	// Register a test source to exercise the positive case
	RegisterModelSource(StaticSource("test-can-discover", "", []AgentModel{{ID: "m"}}))

	tests := []struct {
		name     string
		spec     BackendSpec
		expected bool
	}{
		{
			name:     "with registered model source",
			spec:     BackendSpec{Backend: "test-can-discover"},
			expected: true,
		},
		{
			name:     "with nothing registered",
			spec:     BackendSpec{Backend: "nonexistent_backend_xyz"},
			expected: false,
		},
		{
			name:     "empty spec",
			spec:     BackendSpec{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, CanDiscoverModels(tt.spec))
		})
	}
}

// --- BuildCommonPrompt edge cases ---

func TestBuildCommonPrompt_ReturnsContent(t *testing.T) {
	// BuildCommonPrompt always returns the embedded rules content
	result := BuildCommonPrompt()
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "User Interaction")
	assert.Contains(t, result, "Media Generation")
	// Multi-Agent removed from common prompt
	assert.NotContains(t, result, "Multi-Agent")
	// Media reading rules are separate — must NOT appear in common prompt
	assert.NotContains(t, result, "Media File Handling")
}

// The ask-question rules must tell the model where the description separator
// goes relative to a bold option title. Without it the model bolds the whole
// "label — gloss" phrase and then adds " — explanation", and the parser used to
// split on the inner dash, truncating the label to "**A" (message 52484).
func TestBuildCommonPrompt_AskOptionSeparatorRule(t *testing.T) {
	result := BuildCommonPrompt()
	assert.Contains(t, result, "never inside it, so the whole bold phrase stays the title")
	assert.Contains(t, result, "inside the bold run is part of the title")
	// The «» placeholders are an internal encoding; leaking one into the
	// rendered prompt would reach the model verbatim.
	assert.NotContains(t, result, "«")
	assert.NotContains(t, result, "»")
}

func TestBuildMediaPrompt_ReturnsContent(t *testing.T) {
	result := BuildMediaPrompt()
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "Media File Handling")
	assert.Contains(t, result, "Upload path")
	assert.Contains(t, result, "Reading:")
	// Generation rules are in common prompt, not media prompt
	assert.NotContains(t, result, "Generation:")
}

func TestBuildCommonPrompt_MediaRulesSeparated(t *testing.T) {
	common := BuildCommonPrompt()
	media := BuildMediaPrompt()
	// Common and media prompts are distinct, non-overlapping
	assert.NotContains(t, common, "Media File Handling")
	assert.Contains(t, media, "Media File Handling")
	// Concatenation should produce the full original rules
	full := common + "\n\n" + media
	assert.Contains(t, full, "User Interaction")
	assert.Contains(t, full, "Media Generation")
	assert.Contains(t, full, "Media File Handling")
}

// The media-embed instructions must not be reproducible as a literal template.
//
// Message 65399 (2026-10-25) shipped
// `![设备编排方案](/…/_hero_layouts.html)` — an image embed aimed at a rendered
// HTML page. It is well-formed markdown, so nothing rejected it: the browser
// could not decode it and the user saw a "failed to load" placeholder. The same
// class of mistake produced 34 messages where the model copied the prompt's
// backtick-wrapped placeholder verbatim (`![x](`.clawbench/a.png`)`) and 22 where
// it copied the literal word "desc".
//
// The fix is to give REAL example values and say what the form is for, so the
// model has nothing to copy and no reason to aim an image at a page. This test
// pins those properties — a prompt that goes back to `![desc](`relative_path`)`
// will fail it.
func TestBuildCommonPrompt_MediaEmbedRules(t *testing.T) {
	prompt := BuildCommonPrompt()

	// Placeholders must not be backtick-wrapped: that is what the model copied.
	assert.NotContains(t, prompt, "![desc](`", "placeholder must not be wrapped in backticks")
	assert.NotContains(t, prompt, "`relative_path`", "placeholder must not be wrapped in backticks")

	// Real example values, so there is no placeholder to copy at all.
	assert.Contains(t, prompt, "![short description](.clawbench/generated/img_chart.png)")
	assert.Contains(t, prompt, "[short description](.clawbench/generated/audio_intro.mp3)")
	assert.Contains(t, prompt, "[short description](.clawbench/generated/video_demo.mp4)")

	// The form's scope: image syntax is for real image files only.
	assert.Contains(t, prompt, "renders ONLY for real image files")
	assert.Contains(t, prompt, ".html page")

	// An absolute path works (the render layer serves it via ?target=), so the
	// prompt must not forbid what the implementation supports.
	assert.NotContains(t, prompt, "No absolute paths")
}
