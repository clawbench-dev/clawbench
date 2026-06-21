package ai

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"clawbench/internal/model"

	"github.com/google/uuid"
)

// VeCLIBackend wraps a CLIBackend to add post-stream session-summary parsing.
// VeCLI outputs plain text (not JSON Lines), so VeCLIStreamParser treats each
// stdout line as a content event. After the process exits, the wrapper reads
// the --session-summary JSON file to emit metadata (token counts, duration).
type VeCLIBackend struct {
	inner      *CLIBackend
	summaryMap sync.Map // key: SessionID → value: summaryFilePath string
}

// NewVeCLIBackend creates a new VeCLIBackend instance.
func NewVeCLIBackend() *VeCLIBackend {
	b := &VeCLIBackend{}
	b.inner = &CLIBackend{
		BackendName:  "vecli",
		Cmd:          "vecli",
		BuildArgsFn:  buildVeCLIArgs,
		NewParserFn:  func() LineParser { return &VeCLIStreamParser{} },
		FilterLineFn: nil, // default: skip empty lines only (VeCLI outputs plain text)
		PreStartFn:   b.vecliPreStart,
	}
	return b
}

// Name returns the backend identifier.
func (b *VeCLIBackend) Name() string { return "vecli" }

// vecliPreStart is the preStart hook for CLIBackend. It appends the
// --session-summary flag with a per-request temp file path to cmd.Args,
// and stores the path in summaryMap for post-stream retrieval.
func (b *VeCLIBackend) vecliPreStart(cmd *exec.Cmd, req ChatRequest) {
	summaryDir := filepath.Join(model.BinDir, ".clawbench", "vecli-summary")
	if err := os.MkdirAll(summaryDir, 0o700); err != nil {
		slog.Warn("vecli: failed to create summary dir", "dir", summaryDir, "error", err)
	}
	// req.SessionID is guaranteed non-empty by ExecuteStream
	summaryFile := filepath.Join(summaryDir, req.SessionID+".json")
	b.summaryMap.Store(req.SessionID, summaryFile)
	cmd.Args = append(cmd.Args, "--session-summary", summaryFile)
}

// ExecuteStream runs the VeCLI backend in streaming mode and returns a channel of events.
// It wraps CLIBackend.ExecuteStream to add post-stream session-summary parsing.
//
// Flow:
//  1. Inner CLIBackend spawns vecli process, reads stdout line-by-line
//  2. VeCLIStreamParser emits content events for each line
//  3. On process exit, this wrapper reads the session-summary JSON file
//  4. Emits metadata event (token counts, duration, model) from summary
//  5. Emits done event
//
//nolint:gocognit,gocyclo // complex stream parsing logic
func (b *VeCLIBackend) ExecuteStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	// VeCLI does not support resume — always start a new session
	req.Resume = false

	// Ensure SessionID is non-empty for summaryMap key consistency with vecliPreStart
	if req.SessionID == "" {
		req.SessionID = "vecli-" + uuid.New().String()
	}

	summaryKey := req.SessionID // capture before goroutine

	innerCh, err := b.inner.ExecuteStream(ctx, req)
	if err != nil {
		b.summaryMap.LoadAndDelete(summaryKey) // clean up on error
		return nil, err
	}

	outCh := make(chan StreamEvent, streamChanSize)

	go func() {
		defer close(outCh)

		// Forward all events from inner channel
		for ev := range innerCh {
			// Skip "done" from inner — VeCLIStreamParser never emits it,
			// but skip defensively in case CLIBackend changes behavior
			if ev.Type == "done" {
				continue
			}
			// Use non-blocking send to prevent goroutine leak if consumer has stopped
			select {
			case outCh <- ev:
			default:
			}
		}

		// If context was cancelled (user cancel or disconnect),
		// skip session-summary reading — the process was killed.
		if ctx.Err() != nil {
			select {
			case outCh <- StreamEvent{Type: "done"}:
			default:
			}
			// Clean up summary file if it exists
			if sp, ok := b.summaryMap.LoadAndDelete(summaryKey); ok {
				if path, typeOk := sp.(string); typeOk {
					_ = os.Remove(path)
				}
			}
			return
		}

		// Process exited normally — try to read session-summary for metadata
		summaryPath, ok := b.summaryMap.LoadAndDelete(summaryKey)
		if ok {
			path, typeOk := summaryPath.(string)
			if !typeOk {
				slog.Debug("vecli: summaryPath is not a string", "summaryKey", summaryKey)
			}
			defer func() { _ = os.Remove(path) }()

			if data, readErr := os.ReadFile(path); readErr == nil {
				summary, parseErr := parseVeCLISessionSummary(data)
				if parseErr != nil {
					slog.Debug("vecli: failed to parse session-summary", "error", parseErr)
				} else {
					meta := summary.extractMetadata(req.Model)
					select {
					case outCh <- StreamEvent{Type: "metadata", Meta: meta}:
					default:
					}
				}
			} else {
				slog.Debug("vecli: session-summary file not found", "path", path, "error", readErr)
			}
		}

		// Always emit done at the end
		select {
		case outCh <- StreamEvent{Type: "done"}:
		default:
		}
	}()

	return outCh, nil
}

// parseVeCLISessionSummary is defined in vecli_stream.go
