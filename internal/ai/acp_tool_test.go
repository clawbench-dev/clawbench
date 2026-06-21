package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractMetaToolName(t *testing.T) {
	t.Run("nested namespace with toolName", func(t *testing.T) {
		meta := map[string]any{
			"claudeCode": map[string]any{"toolName": "Edit"},
		}
		assert.Equal(t, "Edit", extractMetaToolName(meta))
	})

	t.Run("flat key not found in nested", func(t *testing.T) {
		meta := map[string]any{
			"claudeCode": map[string]any{"other": "value"},
		}
		assert.Equal(t, "", extractMetaToolName(meta))
	})

	t.Run("namespace not present", func(t *testing.T) {
		meta := map[string]any{"other": "value"}
		assert.Equal(t, "", extractMetaToolName(meta))
	})

	t.Run("nil meta", func(t *testing.T) {
		assert.Equal(t, "", extractMetaToolName(nil))
	})

	t.Run("namespace is not a map", func(t *testing.T) {
		meta := map[string]any{"claudeCode": "string-value"}
		assert.Equal(t, "", extractMetaToolName(meta))
	})
}

func TestExtractMetaToolNameFlat(t *testing.T) {
	t.Run("flat key present", func(t *testing.T) {
		meta := map[string]any{
			"codebuddy.ai/toolName": "Bash",
		}
		assert.Equal(t, "Bash", extractMetaToolNameFlat(meta))
	})

	t.Run("key not present", func(t *testing.T) {
		meta := map[string]any{"other": "value"}
		assert.Equal(t, "", extractMetaToolNameFlat(meta))
	})

	t.Run("nil meta", func(t *testing.T) {
		assert.Equal(t, "", extractMetaToolNameFlat(nil))
	})

	t.Run("value is not string", func(t *testing.T) {
		meta := map[string]any{"codebuddy.ai/toolName": 42}
		assert.Equal(t, "", extractMetaToolNameFlat(meta))
	})
}

func TestGetRemaps(t *testing.T) {
	t.Run("generic_acp has full remap table", func(t *testing.T) {
		remaps := getRemaps("generic_acp")
		assert.NotNil(t, remaps)
		assert.Contains(t, remaps, "oldString")
		assert.Contains(t, remaps, "dirPath")
	})

	t.Run("claude_acp is empty (defaultMappings sufficient)", func(t *testing.T) {
		remaps := getRemaps("claude_acp")
		assert.NotNil(t, remaps)
		assert.Empty(t, remaps)
	})

	t.Run("unknown key returns nil", func(t *testing.T) {
		remaps := getRemaps("nonexistent")
		assert.Nil(t, remaps)
	})

	t.Run("deepseek_cli has path remap", func(t *testing.T) {
		remaps := getRemaps("deepseek_cli")
		assert.NotNil(t, remaps)
		assert.Equal(t, "file_path", remaps["path"])
		assert.Equal(t, "old_string", remaps["search"])
	})
}
