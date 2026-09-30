package skill

import (
	"log/slog"
	"path/filepath"
	"sort"
	"strings"

	"clawbench/internal/model"
)

// ScanAll rescans every configured source and rebuilds the registry.
//
// It is safe to call concurrently with InjectedFor: directory IO happens
// outside the lock and results are published in one write-locked swap.
func (r *Registry) ScanAll() {
	next := r.scanSources()

	r.mu.Lock()
	r.bySource = next
	r.injected = make(map[string][]Skill)
	r.mu.Unlock()
}

// scanSources performs the (slow) filesystem reads and returns the new
// bySource map without touching registry state.
func (r *Registry) scanSources() map[string][]Skill {
	next := make(map[string][]Skill)

	// 1. User-configured local directories. All of them share SourceUserDir
	// priority, so a name present in two of them resolves deterministically by
	// the tie-break in sourceLess (directory path), not by list order.
	for _, dir := range model.ConfigInstance.ResolveSkillsDirs() {
		src := Source{Kind: SourceUserDir, Label: "user dir", Dir: dir}
		if skills := ScanDir(dir, src); len(skills) > 0 {
			next[src.Key()] = skills
		}
	}

	// 2. Git repositories, each cloned to {DataDir}/skills/<slug>.
	for _, repo := range model.ConfigInstance.Skills.Repos {
		dir := RepoDir(repo.Slug)
		if dir == "" {
			continue
		}
		label := repo.Slug
		if label == "" {
			label = repo.URL
		}
		src := Source{Kind: SourceGit, Label: label, Dir: dir}
		if skills := ScanDir(dir, src); len(skills) > 0 {
			next[src.Key()] = skills
		}
	}

	// 3. Native skill directories, one scan per distinct directory.
	next = scanNativeDirs(next)

	return next
}

// scanNativeDirs scans every native skill directory into next, one scan per
// distinct directory.
//
// A directory declared by several agents (the shared ".agents/skills" is read
// by six backends) is scanned ONCE: its content does not depend on who is
// asking, and scanning per agent would list its skills once per agent.
//
// own-ness is deliberately not decided here — it depends on the agent being
// served, so every native entry is stored as SourceOtherNative and InjectedFor
// re-tags the served agent's own directories.
func scanNativeDirs(next map[string][]Skill) map[string][]Skill {
	// SharedSkillsDir is added unconditionally: it is where `npx skills`
	// installs, so it must be discovered even on an install whose agents all
	// happen to be ones that do not read it (Claude, Pi).
	declaredBy := map[string][]string{}
	if dir := ResolveNativeSkillsDir(SharedSkillsDir); dir != "" {
		declaredBy[dir] = nil
	}
	for _, agent := range model.GetAgentList() {
		if agent == nil {
			continue
		}
		spec := model.FindSpecByBackend(agent.Backend)
		if spec == nil {
			continue
		}
		for _, dir := range ResolveNativeSkillsDirs(spec.NativeSkillsDirs) {
			declaredBy[dir] = append(declaredBy[dir], agent.ID)
		}
	}

	dirs := make([]string, 0, len(declaredBy))
	for dir := range declaredBy {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs) // deterministic scan order

	for _, dir := range dirs {
		agents := append([]string(nil), declaredBy[dir]...)
		sort.Strings(agents)
		label := "shared skills"
		if len(agents) > 0 {
			label = strings.Join(agents, "+") + " native"
		}
		src := Source{Kind: SourceOtherNative, AgentID: strings.Join(agents, ","), Label: label, Dir: dir}
		if skills := ScanDir(dir, src); len(skills) > 0 {
			next[src.Key()] = skills
		}
	}
	return next
}

// Invalidate drops the per-agent injection cache. Call after a config change or
// a git sync so the next InjectedFor recomputes.
func (r *Registry) Invalidate() {
	r.mu.Lock()
	r.injected = make(map[string][]Skill)
	r.mu.Unlock()
}

// InjectedFor returns the deduplicated list of skills to inject into the given
// agent's system prompt.
//
// Dedup rule (by canonical name, priority = SourceKind order):
//   - own native > user dir > git > other agent's native
//   - when several entries share a name, the highest-priority one wins
//   - if the winner is the agent's OWN native skill AND the backend loads that
//     directory itself, nothing is injected for that name (the agent already
//     has it). If the backend does NOT auto-load (e.g. CodeBuddy's ACP
//     process), the skill is injected as usual.
//
// Returns nil when skills are disabled or nothing applies.
func (r *Registry) InjectedFor(agentID string) []Skill {
	agent := model.GetAgent(agentID)
	if agent == nil {
		return nil
	}
	spec := model.FindSpecByBackend(agent.Backend)
	var ownDirs []string
	if spec != nil {
		ownDirs = ResolveNativeSkillsDirs(spec.NativeSkillsDirs)
	}
	autoLoads := spec != nil && spec.AutoLoadsNativeSkills

	r.mu.RLock()
	if cached, ok := r.injected[agentID]; ok {
		r.mu.RUnlock()
		return cached
	}
	r.mu.RUnlock()

	if !model.ConfigInstance.Skills.Enabled {
		return nil
	}

	candidates := r.candidatesFor(ownDirs)
	injected := dedupe(candidates, ownDirs, autoLoads)

	r.mu.Lock()
	r.injected[agentID] = injected
	r.mu.Unlock()

	return injected
}

// candidatesFor collects every skill entry visible to an agent, re-tagging the
// agent's own native directories as SourceOwnNative (the scan stored them as
// SourceOtherNative, since a directory's kind depends on who is asking).
//
// A skill whose path lives under one of the agent's own directories counts as
// own. Containment (not equality) is used because a declared directory is a
// tree: a skill may sit several levels below it, or the declared path may be
// the skill itself (single-skill repository layout).
func (r *Registry) candidatesFor(ownDirs []string) []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []Skill
	for _, skills := range r.bySource {
		for _, s := range skills {
			if isUnderAny(s.Path, ownDirs) {
				s.Source.Kind = SourceOwnNative
			}
			out = append(out, s)
		}
	}
	return out
}

// isUnderAny reports whether path is inside (or equal to) one of dirs.
func isUnderAny(path string, dirs []string) bool {
	for _, dir := range dirs {
		if path == dir {
			return true
		}
		if rel, err := filepath.Rel(dir, path); err == nil &&
			rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// dedupe groups candidates by canonical name, picks the highest-priority entry
// per group, drops names the agent already loads natively, and returns the
// survivors sorted by name.
//
// A winner is dropped when it is one of the agent's own native skills AND the
// backend loads its own directories: re-injecting it would duplicate the table
// the backend already built.
func dedupe(candidates []Skill, ownDirs []string, autoLoads bool) []Skill {
	best := make(map[string]Skill, len(candidates))
	for _, s := range candidates {
		key := canonicalName(s.Name)
		if key == "" {
			continue
		}
		cur, exists := best[key]
		if !exists || sourceLess(s.Source, cur.Source) {
			best[key] = s
		}
	}

	out := make([]Skill, 0, len(best))
	for _, s := range best {
		if s.Source.Kind == SourceOwnNative && autoLoads && isUnderAny(s.Path, ownDirs) {
			// The agent loads this directory itself; injecting would duplicate it.
			continue
		}
		out = append(out, s)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// sourceLess orders two sources by dedup priority so the winner never flips
// between scans.
//
// Within a kind the tie-break is the directory path, NOT AgentID: native
// sources are keyed by directory (several agents share one), and user
// directories carry no agent at all. Ordering by path keeps the result
// independent of the order agents or directories happen to be listed in.
func sourceLess(a, b Source) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.Dir < b.Dir
}

// OwnNative returns the skills found under an agent's own native directories,
// regardless of whether the backend auto-loads them. Used to register the
// agent's skills as slash commands.
func (r *Registry) OwnNative(agentID string) []Skill {
	agent := model.GetAgent(agentID)
	if agent == nil {
		return nil
	}
	spec := model.FindSpecByBackend(agent.Backend)
	if spec == nil {
		return nil
	}
	ownDirs := ResolveNativeSkillsDirs(spec.NativeSkillsDirs)
	if len(ownDirs) == 0 {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Skill
	for _, skills := range r.bySource {
		for _, s := range skills {
			if isUnderAny(s.Path, ownDirs) {
				out = append(out, s)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// All returns every discovered skill across all sources, sorted for display.
//
// Native entries are reported as SourceOtherNative, because "own" is relative
// to an agent and this view has no agent in scope; callers that need the
// per-agent view use InjectedFor.
func (r *Registry) All() []Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var out []Skill
	for _, skills := range r.bySource {
		out = append(out, skills...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// RepoDir returns the local checkout directory for a repo slug:
// {DataDir}/skills/<slug>. Returns "" when DataDir or slug is unset, or when
// the slug is not a single safe path segment.
//
// A slug is always derived by model.SkillSlug (which strips separators), but a
// hand-edited config.yaml can carry anything, so the shape is re-checked here:
// the checkout path must stay a direct child of the skills root.
func RepoDir(slug string) string {
	if model.DataDir == "" || slug == "" {
		return ""
	}
	if slug == "." || slug == ".." || strings.ContainsAny(slug, `/\`) {
		slog.Warn("skill: rejecting repo slug that is not a single path segment", "slug", slug)
		return ""
	}
	return filepath.Join(model.DataDir, "skills", slug)
}

// ResetForTest clears the global registry. For testing only.
func ResetForTest() {
	r := Global()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.bySource = make(map[string][]Skill)
	r.injected = make(map[string][]Skill)
}
