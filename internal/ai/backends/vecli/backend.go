package vecli

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"clawbench/internal/ai"
	"clawbench/internal/model"

	"github.com/google/uuid"
)

// VeCLIBackend wraps a ai.CLIBackend to add post-stream session-summary parsing.
// VeCLI outputs plain text (not JSON Lines), so VeCLIStreamParser treats each
// stdout line as a content event. After the process exits, the wrapper reads
// the --session-summary JSON file to emit metadata (token counts, duration).
type VeCLIBackend struct {
	inner      *ai.CLIBackend
	summaryMap sync.Map // key: SessionID → value: summaryFilePath string
}

// NewVeCLIBackend creates a new VeCLIBackend instance.
func newVeCLIBackendImpl() *VeCLIBackend {
	b := &VeCLIBackend{}
	b.inner = &ai.CLIBackend{
		BackendName:  "vecli",
		Cmd:          "vecli",
		BuildArgsFn:  buildVeCLIArgs,
		NewParserFn:  func() ai.LineParser { return &VeCLIStreamParser{} },
		FilterLineFn: nil, // default: skip empty lines only (VeCLI outputs plain text)
		PreStartFn:   b.vecliPreStart,
	}
	return b
}

// Name returns the backend identifier.
func (b *VeCLIBackend) Name() string { return "vecli" }

// vecliPreStart is the preStart hook for ai.CLIBackend. It appends the
// --session-summary flag with a per-request temp file path to cmd.Args,
// and stores the path in summaryMap for post-stream retrieval.
func (b *VeCLIBackend) vecliPreStart(cmd *exec.Cmd, req ai.ChatRequest) {
	summaryDir := filepath.Join(model.DataDir, "vecli-summary")
	if err := os.MkdirAll(summaryDir, 0o700); err != nil {
		slog.Warn("vecli: failed to create summary dir", "dir", summaryDir, "error", err)
	}
	// req.SessionID is guaranteed non-empty by ExecuteStream
	summaryFile := filepath.Join(summaryDir, req.SessionID+".json")
	b.summaryMap.Store(req.SessionID, summaryFile)
	cmd.Args = append(cmd.Args, "--session-summary", summaryFile)
}

// ExecuteStream runs the VeCLI backend in streaming mode and returns a channel of events.
// It wraps ai.CLIBackend.ExecuteStream to add post-stream session-summary parsing.
//
// Flow:
//  1. Inner ai.CLIBackend spawns vecli process, reads stdout line-by-line
//  2. VeCLIStreamParser emits content events for each line
//  3. On process exit, this wrapper reads the session-summary JSON file
//  4. Emits metadata event (token counts, duration, model) from summary
//  5. Emits done event
func (b *VeCLIBackend) ExecuteStream(ctx context.Context, req ai.ChatRequest) (<-chan ai.StreamEvent, error) {
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

	outCh := make(chan ai.StreamEvent, ai.StreamChanSize)

	go func() {
		defer close(outCh)

		// Forward all events from inner channel
		for ev := range innerCh {
			// Skip "done" from inner — VeCLIStreamParser never emits it,
			// but skip defensively in case ai.CLIBackend changes behavior
			if ev.Type == "done" {
				continue
			}
			// Non-blocking send to prevent goroutine leak if consumer stopped;
			// a full channel drops the event with a WARN (emitStreamEvent).
			ai.EmitStreamEvent(outCh, "vecli", ev)
		}

		// If context was cancelled (user cancel or disconnect),
		// skip session-summary reading — the process was killed.
		if ctx.Err() != nil {
			ai.EmitStreamEvent(outCh, "vecli", ai.StreamEvent{Type: "done"})
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
			// The map is written by vecliPreStart and read here; a value that is
			// not a string cannot have come from this code, so it is treated as
			// "no summary" instead of being handed to os.ReadFile/os.Remove.
			// Previously the non-string branch fell through with an empty path,
			// which logged a second, misleading debug line ("file not found") and
			// called os.Remove("").
			path, typeOk := summaryPath.(string)
			if !typeOk {
				slog.Debug("vecli: summaryPath is not a string", "summaryKey", summaryKey)
				ai.EmitStreamEvent(outCh, "vecli", ai.StreamEvent{Type: "done"})
				return
			}
			defer func() { _ = os.Remove(path) }()

			if data, readErr := os.ReadFile(path); readErr == nil {
				summary, parseErr := parseSessionSummary(data)
				if parseErr != nil {
					slog.Debug("vecli: failed to parse session-summary", "error", parseErr)
				} else {
					meta := summary.extractMetadata(req.Model)
					ai.EmitStreamEvent(outCh, "vecli", ai.StreamEvent{Type: "metadata", Meta: meta})
				}
			} else {
				slog.Debug("vecli: session-summary file not found", "path", path, "error", readErr)
			}
		}

		// Always emit done at the end
		ai.EmitStreamEvent(outCh, "vecli", ai.StreamEvent{Type: "done"})
	}()

	return outCh, nil
}

// parseVeCLISessionSummary is defined in vecli_stream.go
