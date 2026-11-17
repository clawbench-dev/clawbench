package handler

import (
	"testing"

	"clawbench/internal/model"

	"github.com/stretchr/testify/require"
)

func TestKBConfigPatch(t *testing.T) {
	orig := model.ConfigInstance
	t.Cleanup(func() { model.ConfigInstance = orig })

	patch := map[string]any{
		"kb": map[string]any{
			"enabled":               false,
			"auto_inject":           false,
			"inject_top_n":          float64(6),
			"inject_max_chars":      float64(1500),
			"inject_min_similarity": 0.5,
			"max_file_mb":           float64(100),
			"max_text_mb":           float64(30),
			"poll_interval":         "3s",
			"embed_batch_size":      float64(8),
		},
	}

	fields, err := validatePatchFields(patch, "")
	require.NoError(t, err)
	require.Contains(t, fields, "kb.inject_top_n")
	require.Contains(t, fields, "kb.embed_batch_size")

	require.NoError(t, validatePatchValues(patch))
	applyConfigPatch(patch)

	kb := model.ConfigInstance.KB
	require.False(t, kb.Enabled)
	require.False(t, kb.AutoInject)
	require.Equal(t, 6, kb.InjectTopN)
	require.Equal(t, 1500, kb.InjectMaxChars)
	require.Equal(t, 0.5, kb.InjectMinSimilarity)
	require.Equal(t, 100, kb.MaxFileMB)
	require.Equal(t, 30, kb.MaxTextMB)
	require.Equal(t, "3s", kb.PollInterval)
	require.Equal(t, 8, kb.EmbedBatchSize)
}

func TestKBConfigValidationRejectsOutOfRange(t *testing.T) {
	cases := []map[string]any{
		{"kb": map[string]any{"inject_top_n": float64(0)}},
		{"kb": map[string]any{"inject_top_n": float64(21)}},
		{"kb": map[string]any{"inject_max_chars": float64(100)}},
		{"kb": map[string]any{"inject_min_similarity": 1.5}},
		{"kb": map[string]any{"max_file_mb": float64(0)}},
		{"kb": map[string]any{"max_text_mb": float64(1000)}},
		{"kb": map[string]any{"embed_batch_size": float64(0)}},
	}
	for _, patch := range cases {
		require.Error(t, validatePatchValues(patch), "patch should be rejected: %v", patch)
	}
}
