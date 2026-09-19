package handler

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"clawbench/internal/model"
	"clawbench/internal/service"

	"github.com/coder/websocket"
)

const (
	// watchWriteTimeout bounds a single WebSocket write (matches ttsWSWriteTimeout).
	watchWriteTimeout = 5 * time.Second

	// watchReadIdleTimeout closes a connection that sends nothing for this long.
	// The client answers the server ping every watchPingInterval, so in practice
	// this only fires for a half-open socket the client has not noticed yet.
	watchReadIdleTimeout = 10 * time.Minute

	// maxWatchClients caps concurrent file-watch connections. The service layer
	// has no cap of its own, and each connection holds up to two inotify
	// watches, so the ceiling is enforced here (mirrors ws.maxSubscriptions).
	maxWatchClients = 20

	// watchMsgWatch re-targets the connection's watched dir/file.
	watchMsgWatch = "watch"
	// watchMsgPong answers a server ping (liveness only).
	watchMsgPong = "pong"
	// watchMsgConnected is sent once after upgrade with the connection's ID.
	watchMsgConnected = "connected"
	// watchMsgPing is the server liveness probe.
	watchMsgPing = "ping"
	// watchMsgError reports a rejected control message without closing the socket.
	watchMsgError = "error"
)

// watchPingInterval is how often the server probes liveness. A var rather than
// a const so tests can shrink it. 30s matches the chat handler's cadence.
var watchPingInterval = 30 * time.Second

// watchClientCount tracks live connections for the maxWatchClients ceiling.
var watchClientCount atomic.Int64

// fileWatchMessage is a client → server control message.
type fileWatchMessage struct {
	Type string `json:"type"`
	Dir  string `json:"dir"`
	File string `json:"file"`
	// Files are additional project-relative paths whose content changes should
	// be reported as file_change — the images a preview is currently rendering.
	// Optional and additive: older clients simply omit it.
	Files []string `json:"files"`
}

// fileWatchServerMsg is a server → client control message. Event frames are
// marshaled straight from service.WatchEvent instead (it already carries
// {"type","path"}), so the frontend sees the same shape it did over SSE.
type fileWatchServerMsg struct {
	Type     string `json:"type"`
	ClientID string `json:"clientId,omitempty"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
}

// newWatchClientID generates a random client ID for file watch connections.
func newWatchClientID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if random generation fails
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// resolveWatchPaths resolves and validates the watch targets against the
// project root. It is a pure twin of validateAndResolvePath: that helper writes
// an HTTP error, which is illegal once the connection has been upgraded, so the
// post-upgrade path needs a variant that only reports failure.
//
// An empty dirRel means the project root itself.
func resolveWatchPaths(projectPath, dirRel, fileRel string) (dirAbs, fileAbs string, ok bool) {
	if dirRel == "" {
		dirAbs = projectPath
	} else {
		abs, valid := model.ValidatePath(projectPath, dirRel)
		if !valid {
			return "", "", false
		}
		dirAbs = abs
	}
	if fileRel != "" {
		abs, valid := model.ValidatePath(projectPath, fileRel)
		if !valid {
			return "", "", false
		}
		fileAbs = abs
	}
	return dirAbs, fileAbs, true
}

// resolveWatchMediaPaths resolves the client's extra media-file watch targets
// against the project root. Paths that are invalid or escape the project are
// dropped rather than failing the whole frame: a single stale image reference
// must not break the file watcher for the open file.
//
// The list is capped at service.MaxMediaWatchPaths; each distinct parent
// directory costs one inotify watch, so an unbounded list would let one client
// exhaust the process-wide limit.
func resolveWatchMediaPaths(projectPath string, rels []string) []string {
	if len(rels) == 0 {
		return nil
	}
	if len(rels) > service.MaxMediaWatchPaths {
		rels = rels[:service.MaxMediaWatchPaths]
	}
	out := make([]string, 0, len(rels))
	seen := make(map[string]struct{}, len(rels))
	for _, rel := range rels {
		if rel == "" {
			continue
		}
		abs, ok := model.ValidatePath(projectPath, rel)
		if !ok {
			continue
		}
		if _, dup := seen[abs]; dup {
			continue
		}
		seen[abs] = struct{}{}
		out = append(out, abs)
	}
	return out
}

// FileWatchWS handles GET /api/file/watch/ws — WebSocket stream of file system
// change notifications. Auth is handled by middleware.Auth before this function
// is called.
//
// This replaced an SSE endpoint: a resident EventSource permanently consumes one
// of the browser's 6 HTTP/1.1 connections per origin, starving parallel REST
// requests on plain-HTTP deployments. A WebSocket is removed from that pool once
// upgraded to 101, so it does not compete.
//
// Protocol:
//   - Query params dir/file set the initial watch target. They are resolved and
//     validated BEFORE the upgrade so path-traversal still yields HTTP 403.
//   - Client sends: {"type":"watch","dir":"<rel>","file":"<rel>",
//     "files":["<rel>",…]} to re-target, and {"type":"pong"} in reply to a
//     server ping. `files` lists extra files whose content changes must be
//     reported (the images a preview currently renders); invalid entries are
//     dropped individually rather than rejecting the frame.
//   - Server sends: {"type":"connected","clientId":"..."} once, then
//     {"type":"dir_change"|"file_change","path":"..."} events,
//     {"type":"ping"} every watchPingInterval, and {"type":"error",...} for a
//     rejected control message (which never closes the socket).
//
// The open file and the media files are watched through their PARENT
// DIRECTORIES, not as direct file watches: an atomic save (write temp + rename)
// replaces the inode, which silently kills a direct file watch.
func FileWatchWS(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	projectPath, ok := requireProject(w, r)
	if !ok {
		return
	}

	fw := service.GlobalFileWatcher
	if fw == nil {
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "FileWatcherNotAvailable")
		return
	}

	// Validate the initial watch target before upgrading so traversal keeps
	// returning HTTP 403 rather than becoming a socket-level error frame.
	dirAbs, fileAbs, ok := resolveWatchPaths(projectPath, r.URL.Query().Get("dir"), r.URL.Query().Get("file"))
	if !ok {
		writeLocalizedError(w, r, model.Forbidden(nil, "AccessDenied"))
		return
	}

	if watchClientCount.Load() >= maxWatchClients {
		slog.Warn("file watch ws: connection limit reached", slog.Int64("limit", maxWatchClients))
		writeLocalizedErrorf(w, r, http.StatusServiceUnavailable, "FileWatcherNotAvailable")
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{
			"http://" + r.Host,
			"https://" + r.Host,
			"http://localhost:*",
			"https://localhost:*",
			"http://127.0.0.1:*",
			"https://127.0.0.1:*",
		},
	})
	if err != nil {
		slog.Error("file watch ws: accept failed", slog.String("error", err.Error()))
		return
	}
	defer func() { _ = conn.CloseNow() }()

	watchClientCount.Add(1)
	defer watchClientCount.Add(-1)

	clientID := newWatchClientID()
	pushCh := fw.RegisterClient(clientID)
	defer fw.UnregisterClient(clientID)
	fw.UpdateWatch(clientID, dirAbs, fileAbs)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Two goroutines write (the read loop sends error frames, the main loop
	// sends events and pings), so writes are serialized. coder/websocket permits
	// one concurrent reader plus one concurrent writer.
	var writeMu sync.Mutex

	if err := writeWatchWSJSON(conn, &writeMu, fileWatchServerMsg{Type: watchMsgConnected, ClientID: clientID}); err != nil {
		slog.Warn("file watch ws: failed to send connected frame", slog.String("clientId", clientID), slog.String("error", err.Error()))
		return
	}

	go readWatchWSMessages(ctx, conn, &writeMu, fw, clientID, projectPath, cancel)

	ping := time.NewTicker(watchPingInterval)
	defer ping.Stop()

	slog.Debug(
		"file watch ws connected",
		slog.String("clientId", clientID),
		slog.String("dir", dirAbs),
		slog.String("file", fileAbs),
	)

	for {
		select {
		case event, ok := <-pushCh:
			if !ok {
				// Channel closed — watcher shutting down.
				return
			}
			if err := writeWatchWSJSON(conn, &writeMu, event); err != nil {
				// A failed event write means the peer is gone; stop rather than
				// waiting for the next ping to notice.
				slog.Debug("file watch ws: event write failed", slog.String("clientId", clientID), slog.String("error", err.Error()))
				return
			}

		case <-ping.C:
			if err := writeWatchWSJSON(conn, &writeMu, fileWatchServerMsg{Type: watchMsgPing}); err != nil {
				// The connection is dead. Close it so the client's onclose fires
				// and it reconnects; returning silently would leave a connection
				// that looks alive but never pings again.
				slog.Warn("file watch ws: ping write failed, closing connection", slog.String("clientId", clientID))
				_ = conn.CloseNow()
				return
			}

		case <-ctx.Done():
			return
		}
	}
}

// readWatchWSMessages reads client control messages until the connection dies
// or ctx is cancelled. It runs until error rather than returning a value.
//
// It calls cancel on exit so the main loop's select unblocks and the handler
// returns — otherwise a client that vanishes without a final frame would leave
// the handler (and its deferred UnregisterClient) parked on the push channel
// until the next fsnotify event.
func readWatchWSMessages(
	ctx context.Context,
	conn *websocket.Conn,
	writeMu *sync.Mutex,
	fw *service.FileWatcher,
	clientID, projectPath string,
	cancel context.CancelFunc,
) {
	defer cancel()
	for {
		// Derive from ctx so the deferred cancel() unblocks a pending read when
		// the handler returns. The timeout guards a half-open socket.
		readCtx, readCancel := context.WithTimeout(ctx, watchReadIdleTimeout)
		_, data, err := conn.Read(readCtx)
		readCancel()
		if err != nil {
			slog.Debug("file watch ws: client disconnected", slog.String("clientId", clientID), slog.String("error", err.Error()))
			return
		}

		var msg fileWatchMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			// Malformed frames are ignored, not fatal — a bad message must not
			// tear down a healthy channel.
			slog.Warn("file watch ws: invalid client message", slog.String("clientId", clientID), slog.String("error", err.Error()))
			continue
		}

		switch msg.Type {
		case watchMsgPong:
			// Liveness only.
		case watchMsgWatch:
			dirAbs, fileAbs, ok := resolveWatchPaths(projectPath, msg.Dir, msg.File)
			if !ok {
				if err := writeWatchWSJSON(conn, writeMu, fileWatchServerMsg{
					Type:    watchMsgError,
					Code:    "AccessDenied",
					Message: "watch path escapes the project root",
				}); err != nil {
					slog.Debug("file watch ws: error-frame write failed", slog.String("clientId", clientID), slog.String("error", err.Error()))
					return
				}
				continue
			}
			fw.UpdateWatch(clientID, dirAbs, fileAbs, resolveWatchMediaPaths(projectPath, msg.Files)...)
		default:
			slog.Warn("file watch ws: unknown client message type", slog.String("clientId", clientID), slog.String("type", msg.Type))
		}
	}
}

// writeWatchWSJSON marshals v and writes it as one text frame. Callers decide
// what a write failure means for them: the ping and event paths treat it as a
// dead peer, the error path just gives up on the frame.
func writeWatchWSJSON(conn *websocket.Conn, mu *sync.Mutex, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	writeCtx, writeCancel := context.WithTimeout(context.Background(), watchWriteTimeout)
	defer writeCancel()
	return conn.Write(writeCtx, websocket.MessageText, data)
}
