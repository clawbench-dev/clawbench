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

func TestGenericACPRemaps(t *testing.T) {
	t.Run("carries the shared camelCase set", func(t *testing.T) {
		assert.Contains(t, GenericACPRemaps, "oldString")
		assert.Contains(t, GenericACPRemaps, "dirPath")
		assert.Contains(t, GenericACPRemaps, "replaceAll")
	})

	t.Run("does not repeat defaultMappings it does not need", func(t *testing.T) {
		// normalizeToolInput applies cmd/exec to every backend, so a copy here
		// would be a second place to keep in sync.
		assert.NotContains(t, GenericACPRemaps, "cmd")
		assert.NotContains(t, GenericACPRemaps, "exec")
	})

	t.Run("is the fallback when the backend registry is not wired", func(t *testing.T) {
		// No backends package imported in this binary, so LookupACPRemapsFn is
		// nil and the shared table must be returned.
		assert.Equal(t, GenericACPRemaps, acpRemapsForBackend("any-backend"))
	})
}
