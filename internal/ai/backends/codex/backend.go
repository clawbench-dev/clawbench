package codex

import ()

// CodexBackend implements AIBackend for OpenAI Codex CLI
type CodexBackend struct{}

// Name returns the backend identifier
func (c *CodexBackend) Name() string {
	return "codex"
}
