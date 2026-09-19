package service

import (
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// WatchEvent represents a file system change event pushed to SSE clients.
type WatchEvent struct {
	Type string `json:"type"` // "dir_change" | "file_change" | "error"
	Path string `json:"path"` // absolute path that changed
}

type watchClient struct {
	dirPath  string // absolute path of watched directory (may be "")
	filePath string // absolute path of watched file (may be "")
	// mediaPaths are additional files whose content changes must be reported
	// (images rendered in a preview). Stored absolute.
	mediaPaths map[string]struct{}
	// watchedDirs is the set of directories this client actually holds a watch
	// on, so a re-target releases exactly what it acquired.
	watchedDirs map[string]struct{}
	pushCh      chan WatchEvent
}

// FileWatcher manages per-connection fsnotify watchers with debounce.
// Singleton pattern — initialized once via InitFileWatcher().
type FileWatcher struct {
	mu      sync.Mutex
	watcher *fsnotify.Watcher
	clients map[string]*watchClient // keyed by clientID
	done    chan struct{}

	// watchedDirs refcounts how many clients need each directory watched.
	// fsnotify watches are per-inode and Add on an already-watched path is not
	// reliably idempotent across backends, so ownership is tracked explicitly:
	// the underlying watch is only removed when the last client releases it.
	watchedDirs map[string]int

	// Debounce timers: key = clientID+"|"+eventType (dir_change) or
	// clientID+"|file_change|"+absPath, value = *time.Timer
	debounceTimers map[string]*time.Timer
	// Pending debounce events: same key, value = the event to fire
	debouncePending map[string]WatchEvent
}

// GlobalFileWatcher is the global singleton, initialized from main.go.
var GlobalFileWatcher *FileWatcher

const (
	watchPushChSize = 16
	watchDebounceMs = 200

	// MaxMediaWatchPaths caps the extra files a single client may ask to watch
	// (images rendered in a preview). Each distinct parent directory costs one
	// inotify watch, so an unbounded list would let one client exhaust the
	// process-wide limit.
	MaxMediaWatchPaths = 200
)

// InitFileWatcher creates the global FileWatcher and starts its event loop.
// Returns error if fsnotify watcher creation fails.
func InitFileWatcher() error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	fw := &FileWatcher{
		watcher:         w,
		clients:         make(map[string]*watchClient),
		done:            make(chan struct{}),
		watchedDirs:     make(map[string]int),
		debounceTimers:  make(map[string]*time.Timer),
		debouncePending: make(map[string]WatchEvent),
	}
	GlobalFileWatcher = fw
	go fw.eventLoop()
	slog.Info("file watcher initialized")
	return nil
}

// StopFileWatcher shuts down the FileWatcher, cleaning up all clients and timers.
// Safe to call multiple times.
func StopFileWatcher() {
	if GlobalFileWatcher == nil {
		return
	}
	fw := GlobalFileWatcher
	GlobalFileWatcher = nil

	// Stop the event loop first so no further debounce work is scheduled.
	close(fw.done)

	// Tear down client state under fw.mu, but close the fsnotify watcher only
	// AFTER releasing it.
	//
	// Every fsnotify Remove/Add runs under fw.mu, so acquiring the lock here
	// waits for any in-flight call to finish. Closing the watcher first races
	// with them: fsnotify's Windows backend checks isClosed(), then queues the
	// request and blocks on a reply that the (by then exiting) reader goroutine
	// never sends — so that caller never returns, and this function, waiting on
	// the same mutex, hangs with it. Draining the clients map under the lock
	// additionally means a later UnregisterClient finds no entry and skips its
	// Remove altogether.
	fw.mu.Lock()
	for id, c := range fw.clients {
		close(c.pushCh)
		delete(fw.clients, id)
	}
	fw.watchedDirs = make(map[string]int)
	for key, timer := range fw.debounceTimers {
		timer.Stop()
		delete(fw.debounceTimers, key)
	}
	fw.mu.Unlock()

	_ = fw.watcher.Close()
	slog.Info("file watcher stopped")
}

// RegisterClient creates a new watch client and returns its push channel.
// The caller must call UnregisterClient when done.
func (fw *FileWatcher) RegisterClient(clientID string) <-chan WatchEvent {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	ch := make(chan WatchEvent, watchPushChSize)
	fw.clients[clientID] = &watchClient{
		mediaPaths:  make(map[string]struct{}),
		watchedDirs: make(map[string]struct{}),
		pushCh:      ch,
	}
	return ch
}

// UnregisterClient removes a client, cancels its debounce timers,
// and removes fsnotify watches if no other client needs them.
func (fw *FileWatcher) UnregisterClient(clientID string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	client, ok := fw.clients[clientID]
	if !ok {
		return
	}

	// Cancel pending debounce timers for this client
	fw.cancelClientTimers(clientID)

	close(client.pushCh)
	delete(fw.clients, clientID)

	// Release every directory this client held; the refcount drops the
	// underlying watch only when the last holder goes away.
	for dir := range client.watchedDirs {
		fw.releaseDirLocked(dir)
	}
	client.watchedDirs = nil
}

// acquireDirLocked takes a new reference to dir and ensures fsnotify is
// watching it. It reports whether the directory is now held, so callers record
// only real acquisitions — a failed Add (e.g. the directory does not exist yet)
// stays retryable on the next UpdateWatch instead of being remembered as
// watched.
// Must be called with fw.mu held.
func (fw *FileWatcher) acquireDirLocked(dir string) bool {
	if dir == "" {
		return false
	}
	if err := fw.watcher.Add(dir); err != nil {
		slog.Debug("failed to watch directory", slog.String("path", dir), slog.String("err", err.Error()))
		return false
	}
	fw.watchedDirs[dir]++
	return true
}

// rearmDirLocked re-issues the fsnotify Add for a directory this client already
// holds, WITHOUT changing the refcount.
//
// inotify silently drops a watch when the watched directory is deleted or moved
// (IN_DELETE_SELF / IN_MOVE_SELF), and fsnotify prunes its own bookkeeping
// without telling us — so the refcount can claim a watch that no longer exists.
// Re-adding is idempotent (inotify returns the same descriptor via IN_MASK_ADD),
// so re-targeting doubles as a repair. A failure is logged, not fatal: the
// reference is retained because the client still wants the path, and the next
// re-target retries.
// Must be called with fw.mu held.
func (fw *FileWatcher) rearmDirLocked(dir string) {
	if dir == "" {
		return
	}
	if err := fw.watcher.Add(dir); err != nil {
		slog.Debug("failed to re-arm directory watch", slog.String("path", dir), slog.String("err", err.Error()))
	}
}

// releaseDirLocked drops one reference to dir, removing the fsnotify watch when
// the last reference goes away. Must be called with fw.mu held.
func (fw *FileWatcher) releaseDirLocked(dir string) {
	if dir == "" {
		return
	}
	n, ok := fw.watchedDirs[dir]
	if !ok {
		return
	}
	if n <= 1 {
		delete(fw.watchedDirs, dir)
		_ = fw.watcher.Remove(dir)
		return
	}
	fw.watchedDirs[dir] = n - 1
}

// UpdateWatch changes the watched paths for a client.
// Diffs old vs new paths, adding/removing fsnotify watches as needed.
//
// mediaPaths are additional files whose content changes should be reported as
// file_change (images rendered in a preview). Their PARENT DIRECTORIES are
// watched rather than the files themselves: an atomic save (write temp +
// rename) replaces the inode, which silently kills a direct file watch, and the
// same directory watch also covers the currently open file for the same reason.
func (fw *FileWatcher) UpdateWatch(clientID, dirPath, filePath string, mediaPaths ...string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	client, ok := fw.clients[clientID]
	if !ok {
		slog.Debug("UpdateWatch: client not found", slog.String("clientId", clientID))
		return
	}

	oldDir := client.dirPath
	oldFile := client.filePath
	oldWatched := client.watchedDirs
	if oldWatched == nil {
		oldWatched = make(map[string]struct{})
	}

	slog.Debug(
		"UpdateWatch",
		slog.String("clientId", clientID),
		slog.String("oldDir", oldDir),
		slog.String("oldFile", oldFile),
		slog.String("newDir", dirPath),
		slog.String("newFile", filePath),
		slog.Int("mediaPaths", len(mediaPaths)),
	)

	// Build the new set of directories that must be watched: the browsed
	// directory, the open file's parent, and each media file's parent.
	newWatched := make(map[string]struct{}, 2+len(mediaPaths))
	if dirPath != "" {
		newWatched[dirPath] = struct{}{}
	}
	if filePath != "" {
		newWatched[filepath.Dir(filePath)] = struct{}{}
	}
	newMedia := make(map[string]struct{}, len(mediaPaths))
	for _, p := range mediaPaths {
		if p == "" {
			continue
		}
		newMedia[p] = struct{}{}
		newWatched[filepath.Dir(p)] = struct{}{}
	}

	// Acquire before releasing so a directory shared by both the old and new
	// sets is never dropped and re-added (which would briefly lose events).
	// Only successfully acquired directories are recorded; a failed Add stays
	// out of the set so the next UpdateWatch retries it.
	held := make(map[string]struct{}, len(newWatched))
	for dir := range newWatched {
		if _, already := oldWatched[dir]; already {
			// Still held from the previous target: keep the reference and
			// re-issue the fsnotify Add so a watch inotify silently dropped
			// (directory deleted/recreated) is repaired.
			fw.rearmDirLocked(dir)
			held[dir] = struct{}{}
			continue
		}
		if fw.acquireDirLocked(dir) {
			held[dir] = struct{}{}
		}
	}
	for dir := range oldWatched {
		if _, keep := held[dir]; keep {
			continue
		}
		fw.releaseDirLocked(dir)
	}

	// Update client state
	client.dirPath = dirPath
	client.filePath = filePath
	client.mediaPaths = newMedia
	client.watchedDirs = held

	// Cancel debounce timers for this client (old paths are stale)
	fw.cancelClientTimers(clientID)
}

// cancelClientTimers stops and removes all debounce timers for a client.
// Must be called with fw.mu held.
func (fw *FileWatcher) cancelClientTimers(clientID string) {
	prefix := clientID + "|"
	for key, timer := range fw.debounceTimers {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			timer.Stop()
			delete(fw.debounceTimers, key)
			delete(fw.debouncePending, key)
		}
	}
}

// eventLoop reads fsnotify events and routes them to clients with debounce.
func (fw *FileWatcher) eventLoop() {
	for {
		select {
		case <-fw.done:
			return

		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}
			fw.handleFsEvent(event)

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("fsnotify error", slog.String("err", err.Error()))
		}
	}
}

// handleFsEvent processes a single fsnotify event, matching it to clients.
func (fw *FileWatcher) handleFsEvent(event fsnotify.Event) { //nolint:gocyclo // multi-event-type filesystem handler
	fw.mu.Lock()
	defer fw.mu.Unlock()

	absPath := event.Name

	for clientID, client := range fw.clients {
		var eventType string

		// Match a content change on the open file or on any registered media
		// file FIRST: Write/Create/Rename mean content changed; Remove means the
		// file was deleted. Both are reported as file_change — the client
		// decides what to do (the viewer closes the open file; a deleted image
		// is left to the browser's own error handling).
		//
		// These paths are matched by exact path even though the underlying
		// fsnotify watch is on their parent directory — fsnotify reports the
		// event against the child path, so the exact comparison still holds.
		contentChanged := absPath == client.filePath
		if !contentChanged {
			if _, ok := client.mediaPaths[absPath]; ok {
				contentChanged = true
			}
		}
		if contentChanged {
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove) {
				eventType = "file_change"
			}
		}

		// Match directory watch: Create/Remove/Rename affect directory listing.
		// When a file is created/removed inside a watched directory, fsnotify
		// reports the event on the CHILD path (e.g., /dir/newfile.txt), not
		// the directory itself. So we check both exact match and child match.
		// Only the browsed directory drives dir_change — the parent directories
		// watched for the open/media files are an implementation detail and must
		// not refresh the listing.
		// Only emit dir_change if no file_change was already matched (i.e., the
		// event is not for a specifically-watched file).
		if eventType == "" && client.dirPath != "" {
			dirMatch := false
			if absPath == client.dirPath {
				// Direct event on the directory (e.g., Rename of the dir itself)
				dirMatch = event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename)
			} else if strings.HasPrefix(absPath, client.dirPath+string(filepath.Separator)) {
				// Child event — file created/removed/renamed inside the directory
				dirMatch = event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename)
			}
			if dirMatch {
				eventType = "dir_change"
			}
		}

		if eventType == "" {
			continue
		}

		slog.Debug(
			"handleFsEvent matched",
			slog.String("clientId", clientID),
			slog.String("eventType", eventType),
			slog.String("absPath", absPath),
			slog.String("clientFilePath", client.filePath),
			slog.String("clientDirPath", client.dirPath),
		)

		// Debounce: reset timer for this client+event type. file_change is keyed
		// per path so two different images changing in the same window each get
		// their own event instead of one overwriting the other.
		debounceKey := clientID + "|" + eventType
		if eventType == "file_change" {
			debounceKey += "|" + absPath
		}
		if timer, exists := fw.debounceTimers[debounceKey]; exists {
			timer.Stop()
		}

		we := WatchEvent{
			Type: eventType,
			Path: absPath,
		}
		fw.debouncePending[debounceKey] = we

		fw.debounceTimers[debounceKey] = time.AfterFunc(watchDebounceMs*time.Millisecond, func() {
			fw.fireDebouncedEvent(clientID, debounceKey)
		})
	}
}

// fireDebouncedEvent is called when a debounce timer expires.
// It pushes the pending event to the client's channel.
func (fw *FileWatcher) fireDebouncedEvent(clientID, debounceKey string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	we, ok := fw.debouncePending[debounceKey]
	if !ok {
		return
	}
	delete(fw.debouncePending, debounceKey)
	delete(fw.debounceTimers, debounceKey)

	client, ok := fw.clients[clientID]
	if !ok {
		return
	}

	select {
	case client.pushCh <- we:
		slog.Debug(
			"file watch event pushed",
			slog.String("clientId", clientID),
			slog.String("type", we.Type),
			slog.String("path", we.Path),
		)
	default:
		// Channel full — drop event (client will get the next one)
		slog.Debug(
			"file watch push channel full, dropping event",
			slog.String("clientId", clientID),
			slog.String("type", we.Type),
		)
	}
}
