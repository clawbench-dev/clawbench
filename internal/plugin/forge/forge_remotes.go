package forge

import (
	"context"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"clawbench/internal/forge"
	"clawbench/internal/service"
)

// gitRemote is a named git remote and its fetch URL.
type gitRemote struct {
	Name string
	URL  string
}

// listGitRemotes runs `git remote -v` in the project and returns the unique
// fetch remotes. Returns an error when the project is not a git repository or
// git is unavailable.
func listGitRemotes(projectPath string) ([]gitRemote, error) {
	// Bound the call: a hung git (e.g. a stale lock) must not stall the request.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "remote", "-v")
	cmd.Dir = projectPath
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var remotes []gitRemote
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		// Format: "<name>\t<url> (fetch|push)"
		if len(fields) < 3 {
			continue
		}
		name, url, kind := fields[0], fields[1], fields[2]
		if kind != "(fetch)" {
			continue
		}
		key := name + "\x00" + url
		if seen[key] {
			continue
		}
		seen[key] = true
		remotes = append(remotes, gitRemote{Name: name, URL: url})
	}
	return remotes, nil
}

// verifyBindingReachable issues one read against a binding the user is about to
// save, so a repository this server cannot use is refused instead of stored.
//
// The read is the binding's own list query (one page), through the same
// credential, scheme, platform and TLS policy as every later request: an answer
// here means the panel will show that repository rather than an error card.
// Issue #722 was a binding the server accepted after which every read failed,
// leaving "bound" in the panel and a load error everywhere else.
//
// Only an explicit bind is checked. Auto-binding never sends the credential
// somewhere the user has not confirmed, and a repository that becomes
// unreachable later is the poller's retry/backoff, not a bind-time question.
// A provider the normal path could not build is reported there, as before.
func verifyBindingReachable(r *http.Request, pf service.ProjectForge) error {
	if shouldSkipProbe(pf) {
		return nil
	}
	provider, err := newForgeProvider(&pf)
	if err != nil {
		return nil
	}
	_, err = provider.ListItems(r.Context(), forge.ListOptions{Type: forge.ItemTypeIssue, State: "open", Page: 1, PerPage: 1})
	if err != nil && !isRefusal(err) {
		return nil
	}
	return err
}

// isRefusal reports whether a failed probe means the platform ANSWERED and said
// no, as opposed to this machine failing to reach it.
//
// Only a refusal is a reason to refuse the bind. Every other failure — the name
// not resolving, connection refused, TLS, a timeout — says nothing about the
// repository: it is the same connectivity problem the panel reports after
// binding, and rejecting the bind there would make an internal name this
// machine cannot see unbindable. Those bindings are stored as the user wrote
// them, exactly as before this change, and the first read reports the error.
func isRefusal(err error) bool {
	var fe *forge.Error
	if !errors.As(err, &fe) {
		return false
	}
	// 401/403 (auth), 429 (rate limit) and 5xx carry a status the platform chose
	// to answer with. A transport failure is ErrKindNetwork; anything without a
	// status did not come from the instance.
	//
	// 404 is deliberately NOT a refusal: a private repository is indistinguishable
	// from a missing one, and an instance without a token answers 404 for every
	// repository. Refusing there would make a private self-hosted repository
	// unbindable until a token is added — the very "bound but nothing loads"
	// state this check exists to prevent would then become "cannot bind at all".
	return fe.Kind != forge.ErrKindNetwork && fe.Status != 0 && fe.Status != 404
}

// shouldSkipProbe reports whether probing this binding would prove nothing.
//
// The one case is a host with neither a scheme nor a port: an ssh remote whose
// scp form names no port (ssh itself says nothing about the API scheme). The
// probe would then go to https on 443, and whether that answers says more about
// the machine than about the repository — a self-hosted instance usually does
// not serve there. Such a binding is accepted as written, exactly as before
// this change, and the first real read reports anything wrong.
func shouldSkipProbe(pf service.ProjectForge) bool {
	return pf.Scheme == "" && !strings.Contains(pf.Host, ":")
}
