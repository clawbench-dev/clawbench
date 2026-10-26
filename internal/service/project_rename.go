package service

import (
	"errors"
	"fmt"
	"os"

	"clawbench/internal/store"
)

// Sentinel errors returned by RenameProjectData so the handler can map them to
// distinct HTTP statuses without string matching.
var (
	// ErrProjectRenameForbidden: the id is the reserved global-scope sentinel.
	ErrProjectRenameForbidden = errors.New("project cannot be renamed")
	// ErrProjectPathInvalid: the new path is empty, missing, or not a directory.
	ErrProjectPathInvalid = errors.New("project path is not a directory")
	// ErrProjectPathExists: another project already owns the new path.
	ErrProjectPathExists = errors.New("project already exists at path")
)

// RenameProjectData points a project at a different directory on disk.
//
// This is the operation the project-id refactor exists to make cheap: no
// project-scoped table stores the path, so a single UPDATE on the projects row
// makes every session, message, tag, task, share, usage row, notification and
// project-scoped agent follow the project to its new location. The directory on
// disk is NOT moved — the caller is telling ClawBench where it went.
//
// Returns the previous path so the handler can update a client that has that
// project open (its cookie still carries the old path).
//
// Refusals:
//   - the global-scope sentinel has no path;
//   - an unknown id;
//   - a new path that is missing or not a directory (nothing to point at);
//   - an existing project at the new path (colliding would silently need a
//     merge, which is a different operation);
//   - any session of the project currently running: the runner captured the old
//     path when it started, and once the registry no longer maps that path a
//     later write would re-register the old path as a brand-new project.
func RenameProjectData(id int64, newPath string) (string, error) {
	if id == store.GlobalScopeProjectID {
		return "", ErrProjectRenameForbidden
	}

	oldPath, ok, err := store.ProjectPathForID(id)
	if err != nil {
		return "", fmt.Errorf("resolve project path: %w", err)
	}
	if !ok {
		return "", ErrProjectNotFound
	}

	newCanon := store.NormalizeProjectPath(newPath)
	if newCanon == "" {
		return "", ErrProjectPathInvalid
	}
	info, statErr := os.Stat(newCanon)
	if statErr != nil || !info.IsDir() {
		return "", ErrProjectPathInvalid
	}

	if newCanon == oldPath {
		return oldPath, nil
	}

	if _, exists, existsErr := store.ProjectIDByPath(newCanon); existsErr != nil {
		return "", fmt.Errorf("check target project path: %w", existsErr)
	} else if exists {
		return "", ErrProjectPathExists
	}

	// Best-effort guard against renaming under a live turn: a runner captured
	// the old path when it started and would re-register that path as a new
	// project on its next write. Refusing while a turn is already streaming
	// closes the common case; a turn that starts in the tiny window between
	// this check and the UPDATE is not otherwise synchronized with the registry.
	running, err := projectHasRunningSession(id)
	if err != nil {
		return "", fmt.Errorf("check running sessions: %w", err)
	}
	if running {
		return "", ErrProjectHasRunningSessions
	}

	if err := store.RenameProject(oldPath, newCanon); err != nil {
		return "", fmt.Errorf("rename project: %w", err)
	}
	return oldPath, nil
}
