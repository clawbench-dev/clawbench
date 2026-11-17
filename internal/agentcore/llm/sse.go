package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
)

// maxSSELineBytes bounds one server-sent-event line.
//
// A single line carrying a long tool-call argument easily exceeds the 64 KiB
// scanner default; 4 MiB covers a large file write without unbounded growth.
const maxSSELineBytes = 4 * 1024 * 1024

// newSSEScanner builds the line scanner both streaming clients share.
func newSSEScanner(body io.Reader) *bufio.Scanner {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSELineBytes)
	return scanner
}

// nextSSEPayload advances the scanner to the next `data:` payload and returns
// false when the body ends.
//
// The eventsource framing shared by both protocols is handled here: blank lines
// and comment lines (": keepalive") carry nothing, a `data:` prefix is stripped,
// and any other line is skipped. Decoding stays with the caller, because the two
// protocols deliberately disagree on what to do with a frame that does not
// parse.
func nextSSEPayload(scanner *bufio.Scanner) (string, bool) {
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "data:")), true
	}
	return "", false
}

// The two SSE decoders. They live here, not next to their protocol structs,
// because a frame that does not parse is a skip for BOTH: gateways inject
// keepalives and vendor-specific frames, and failing the turn over one of them
// would lose an answer that is otherwise complete.
func decodeOAStreamChunk(payload string) (oaStreamChunk, bool) {
	var chunk oaStreamChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return oaStreamChunk{}, false
	}
	return chunk, true
}

func decodeAnStreamEvent(payload string) (anStreamEvent, bool) {
	var ev anStreamEvent
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return anStreamEvent{}, false
	}
	return ev, true
}

// emitEach emits every event in order and reports whether all were delivered.
// A false result means the consumer or the context is gone, so the caller must
// stop reading the body.
func emitEach(ctx context.Context, ch chan<- Event, events []Event) bool {
	for _, ev := range events {
		if !emit(ctx, ch, ev) {
			return false
		}
	}
	return true
}
