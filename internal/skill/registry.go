package skill

import (
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"clawbench/internal/model"
)

// ScanAll rescans every configured source and rebuilds the registry.
//
// It is safe to call concurrently with InjectedFor: directory IO happens
// outside the lock and results are published in one write-locked swap.
//
// Project-scoped scans are dropped too: their directories were read under the
// previous global view, so a rescan invalidates them along with everything else.
func (r *Registry) ScanAll() {
	bySource, skipped, stamps := r.scanSources()

	r.mu.Lock()
	r.bySource = bySource
	r.skippedBySource = skipped
	r.sourceStamps = stamps
	r.projectSources = make(map[string][]Skill)
	r.projectSkipped = make(map[string][]Skipped)
	r.projectStamps = make(map[string]dirStamp)
	r.injectedProject = make(map[string][]Skill)
	r.mu.Unlock()
}

// scanSources performs the (slow) filesystem reads and returns the new
// bySource map, the per-source skip reports, and the directory stamps taken
// after the walk (used by EnsureFresh), without touching registry state.
func (r *Registry) scanSources() (map[string][]Skill, map[string][]Skipped, map[string]dirStamp) {
	next := make(map[string][]Skill)
	skipped := make(map[string][]Skipped)

	// 1. User-configured local directories. All of them share SourceUserDir
	// priority, so a name present in two of them resolves deterministically by
	// the tie-break in sourceLess (directory path), not by list order.
	for _, dir := range model.ConfigInstance.ResolveSkillsDirs() {
		src := Source{Kind: SourceUserDir, Label: "user dir", Dir: dir}
		skills, sk := ScanDirReport(dir, src)
		if len(skills) > 0 {
			next[src.Key()] = skills
		}
		if len(sk) > 0 {
			skipped[src.Key()] = sk
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
		skills, sk := ScanDirReport(dir, src)
		if len(skills) > 0 {
			next[src.Key()] = skills
		}
		if len(sk) > 0 {
			skipped[src.Key()] = sk
		}
	}

	// 3. Native skill directories, one scan per distinct directory.
	scanNativeDirs(next, skipped)

	stamps := make(map[string]dirStamp)
	for _, dir := range r.sourceRoots() {
		stamps[dir] = stampDir(dir)
	}
	return next, skipped, stamps
}

// nativeDirsByAgent returns every native skill directory mapped to the agents
// that declare it. The shared directory is always present with no owners.
//
// It is the single source of "which directories count as native": both the
// scan and EnsureFresh's freshness check derive their directory list from it,
// so a directory can never be scanned but not watched (or vice versa).
func nativeDirsByAgent() map[string][]string {
	declaredBy := map[string][]string{}
	// SharedSkillsDir is added unconditionally: it is where `npx skills`
	// installs, so it must be discovered even on an install whose agents all
	// happen to be ones that do not read it (Claude, Pi).
	if sharedDir := ResolveNativeSkillsDir(SharedSkillsDir); sharedDir != "" {
		declaredBy[sharedDir] = nil
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
	return declaredBy
}

// sourceRoots lists every directory whose DIRECT children decide whether a
// rescan is needed: user dirs, git checkouts, and native dirs.
func (r *Registry) sourceRoots() []string {
	var dirs []string
	dirs = append(dirs, model.ConfigInstance.ResolveSkillsDirs()...)
	for _, repo := range model.ConfigInstance.Skills.Repos {
		if dir := RepoDir(repo.Slug); dir != "" {
			dirs = append(dirs, dir)
		}
	}
	for dir := range nativeDirsByAgent() {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs
}

// dirStamp is a cheap fingerprint of a directory's immediate contents.
//
// mtime alone is not enough: several filesystems store timestamps at one-second
// granularity, so a skill installed right after a scan could keep the same
// mtime. The entry count catches add/remove even then. Editing a SKILL.md in
// place changes neither — that still needs a manual rescan (see EnsureFresh).
type dirStamp struct {
	modTime int64
	entries int
}

// stampDir returns the current stamp for dir. A missing or unreadable
// directory yields the zero stamp, which differs from any real one — that is
// how a newly created directory is detected.
func stampDir(dir string) dirStamp {
	if dir == "" {
		return dirStamp{}
	}
	info, err := os.Stat(dir)
	if err != nil {
		return dirStamp{}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dirStamp{modTime: info.ModTime().UnixNano()}
	}
	return dirStamp{modTime: info.ModTime().UnixNano(), entries: len(entries)}
}

// freshCheckInterval throttles EnsureFresh. Injecting the skill table happens
// on every turn, and stat-ing a dozen directories per turn buys nothing: a
// skill installed by hand appearing two seconds later is indistinguishable to
// the user.
var freshCheckInterval = 2 * time.Second

// EnsureFresh rescans when a source root's contents changed since the last
// scan, so a skill dropped into a directory by hand shows up without the user
// clicking "rescan".
//
// It compares directory stamps, not contents: the walk is skipped unless
// something was added to or removed from a source root. Directories nested
// below a root (a category folder inside a cloned repository) are not watched —
// those change through a git pull, which already triggers a scan.
//
// Safe to call from any goroutine; the actual rescan is ScanAll.
func (r *Registry) EnsureFresh() {
	now := time.Now()
	r.mu.RLock()
	last := r.lastFreshCheck
	r.mu.RUnlock()

	if now.Sub(last) < freshCheckInterval {
		return
	}

	r.mu.Lock()
	// Re-check under the write lock: a concurrent caller may have just
	// refreshed and rescanned, and the second caller must not repeat it.
	if now.Sub(r.lastFreshCheck) < freshCheckInterval {
		r.mu.Unlock()
		return
	}
	r.lastFreshCheck = now
	stamps := r.sourceStamps
	r.mu.Unlock()

	for _, dir := range r.sourceRoots() {
		if stampDir(dir) != stamps[dir] {
			slog.Info("skill: source directory changed, rescanning", "dir", dir)
			r.ScanAll()
			return
		}
	}
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
func scanNativeDirs(next map[string][]Skill, skipped map[string][]Skipped) {
	declaredBy := nativeDirsByAgent()
	sharedDir := ResolveNativeSkillsDir(SharedSkillsDir)

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
		src := Source{
			Kind:    SourceOtherNative,
			AgentID: strings.Join(agents, ","),
			Label:   label,
			Dir:     dir,
			Shared:  dir == sharedDir,
		}
		skills, sk := ScanDirReport(dir, src)
		if len(skills) > 0 {
			next[src.Key()] = skills
		}
		if len(sk) > 0 {
			skipped[src.Key()] = sk
		}
	}
}

// Invalidate drops the per-agent injection cache. Call after a config change or
// a git sync so the next InjectedFor recomputes.
//
// The project-scoped scan cache is intentionally kept: a project's
// .agents/skills directory is unaffected by a config/git change, and dropping
// it would force a filesystem walk on the next turn of every project session.
func (r *Registry) Invalidate() {
	r.mu.Lock()
	r.injectedProject = make(map[string][]Skill)
	r.mu.Unlock()
}

// scanProject returns the project-scoped skills for projectPath, scanning
// <projectPath>/.agents/skills and caching the result until the directory's
// stamp changes.
//
// It is the same convention qoder and codewhale read when their cwd is the
// project root: skills committed alongside the project, visible only to that
// project's sessions. An empty projectPath scans nothing.
//
// The cache is validated by a directory stamp rather than kept forever: a skill
// committed (or deleted) while the session is open must show up without the
// user clicking "rescan". A missing directory stamps as zero, so a directory
// created later is detected the same way.
func (r *Registry) scanProject(projectPath string) []Skill {
	if projectPath == "" {
		return nil
	}
	dir := filepath.Join(projectPath, ".agents", "skills")
	stamp := stampDir(dir)

	r.mu.RLock()
	cached, ok := r.projectSources[projectPath]
	cachedStamp := r.projectStamps[projectPath]
	r.mu.RUnlock()
	if ok && cachedStamp == stamp {
		return cached
	}

	src := Source{Kind: SourceProject, Label: "project", Dir: dir}
	skills, skipped := ScanDirReport(dir, src)

	suffix := "\x00" + projectPath
	r.mu.Lock()
	r.projectSources[projectPath] = skills
	r.projectStamps[projectPath] = stamp
	if len(skipped) > 0 {
		r.projectSkipped[projectPath] = skipped
	} else {
		delete(r.projectSkipped, projectPath)
	}
	// The project's injected table is now stale for EVERY agent: drop those
	// cache entries so a skill added or removed mid-session is not masked by a
	// table built a moment earlier.
	for key := range r.injectedProject {
		if strings.HasSuffix(key, suffix) {
			delete(r.injectedProject, key)
		}
	}
	r.mu.Unlock()
	return skills
}

// InjectedFor returns the deduplicated list of skills to inject into the given
// agent's system prompt, without any project scope. Callers that know the
// session's project should use InjectedForProject instead.
//
// Dedup rule (by canonical name, priority = SourceKind order):
//   - own native > user dir > project > git > other agent's native
//   - when several entries share a name, the highest-priority one wins
//   - names listed in config skills.disabled are dropped entirely
//   - if the winner is the agent's OWN native skill AND the backend loads that
//     directory itself, nothing is injected for that name (the agent already
//     has it). If the backend does NOT auto-load (e.g. CodeBuddy's ACP
//     process), the skill is injected as usual.
//
// Returns nil when skills are disabled or nothing applies.
func (r *Registry) InjectedFor(agentID string) []Skill {
	return r.InjectedForProject(agentID, "")
}

// InjectedForProject is InjectedFor with project scope: the session's project
// root contributes its <projectPath>/.agents/skills entries at SourceProject
// priority (below the agent's own native dirs and the user's configured dirs).
//
// Project entries are injected only when skills.project_auto_inject is on. The
// working directory may be a repository the user never read, and a skill's
// description is copied verbatim into the system prompt on every turn, so this
// is opt-in. The listing and "#name" resolution are unaffected.
//
// When enabled, a project skill is injected even for a backend that loads the
// directory itself: unlike the shared ~/.agents/skills (whose ownership is
// declared per backend via NativeSkillsDirs), the project directory is not a
// declared native dir, so dedupe never treats it as own.
//
// An empty projectPath is exactly InjectedFor.
func (r *Registry) InjectedForProject(agentID, projectPath string) []Skill {
	agent := model.GetAgent(agentID)
	if agent == nil {
		return nil
	}
	// A skill installed by hand since the last scan must be visible on the next
	// turn, not after someone clicks "rescan".
	r.EnsureFresh()
	spec := model.FindSpecByBackend(agent.Backend)
	var ownDirs []string
	if spec != nil {
		ownDirs = ResolveNativeSkillsDirs(spec.NativeSkillsDirs)
	}
	autoLoads := spec != nil && spec.AutoLoadsNativeSkills

	enabled := model.ConfigInstance.Skills.Enabled
	projectInject := model.ConfigInstance.Skills.ProjectAutoInject

	// Scan the project directory before reading the injection cache: the scan is
	// stamp-validated (steady state: one stat + one readdir) and is what drops
	// this project's cached tables when a skill appears or disappears
	// mid-session. Only needed when project skills can actually be injected —
	// otherwise a project change cannot affect the table.
	var projectSkills []Skill
	if enabled && projectInject {
		projectSkills = r.scanProject(projectPath)
	}

	cacheKey := agentID + "\x00" + projectPath
	r.mu.RLock()
	if cached, ok := r.injectedProject[cacheKey]; ok {
		r.mu.RUnlock()
		return cached
	}
	r.mu.RUnlock()

	if !enabled {
		return nil
	}

	candidates := r.candidatesFor(ownDirs)
	// Project-scoped skills are opt-in for AUTOMATIC injection: their
	// description is copied verbatim into the system prompt on every turn, and
	// the working directory may be a repository the user never read. The
	// listing and "#name" resolution are unaffected (see AllForProject and
	// ResolveForProject).
	if projectInject {
		candidates = append(candidates, projectSkills...)
	}
	injected := dedupe(candidates, ownDirs, autoLoads, model.ConfigInstance.DisabledSkillSet())

	r.mu.Lock()
	r.injectedProject[cacheKey] = injected
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
// per group, drops names the agent already loads natively, drops names the user
// switched off, and returns the survivors sorted by name.
//
// A winner is dropped when it is one of the agent's own native skills AND the
// backend loads its own directories: re-injecting it would duplicate the table
// the backend already built.
//
// disabled is applied BEFORE grouping, so switching a name off silences every
// source carrying it — which is what a user means by "turn this skill off".
func dedupe(candidates []Skill, ownDirs []string, autoLoads bool, disabled map[string]bool) []Skill {
	best := make(map[string]Skill, len(candidates))
	for _, s := range candidates {
		key := canonicalName(s.Name)
		if key == "" || disabled[key] {
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
	sortSkills(out)
	return out
}

// AllForProject returns every discovered skill across all sources plus the
// project-scoped skills for projectPath, sorted for display.
//
// This is the view GET /api/skills renders: the caller passes the requesting
// project (from the cookie), so a project's own .agents/skills appear in the
// list while another project's never do.
//
// Project entries whose path is already listed globally are dropped: when the
// project root is $HOME, <project>/.agents/skills IS the shared ~/.agents/skills
// that scanNativeDirs already scans, and the same SKILL.md would otherwise be
// listed twice (the UI keys rows by path, so duplicates break its :key). The
// global entry wins; the dedup only affects this listing, not injection
// (dedupe collapses same-named entries by source priority).
func (r *Registry) AllForProject(projectPath string) []Skill {
	r.EnsureFresh()
	out := r.All()
	if projectPath != "" {
		seen := make(map[string]bool, len(out))
		for _, s := range out {
			seen[s.Path] = true
		}
		for _, s := range r.scanProject(projectPath) {
			if !seen[s.Path] {
				out = append(out, s)
			}
		}
	}
	sortSkills(out)
	return out
}

// AllSkipped returns the directories that look like skills but could not be
// loaded, across every global source plus the project directory.
//
// The listing API renders these so "I installed it and it is not in the list"
// has an answer. Paths are deduplicated because two sources may overlap (a
// project root that is also $HOME), and sorted for deterministic output.
func (r *Registry) AllSkipped(projectPath string) []Skipped {
	r.EnsureFresh()
	if projectPath != "" {
		r.scanProject(projectPath) // populate projectSkipped
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]bool)
	var out []Skipped
	appendAll := func(list []Skipped) {
		for _, s := range list {
			if s.Path == "" || seen[s.Path] {
				continue
			}
			seen[s.Path] = true
			out = append(out, s)
		}
	}
	for _, list := range r.skippedBySource {
		appendAll(list)
	}
	// Only the requested project's report: projectSkipped holds an entry per
	// scanned project, and another project's paths must not leak into this view.
	if projectPath != "" {
		appendAll(r.projectSkipped[projectPath])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// EffectivePathsFrom returns canonical name → winning SKILL.md path for a
// listing, using the same source priority as dedupe/ResolveForProject.
//
// The settings page uses it to mark shadowed rows: two skills with the same
// name are both listed, but only one of them is what "#name" resolves to. It
// takes an already-fetched listing so the caller does not pay for a second
// scan.
func EffectivePathsFrom(skills []Skill) map[string]string {
	best := make(map[string]Skill, len(skills))
	for _, s := range skills {
		key := canonicalName(s.Name)
		if key == "" {
			continue
		}
		if cur, ok := best[key]; !ok || sourceLess(s.Source, cur.Source) {
			best[key] = s
		}
	}
	out := make(map[string]string, len(best))
	for key, s := range best {
		out[key] = s.Path
	}
	return out
}

// ResolveForProject returns the highest-priority skill whose canonical name
// matches, from the project-scoped view (every global source plus
// <projectPath>/.agents/skills).
//
// This backs the user's explicit "#name" reference in the chat input. It is
// deliberately NOT gated by Skills.Enabled: that switch controls AUTOMATIC
// injection (InjectedForProject), whereas an explicit reference is a direct
// user instruction and must keep working when automatic injection is off.
// Both callers rely on this asymmetry, so do not add the gate here.
//
// Name comparison uses canonicalName (lowercased, leading "/" stripped), the
// same identity dedupe() uses. The winner is chosen with sourceLess, so the
// resolved skill is the same entry the listing view (GET /api/skills) shows for
// that name. Note that view reports every native directory as "other" — own-ness
// is relative to an agent, and this view has no agent in scope — so a project
// skill outranks a native-directory skill of the same name, and a user-dir
// skill outranks a project one.
// Returns false for an empty name or no match.
func (r *Registry) ResolveForProject(name, projectPath string) (Skill, bool) {
	key := canonicalName(name)
	if key == "" {
		return Skill{}, false
	}
	var best Skill
	found := false
	for _, s := range r.AllForProject(projectPath) {
		if canonicalName(s.Name) != key {
			continue
		}
		if !found || sourceLess(s.Source, best.Source) {
			best = s
			found = true
		}
	}
	return best, found
}

// sortSkills orders skills by name then path for deterministic output.
func sortSkills(skills []Skill) {
	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Name != skills[j].Name {
			return skills[i].Name < skills[j].Name
		}
		return skills[i].Path < skills[j].Path
	})
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
	r.skippedBySource = make(map[string][]Skipped)
	r.sourceStamps = make(map[string]dirStamp)
	r.lastFreshCheck = time.Time{}
	r.projectSources = make(map[string][]Skill)
	r.projectSkipped = make(map[string][]Skipped)
	r.projectStamps = make(map[string]dirStamp)
	r.injectedProject = make(map[string][]Skill)
}
