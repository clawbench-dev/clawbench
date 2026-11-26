package live

import "clawbench/internal/plugins"

// MountLiveRoutes 挂载 live 功能的全部 HTTP 接口。
//
// 由 plugins 注册表驱动：handler.RegisterRoutes 构造好 Mount 之后遍历
// plugins.All()，调到本函数（见 handler/mount_builtin.go）。
//
// 注入的 m.Auth / m.Public 已经把鉴权中间件、功能门控、路由表登记都包好了，
// 这里只写 pattern 与 handler。
//
// 这一组接口原先写在 handler.go 的 RegisterRoutes 里。搬出来之后，删掉
// live 功能的代价是"删掉 internal/plugin/live/ 整个目录"，不必再去
// 那张巨型路由表里一片片找。
func MountLiveRoutes(m plugins.Mount) {
	// Live screen/audio share. Management is authenticated (the publisher must
	// own the project); the viewer socket is public because the share token IS
	// the credential, exactly like the file/session share endpoints above.
	m.Auth("/api/live-share", ServeLiveShareManage)
	m.Auth("/api/live-share/publish/ws", LiveSharePublishWS)
	// 中继服务 (LiveKit) endpoint + credential. Installation-wide, not per
	// meeting: the create dialog edits it, every meeting that opts in uses it.
	// The secret is write-only — see handler/live_sfu.go.
	m.Auth("/api/live-share/sfu", ServeLiveSFUConfig)
	m.Auth("/api/live-share/sfu/test", ServeLiveSFUTest)
	// Signaling proxy for a LOOPBACK relay. Public like the viewer socket: an
	// https page cannot open ws://, so it asks the origin instead, and the
	// LiveKit token it carries is the credential. Only 127.0.0.1 is ever
	// dialed — see handler/live_proxy.go.
	m.Public("/livekit/", ServeLiveKitProxy)
	// AI 听记: the publisher uploads one audio segment per channel, reads the
	// merged timeline back, and asks for the minutes once the share ends.
	m.Auth("/api/live-share/notes", ServeLiveShareNotes)
	m.Auth("/api/live-share/notes/segment", ServeLiveShareNoteSegment)
	m.Auth("/api/live-share/notes/stream", LiveNotesStreamWS)
	// Folds the per-segment audio into one file per channel plus the
	// whole-meeting mix; called once the upload queue has drained.
	m.Auth("/api/live-share/notes/audio/merge", ServeLiveNotesAudioMerge)
	m.Auth("/api/live-share/notes/minutes", ServeLiveShareNotesMinutes)
	// Chat is persisted as it happens (the publisher is the only writer): the
	// meeting record keeps it after the room is gone.
	m.Auth("/api/live-share/notes/chat", ServeLiveShareNoteChat)
	// Files opened during the meeting, as intervals. Same reasoning as chat:
	// the publisher reports them while the room is live, the record keeps them.
	m.Auth("/api/live-share/notes/files", ServeLiveShareNoteFiles)
	// Who was sharing the screen, as intervals. Recorded as transitions so the
	// timeline can never show two sharers at once.
	m.Auth("/api/live-share/notes/screen", ServeLiveShareNoteScreen)
	// Key frames captured by the publisher while a screen is on the wire: POST
	// uploads one, DELETE removes one, GET (below) lists them.
	m.Auth("/api/live-share/notes/shot", ServeLiveShareNoteShot)
	m.Auth("/api/live-share/notes/shots", ServeLiveNotesShots)
	// The owner's own copy of a frame. The public endpoint under /api/share
	// follows the record's publication switch; the owner's record must not need
	// it (their transcript showed these pictures while the room was open).
	m.Auth("/api/live-share/notes/shot/image", ServeLiveNoteShotImage)
	// Screen recording (picture + annotations + the meeting's audio mix). The
	// publisher appends ordered chunks while the switch is on; finish closes the
	// session. The file lives beside the other artifacts, so the record's
	// resource tab lists it with no extra table.
	m.Auth("/api/live-share/notes/recording", ServeLiveShareNoteRecording)
	m.Auth("/api/live-share/notes/recording/finish", ServeLiveShareNoteRecordingFinish)
	// Meeting records: GET lists the project's meetings (the database row is the
	// record, permanent), DELETE removes one with its folder.
	m.Auth("/api/live-share/notes/sessions", ServeLiveNotesSessions)
	// Whether this record's link stays readable after the meeting ends. Off by
	// default: the token is minted automatically for viewers to join, which is
	// not the same as publishing the archived record.
	m.Auth("/api/live-share/notes/sessions/share", ServeLiveNotesShare)
	// Clear every record of this project in one call. Its own path (not a query
	// flag on the single delete) because the two answer differently: one either
	// deletes or refuses, the other deletes what it can and reports the rest.
	m.Auth("/api/live-share/notes/sessions/all", ServeLiveNotesClear)
	m.Auth("/api/live-share/notes/sessions/artifact", ServeLiveNotesArtifact)
	m.Auth("/api/live-share/notes/sessions/chat", ServeLiveNotesChat)
	// Import an existing recording or video as a meeting: the file is decoded to
	// the same mono Opus track a solo recording produces, then the ordinary
	// transcript → proofread → minutes chain runs on it.
	m.Auth("/api/live-share/notes/import", ServeLiveNotesImport)
	// Regeneration: plan first (the confirmation sheet), then accept or cancel.
	// The plan endpoint is read-only; the POST recomputes the plan instead of
	// trusting the client's copy.
	m.Auth("/api/live-share/notes/regenerate/plan", ServeLiveShareNotesRegeneratePlan)
	m.Auth("/api/live-share/notes/regenerate/cancel", ServeLiveShareNotesRegenerateCancel)
	m.Auth("/api/live-share/notes/regenerate", ServeLiveShareNotesRegenerate)
}
