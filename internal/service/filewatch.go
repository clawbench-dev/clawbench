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
	pushCh   chan WatchEvent
}

// FileWatcher manages per-connection fsnotify watchers with debounce.
// Singleton pattern — initialized once via InitFileWatcher().
type FileWatcher struct {
	mu      sync.Mutex
	watcher *fsnotify.Watcher
	clients map[string]*watchClient // keyed by clientID
	done    chan struct{}

	// Debounce timers: key = clientID+"|"+eventType, value = *time.Timer
	debounceTimers map[string]*time.Timer
	// Pending debounce events: key = clientID+"|"+eventType, value = the event to fire
	debouncePending map[string]WatchEvent
}

// GlobalFileWatcher is the global singleton, initialized from main.go.
var GlobalFileWatcher *FileWatcher

const (
	watchPushChSize = 16
	watchDebounceMs = 200
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
		pushCh: ch,
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

	// Record paths before removing client
	oldDir := client.dirPath
	oldFile := client.filePath

	close(client.pushCh)
	delete(fw.clients, clientID)

	// Unwatch paths if no other client needs them
	if oldDir != "" && !fw.isPathWatchedByOthers(clientID, oldDir) {
		_ = fw.watcher.Remove(oldDir)
	}
	if oldFile != "" && !fw.isPathWatchedByOthers(clientID, oldFile) {
		_ = fw.watcher.Remove(oldFile)
	}
}

// UpdateWatch changes the watched paths for a client.
// Diffs old vs new paths, adding/removing fsnotify watches as needed.
func (fw *FileWatcher) UpdateWatch(clientID, dirPath, filePath string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	client, ok := fw.clients[clientID]
	if !ok {
		slog.Debug("UpdateWatch: client not found", slog.String("clientId", clientID))
		return
	}

	oldDir := client.dirPath
	oldFile := client.filePath

	slog.Debug(
		"UpdateWatch",
		slog.String("clientId", clientID),
		slog.String("oldDir", oldDir),
		slog.String("oldFile", oldFile),
		slog.String("newDir", dirPath),
		slog.String("newFile", filePath),
	)

	// Update client state
	client.dirPath = dirPath
	client.filePath = filePath

	// Cancel debounce timers for this client (old paths are stale)
	fw.cancelClientTimers(clientID)

	// Diff directory watch
	if oldDir != dirPath {
		if oldDir != "" && !fw.isPathWatchedByOthers(clientID, oldDir) {
			_ = fw.watcher.Remove(oldDir)
		}
		if dirPath != "" {
			if err := fw.watcher.Add(dirPath); err != nil {
				slog.Warn(
					"failed to watch directory",
					slog.String("path", dirPath),
					slog.String("err", err.Error()),
				)
			}
		}
	}

	// Diff file watch
	if oldFile != filePath {
		if oldFile != "" && !fw.isPathWatchedByOthers(clientID, oldFile) {
			_ = fw.watcher.Remove(oldFile)
		}
		if filePath != "" {
			if err := fw.watcher.Add(filePath); err != nil {
				slog.Warn(
					"failed to watch file",
					slog.String("path", filePath),
					slog.String("err", err.Error()),
				)
			}
		}
	}
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

// isPathWatchedByOthers checks if any client other than excludeID is watching the given path.
// Must be called with fw.mu held.
func (fw *FileWatcher) isPathWatchedByOthers(excludeID, absPath string) bool {
	for id, c := range fw.clients {
		if id == excludeID {
			continue
		}
		if c.dirPath == absPath || c.filePath == absPath {
			return true
		}
	}
	return false
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

		// Match file watch FIRST: Write/Create/Rename mean file content changed;
		// Remove means the file was deleted (viewer should close).
		// File match takes priority over directory match because the client explicitly
		// opened this file and wants content updates, not just listing changes.
		if client.filePath != "" && absPath == client.filePath {
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove) {
				eventType = "file_change"
			}
		}

		// Match directory watch: Create/Remove/Rename affect directory listing.
		// When a file is created/removed inside a watched directory, fsnotify
		// reports the event on the CHILD path (e.g., /dir/newfile.txt), not
		// the directory itself. So we check both exact match and child match.
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

		// Debounce: reset timer for this client+event type
		debounceKey := clientID + "|" + eventType
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
