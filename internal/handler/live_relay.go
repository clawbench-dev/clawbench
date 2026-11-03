package handler

// Relay-side enforcement of the audience speech rules.
//
// The room model (internal/liveshare) is transport-agnostic: it records who MAY
// speak and who IS speaking. In p2p mode that is the whole story, because the
// publisher's peer connection only carries audio from a viewer that put a track
// on it. In relay (sfu) mode the browsers publish to LiveKit directly, so the
// room's opinion has no force of its own: a viewer whose LiveKit permission
// still says "may publish" keeps sending audio no matter what the roster shows.
//
// Every path that changes a viewer's PERMISSION therefore pushes the new value
// to LiveKit as well. The permission is not "is speaking" — it is "may speak",
// and it is exactly what the join token grants (see joinedViewerMessage):
//
//	canPublish = room talk switch && not individually muted
//
// The viewer's own microphone toggle (talk-start / talk-end), the host's
// talk-revoke and mute-all all sit INSIDE that permission and change nothing
// about it: they tell the browser to stop its track, exactly as in p2p mode.
// That is deliberate — mute-all silences the room for now, it does not take
// speech away, and a viewer must be able to speak again afterwards. Only the
// room switch, the per-viewer mute and removal are permission changes.
//
// Everything here is best-effort: a slow or unreachable relay must not fail,
// stall or reorder the control frames that carry the meeting itself. Failures
// are logged, and the room state (which every UI reads) stays authoritative.

import (
	"context"
	"log/slog"
	"time"

	"clawbench/internal/liveshare"
)

// relayCallTimeout bounds one control-plane call. Long enough for a slow link,
// short enough that a black-holed relay cannot hold the publisher's control
// loop: in relay mode that loop carries no SDP/ICE (the browsers exchange media
// with LiveKit), so the only thing waiting behind a slow call is the host's own
// next click.
const relayCallTimeout = 5 * time.Second

// relayViewerIdentity is a viewer's identity inside LiveKit.
//
// Mirrors the prefix joinedViewerMessage signs into the token; the two must
// agree, or every permission call names a participant that does not exist and
// fails with a 404 the host never sees.
func relayViewerIdentity(viewerID string) string {
	return "viewer:" + viewerID
}

// syncRelayPublish pushes one viewer's effective publish permission.
//
// No-op for a p2p room, and that check comes first: a room that never had a
// relay must not pay for (or log) a call it can never make.
func syncRelayPublish(room *liveshare.Room, viewerID string, canPublish bool) {
	if !room.UseRelay() {
		return
	}
	cfg := room.LiveKit()
	ctx, cancel := context.WithTimeout(context.Background(), relayCallTimeout)
	defer cancel()
	if err := cfg.UpdateParticipantPermission(ctx, room.Token(), relayViewerIdentity(viewerID), canPublish); err != nil {
		slog.Warn("liveshare: relay permission sync failed",
			"viewer", viewerID, "canPublish", canPublish, "err", err)
	}
}

// syncRelayViewer re-derives one viewer's permission from the room and pushes
// it. Callers have already applied their change to the room, so reading it back
// is what keeps the formula in one place instead of at every call site.
func syncRelayViewer(room *liveshare.Room, viewerID string) {
	if !room.UseRelay() {
		return
	}
	canPublish := room.TalkEnabled()
	if v := room.Viewer(viewerID); v != nil && v.Muted() {
		canPublish = false
	}
	syncRelayPublish(room, viewerID, canPublish)
}

// removeRelayViewer hangs up a kicked viewer's relay connection.
//
// The room already dropped the member and closed its ClawBench socket, so the
// page is gone; without this the browser's LiveKit connection would linger
// until its own timeout, and the host's participant list would keep a ghost.
func removeRelayViewer(room *liveshare.Room, viewerID string) {
	if !room.UseRelay() {
		return
	}
	cfg := room.LiveKit()
	ctx, cancel := context.WithTimeout(context.Background(), relayCallTimeout)
	defer cancel()
	if err := cfg.RemoveParticipant(ctx, room.Token(), relayViewerIdentity(viewerID)); err != nil {
		slog.Warn("liveshare: relay participant removal failed", "viewer", viewerID, "err", err)
	}
}
