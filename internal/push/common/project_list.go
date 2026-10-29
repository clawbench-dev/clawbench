package common

import (
	"fmt"
	"strconv"
	"strings"
)

// RenderProjectList renders the /new project list. Shared by both backends so
// the index a user reads is the index they type back.
//
// A missing directory is flagged inline: the session can still be created, but
// the user should know the agent may fail to run there.
func RenderProjectList(refs []ProjectRef) string {
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
	return sb.String()
}

// SelectProject resolves a /new selector against the rendered list.
//
// Only two forms are accepted: a 1-based index from that same list, or a path
// that exactly matches a listed project. Basename matching is deliberately
// absent — two projects named "web" would be indistinguishable, and silently
// picking one would create the session in the wrong repo.
//
// This is the render-layer selector; the service layer re-validates the path
// against the project registry (the push package cannot import service without
// creating a cycle), so a path that is not registered is still refused there.
func SelectProject(refs []ProjectRef, arg string) (string, ProjectRef, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", ProjectRef{}, fmt.Errorf("%s", T("PushNewProjectNotFound"))
	}

	if n, err := strconv.Atoi(arg); err == nil {
		for _, r := range refs {
			if r.Index == n {
				return r.Path, r, nil
			}
		}
		return "", ProjectRef{}, fmt.Errorf("%s",
			T("PushNewProjectOutOfRange", map[string]any{"Count": len(refs)}))
	}

	for _, r := range refs {
		if r.Path == arg {
			return r.Path, r, nil
		}
	}
	return "", ProjectRef{}, fmt.Errorf("%s", T("PushNewProjectNotFound"))
}
