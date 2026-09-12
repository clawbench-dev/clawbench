package handler

import (
	"context"
	"os/exec"
	"strings"
	"time"
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
