package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupFileWatcher creates a FileWatcher for testing without setting the global.
func setupFileWatcher(t *testing.T) *FileWatcher {
	t.Helper()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		t.Fatalf("failed to create fsnotify watcher: %v", err)
	}
	fw := &FileWatcher{
		watcher:         w,
		clients:         make(map[string]*watchClient),
		done:            make(chan struct{}),
		debounceTimers:  make(map[string]*time.Timer),
		debouncePending: make(map[string]WatchEvent),
	}
	go fw.eventLoop()
	t.Cleanup(func() {
		close(fw.done)
		fw.watcher.Close()
	})
	return fw
}

// collectEvents reads events from a channel with timeout.
func collectEvents(ch <-chan WatchEvent, count int, timeout time.Duration) []WatchEvent {
	var events []WatchEvent
	deadline := time.After(timeout)
	for len(events) < count {
		select {
		case e, ok := <-ch:
			if !ok {
				return events
			}
			events = append(events, e)
		case <-deadline:
			return events
		}
	}
	return events
}

// ---------- Init / Stop lifecycle ----------

func TestInitFileWatcher(t *testing.T) {
	orig := GlobalFileWatcher
	defer func() { GlobalFileWatcher = orig }()

	err := InitFileWatcher()
	assert.NoError(t, err)
	assert.NotNil(t, GlobalFileWatcher)
	assert.NotNil(t, GlobalFileWatcher.watcher)
	assert.NotNil(t, GlobalFileWatcher.clients)

	// Cleanup
	StopFileWatcher()
}

func TestStopFileWatcher_Nil(t *testing.T) {
	orig := GlobalFileWatcher
	defer func() { GlobalFileWatcher = orig }()

	GlobalFileWatcher = nil
	StopFileWatcher()
	// Should not panic
}

func TestInitAndStopIdempotent(t *testing.T) {
	orig := GlobalFileWatcher
	defer func() { GlobalFileWatcher = orig }()

	err := InitFileWatcher()
	assert.NoError(t, err)
	StopFileWatcher()

	// Second stop should not panic
	StopFileWatcher()
}

// ---------- RegisterClient / UnregisterClient ----------

func TestRegisterClient(t *testing.T) {
	fw := setupFileWatcher(t)

	ch := fw.RegisterClient("client1")
	assert.NotNil(t, ch)

	fw.mu.Lock()
	_, ok := fw.clients["client1"]
	fw.mu.Unlock()
	assert.True(t, ok)
}

func TestRegisterClient_ChannelCapacity(t *testing.T) {
	fw := setupFileWatcher(t)

	fw.RegisterClient("client1")

	// Access the channel directly from the client struct
	fw.mu.Lock()
	ch := fw.clients["client1"].pushCh
	fw.mu.Unlock()

	// Channel should have capacity of watchPushChSize
	for range watchPushChSize {
		select {
		case ch <- WatchEvent{Type: "file_change", Path: "/test"}:
		default:
			t.Fatalf("channel should accept %d events", watchPushChSize)
		}
	}
	// Next write should block (channel full)
	select {
	case ch <- WatchEvent{Type: "file_change", Path: "/test"}:
		t.Fatal("channel should be full")
	default:
		// Expected
	}
}

func TestUnregisterClient(t *testing.T) {
	fw := setupFileWatcher(t)

	ch := fw.RegisterClient("client1")
	fw.UpdateWatch("client1", "/tmp", "/tmp/test.txt")

	fw.UnregisterClient("client1")

	fw.mu.Lock()
	_, ok := fw.clients["client1"]
	fw.mu.Unlock()
	assert.False(t, ok)

	// Channel should be closed
	_, open := <-ch
	assert.False(t, open)
}

func TestUnregisterClient_UnknownID(t *testing.T) {
	fw := setupFileWatcher(t)

	// Should not panic
	fw.UnregisterClient("nonexistent")
}

func TestUnregisterClient_RemovesOrphanWatches(t *testing.T) {
	fw := setupFileWatcher(t)

	dir := t.TempDir()
	fw.RegisterClient("c1")
	fw.UpdateWatch("c1", dir, "")

	// Watch should be active
	fw.mu.Lock()
	assert.Equal(t, dir, fw.clients["c1"].dirPath)
	fw.mu.Unlock()

	fw.UnregisterClient("c1")

	// After unregister, no clients left; watch should be removed from fsnotify
	// We can verify by re-adding the same path (should succeed, not fail)
	err := fw.watcher.Add(dir)
	assert.NoError(t, err)
}

func TestUnregisterClient_SharedPathNotRemoved(t *testing.T) {
	fw := setupFileWatcher(t)

	dir := t.TempDir()
	fw.RegisterClient("c1")
	fw.RegisterClient("c2")
	fw.UpdateWatch("c1", dir, "")
	fw.UpdateWatch("c2", dir, "")

	// Unregister c1 — dir should still be watched because c2 needs it
	fw.UnregisterClient("c1")

	// c2 should still be in clients
	fw.mu.Lock()
	_, ok := fw.clients["c2"]
	fw.mu.Unlock()
	assert.True(t, ok)
}

// ---------- UpdateWatch ----------

func TestUpdateWatch_SetsPaths(t *testing.T) {
	fw := setupFileWatcher(t)
	fw.RegisterClient("c1")

	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	_ = os.WriteFile(file, []byte("hello"), 0o644)

	fw.UpdateWatch("c1", dir, file)

	fw.mu.Lock()
	c := fw.clients["c1"]
	assert.Equal(t, dir, c.dirPath)
	assert.Equal(t, file, c.filePath)
	fw.mu.Unlock()
}

func TestUpdateWatch_UnknownClient(t *testing.T) {
	fw := setupFileWatcher(t)

	// Should not panic, just log debug
	fw.UpdateWatch("nonexistent", "/tmp", "/tmp/test.txt")
}

func TestUpdateWatch_DiffDirectory(t *testing.T) {
	fw := setupFileWatcher(t)
	fw.RegisterClient("c1")

	dir1 := t.TempDir()
	dir2 := t.TempDir()

	fw.UpdateWatch("c1", dir1, "")
	fw.UpdateWatch("c1", dir2, "")

	fw.mu.Lock()
	c := fw.clients["c1"]
	assert.Equal(t, dir2, c.dirPath)
	fw.mu.Unlock()
}

func TestUpdateWatch_DiffFile(t *testing.T) {
	fw := setupFileWatcher(t)
	fw.RegisterClient("c1")

	dir := t.TempDir()
	file1 := filepath.Join(dir, "a.txt")
	file2 := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(file1, []byte("a"), 0o644)
	_ = os.WriteFile(file2, []byte("b"), 0o644)

	fw.UpdateWatch("c1", dir, file1)
	fw.UpdateWatch("c1", dir, file2)

	fw.mu.Lock()
	c := fw.clients["c1"]
	assert.Equal(t, file2, c.filePath)
	fw.mu.Unlock()
}

func TestUpdateWatch_SamePathNoDoubleAdd(t *testing.T) {
	fw := setupFileWatcher(t)
	fw.RegisterClient("c1")

	dir := t.TempDir()

	// UpdateWatch with same path twice should not cause issues
	fw.UpdateWatch("c1", dir, "")
	fw.UpdateWatch("c1", dir, "")

	fw.mu.Lock()
	c := fw.clients["c1"]
	assert.Equal(t, dir, c.dirPath)
	fw.mu.Unlock()
}

func TestUpdateWatch_EmptyPaths(t *testing.T) {
	fw := setupFileWatcher(t)
	fw.RegisterClient("c1")

	// Empty paths should not be watched
	fw.UpdateWatch("c1", "", "")

	fw.mu.Lock()
	c := fw.clients["c1"]
	assert.Equal(t, "", c.dirPath)
	assert.Equal(t, "", c.filePath)
	fw.mu.Unlock()
}

func TestUpdateWatch_CancelsDebounceTimers(t *testing.T) {
	fw := setupFileWatcher(t)
	fw.RegisterClient("c1")

	dir := t.TempDir()
	fw.UpdateWatch("c1", dir, "")

	// Simulate a pending debounce event
	fw.mu.Lock()
	key := "c1|dir_change"
	fw.debounceTimers[key] = time.AfterFunc(5*time.Second, func() {})
	fw.debouncePending[key] = WatchEvent{Type: "dir_change", Path: dir}
	fw.mu.Unlock()

	// UpdateWatch should cancel the old timer
	fw.UpdateWatch("c1", t.TempDir(), "")

	fw.mu.Lock()
	_, hasTimer := fw.debounceTimers[key]
	_, hasPending := fw.debouncePending[key]
	fw.mu.Unlock()
	assert.False(t, hasTimer, "old debounce timer should be cancelled")
	assert.False(t, hasPending, "old debounce pending should be cleared")
}

// ---------- handleFsEvent ----------

func TestHandleFsEvent_DirCreate(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	fw.UpdateWatch("c1", dir, "")

	// Simulate a Create event on the watched directory
	fw.handleFsEvent(fsnotify.Event{
		Name: dir,
		Op:   fsnotify.Create,
	})

	events := collectEvents(ch, 1, 500*time.Millisecond)
	assert.Len(t, events, 1)
	assert.Equal(t, "dir_change", events[0].Type)
	assert.Equal(t, dir, events[0].Path)
}

func TestHandleFsEvent_DirRemove(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	fw.UpdateWatch("c1", dir, "")

	fw.handleFsEvent(fsnotify.Event{
		Name: dir,
		Op:   fsnotify.Remove,
	})

	events := collectEvents(ch, 1, 500*time.Millisecond)
	assert.Len(t, events, 1)
	assert.Equal(t, "dir_change", events[0].Type)
}

func TestHandleFsEvent_DirRename(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	fw.UpdateWatch("c1", dir, "")

	fw.handleFsEvent(fsnotify.Event{
		Name: dir,
		Op:   fsnotify.Rename,
	})

	events := collectEvents(ch, 1, 500*time.Millisecond)
	assert.Len(t, events, 1)
	assert.Equal(t, "dir_change", events[0].Type)
}

func TestHandleFsEvent_DirWriteIgnored(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	fw.UpdateWatch("c1", dir, "")

	// Write event on a directory should be ignored
	fw.handleFsEvent(fsnotify.Event{
		Name: dir,
		Op:   fsnotify.Write,
	})

	events := collectEvents(ch, 1, 300*time.Millisecond)
	assert.Len(t, events, 0, "dir Write should not trigger dir_change")
}

func TestHandleFsEvent_FileWrite(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	_ = os.WriteFile(file, []byte("hello"), 0o644)

	fw.UpdateWatch("c1", dir, file)

	fw.handleFsEvent(fsnotify.Event{
		Name: file,
		Op:   fsnotify.Write,
	})

	events := collectEvents(ch, 1, 500*time.Millisecond)
	assert.Len(t, events, 1)
	assert.Equal(t, "file_change", events[0].Type)
	assert.Equal(t, file, events[0].Path)
}

func TestHandleFsEvent_FileCreate(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	file := filepath.Join(dir, "new.txt")

	fw.UpdateWatch("c1", dir, file)

	fw.handleFsEvent(fsnotify.Event{
		Name: file,
		Op:   fsnotify.Create,
	})

	events := collectEvents(ch, 1, 500*time.Millisecond)
	assert.Len(t, events, 1)
	assert.Equal(t, "file_change", events[0].Type)
}

func TestHandleFsEvent_FileRename(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	_ = os.WriteFile(file, []byte("hello"), 0o644)

	fw.UpdateWatch("c1", dir, file)

	fw.handleFsEvent(fsnotify.Event{
		Name: file,
		Op:   fsnotify.Rename,
	})

	events := collectEvents(ch, 1, 500*time.Millisecond)
	assert.Len(t, events, 1)
	assert.Equal(t, "file_change", events[0].Type)
}

func TestHandleFsEvent_FileRemove(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	_ = os.WriteFile(file, []byte("hello"), 0o644)

	fw.UpdateWatch("c1", dir, file)

	// Remove event on a specifically-watched file should emit file_change
	fw.handleFsEvent(fsnotify.Event{
		Name: file,
		Op:   fsnotify.Remove,
	})

	events := collectEvents(ch, 1, 500*time.Millisecond)
	assert.Len(t, events, 1)
	assert.Equal(t, "file_change", events[0].Type)
	assert.Equal(t, file, events[0].Path)
}

func TestHandleFsEvent_UnrelatedPath(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	fw.UpdateWatch("c1", dir, "")

	// Event for a path not watched by this client
	fw.handleFsEvent(fsnotify.Event{
		Name: "/completely/unrelated/path",
		Op:   fsnotify.Write,
	})

	events := collectEvents(ch, 1, 300*time.Millisecond)
	assert.Len(t, events, 0, "unrelated path should not trigger event")
}

func TestHandleFsEvent_DirAndFileSamePath(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	// When dir and filePath point to the same path, file_change takes priority
	// because the client explicitly opened this file for content watching
	fw.UpdateWatch("c1", dir, dir)

	fw.handleFsEvent(fsnotify.Event{
		Name: dir,
		Op:   fsnotify.Create,
	})

	events := collectEvents(ch, 1, 500*time.Millisecond)
	assert.Len(t, events, 1)
	assert.Equal(t, "file_change", events[0].Type, "file match takes priority when dir and file are the same path")
}

func TestHandleFsEvent_FileChangeOverridesDirChange(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	file := filepath.Join(dir, "watched.txt")
	_ = os.WriteFile(file, []byte("hello"), 0o644)

	fw.UpdateWatch("c1", dir, file)

	// Write to the watched file — should get file_change, not dir_change
	fw.handleFsEvent(fsnotify.Event{
		Name: file,
		Op:   fsnotify.Write,
	})

	events := collectEvents(ch, 1, 500*time.Millisecond)
	assert.Len(t, events, 1)
	assert.Equal(t, "file_change", events[0].Type)
}

func TestHandleFsEvent_SiblingFileGivesDirChange(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	watchedFile := filepath.Join(dir, "watched.txt")
	siblingFile := filepath.Join(dir, "sibling.txt")
	_ = os.WriteFile(watchedFile, []byte("hello"), 0o644)

	fw.UpdateWatch("c1", dir, watchedFile)

	// Create a sibling file — should get dir_change, not file_change
	fw.handleFsEvent(fsnotify.Event{
		Name: siblingFile,
		Op:   fsnotify.Create,
	})

	events := collectEvents(ch, 1, 500*time.Millisecond)
	assert.Len(t, events, 1)
	assert.Equal(t, "dir_change", events[0].Type)
}

func TestHandleFsEvent_MultipleClients(t *testing.T) {
	fw := setupFileWatcher(t)
	ch1 := fw.RegisterClient("c1")
	ch2 := fw.RegisterClient("c2")

	dir := t.TempDir()
	file := filepath.Join(dir, "shared.txt")
	_ = os.WriteFile(file, []byte("hello"), 0o644)

	fw.UpdateWatch("c1", dir, file)
	fw.UpdateWatch("c2", dir, file)

	fw.handleFsEvent(fsnotify.Event{
		Name: file,
		Op:   fsnotify.Write,
	})

	events1 := collectEvents(ch1, 1, 500*time.Millisecond)
	events2 := collectEvents(ch2, 1, 500*time.Millisecond)
	assert.Len(t, events1, 1)
	assert.Len(t, events2, 1)
	assert.Equal(t, "file_change", events1[0].Type)
	assert.Equal(t, "file_change", events2[0].Type)
}

func TestHandleFsEvent_ClientRemovedMidEvent(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	fw.UpdateWatch("c1", dir, "")

	// Remove client before event
	fw.UnregisterClient("c1")

	// Now fire event — should not panic
	fw.handleFsEvent(fsnotify.Event{
		Name: dir,
		Op:   fsnotify.Create,
	})

	// Channel should be closed
	_, open := <-ch
	assert.False(t, open)
}

// ---------- Debounce ----------

func TestDebounce_Coalescing(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	fw.UpdateWatch("c1", dir, "")

	// Fire 5 rapid events
	for range 5 {
		fw.handleFsEvent(fsnotify.Event{
			Name: dir,
			Op:   fsnotify.Create,
		})
	}

	// Wait for debounce to settle (200ms debounce + buffer)
	time.Sleep(500 * time.Millisecond)

	// Should only receive 1 coalesced event
	events := collectEvents(ch, 10, 300*time.Millisecond)
	assert.Len(t, events, 1, "rapid events should be coalesced into one")
	assert.Equal(t, "dir_change", events[0].Type)
}

func TestDebounce_DifferentEventTypesNotCoalesced(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	_ = os.WriteFile(file, []byte("hello"), 0o644)

	fw.UpdateWatch("c1", dir, file)

	// Fire dir_change and file_change — they are different debounce keys
	fw.handleFsEvent(fsnotify.Event{
		Name: dir,
		Op:   fsnotify.Create,
	})
	fw.handleFsEvent(fsnotify.Event{
		Name: file,
		Op:   fsnotify.Write,
	})

	events := collectEvents(ch, 2, 500*time.Millisecond)
	assert.Len(t, events, 2)

	types := map[string]bool{}
	for _, e := range events {
		types[e.Type] = true
	}
	assert.True(t, types["dir_change"])
	assert.True(t, types["file_change"])
}

// ---------- isPathWatchedByOthers ----------

func TestIsPathWatchedByOthers_True(t *testing.T) {
	fw := setupFileWatcher(t)
	fw.RegisterClient("c1")
	fw.RegisterClient("c2")
	fw.UpdateWatch("c1", "/dir1", "/file1")
	fw.UpdateWatch("c2", "/dir1", "/file2")

	fw.mu.Lock()
	result := fw.isPathWatchedByOthers("c1", "/dir1")
	fw.mu.Unlock()
	assert.True(t, result, "c2 watches /dir1 as dirPath")
}

func TestIsPathWatchedByOthers_False(t *testing.T) {
	fw := setupFileWatcher(t)
	fw.RegisterClient("c1")
	fw.RegisterClient("c2")
	fw.UpdateWatch("c1", "/dir1", "/file1")
	fw.UpdateWatch("c2", "/dir2", "/file2")

	fw.mu.Lock()
	result := fw.isPathWatchedByOthers("c1", "/dir1")
	fw.mu.Unlock()
	assert.False(t, result, "no other client watches /dir1")
}

func TestIsPathWatchedByOthers_FilePathMatch(t *testing.T) {
	fw := setupFileWatcher(t)
	fw.RegisterClient("c1")
	fw.RegisterClient("c2")
	fw.UpdateWatch("c1", "/dir1", "/shared.txt")
	fw.UpdateWatch("c2", "/dir2", "/shared.txt")

	fw.mu.Lock()
	result := fw.isPathWatchedByOthers("c1", "/shared.txt")
	fw.mu.Unlock()
	assert.True(t, result, "c2 watches /shared.txt as filePath")
}

func TestIsPathWatchedByOthers_NoOthers(t *testing.T) {
	fw := setupFileWatcher(t)
	fw.RegisterClient("c1")
	fw.UpdateWatch("c1", "/dir1", "")

	fw.mu.Lock()
	result := fw.isPathWatchedByOthers("c1", "/dir1")
	fw.mu.Unlock()
	assert.False(t, result, "no other clients")
}

// ---------- cancelClientTimers ----------

func TestCancelClientTimers(t *testing.T) {
	fw := setupFileWatcher(t)
	fw.RegisterClient("c1")

	fw.mu.Lock()
	key1 := "c1|dir_change"
	key2 := "c1|file_change"
	key3 := "c2|dir_change"
	fw.debounceTimers[key1] = time.AfterFunc(5*time.Second, func() {})
	fw.debounceTimers[key2] = time.AfterFunc(5*time.Second, func() {})
	fw.debounceTimers[key3] = time.AfterFunc(5*time.Second, func() {})
	fw.debouncePending[key1] = WatchEvent{Type: "dir_change"}
	fw.debouncePending[key2] = WatchEvent{Type: "file_change"}
	fw.debouncePending[key3] = WatchEvent{Type: "dir_change"}
	fw.mu.Unlock()

	fw.mu.Lock()
	fw.cancelClientTimers("c1")
	fw.mu.Unlock()

	fw.mu.Lock()
	_, has1 := fw.debounceTimers[key1]
	_, has2 := fw.debounceTimers[key2]
	_, has3 := fw.debounceTimers[key3]
	fw.mu.Unlock()

	assert.False(t, has1, "c1|dir_change timer should be cancelled")
	assert.False(t, has2, "c1|file_change timer should be cancelled")
	assert.True(t, has3, "c2|dir_change timer should remain")
}

// ---------- fireDebouncedEvent ----------

func TestFireDebouncedEvent(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	fw.UpdateWatch("c1", dir, "")

	key := "c1|dir_change"
	we := WatchEvent{Type: "dir_change", Path: dir}

	fw.mu.Lock()
	fw.debouncePending[key] = we
	fw.mu.Unlock()

	fw.fireDebouncedEvent("c1", key)

	events := collectEvents(ch, 1, 300*time.Millisecond)
	assert.Len(t, events, 1)
	assert.Equal(t, "dir_change", events[0].Type)
	assert.Equal(t, dir, events[0].Path)

	// Pending should be cleared
	fw.mu.Lock()
	_, hasPending := fw.debouncePending[key]
	_, hasTimer := fw.debounceTimers[key]
	fw.mu.Unlock()
	assert.False(t, hasPending)
	assert.False(t, hasTimer)
}

func TestFireDebouncedEvent_NoPendingEvent(t *testing.T) {
	fw := setupFileWatcher(t)
	fw.RegisterClient("c1")

	// Should not panic
	fw.fireDebouncedEvent("c1", "c1|dir_change")
}

func TestFireDebouncedEvent_ClientRemoved(t *testing.T) {
	fw := setupFileWatcher(t)
	fw.RegisterClient("c1")

	key := "c1|dir_change"
	fw.mu.Lock()
	fw.debouncePending[key] = WatchEvent{Type: "dir_change", Path: "/test"}
	fw.mu.Unlock()

	fw.UnregisterClient("c1")

	// Should not panic
	fw.fireDebouncedEvent("c1", key)
}

func TestFireDebouncedEvent_ChannelFull(t *testing.T) {
	fw := setupFileWatcher(t)
	fw.RegisterClient("c1")

	dir := t.TempDir()
	fw.UpdateWatch("c1", dir, "")

	// Access the channel directly from the client struct
	fw.mu.Lock()
	ch := fw.clients["c1"].pushCh
	fw.mu.Unlock()

	// Fill the channel
	for range watchPushChSize {
		ch <- WatchEvent{Type: "dir_change", Path: dir}
	}

	key := "c1|dir_change"
	fw.mu.Lock()
	fw.debouncePending[key] = WatchEvent{Type: "dir_change", Path: dir}
	fw.mu.Unlock()

	// Should not block or panic — event is dropped
	fw.fireDebouncedEvent("c1", key)
}

// ---------- Integration: real fsnotify events ----------

func TestFileWatcher_RealDirChangeEvent(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	fw.UpdateWatch("c1", dir, "")

	// Wait for watch to be established
	time.Sleep(100 * time.Millisecond)

	// Create a new file in the directory — this should trigger a Create event on the dir
	newFile := filepath.Join(dir, "newfile.txt")
	err := os.WriteFile(newFile, []byte("test"), 0o644)
	assert.NoError(t, err)

	events := collectEvents(ch, 1, 2*time.Second)
	if assert.Len(t, events, 1) {
		assert.Equal(t, "dir_change", events[0].Type)
	}
}

func TestFileWatcher_RealFileWriteEvent(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	err := os.WriteFile(file, []byte("initial"), 0o644)
	assert.NoError(t, err)

	fw.UpdateWatch("c1", dir, file)

	// Wait for watch to be established
	time.Sleep(100 * time.Millisecond)

	// Modify the file
	err = os.WriteFile(file, []byte("modified"), 0o644)
	assert.NoError(t, err)

	events := collectEvents(ch, 1, 2*time.Second)
	if assert.Len(t, events, 1) {
		assert.Equal(t, "file_change", events[0].Type)
	}
}

func TestFileWatcher_DebounceRealEvents(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	err := os.WriteFile(file, []byte("initial"), 0o644)
	assert.NoError(t, err)

	fw.UpdateWatch("c1", dir, file)

	time.Sleep(100 * time.Millisecond)

	// Rapid writes
	for i := range 5 {
		os.WriteFile(file, []byte("modify"+string(rune('0'+i))), 0o644)
	}

	// Wait for debounce to settle
	time.Sleep(500 * time.Millisecond)

	events := collectEvents(ch, 10, 300*time.Millisecond)
	// Should get at most a few events, not 5 separate ones
	assert.LessOrEqual(t, len(events), 3, "rapid writes should be debounced")
}

func TestFileWatcher_MultipleClientsRealEvents(t *testing.T) {
	fw := setupFileWatcher(t)
	ch1 := fw.RegisterClient("c1")
	ch2 := fw.RegisterClient("c2")

	dir := t.TempDir()
	file := filepath.Join(dir, "shared.txt")
	_ = os.WriteFile(file, []byte("hello"), 0o644)

	fw.UpdateWatch("c1", dir, file)
	fw.UpdateWatch("c2", dir, file)

	time.Sleep(100 * time.Millisecond)

	// Modify the file
	_ = os.WriteFile(file, []byte("world"), 0o644)

	events1 := collectEvents(ch1, 1, 2*time.Second)
	events2 := collectEvents(ch2, 1, 2*time.Second)

	assert.Len(t, events1, 1)
	assert.Len(t, events2, 1)
	assert.Equal(t, "file_change", events1[0].Type)
	assert.Equal(t, "file_change", events2[0].Type)
}

func TestFileWatcher_UpdateWatchSwitchesFile(t *testing.T) {
	fw := setupFileWatcher(t)
	ch := fw.RegisterClient("c1")

	dir := t.TempDir()
	file1 := filepath.Join(dir, "a.txt")
	file2 := filepath.Join(dir, "b.txt")
	_ = os.WriteFile(file1, []byte("a"), 0o644)
	_ = os.WriteFile(file2, []byte("b"), 0o644)

	fw.UpdateWatch("c1", dir, file1)
	time.Sleep(100 * time.Millisecond)

	// Switch to watching file2
	fw.UpdateWatch("c1", dir, file2)
	time.Sleep(100 * time.Millisecond)

	// Modify file2 — should trigger file_change
	_ = os.WriteFile(file2, []byte("b-modified"), 0o644)

	events := collectEvents(ch, 1, 2*time.Second)
	if assert.Len(t, events, 1) {
		assert.Equal(t, "file_change", events[0].Type)
		assert.Equal(t, file2, events[0].Path)
	}
}

// ---------- Concurrency safety ----------

func TestFileWatcher_ConcurrentRegisterUnregister(t *testing.T) {
	fw := setupFileWatcher(t)

	var done atomic.Int32
	for i := range 20 {
		id := string(rune('a' + i))
		go func() {
			ch := fw.RegisterClient(id)
			dir := t.TempDir()
			fw.UpdateWatch(id, dir, "")
			time.Sleep(50 * time.Millisecond)
			fw.UnregisterClient(id)
			// Channel should be closed
			<-ch
			done.Add(1)
		}()
	}

	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, int32(20), done.Load())
}

// TestStopFileWatcher_DoesNotCloseWatcherBeforeTakingLock pins the ordering
// inside StopFileWatcher: the fsnotify watcher is closed only AFTER fw.mu has
// been acquired and the client map drained.
//
// The reverse order deadlocks on Windows. fsnotify's Windows backend implements
// Remove by queueing a request and blocking on a reply channel that its reader
// goroutine fills; once the watcher is closed that goroutine is gone, so Remove
// blocks forever. UnregisterClient calls Remove while holding fw.mu, so a
// StopFileWatcher that closed the watcher first and then waited for fw.mu hung
// behind it. Because StopFileWatcher only runs at process shutdown, the visible
// symptom was the whole server never exiting — the Windows CI run reported it as
// "test timed out after 10m0s" with one goroutine parked in
// StopFileWatcher -> sync.Mutex.Lock and a peer parked in
// UnregisterClient -> fsnotify Remove.
//
// The deadlock needs the Windows backend, but the ordering that causes it is
// observable everywhere: hold fw.mu (standing in for an UnregisterClient inside
// Remove), start StopFileWatcher, and check whether the watcher is already
// closed. With the correct order it is not — the teardown is still waiting for
// the mutex — whereas the old order closed it before ever taking the lock.
func TestStopFileWatcher_DoesNotCloseWatcherBeforeTakingLock(t *testing.T) {
	orig := GlobalFileWatcher
	defer func() { GlobalFileWatcher = orig }()

	require.NoError(t, InitFileWatcher())
	fw := GlobalFileWatcher
	require.NotNil(t, fw)

	dir := t.TempDir()
	fw.RegisterClient("holder")
	fw.UpdateWatch("holder", dir, "")

	// Stand in for an UnregisterClient that is inside watcher.Remove.
	fw.mu.Lock()

	finished := make(chan struct{})
	go func() {
		StopFileWatcher()
		close(finished)
	}()

	// Let the teardown goroutine run up to its lock acquisition.
	time.Sleep(100 * time.Millisecond)

	// While the lock is held the watcher must still be open. Add reports
	// ErrClosed once Close has run, so this distinguishes "Close happens after
	// the drain" (correct) from "Close happens first" (deadlocks on Windows).
	probe := t.TempDir()
	err := fw.watcher.Add(probe)
	assert.NotErrorIs(t, err, fsnotify.ErrClosed,
		"watcher was closed before fw.mu was acquired; on Windows that deadlocks "+
			"against UnregisterClient -> fsnotify Remove, which also runs under fw.mu")

	fw.mu.Unlock()

	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("StopFileWatcher did not finish after the lock was released")
	}
}

// ---------- WatchEvent struct ----------

func TestWatchEvent_JSONMarshal(t *testing.T) {
	we := WatchEvent{Type: "file_change", Path: "/test/file.txt"}
	data, err := json.Marshal(we)
	assert.NoError(t, err)

	var decoded WatchEvent
	err = json.Unmarshal(data, &decoded)
	assert.NoError(t, err)
	assert.Equal(t, "file_change", decoded.Type)
	assert.Equal(t, "/test/file.txt", decoded.Path)
}

func TestWatchEvent_Fields(t *testing.T) {
	we := WatchEvent{Type: "dir_change", Path: "/project"}
	assert.Equal(t, "dir_change", we.Type)
	assert.Equal(t, "/project", we.Path)
}
