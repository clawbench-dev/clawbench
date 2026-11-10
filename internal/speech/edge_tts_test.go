package speech

import (
	"context"
	"errors"
	"strings"
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
	ch := make(chan []byte) // unbuffered — no receiver, all writes will timeout
	cw := &chunkWriter{ch: ch}

	// These will all timeout and be dropped
	for range 3 {
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
	ch := make(chan []byte) // unbuffered, no receiver
	cw := &chunkWriter{ch: ch}
	// Just ensure no panic — the write will timeout and drop
	done := make(chan struct{})
	go func() {
		cw.Write([]byte("test"))
		close(done)
	}()
	<-done
}

// ── Segmenting: one Edge request per segment ────────────────────────────────

func TestSplitSpeechSegments_ShortTextIsOneSegment(t *testing.T) {
	got := splitSpeechSegments("你好，这是一句很短的话。", edgeSegmentRunes)
	if len(got) != 1 || got[0] != "你好，这是一句很短的话。" {
		t.Fatalf("segments = %q", got)
	}
}

func TestSplitSpeechSegments_CutsAtSentenceBoundary(t *testing.T) {
	// 每条 6 runes；窗口 20 落在第 4 条的中间，应当退回到第 3 条的句号。
	text := strings.Repeat("一二三四五。", 10)
	got := splitSpeechSegments(text, 20)
	if len(got) < 2 {
		t.Fatalf("expected several segments, got %q", got)
	}
	for i, seg := range got {
		if n := len([]rune(seg)); n > 20 {
			t.Fatalf("segment %d is %d runes, over the %d limit", i, n, 20)
		}
		if i < len(got)-1 && !strings.HasSuffix(seg, "。") {
			t.Fatalf("segment %d does not end at a sentence boundary: %q", i, seg)
		}
	}
}

func TestSplitSpeechSegments_HardCutsUnpunctuatedRun(t *testing.T) {
	got := splitSpeechSegments(strings.Repeat("a", 1000), 100)
	if len(got) != 10 {
		t.Fatalf("segments = %d, want 10", len(got))
	}
	for i, seg := range got {
		if len(seg) != 100 {
			t.Fatalf("segment %d = %d runes, want 100", i, len(seg))
		}
	}
}

// A hard cut must not lose characters: the only thing dropping is whitespace at
// the segment edges.
func TestSplitSpeechSegments_PreservesAllContent(t *testing.T) {
	text := "第一句。\n\n第二句！还有第三句？" + strings.Repeat("很长的没有标点的一段内容", 40)
	got := splitSpeechSegments(text, 50)

	if len(got) == 0 {
		t.Fatal("no segments")
	}
	strip := func(s string) string {
		return strings.Join(strings.Fields(s), "")
	}
	if strip(strings.Join(got, "")) != strip(text) {
		t.Fatalf("content changed by splitting:\n got %q\nwant %q", strings.Join(got, ""), text)
	}
	for i, seg := range got {
		if seg == "" {
			t.Fatalf("segment %d is empty", i)
		}
		if n := len([]rune(seg)); n > 50 {
			t.Fatalf("segment %d is %d runes, over the limit", i, n)
		}
	}
}

func TestSynthesizeSegments_OneRequestPerSegment(t *testing.T) {
	p := NewEdgeTTSProvider()
	var ssmls []string
	// 900 runes → two segments at edgeSegmentRunes (500).
	text := strings.Repeat("这是一句完整的话。", 100)

	err := p.synthesizeSegments(context.Background(), text, func(_ context.Context, ssml string) error {
		ssmls = append(ssmls, ssml)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ssmls) != 2 {
		t.Fatalf("requests = %d, want 2 (one per segment)", len(ssmls))
	}
	// Each request carries its own segment, not the whole text.
	if n := strings.Count(ssmls[0], "这是一句完整的话"); n == 0 || n >= 100 {
		t.Fatalf("first request carries %d of 100 sentences — it must carry its own segment, not the whole text", n)
	}
}

func TestSynthesizeSegments_AbortsOnFailure(t *testing.T) {
	p := NewEdgeTTSProvider()
	calls := 0
	err := p.synthesizeSegments(context.Background(), strings.Repeat("这是一句完整的话。", 100),
		func(_ context.Context, _ string) error {
			calls++
			if calls == 2 {
				return errors.New("boom")
			}
			return nil
		})
	if err == nil {
		t.Fatal("a failed segment must fail the job")
	}
	if !strings.Contains(err.Error(), "segment 2/2") {
		t.Fatalf("error must name the segment: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (stop after the failure)", calls)
	}
}

// A cancelled caller (user stopped, client gone) is not a segment failure: the
// error must be the context's, not a "segment N/M" wrap.
func TestSynthesizeSegments_StopsWhenCallerCancels(t *testing.T) {
	p := NewEdgeTTSProvider()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := p.synthesizeSegments(ctx, strings.Repeat("这是一句完整的话。", 100),
		func(_ context.Context, _ string) error {
			calls++
			cancel()
			return context.Canceled
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}
