package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"clawbench/internal/ai"
	"clawbench/internal/service"
	"clawbench/internal/store"
)

// Errors of a re-bind request (issue #685), mapped to distinct statuses so the
// client can say what went wrong instead of a generic failure.
var (
	// errMoveTargetInvalid: the path is not an EXISTING registered project.
	errMoveTargetInvalid = errors.New("move target is not an existing project")
	// errMoveNotAllowed: a task execution or a hidden group-member row (which
	// only moves together with its group) cannot be a move target.
	errMoveNotAllowed = errors.New("this session cannot be moved")
	// errMoveSessionRunning: the conversation is producing right now.
	errMoveSessionRunning = errors.New("session is running")
	// errMoveTruncatedStream: a reply is still being written, so its message
	// rows are mid-stream and their project attribution would be split.
	errMoveTruncatedStream = errors.New("session has an unfinished message")
)

// moveSessionToProject re-binds a conversation (issue #685) from the project it
// was created under to another EXISTING project, keeping all of its history.
// Group chats move as a whole (group row + every member row); single chats move
// on their own.
//
// The target is resolved by resolveMoveTarget and the "is anything still
// writing?" guards live in ensureSessionIdle, so this function is the order in
// which they run.
//
// The row changes live in service.MoveSessionToProject (including clearing the
// native session id so the next turn re-creates the agent session under the new
// directory instead of resuming one bound to the old one). What this layer must
// also do is close the cached agent connections: they were spawned with the OLD
// directory as their working directory and pin that for their whole lifetime,
// so leaving them alive would keep the agent working in the project the user
// just moved the conversation away from.
func moveSessionToProject(sessionID, projectPath string) error {
	canon, targetID, err := resolveMoveTarget(projectPath)
	if err != nil {
		return err
	}
	if idleErr := ensureSessionIdle(sessionID); idleErr != nil {
		return idleErr
	}
	moved, err := service.MoveSessionToProject(sessionID, canon, targetID)
	if err != nil {
		return err
	}
	if !moved {
		return errMoveNotAllowed
	}
	// Close the cached agent processes. Safe because the guards above refuse a
	// session that is producing; the next turn re-spawns under the new working
	// directory. Async, matching the other close sites: CloseConn may block on
	// cmd.Wait(). closeGroupMemberConns covers the group's member rows (the
	// group row itself has no connection).
	if service.IsGroupSession(sessionID) {
		closeGroupMemberConns(sessionID)
	} else {
		go ai.GetACPConnManager().CloseConn(sessionID)
	}
	return nil
}

// resolveMoveTarget turns the requested path into a registered project that is
// still usable on disk.
//
// An "existing" project is one already registered in the projects table: the
// caller picks from a list of them, so an unknown path is a stale/mistyped
// request, not a request to create a project. The directory itself is checked
// too: the picker hides projects whose directory is gone, but it can vanish
// between the list request and this one, and filing the conversation under a
// missing directory would give the next turn a working directory that does not
// exist.
func resolveMoveTarget(projectPath string) (string, int64, error) {
	canon := store.NormalizeProjectPath(projectPath)
	if canon == "" {
		return "", 0, errMoveTargetInvalid
	}
	targetID, registered, err := store.ProjectIDByPath(canon)
	if err != nil {
		return "", 0, err
	}
	if !registered || targetID == 0 {
		return "", 0, errMoveTargetInvalid
	}
	if info, statErr := os.Stat(canon); statErr != nil || !info.IsDir() {
		return "", 0, errMoveTargetInvalid
	}
	return canon, targetID, nil
}

// ensureSessionIdle refuses a move while anything is still writing for the
// conversation.
//
// A conversation that is producing (or has a message row still mid-stream, e.g.
// its finalize came in a concurrent request) writes its next rows under the
// project id it captured at the start of the turn, so moving it now would split
// ONE reply across two projects. A group turn is registered under the GROUP id,
// but its members have their own connections and may be compacting between
// turns; a compaction writes member state, so it is just as unsafe to move
// across an in-flight one.
func ensureSessionIdle(sessionID string) error {
	if service.IsSessionRunning(sessionID) {
		return errMoveSessionRunning
	}
	if service.IsGroupSession(sessionID) {
		if service.GroupHasCompactingMember(sessionID) {
			return errMoveSessionRunning
		}
		members, err := service.ListGroupMembers(sessionID)
		if err != nil {
			return err
		}
		for _, m := range members {
			if service.IsSessionRunning(m.ID) {
				return errMoveSessionRunning
			}
		}
	}
	streaming, err := service.SessionHasStreamingMessage(sessionID)
	if err != nil {
		return err
	}
	if streaming {
		return errMoveTruncatedStream
	}
	return nil
}

// writeSessionMoveError maps the re-bind errors to HTTP statuses. Anything else
// is a genuine write failure.
func writeSessionMoveError(w http.ResponseWriter, r *http.Request, err error) {
	var unavailable *service.MoveUnavailableMembersError
	switch {
	case errors.Is(err, errMoveTargetInvalid):
		writeLocalizedErrorf(w, r, http.StatusUnprocessableEntity, "MoveTargetInvalid")
	case errors.Is(err, errMoveNotAllowed):
		writeLocalizedErrorf(w, r, http.StatusUnprocessableEntity, "MoveNotAllowed")
	case errors.As(err, &unavailable):
		writeLocalizedErrorf(w, r, http.StatusUnprocessableEntity, "MoveMembersUnavailable",
			map[string]any{
				"Members": strings.Join(unavailable.Names, ", "),
				// Machine-readable form for the client's own localization (the
				// response message above is already localized for this request).
				"members": unavailable.Names,
			})
	case errors.Is(err, errMoveSessionRunning), errors.Is(err, errMoveTruncatedStream):
		writeLocalizedErrorf(w, r, http.StatusConflict, "GroupRunning")
	default:
		slog.Error("move session to project failed", slog.String("error", err.Error()))
		writeLocalizedErrorf(w, r, http.StatusInternalServerError, "InternalError")
	}
}
