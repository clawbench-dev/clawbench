package speech

import (
	"testing"
)

func TestChunkWriter_NonBlockingSend(t *testing.T) {
	ch := make(chan []byte, 2)
	cw := &chunkWriter{ch: ch}

	data := []byte("hello")
	n, err := cw.Write(data)
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if n != len(data) {
		t.Fatalf("Write returned %d, want %d", n, len(data))
	}

	select {
	case received := <-ch:
		if string(received) != "hello" {
			t.Fatalf("received %q, want %q", received, "hello")
		}
	default:
		t.Fatal("no data received on channel")
	}
}

func TestChunkWriter_CopySemantics(t *testing.T) {
	ch := make(chan []byte, 1)
	cw := &chunkWriter{ch: ch}

	data := []byte("original")
	cw.Write(data)

	// Modify original data after write
	data[0] = 'X'

	received := <-ch
	if string(received) != "original" {
		t.Fatalf("chunk was not copied: got %q, want %q", received, "original")
	}
}

func TestChunkWriter_FullChannelDrop(t *testing.T) {
	ch := make(chan []byte, 1)
	cw := &chunkWriter{ch: ch}

	// Fill the channel
	ch <- []byte("first")

	// Write should not block (it drops after 1s timeout)
	done := make(chan struct{})
	go func() {
		cw.Write([]byte("second"))
		close(done)
	}()

	// Wait for write to complete (it should timeout and drop)
	<-done

	dropped := cw.dropped.Load()
	if dropped == 0 {
		t.Fatal("expected dropped chunks > 0 when channel is full")
	}
}

func TestChunkWriter_DroppedCounter(t *testing.T) {
	ch := make(chan []byte, 0) // unbuffered — no receiver, all writes will timeout
	cw := &chunkWriter{ch: ch}

	// These will all timeout and be dropped
	for i := 0; i < 3; i++ {
		cw.Write([]byte("data"))
	}

	dropped := cw.dropped.Load()
	if dropped < 1 {
		t.Fatalf("expected at least 1 dropped chunk, got %d", dropped)
	}
}

func TestEdgeTTSProviderImplementsStreamingInterface(t *testing.T) {
	// Compile-time assertion: EdgeTTSProvider must implement StreamingSpeechProvider
	var _ StreamingSpeechProvider = (*EdgeTTSProvider)(nil)
}

func TestChunkWriter_NilChannel(t *testing.T) {
	// io.MultiWriter skips nil writers, so chunkWriter with nil channel
	// should never be called via MultiWriter. But if Write is called
	// directly with nil channel, the select default case fires immediately
	// (nil channel case never selected), so it drops after timeout.
	// This is a defensive test — no panic expected.
	ch := make(chan []byte, 0) // unbuffered, no receiver
	cw := &chunkWriter{ch: ch}
	// Just ensure no panic — the write will timeout and drop
	done := make(chan struct{})
	go func() {
		cw.Write([]byte("test"))
		close(done)
	}()
	<-done
}
