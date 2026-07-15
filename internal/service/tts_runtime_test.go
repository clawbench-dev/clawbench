package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func cleanupTTSJobs() {
	ttsJobs.Range(func(key, _ interface{}) bool {
		ttsJobs.Delete(key)
		return true
	})
}

func TestRegisterTTSJob(t *testing.T) {
	cleanupTTSJobs()
	defer cleanupTTSJobs()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := RegisterTTSJob("tts-1", cancel)

	assert.Equal(t, "tts-1", job.ID)
	assert.NotNil(t, job.StreamCh)
	assert.NotNil(t, job.Cancel)
	assert.NotNil(t, job.Done)

	// Should be retrievable
	got, ok := GetTTSJob("tts-1")
	assert.True(t, ok)
	assert.Equal(t, job, got)
}

func TestRegisterStreamingTTSJob(t *testing.T) {
	cleanupTTSJobs()
	defer cleanupTTSJobs()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := RegisterStreamingTTSJob("tts-stream-1", cancel)

	assert.Equal(t, "tts-stream-1", job.ID)
	assert.NotNil(t, job.StreamCh)
	assert.NotNil(t, job.AudioCh, "AudioCh should be created for streaming job")
	assert.NotNil(t, job.Cancel)
	assert.NotNil(t, job.Done)

	// AudioCh should be buffered
	assert.Equal(t, audioChunkBufSize, cap(job.AudioCh), "AudioCh should have audioChunkBufSize capacity")

	// Should be retrievable
	got, ok := GetTTSJob("tts-stream-1")
	assert.True(t, ok)
	assert.Equal(t, job, got)
}

func TestGetTTSJob_NotFound(t *testing.T) {
	cleanupTTSJobs()

	_, ok := GetTTSJob("nonexistent")
	assert.False(t, ok)
}

func TestGetTTSJob_AfterUnregister(t *testing.T) {
	cleanupTTSJobs()
	defer cleanupTTSJobs()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	RegisterTTSJob("tts-temp", cancel)
	UnregisterTTSJob("tts-temp")

	_, ok := GetTTSJob("tts-temp")
	assert.False(t, ok)
}

func TestUnregisterTTSJob_ClosesStreamChannel(t *testing.T) {
	cleanupTTSJobs()
	defer cleanupTTSJobs()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := RegisterTTSJob("tts-close", cancel)
	UnregisterTTSJob("tts-close")

	// Reading from closed channel should return zero value with ok=false
	_, ok := <-job.StreamCh
	assert.False(t, ok, "StreamCh should be closed after unregister")
}

func TestUnregisterTTSJob_Idempotent(t *testing.T) {
	cleanupTTSJobs()
	defer cleanupTTSJobs()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	RegisterTTSJob("tts-idem", cancel)
	UnregisterTTSJob("tts-idem")
	// Second unregister should not panic
	assert.NotPanics(t, func() {
		UnregisterTTSJob("tts-idem")
	})
}

func TestSendTTSEvent_Success(t *testing.T) {
	cleanupTTSJobs()
	defer cleanupTTSJobs()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := RegisterTTSJob("tts-send", cancel)

	event := TTSEvent{Type: "phase", Phase: "summarizing"}
	sent := SendTTSEvent("tts-send", event)
	assert.True(t, sent)

	// Verify the event was sent
	received := <-job.StreamCh
	assert.Equal(t, "phase", received.Type)
	assert.Equal(t, "summarizing", received.Phase)
}

func TestSendTTSEvent_JobNotFound(t *testing.T) {
	cleanupTTSJobs()

	sent := SendTTSEvent("nonexistent", TTSEvent{Type: "phase"})
	assert.False(t, sent)
}

func TestSendTTSEvent_FullChannel(t *testing.T) {
	cleanupTTSJobs()
	defer cleanupTTSJobs()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	RegisterTTSJob("tts-full", cancel)

	// Fill the channel buffer (capacity is 16)
	for range 16 {
		sent := SendTTSEvent("tts-full", TTSEvent{Type: "phase", Phase: "step"})
		assert.True(t, sent)
	}

	// Next send should fail (non-blocking)
	sent := SendTTSEvent("tts-full", TTSEvent{Type: "result"})
	assert.False(t, sent, "SendTTSEvent should return false when channel is full")
}

func TestCloseTTSJobDone(t *testing.T) {
	cleanupTTSJobs()
	defer cleanupTTSJobs()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := RegisterTTSJob("tts-done", cancel)

	// Done channel should be open (not closed)
	select {
	case <-job.Done:
		t.Fatal("Done should not be closed yet")
	default:
		// expected
	}

	CloseTTSJobDone("tts-done")

	// Now Done should be closed
	select {
	case <-job.Done:
		// expected
	case <-time.After(time.Second):
		t.Fatal("Done should be closed after CloseTTSJobDone")
	}
}

func TestCloseTTSJobDone_Idempotent(t *testing.T) {
	cleanupTTSJobs()
	defer cleanupTTSJobs()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	RegisterTTSJob("tts-done2", cancel)

	// Close twice should not panic
	CloseTTSJobDone("tts-done2")
	assert.NotPanics(t, func() {
		CloseTTSJobDone("tts-done2")
	})
}

func TestCloseTTSJobDone_JobNotFound(t *testing.T) {
	cleanupTTSJobs()

	// Should not panic on nonexistent job
	assert.NotPanics(t, func() {
		CloseTTSJobDone("nonexistent")
	})
}

func TestCancelTTSJob(t *testing.T) {
	cleanupTTSJobs()
	defer cleanupTTSJobs()

	ctx, cancel := context.WithCancel(context.Background())
	RegisterTTSJob("tts-cancel", cancel)

	// Context should not be done yet
	assert.NoError(t, ctx.Err())

	CancelTTSJob("tts-cancel")

	// Context should be cancelled now
	assert.Error(t, ctx.Err())
	assert.Equal(t, context.Canceled, ctx.Err())
}

func TestCancelTTSJob_NotFound(t *testing.T) {
	cleanupTTSJobs()

	// Should not panic on nonexistent job
	assert.NotPanics(t, func() {
		CancelTTSJob("nonexistent")
	})
}

func TestTTSJob_ResultEvent(t *testing.T) {
	cleanupTTSJobs()
	defer cleanupTTSJobs()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := RegisterTTSJob("tts-result", cancel)

	event := TTSEvent{
		Type:             "result",
		AudioPath:        "/tmp/audio.mp3",
		Summary:          "AI response summary",
		SynthesizeFailed: false,
	}
	sent := SendTTSEvent("tts-result", event)
	assert.True(t, sent)

	received := <-job.StreamCh
	assert.Equal(t, "result", received.Type)
	assert.Equal(t, "/tmp/audio.mp3", received.AudioPath)
	assert.Equal(t, "AI response summary", received.Summary)
	assert.False(t, received.SynthesizeFailed)
}

func TestTTSJob_FailedResultEvent(t *testing.T) {
	cleanupTTSJobs()
	defer cleanupTTSJobs()

	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := RegisterTTSJob("tts-fail", cancel)

	event := TTSEvent{
		Type:             "result",
		SynthesizeFailed: true,
		SynthesizeError:  "TTS engine unavailable",
	}
	sent := SendTTSEvent("tts-fail", event)
	assert.True(t, sent)

	received := <-job.StreamCh
	assert.True(t, received.SynthesizeFailed)
	assert.Equal(t, "TTS engine unavailable", received.SynthesizeError)
}

func TestEvictTTSCache_DeletesOldest(t *testing.T) {
	dir := t.TempDir()
	ttsDir := filepath.Join(dir, ".clawbench", "generated", "tts")
	require.NoError(t, os.MkdirAll(ttsDir, 0o755))

	// Create 5 mp3 files with ascending mtimes
	for i := range 5 {
		name := filepath.Join(ttsDir, fmt.Sprintf("audio-%d.mp3", i))
		require.NoError(t, os.WriteFile(name, []byte("fake audio"), 0o644))
		// Set mtime so they're ordered
		mtime := time.Now().Add(-time.Duration(5-i) * time.Minute)
		require.NoError(t, os.Chtimes(name, mtime, mtime))
	}

	// Also create a companion summary for the oldest file
	require.NoError(t, os.WriteFile(filepath.Join(ttsDir, "audio-0.mp3.summary.txt"), []byte("summary"), 0o644))

	// Evict down to 3 files (should delete 2 oldest)
	EvictTTSCache(dir, 3)

	entries, err := os.ReadDir(ttsDir)
	require.NoError(t, err)
	mp3Count := 0
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".mp3" {
			mp3Count++
		}
	}
	assert.Equal(t, 3, mp3Count, "should keep only 3 mp3 files")

	// Oldest files should be deleted
	_, err = os.Stat(filepath.Join(ttsDir, "audio-0.mp3"))
	assert.True(t, os.IsNotExist(err), "audio-0.mp3 should be deleted")
	_, err = os.Stat(filepath.Join(ttsDir, "audio-0.mp3.summary.txt"))
	assert.True(t, os.IsNotExist(err), "companion summary should be deleted")
}

func TestEvictTTSCache_NoEvictionWhenUnderLimit(t *testing.T) {
	dir := t.TempDir()
	ttsDir := filepath.Join(dir, ".clawbench", "generated", "tts")
	require.NoError(t, os.MkdirAll(ttsDir, 0o755))

	require.NoError(t, os.WriteFile(filepath.Join(ttsDir, "audio.mp3"), []byte("audio"), 0o644))

	EvictTTSCache(dir, 5)

	entries, _ := os.ReadDir(ttsDir)
	assert.Len(t, entries, 1, "should not evict when under limit")
}

func TestEvictTTSCache_Unlimited(t *testing.T) {
	dir := t.TempDir()
	ttsDir := filepath.Join(dir, ".clawbench", "generated", "tts")
	require.NoError(t, os.MkdirAll(ttsDir, 0o755))

	for i := range 10 {
		require.NoError(t, os.WriteFile(filepath.Join(ttsDir, fmt.Sprintf("audio-%d.wav", i)), []byte("audio"), 0o644))
	}

	EvictTTSCache(dir, 0) // unlimited
	entries, _ := os.ReadDir(ttsDir)
	assert.Len(t, entries, 10, "should not evict when maxFiles=0")
}

func TestEvictTTSCache_NonexistentDir(t *testing.T) {
	EvictTTSCache("/nonexistent/path/for/test", 1)
	// Should not panic
}
