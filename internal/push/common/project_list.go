package common

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// RenderProjectList renders the /new project list. Shared by all three push
// backends so the index a user reads is the index they type back.
//
// A missing directory is flagged inline: the session can still be created, but
// the user should know the agent may fail to run there.
//
// maxRunes is the channel's single-message ceiling (<=0 means no limit). When
// the list does not fit, entries are dropped whole from the tail rather than
// cut mid-line — a half path with a dangling backtick is unusable — and a note
// says how many were shown. The list stays actionable because /new also accepts
// a full path, so a project that fell off can still be selected.
func RenderProjectList(refs []ProjectRef, maxRunes int) string {
	out := renderProjectList(refs, 0)
	if maxRunes <= 0 || utf8.RuneCountInString(out) <= maxRunes {
		return out
	}
	for n := len(refs) - 1; n >= 0; n-- {
		if candidate := renderProjectList(refs[:n], len(refs)); utf8.RuneCountInString(candidate) <= maxRunes {
			return candidate
		}
	}
	// Not even the header fits the budget. Send something the platform accepts
	// instead of a message it will reject outright.
	return string([]rune(out)[:maxRunes]) + "…"
}

// renderProjectList renders refs. total > len(refs) means the list was cut, and
// the trailing note says so.
func renderProjectList(refs []ProjectRef, total int) string {
	var sb strings.Builder
	sb.WriteString("### " + T("PushNewTitle") + "\n" + T("PushNewListIntro") + "\n\n")
	if len(refs) == 0 {
		sb.WriteString(T("PushNewNoProjects") + "\n")
	}
	for _, r := range refs {
		mark := ""
		if !r.Exists {
			mark = " ⚠️"
		}
		fmt.Fprintf(&sb, "%d. `%s`%s\n", r.Index, r.Path, mark)
	}
	sb.WriteString("\n" + T("PushNewListUsage"))
	if total > len(refs) {
		sb.WriteString("\n\n" + T("PushNewListTruncated", map[string]any{
			TemplateCount: len(refs),
			TemplateTotal: total,
		}))
	}
	return sb.String()
}

// SelectProject resolves a /new selector against the rendered list.
//
// Only two forms are accepted: a 1-based index from that same list, or a path
// that exactly matches a listed project. Basename matching is deliberately
// absent — two projects named "web" would be indistinguishable, and silently
// picking one would create the session in the wrong repo.
//
// This is the render-layer selector. The service layer re-validates the path
// against the project registry on create (the push package cannot import
// service without creating a cycle), so a path that is not registered is still
// refused there.
//
// The index form takes only the FIRST token: the agreed contract is that text
// after the selector is ignored ("/new 1 please" still means project 1). The
// path form uses the whole remainder so paths containing spaces still resolve.
func SelectProject(refs []ProjectRef, arg string) (string, ProjectRef, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", ProjectRef{}, fmt.Errorf("%s", T("PushNewProjectNotFound"))
	}

	// Index form: first token only.
	head := arg
	if idx := strings.IndexAny(arg, " \t\n"); idx >= 0 {
		head = arg[:idx]
	}
	if n, err := strconv.Atoi(head); err == nil {
		for _, r := range refs {
			if r.Index == n {
				return r.Path, r, nil
			}
		}
		return "", ProjectRef{}, fmt.Errorf("%s",
			T("PushNewProjectOutOfRange", map[string]any{TemplateCount: len(refs)}))
	}

	// Path form: the whole remainder, so "…/my project" works.
	for _, r := range refs {
		if r.Path == arg {
			return r.Path, r, nil
		}
	}
	return "", ProjectRef{}, fmt.Errorf("%s", T("PushNewProjectNotFound"))
}
