package ai

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractContentText_Nil(t *testing.T) {
	assert.Equal(t, "", extractContentText(nil))
}

func TestExtractContentText_Empty(t *testing.T) {
	assert.Equal(t, "", extractContentText(json.RawMessage("")))
}

func TestExtractContentText_PlainString(t *testing.T) {
	result := extractContentText(json.RawMessage(`"hello world"`))
	assert.Equal(t, "hello world", result)
}

func TestExtractContentText_ArrayOfTextBlocks(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"line1"},{"type":"text","text":"line2"}]`)
	result := extractContentText(raw)
	assert.Equal(t, "line1\nline2", result)
}

func TestExtractContentText_ArrayWithNonTextBlocks(t *testing.T) {
	// Non-text blocks increment the index, so text at index 1 gets "\n" prefix
	raw := json.RawMessage(`[{"type":"image","url":"x"},{"type":"text","text":"only-text"}]`)
	result := extractContentText(raw)
	assert.Equal(t, "\nonly-text", result)
}

func TestExtractContentText_SingleTextBlock(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":"single"}]`)
	result := extractContentText(raw)
	assert.Equal(t, "single", result)
}

func TestExtractContentText_FallbackRawString(t *testing.T) {
	// Invalid JSON — neither string nor array — falls back to raw string
	raw := json.RawMessage(`{invalid}`)
	result := extractContentText(raw)
	assert.Equal(t, "{invalid}", result)
}

func TestExtractContentText_ArrayWithEmptyText(t *testing.T) {
	raw := json.RawMessage(`[{"type":"text","text":""}]`)
	result := extractContentText(raw)
	assert.Equal(t, "", result)
}

func TestStreamParser_GetCapturedSessionID(t *testing.T) {
	p := &testLineParser{}
	assert.Equal(t, "", p.GetCapturedSessionID())
}
