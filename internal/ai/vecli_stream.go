package ai

import (
	"encoding/json"
	"fmt"
)

// VeCLIStreamParser treats each stdout line as a content event.
// VeCLI outputs plain text (not JSON Lines) because it has removed
// the --output-format flag that Kimi CLI supports.
type VeCLIStreamParser struct{}

// ParseLine emits a content event for each line of VeCLI output.
// Every line is treated as plain text content.
func (p *VeCLIStreamParser) ParseLine(line string, ch chan<- StreamEvent) {
	ch <- StreamEvent{Type: "content", Content: line + "\n"}
}

// GetCapturedSessionID returns empty string — VeCLI has no session resume support.
func (p *VeCLIStreamParser) GetCapturedSessionID() string { return "" }

// buildVeCLIArgs constructs the CLI arguments for VeCLI non-interactive mode.
// The --session-summary flag is NOT added here; it's appended by vecliPreStart
// since the file path varies per request.
func buildVeCLIArgs(req ChatRequest) []string {
	// VeCLI has no --system-prompt flag — inject into user prompt.
	prompt := InjectSystemPrompt(req)

	args := []string{
		"--yolo",
		"--prompt", prompt,
	}

	// Working directory — VeCLI inherits --include-directories from Kimi CLI
	if req.WorkDir != "" {
		args = append(args, "--include-directories", req.WorkDir)
	}

	// Model override
	if req.Model != "" {
		args = append(args, "--model", req.Model)
	}

	// NO --output-format (removed in VeCLI)
	// NO --resume (not supported by VeCLI)
	// --session-summary is added by vecliPreStart, not here

	return args
}

// VeCLISessionSummary represents the JSON structure written by --session-summary.
// VeCLI writes this file on process exit via uiTelemetryService.getMetrics().
// The format is nested with models map keyed by model name.
type VeCLISessionSummary struct {
	SessionMetrics struct {
		Models map[string]struct {
			API struct {
				TotalRequests  int `json:"totalRequests"`
				TotalErrors    int `json:"totalErrors"`
				TotalLatencyMs int `json:"totalLatencyMs"`
			} `json:"api"`
			Tokens struct {
				Prompt     int `json:"prompt"`
				Candidates int `json:"candidates"`
				Total      int `json:"total"`
				Cached     int `json:"cached"`
				Thoughts   int `json:"thoughts"`
				Tool       int `json:"tool"`
			} `json:"tokens"`
		} `json:"models"`
		Tools struct {
			TotalCalls      int `json:"totalCalls"`
			TotalSuccess    int `json:"totalSuccess"`
			TotalFail       int `json:"totalFail"`
			TotalDurationMs int `json:"totalDurationMs"`
		} `json:"tools"`
		Files struct {
			TotalLinesAdded   int `json:"totalLinesAdded"`
			TotalLinesRemoved int `json:"totalLinesRemoved"`
		} `json:"files"`
	} `json:"sessionMetrics"`
}

// extractMetadata parses the session-summary and returns a Metadata event.
// It extracts token counts and duration from the model entry matching reqModel.
// If no matching model is found, it falls back to the first entry.
// If no model entries exist, it falls back to the request model name.
func (s *VeCLISessionSummary) extractMetadata(reqModel string) *Metadata {
	meta := &Metadata{
		StopReason: "stop",
	}
	// Prefer the model matching reqModel, then fall back to first entry
	for name, m := range s.SessionMetrics.Models {
		if name != reqModel && meta.Model != "" {
			continue
		}
		meta.Model = name
		meta.InputTokens = m.Tokens.Prompt
		meta.OutputTokens = m.Tokens.Candidates
		meta.DurationMs = m.API.TotalLatencyMs
		if name == reqModel {
			break
		}
	}
	if meta.Model == "" && reqModel != "" {
		meta.Model = reqModel
	}
	return meta
}

// parseVeCLISessionSummary is a helper to parse a session-summary JSON file.
func parseVeCLISessionSummary(data []byte) (*VeCLISessionSummary, error) {
	var summary VeCLISessionSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		return nil, fmt.Errorf("vecli: failed to parse session-summary: %w", err)
	}
	return &summary, nil
}
