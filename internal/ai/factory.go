package ai

import (
	"fmt"
)

// NewBackend creates a backend instance based on the backend type
func NewBackend(backendType string) (AIBackend, error) {
	switch backendType {
	case "claude":
		return &AutoResumeBackend{inner: claudeBackend}, nil
	case "codebuddy":
		return &AutoResumeBackend{inner: codebuddyBackend}, nil
	case "opencode":
		return opencodeBackend, nil
	case "gemini":
		return geminiBackend, nil
	case "codex":
		return &CodexBackend{}, nil
	case "qoder":
		return &AutoResumeBackend{inner: qoderBackend}, nil
	case "vecli":
		return NewVeCLIBackend(), nil
	case "deepseek":
		return &AutoResumeBackend{inner: deepseekBackend}, nil
	case "pi":
		return &AutoResumeBackend{inner: piBackend}, nil
	case "mock":
		return NewMockAIBackend(), nil
	default:
		return nil, fmt.Errorf("unsupported backend type: %s (supported: claude, codebuddy, opencode, gemini, codex, qoder, vecli, deepseek, pi, mock)", backendType)
	}
}
