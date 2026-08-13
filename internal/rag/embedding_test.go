package rag

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeEmbeddingBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "plain root", in: "http://localhost:11434", want: "http://localhost:11434"},
		{name: "trailing slash", in: "http://localhost:11434/", want: "http://localhost:11434"},
		{name: "with /v1", in: "http://localhost:11434/v1", want: "http://localhost:11434"},
		{name: "with /v1 slash", in: "http://localhost:11434/v1/", want: "http://localhost:11434"},
		{name: "with /v1/embeddings", in: "http://localhost:11434/v1/embeddings", want: "http://localhost:11434"},
		{name: "https vllm", in: "https://api.openai.com/v1", want: "https://api.openai.com"},
		{name: "missing scheme", in: "localhost:11434", want: "http://localhost:11434"},
		{name: "missing scheme with path", in: "192.168.1.5:8000/v1", want: "http://192.168.1.5:8000"},
		{name: "whitespace trimmed", in: "  http://host:11434  ", want: "http://host:11434"},
		{name: "non-v1 custom path preserved", in: "http://host:8000/custom", want: "http://host:8000/custom"},
		{name: "ftp scheme", in: "ftp://host:21", wantErr: true},
		{name: "file scheme", in: "file:///etc/hosts", wantErr: true},
		{name: "empty", in: "", wantErr: true},
		{name: "whitespace only", in: "   ", wantErr: true},
		{name: "no host", in: "http://", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeEmbeddingBaseURL(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeEmbeddingBaseURL_AppendsToEmbeddings(t *testing.T) {
	// Verify the normalized root produces a correct single /v1/embeddings URL
	// regardless of whether the user supplied the /v1 or /v1/embeddings suffix.
	for _, in := range []string{
		"http://localhost:11434",
		"http://localhost:11434/v1",
		"http://localhost:11434/v1/embeddings",
		"http://localhost:11434/v1/",
	} {
		norm, err := NormalizeEmbeddingBaseURL(in)
		require.NoError(t, err)
		assert.Equal(t, "http://localhost:11434/v1/embeddings", norm+"/v1/embeddings")
	}
}
