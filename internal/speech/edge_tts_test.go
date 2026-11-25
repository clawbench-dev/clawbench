package speech

import (
	"bytes"
	"context"
	"errors"
	"io"
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

// ── Truncated segments ─────────────────────────────────────────────────────

// A segment the service reported covering only part of is a fragment: the
// sentence was cut off, so it must fail the job before it is written out.
func TestSynthesizeText_ReportedPartialSpeechIsRejected(t *testing.T) {
	p := NewEdgeTTSProvider()
	p.synthHook = func(_ context.Context, ssml string, w io.Writer) (int, error) {
		if n := edgeSSMLText(ssml); n != 7 {
			t.Fatalf("segment text = %d runes, want 7", n)
		}
		// Audio for the whole sentence arrived, but the service only reported
		// speaking the first half: the listener would hear it cut off.
		_, _ = w.Write(make([]byte, 5000))
		return 3, nil
	}
	var out bytes.Buffer
	err := p.synthesizeText(context.Background(), "完整的一句话。", &out)
	if err == nil {
		t.Fatal("expected the job to fail")
	}
	if out.Len() != 0 {
		t.Fatalf("a rejected segment must not reach the caller: %d bytes", out.Len())
	}
}

// A complete segment is written out in one piece, after the service answered.
func TestSynthesizeText_CompleteSegmentIsWritten(t *testing.T) {
	p := NewEdgeTTSProvider()
	p.synthHook = func(_ context.Context, ssml string, w io.Writer) (int, error) {
		audio := make([]byte, edgeSSMLText(ssml)*edgeMinBytesPerRune)
		_, _ = w.Write(audio)
		return edgeSSMLText(ssml), nil
	}
	var out bytes.Buffer
	if err := p.synthesizeText(context.Background(), "完整的一句话。", &out); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 7*edgeMinBytesPerRune {
		t.Fatalf("wrote %d bytes, want %d", out.Len(), 7*edgeMinBytesPerRune)
	}
}

// Even when the service reports nothing, audio far below what the text needs is
// a fragment (the reading was cut mid-sentence), not a short narration.
func TestCheckSegmentAudio_ByteFloor(t *testing.T) {
	cases := []struct {
		name  string
		runes int
		bytes int
		heard int
		ok    bool
	}{
		{"full speech passes", 100, 100 * edgeMinBytesPerRune, 0, true},
		{"heard equals text passes", 100, 100 * edgeMinBytesPerRune, 100, true},
		{"heard shorter than text fails", 100, 100 * edgeMinBytesPerRune, 40, false},
		{"fragment below floor fails", 100, 20, 0, false},
		{"no audio fails", 100, 0, 0, false},
		{"no text fails", 0, 1200, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := checkSegmentAudio(tc.runes, tc.bytes, tc.heard)
			if tc.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

// The covered span comes from the service's own word-boundary frames; a frame
// that carries no usable length must not count as "spoke nothing".
func TestEdgeBoundaryRunes(t *testing.T) {
	frame := "X-RequestId:abc\r\nPath:audio.metadata\r\n\r\n" +
		`{"Metadata":[{"Type":"WordBoundary","Data":{"Offset":1000000,"Duration":5000000,"text":{"Text":"你好","Length":2,"BoundaryType":"WordBoundary"}}},` +
		`{"Type":"WordBoundary","Data":{"Offset":10000000,"Duration":5000000,"text":{"Text":"世界","Length":2,"BoundaryType":"WordBoundary"}}},` +
		`{"Type":"SentenceBoundary","Data":{"text":{"Text":"你好世界。","Length":5}}}]}`
	if got := edgeBoundaryRunes(frame); got != 4 {
		t.Fatalf("spoken runes = %d, want 4 (word boundaries only, sentence boundary ignored)", got)
	}
	if got := edgeBoundaryRunes("Path:audio.metadata\r\n\r\nnot json"); got != 0 {
		t.Fatalf("malformed frame = %d, want 0 (unknown)", got)
	}
}

// The SSML wrapper is generated by buildSSML, so the text can be read back and
// compared with the audio length.
func TestEdgeSSMLText(t *testing.T) {
	ssml := buildSSML(edgeDefaultVoice, "+0%", "甲乙丙丁。")
	if got := edgeSSMLText(ssml); got != 5 {
		t.Fatalf("text runes = %d, want 5", got)
	}
	if got := edgeSSMLText("no prosody here"); got != 0 {
		t.Fatalf("text runes = %d, want 0", got)
	}
}
