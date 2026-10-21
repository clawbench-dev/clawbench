package handler

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"

	"clawbench/internal/version"
)

// instanceID identifies THIS process, not the build.
//
// The settings page needs it to tell "the server came back" apart from "the
// server never went away". Polling for any 200 was not enough: the outgoing
// process keeps answering normally until srv.Shutdown (step 4 of the shutdown
// sequence, after the stream-finalize grace), so a poll that fires 2s after the
// restart request hits the OLD process, sees 200, and reloads onto the same
// build. The version string cannot serve as the identity either — a restart
// without an upgrade legitimately keeps the same version.
var instanceID = newInstanceID()

// instanceStartedAt is this process's start time, exposed for diagnostics.
var instanceStartedAt = time.Now()

func newInstanceID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Identity only needs to be unique among the processes a browser will
		// ever talk to; a timestamp is a fine last resort.
		return "t" + time.Now().Format("20060102150405.000000000")
	}
	return hex.EncodeToString(b)
}

// ServeServerInstance handles GET /api/server/instance.
//
// Deliberately tiny: the settings page polls it every 2s while waiting for a
// restart, so it must stay cheap and must not touch the DB or the config file.
func ServeServerInstance(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"instance_id": instanceID,
		"started_at":  instanceStartedAt.UTC().Format(time.RFC3339Nano),
		"version":     version.Get(),
	})
}
