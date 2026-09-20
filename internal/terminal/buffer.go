package terminal

import "sync"

// ringEntry holds a single line of terminal output as raw bytes.
type ringEntry struct {
	data []byte
}

// RingBuffer is a thread-safe circular buffer for terminal output.
// It stores raw byte slices (preserving ANSI escape sequences) split by newlines,
// with configurable line count, per-line size limit, and total memory cap.
type RingBuffer struct {
	mu            sync.Mutex
	lines         []ringEntry
	capacity      int // max line count
	maxLineBytes  int // per-line byte cap
	maxTotalBytes int // total buffer byte limit
	totalBytes    int // current total bytes across all lines
	head          int // index of oldest line
	count         int // number of lines in buffer
}

// NewRingBuffer creates a ring buffer with the given configuration.
func NewRingBuffer(capacity, maxLineBytes, maxTotalBytes int) *RingBuffer {
	return &RingBuffer{
		lines:         make([]ringEntry, capacity),
		capacity:      capacity,
		maxLineBytes:  maxLineBytes,
		maxTotalBytes: maxTotalBytes,
	}
}

// Write appends raw PTY output bytes to the buffer.
// Data is split on '\n'; the '\n' byte is kept with the preceding line
// to preserve ANSI escape sequences that span line boundaries.
func (rb *RingBuffer) Write(p []byte) {
	if len(p) == 0 {
		return
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	// Split on \n, keeping \n with the preceding line
	start := 0
	for i := range p {
		if p[i] == '\n' {
			line := p[start : i+1] // includes \n
			rb.addLine(line)
			start = i + 1
		}
	}
	// Remaining data without trailing \n
	if start < len(p) {
		rb.addLine(p[start:])
	}
}

// addLine adds a single line to the ring buffer, enforcing size limits.
func (rb *RingBuffer) addLine(line []byte) {
	// Copy before storing. The caller may reuse the slice it passes to Write —
	// readPTY reuses a single 4096-byte buffer for every Read — so retaining a
	// sub-slice would let the next read overwrite lines that are already
	// buffered, corrupting the replay (and splitting escape sequences mid-way).
	owned := make([]byte, len(line))
	copy(owned, line)
	line = owned

	// Enforce per-line size limit
	if len(line) > rb.maxLineBytes {
		// Truncate and add reset + indicator
		truncated := make([]byte, rb.maxLineBytes, rb.maxLineBytes+20)
		copy(truncated, line[:rb.maxLineBytes])
		truncated = append(truncated, "\x1b[0m\r\n...[truncated]\r\n"...)
		line = truncated
	}

	// Enforce total byte limit by evicting oldest lines
	needed := len(line)
	for rb.totalBytes+needed > rb.maxTotalBytes && rb.count > 0 {
		rb.evictOldest()
	}

	// Enforce line count limit
	if rb.count >= rb.capacity {
		rb.evictOldest()
	}

	// Calculate write position
	idx := (rb.head + rb.count) % rb.capacity
	if rb.count < rb.capacity {
		rb.count++
	} else {
		// Overwriting: shouldn't happen since we evicted above, but be safe
		rb.head = (rb.head + 1) % rb.capacity
	}

	rb.lines[idx] = ringEntry{data: line}
	rb.totalBytes += len(line)
}

// evictOldest removes the oldest line from the buffer.
func (rb *RingBuffer) evictOldest() {
	if rb.count == 0 {
		return
	}
	oldest := rb.lines[rb.head]
	rb.totalBytes -= len(oldest.data)
	// Clear reference to allow GC
	rb.lines[rb.head] = ringEntry{}
	rb.head = (rb.head + 1) % rb.capacity
	rb.count--
}

// Replay returns all buffered lines concatenated into a single byte slice.
// This preserves ANSI escape sequences that span line boundaries.
func (rb *RingBuffer) Replay() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == 0 {
		return nil
	}

	// Pre-calculate total size for efficient allocation
	totalSize := 0
	for i := range rb.count {
		idx := (rb.head + i) % rb.capacity
		totalSize += len(rb.lines[idx].data)
	}

	result := make([]byte, 0, totalSize)
	for i := range rb.count {
		idx := (rb.head + i) % rb.capacity
		result = append(result, rb.lines[idx].data...)
	}
	return result
}

// Reset clears all buffered data.
func (rb *RingBuffer) Reset() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// Clear references for GC
	for i := range rb.lines {
		rb.lines[i] = ringEntry{}
	}
	rb.head = 0
	rb.count = 0
	rb.totalBytes = 0
}
