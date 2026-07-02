package service

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// TTSEvent represents a single event in the TTS generation pipeline.
type TTSEvent struct {
	Type             string `json:"type"`                       // "phase", "result"
	Phase            string `json:"phase,omitempty"`            // "summarizing", "synthesizing" (for type="phase")
	AudioPath        string `json:"audioPath,omitempty"`        // (for type="result")
	Summary          string `json:"summary,omitempty"`          // (for type="result")
	SynthesizeFailed bool   `json:"synthesizeFailed,omitempty"` // (for type="result")
	SynthesizeError  string `json:"synthesizeError,omitempty"`  // (for type="result")
}

// TTSJob represents an in-flight TTS generation job.
type TTSJob struct {
	ID       string
	StreamCh chan TTSEvent
	Cancel   context.CancelFunc
	Done     chan struct{} // closed when job goroutine finishes
}

// ttsJobs stores active TTS jobs keyed by job ID (cache key).
var ttsJobs sync.Map // map[string]*TTSJob

// RegisterTTSJob creates and registers a new TTS job.
func RegisterTTSJob(id string, cancel context.CancelFunc) *TTSJob {
	job := &TTSJob{
		ID:       id,
		StreamCh: make(chan TTSEvent, 16),
		Cancel:   cancel,
		Done:     make(chan struct{}),
	}
	ttsJobs.Store(id, job)
	return job
}

// GetTTSJob returns the TTS job by ID.
func GetTTSJob(id string) (*TTSJob, bool) {
	val, ok := ttsJobs.Load(id)
	if !ok {
		return nil, false
	}
	job, ok := val.(*TTSJob)
	return job, ok
}

// UnregisterTTSJob removes the TTS job and closes its stream channel.
func UnregisterTTSJob(id string) {
	if val, ok := ttsJobs.LoadAndDelete(id); ok {
		if job, ok := val.(*TTSJob); ok {
			close(job.StreamCh)
		}
	}
}

// SendTTSEvent sends an event to the job's stream channel (non-blocking).
// Returns true if the event was sent successfully.
func SendTTSEvent(id string, event TTSEvent) bool {
	val, ok := ttsJobs.Load(id)
	if !ok {
		return false
	}
	job, ok := val.(*TTSJob)
	if !ok {
		return false
	}
	select {
	case job.StreamCh <- event:
		return true
	default:
		return false
	}
}

// CloseTTSJobDone signals that the job goroutine has finished.
func CloseTTSJobDone(id string) {
	val, ok := ttsJobs.Load(id)
	if !ok {
		return
	}
	job, ok := val.(*TTSJob)
	if !ok {
		return
	}
	select {
	case <-job.Done:
		// Already closed
	default:
		close(job.Done)
	}
}

// CancelTTSJob cancels a running TTS job. Used when the SSE client disconnects.
func CancelTTSJob(id string) {
	val, ok := ttsJobs.Load(id)
	if !ok {
		return
	}
	job, ok := val.(*TTSJob)
	if !ok {
		return
	}
	job.Cancel()
}

// cachedTTSFile records a cached TTS audio file with its modification time.
type cachedTTSFile struct {
	name  string
	mtime int64 // Unix nano for sorting
}

// EvictTTSCache removes the oldest cached TTS audio files when the total
// count exceeds the configured limit. It also cleans up the corresponding
// .summary.txt companion files and tts_summaries DB rows.
//
// Call this after successfully generating a new TTS audio file.
// maxFiles <= 0 means unlimited (no eviction).
func EvictTTSCache(projectPath string, maxFiles int) {
	if maxFiles <= 0 {
		return
	}

	// Project-relative path (not server DataDir)
	ttsDir := filepath.Join(projectPath, ".clawbench", "generated", "tts")
	entries, err := os.ReadDir(ttsDir)
	if err != nil {
		return // directory may not exist yet
	}

	// Collect audio files only (.mp3, .wav), skip .summary.txt and other files
	var files []cachedTTSFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if ext != ".mp3" && ext != ".wav" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, cachedTTSFile{
			name:  name,
			mtime: info.ModTime().UnixNano(),
		})
	}

	if len(files) <= maxFiles {
		return
	}

	// Sort oldest first (lowest mtime)
	sort.Slice(files, func(i, j int) bool {
		return files[i].mtime < files[j].mtime
	})

	// Delete the oldest files to bring count down to maxFiles
	deleteCount := len(files) - maxFiles
	for i := range deleteCount {
		absPath := filepath.Join(ttsDir, files[i].name)
		if err := os.Remove(absPath); err != nil {
			continue
		}

		// Remove companion .summary.txt if it exists (legacy file-based cache)
		_ = os.Remove(absPath + ".summary.txt")
	}

	if deleteCount > 0 {
		slog.Info(
			"tts cache eviction completed",
			slog.Int("deleted", deleteCount),
			slog.Int("remaining", len(files)-deleteCount),
			slog.Int("max", maxFiles),
		)
	}
}
