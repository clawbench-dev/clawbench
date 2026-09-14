// Package model — unified model discovery.
//
// This file replaces the previous arrangement, where thirteen backend packages
// each registered a free-form `func() []AgentModel` via
// RegisterDiscoverModelsFunc and the merge with ACP-reported models happened
// in the frontend. Three properties made that arrangement hard to reason about:
//
//   - No authority: CLI, ACP-runtime and hardcoded lists competed, and the
//     "who wins" rule differed per consumer (models merged in the frontend,
//     thinking levels overwritten in the backend, validation forked by
//     transport).
//   - No caching: every discovery call re-exec'd CLIs or re-read multi-MB
//     binaries, and the startup path ran the whole set twice.
//   - No shared vocabulary: each backend invented its own fallback marking,
//     dedup and display-name logic.
//
// The replacement has exactly one of each:
//
//   - One source per backend, described by a ModelSource (CLI command, static
//     catalog, or a plugin probe for the odd layouts).
//   - One merge point, ResolveModels, which treats ACP membership as
//     authoritative and the CLI list as the source of order and display names.
//   - One cache, TTL-based and shared by the startup path and the HTTP refresh
//     endpoint.
package model

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// ModelSourceKind describes how a ModelSource produces its list. It exists so
// callers and diagnostics can tell a cheap static catalog from a probe that
// shells out.
type ModelSourceKind string

const (
	// SourceKindStatic is a built-in catalog gated on CLI presence.
	SourceKindStatic ModelSourceKind = "static"
	// SourceKindCLI runs a CLI subcommand and parses its output.
	SourceKindCLI ModelSourceKind = "cli"
	// SourceKindPlugin is a custom probe for layouts that need more than a
	// single command (config files, binary strings, bundled JS).
	SourceKindPlugin ModelSourceKind = "plugin"
)

// ModelSource produces the CLI-side model list for one backend.
//
// Discover returns the models and, when it returns nothing, a human-readable
// explanation of what was probed. The detail is per-call — it must not be
// stashed in package state, because discovery also runs from a background
// refresher and a manual refresh can race it.
type ModelSource interface {
	Backend() string
	Kind() ModelSourceKind
	Discover() ([]AgentModel, string)
}

// ---------------------------------------------------------------------------
// Registry
// ---------------------------------------------------------------------------

var (
	modelSources   = make(map[string]ModelSource)
	modelSourcesMu sync.RWMutex
)

// RegisterModelSource registers the model source for a backend, replacing any
// previous registration. Backends call this from init().
func RegisterModelSource(src ModelSource) {
	if src == nil || src.Backend() == "" {
		return
	}
	modelSourcesMu.Lock()
	defer modelSourcesMu.Unlock()
	modelSources[src.Backend()] = src
}

// LookupModelSource returns the registered source for a backend.
func LookupModelSource(backend string) (ModelSource, bool) {
	modelSourcesMu.RLock()
	defer modelSourcesMu.RUnlock()
	src, ok := modelSources[backend]
	return src, ok
}

// HasModelSource reports whether a backend can refresh its model list.
func HasModelSource(backend string) bool {
	_, ok := LookupModelSource(backend)
	return ok
}

// RegisteredModelSources returns the set of backends with a registered source.
// Used by startup to walk the registry without guessing at backend names.
func RegisteredModelSources() []string {
	modelSourcesMu.RLock()
	defer modelSourcesMu.RUnlock()
	out := make([]string, 0, len(modelSources))
	for id := range modelSources {
		out = append(out, id)
	}
	return out
}

// snapshotModelSources returns a copy of the registry, so a test can clear it
// and restore the real backend registrations afterwards. Tests must not call
// resetModelSources directly: backend init() runs once per process, so wiping
// the map would leave every other test in the package without sources.
func snapshotModelSources() map[string]ModelSource {
	modelSourcesMu.RLock()
	defer modelSourcesMu.RUnlock()
	out := make(map[string]ModelSource, len(modelSources))
	for k, v := range modelSources {
		out[k] = v
	}
	return out
}

// restoreModelSources replaces the registry with a snapshot.
func restoreModelSources(snapshot map[string]ModelSource) {
	modelSourcesMu.Lock()
	defer modelSourcesMu.Unlock()
	modelSources = snapshot
}

// resetModelSources clears the registry. Test-only seam; pair it with a
// snapshotModelSources/restoreModelSources pair.
func resetModelSources() {
	modelSourcesMu.Lock()
	defer modelSourcesMu.Unlock()
	modelSources = make(map[string]ModelSource)
}

// SnapshotModelSourcesForTest returns an opaque snapshot of the source registry.
// Exported so tests in the model_test package can isolate the registry the same
// way the internal tests do.
func SnapshotModelSourcesForTest() func() {
	snapshot := snapshotModelSources()
	return func() { restoreModelSources(snapshot) }
}

// ResetModelSourcesForTest clears the source registry. Exported for model_test.
func ResetModelSourcesForTest() {
	resetModelSources()
}

// ---------------------------------------------------------------------------
// Cache
// ---------------------------------------------------------------------------

// defaultDiscoveryTTL bounds how often a backend probe may re-run. Probes shell
// out to CLIs and read multi-MB binaries, so the startup path and the manual
// refresh endpoint share this cache instead of each doing their own work.
const defaultDiscoveryTTL = 5 * time.Minute

type cacheEntry struct {
	models   []AgentModel
	detail   string
	cachedAt time.Time
}

// discoveryCache memoizes probe results per backend for a TTL. It deliberately
// caches empty results too: a failing probe is the expensive case, and
// re-running it on every call is what made the old startup path slow.
type discoveryCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	ttl     time.Duration
	now     func() time.Time // injectable clock for tests

	// probing serializes concurrent first-callers per backend. Without it two
	// callers would probe simultaneously and the later write would win, so a
	// transient failure could overwrite a concurrent success (or vice versa) and
	// be cached for the whole TTL.
	probing map[string]*sync.Mutex
}

func newDiscoveryCache(ttl time.Duration) *discoveryCache {
	return &discoveryCache{
		entries: make(map[string]cacheEntry),
		ttl:     ttl,
		now:     time.Now,
		probing: make(map[string]*sync.Mutex),
	}
}

// probeLock returns the per-backend probe mutex, creating it on first use.
func (c *discoveryCache) probeLock(backend string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.probing == nil {
		c.probing = make(map[string]*sync.Mutex)
	}
	mu, ok := c.probing[backend]
	if !ok {
		mu = &sync.Mutex{}
		c.probing[backend] = mu
	}
	return mu
}

var globalDiscoveryCache = newDiscoveryCache(defaultDiscoveryTTL)

// get returns the cached result for a backend, probing on a miss or after the
// TTL.
//
// The probe runs outside the cache lock so a slow CLI cannot block other
// backends, but under a per-backend lock so concurrent first-callers for the SAME
// backend probe once. The double-check inside the lock picks up a result a
// sibling just wrote, so a transient failure cannot overwrite a concurrent
// success.
func (c *discoveryCache) get(backend string, probe func() ([]AgentModel, string)) ([]AgentModel, string) {
	c.mu.Lock()
	entry, ok := c.entries[backend]
	c.mu.Unlock()

	if ok && c.now().Sub(entry.cachedAt) < c.ttl {
		return cloneModels(entry.models), entry.detail
	}

	lock := c.probeLock(backend)
	lock.Lock()
	defer lock.Unlock()

	// Re-check: another goroutine may have probed while we waited for the lock.
	c.mu.Lock()
	entry, ok = c.entries[backend]
	c.mu.Unlock()
	if ok && c.now().Sub(entry.cachedAt) < c.ttl {
		return cloneModels(entry.models), entry.detail
	}

	models, detail := probe()

	c.mu.Lock()
	c.entries[backend] = cacheEntry{
		models:   cloneModels(models),
		detail:   detail,
		cachedAt: c.now(),
	}
	c.mu.Unlock()

	// Hand back a copy as well, so a caller cannot mutate the cached entry (or a
	// probe's shared catalog) through the slice it receives.
	return cloneModels(models), detail
}

// invalidate drops one backend's cached result, forcing the next lookup to
// re-probe. Used by the manual refresh endpoint.
func (c *discoveryCache) invalidate(backend string) {
	c.mu.Lock()
	delete(c.entries, backend)
	c.mu.Unlock()
}

// invalidateAll drops every cached result.
func (c *discoveryCache) invalidateAll() {
	c.mu.Lock()
	c.entries = make(map[string]cacheEntry)
	c.mu.Unlock()
}

// InvalidateDiscoveredModels forces the next discovery for a backend to re-probe.
func InvalidateDiscoveredModels(backend string) {
	globalDiscoveryCache.invalidate(backend)
}

// InvalidateAllDiscoveredModels forces the next discovery for every backend to
// re-probe. Used by the explicit rescan endpoint.
func InvalidateAllDiscoveredModels() {
	globalDiscoveryCache.invalidateAll()
}

// cloneModels copies a slice so cached entries cannot be mutated by callers.
func cloneModels(in []AgentModel) []AgentModel {
	if in == nil {
		return nil
	}
	out := make([]AgentModel, len(in))
	copy(out, in)
	return out
}

// ---------------------------------------------------------------------------
// Discovery entry point
// ---------------------------------------------------------------------------

// DiscoverWithDetail returns the CLI-side model list for a backend and, when
// the list is empty, an explanation of what was probed. Both are cached.
//
// A backend with no registered source returns (nil, "") — it has no discovery
// story at all, which is different from a source that ran and found nothing.
//
// This is a variable so tests can substitute a probe without touching the
// registry; DiscoverModels delegates here, so overriding one covers both.
var DiscoverWithDetail = discoverWithDetail

func discoverWithDetail(backend string) ([]AgentModel, string) {
	src, ok := LookupModelSource(backend)
	if !ok {
		return nil, ""
	}
	return globalDiscoveryCache.get(backend, func() ([]AgentModel, string) {
		return safeDiscover(src)
	})
}

// DiscoverModels returns just the model list for a backend.
var DiscoverModels = func(backend string) []AgentModel {
	models, _ := DiscoverWithDetail(backend)
	return models
}

// safeDiscover runs a probe, converting a panic into a failure detail. Probes
// parse output from third-party binaries and read files they do not own, so a
// crash there must not take the server down.
func safeDiscover(src ModelSource) (models []AgentModel, detail string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("model discovery probe panicked", "backend", src.Backend(), "panic", r)
			models = nil
			detail = fmt.Sprintf("discovery probe panicked: %v", r)
		}
	}()
	return src.Discover()
}

// ---------------------------------------------------------------------------
// The single merge point
// ---------------------------------------------------------------------------

// ResolveModels combines the CLI-discovered list with the ACP-reported list and
// returns the list the user should see.
//
// The rule set, in one place instead of three:
//
//   - When ACP reports models, they are authoritative for MEMBERSHIP: the agent
//     knows which models it can actually run, and a CLI list can be stale (a
//     redirected endpoint, a model retired server-side). A CLI model absent
//     from the ACP list is dropped.
//   - The CLI list is authoritative for ORDER and for names it supplies; an ACP
//     display name wins for a matching ID because it reflects the live runtime
//     (e.g. an env redirect showing the real backing model).
//   - ACP-only models are appended, in ACP order.
//   - Exactly one model is default. Priority: the session's current model, then
//     the CLI default flag, then the first entry.
//
// Inputs are never mutated.
func ResolveModels(cliModels, acpModels []AgentModel, currentModelID string) []AgentModel {
	acpByID, acpOrder := indexACPModels(acpModels)

	// Split the ACP list into tier aliases and concrete models. The two behave
	// differently: a concrete ACP list is authoritative for membership, while an
	// alias list cannot be, because its IDs never match a CLI model ID.
	aliasNames, concreteACP := splitTierAliases(acpByID)

	seen := make(map[string]struct{}, len(cliModels)+len(acpByID))
	out := make([]AgentModel, 0, len(cliModels)+len(acpByID))
	usedAliases := make(map[string]struct{})

	// The CLI list supplies order and names; ACP contributes display names and
	// (when concrete) membership.
	cliDefaultID := ""
	for _, m := range cliModels {
		entry, ok := resolveCLIEntry(m, acpByID, concreteACP, aliasNames, seen, usedAliases)
		if !ok {
			continue
		}
		if m.Default && cliDefaultID == "" {
			cliDefaultID = strings.TrimSpace(m.ID)
		}
		out = append(out, entry)
	}

	// ACP-only entries: concrete models, plus tier aliases no CLI entry covered.
	for _, id := range acpOrder {
		entry, ok := resolveACPOnlyEntry(id, acpByID, usedAliases, seen)
		if !ok {
			continue
		}
		out = append(out, entry)
	}

	if len(out) == 0 {
		return nil
	}

	defaultID := pickDefaultID(out, currentModelID, cliDefaultID)
	for i := range out {
		out[i].Default = out[i].ID == defaultID
	}
	return out
}

// resolveCLIEntry maps one CLI-discovered model to its resolved entry.
//
// It reports false when the model must be dropped: a blank/duplicate ID, or an ID
// the concrete ACP list does not report (the runtime cannot run it). Tier aliases
// are handled separately — they name a tier rather than assert membership, so
// they never cause a drop.
func resolveCLIEntry(
	m AgentModel,
	acpByID map[string]AgentModel,
	concreteACP map[string]AgentModel,
	aliasNames map[string]string,
	seen map[string]struct{},
	usedAliases map[string]struct{},
) (AgentModel, bool) {
	id := strings.TrimSpace(m.ID)
	if id == "" {
		return AgentModel{}, false
	}
	if _, dup := seen[id]; dup {
		return AgentModel{}, false
	}
	if len(concreteACP) > 0 {
		if _, supported := concreteACP[id]; !supported {
			return AgentModel{}, false
		}
	}
	seen[id] = struct{}{}

	name := m.Name
	switch {
	case strings.TrimSpace(acpByID[id].Name) != "":
		// An ACP entry for this exact ID is the most specific name.
		name = acpByID[id].Name
	default:
		// A tier alias names the entry for its tier while the concrete ID stays,
		// so the user sees the real backing model but the CLI still receives an
		// ID it understands.
		if alias, ok := aliasForCLIModel(id, aliasNames); ok {
			name = aliasNames[alias]
			usedAliases[alias] = struct{}{}
		}
	}
	return AgentModel{ID: id, Name: name}, true
}

// resolveACPOnlyEntry maps an ACP entry with no CLI counterpart. A tier alias is
// still offered (a selectable tier must not be silently lost) except for the meta
// "default" marker, which is a fallback, not a model.
func resolveACPOnlyEntry(
	id string,
	acpByID map[string]AgentModel,
	usedAliases map[string]struct{},
	seen map[string]struct{},
) (AgentModel, bool) {
	if _, dup := seen[id]; dup {
		return AgentModel{}, false
	}
	if alias, isAlias := aliasNameFor(id); isAlias {
		if alias == metaTierDefault {
			return AgentModel{}, false
		}
		if _, used := usedAliases[alias]; used {
			return AgentModel{}, false
		}
	}
	seen[id] = struct{}{}
	return AgentModel{ID: id, Name: acpByID[id].Name}, true
}

// tierAliases are ACP model IDs that name a capability TIER rather than a model.
// The claude ACP agent uses them to expose a redirected endpoint: the alias ID is
// what the CLI accepts, and its display name carries the real backing model.
var tierAliases = map[string]struct{}{
	"default": {}, "opus": {}, "sonnet": {}, "haiku": {}, "fast": {}, "plan": {},
}

// metaTierDefault is the fallback marker alias, which is not a selectable model.
const metaTierDefault = "default"

// isTierAliasID reports whether an ID is a tier alias rather than a model ID.
// Case and separators are ignored, so "Opus" and "claude-opus" normalize alike.
func isTierAliasID(id string) bool {
	_, ok := aliasNameFor(id)
	return ok
}

// aliasNameFor normalizes an ID and reports the alias it denotes.
func aliasNameFor(id string) (string, bool) {
	normalized := strings.ToLower(id)
	var b strings.Builder
	for _, r := range normalized {
		if r >= 'a' && r <= 'z' {
			b.WriteRune(r)
		}
	}
	candidate := b.String()
	if _, ok := tierAliases[candidate]; ok {
		return candidate, true
	}
	return "", false
}

// splitTierAliases separates the ACP list into alias display names (keyed by
// alias) and the concrete models. An alias with no display name is ignored: it
// would contribute nothing.
func splitTierAliases(acpByID map[string]AgentModel) (map[string]string, map[string]AgentModel) {
	aliasNames := make(map[string]string)
	concrete := make(map[string]AgentModel, len(acpByID))

	for id, m := range acpByID {
		alias, isAlias := aliasNameFor(id)
		if !isAlias {
			concrete[id] = m
			continue
		}
		if alias == metaTierDefault {
			continue
		}
		if name := strings.TrimSpace(m.Name); name != "" {
			aliasNames[alias] = name
		}
	}
	return aliasNames, concrete
}

// aliasForCLIModel finds the tier alias a concrete CLI model ID belongs to, by
// looking for the alias as a token of the ID. "claude-sonnet-4-6" matches
// "sonnet"; "claude-opus-4-5" matches "opus".
func aliasForCLIModel(cliID string, aliasNames map[string]string) (string, bool) {
	if len(aliasNames) == 0 {
		return "", false
	}
	tokens := tokenizeModelID(cliID)
	for alias := range aliasNames {
		if _, ok := tokens[alias]; ok {
			return alias, true
		}
	}
	return "", false
}

// tokenizeModelID splits a model ID into lowercase alphabetic tokens.
func tokenizeModelID(id string) map[string]struct{} {
	tokens := make(map[string]struct{})
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		default:
			if b.Len() > 0 {
				tokens[b.String()] = struct{}{}
				b.Reset()
			}
		}
	}
	if b.Len() > 0 {
		tokens[b.String()] = struct{}{}
	}
	return tokens
}

// indexACPModels builds a lookup and a stable ordering for the ACP list, dropping
// blank and duplicate IDs.
func indexACPModels(acpModels []AgentModel) (map[string]AgentModel, []string) {
	byID := make(map[string]AgentModel, len(acpModels))
	order := make([]string, 0, len(acpModels))
	for _, m := range acpModels {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		if _, seen := byID[id]; seen {
			continue
		}
		m.ID = id
		byID[id] = m
		order = append(order, id)
	}
	return byID, order
}

// pickDefaultID chooses the single default model.
//
// Priority: the session's current model, then the CLI default flag, then the
// first entry. A current ID that is not in the resolved list (the agent retired
// it, or the ACP list dropped it) must not leave the list with no default at all.
func pickDefaultID(models []AgentModel, currentModelID, cliDefaultID string) string {
	if currentModelID != "" && containsID(models, currentModelID) {
		return currentModelID
	}
	if cliDefaultID != "" && containsID(models, cliDefaultID) {
		return cliDefaultID
	}
	return models[0].ID
}

func containsID(models []AgentModel, id string) bool {
	for _, m := range models {
		if m.ID == id {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Source constructors
// ---------------------------------------------------------------------------

// StaticSource declares a built-in catalog, available only while the named CLI
// is installed. Pass an empty command for backends with no CLI (mock,
// ACP-only): those are always considered available.
//
// A static source is the right shape for a backend whose CLI has no model-list
// command — the catalog is maintained in catalogs.go rather than scattered
// through the backend package.
func StaticSource(backend, cliCmd string, catalog []AgentModel) ModelSource {
	return &staticSource{backend: backend, cliCmd: cliCmd, catalog: catalog}
}

type staticSource struct {
	backend string
	cliCmd  string
	catalog []AgentModel
}

func (s *staticSource) Backend() string       { return s.backend }
func (s *staticSource) Kind() ModelSourceKind { return SourceKindStatic }

func (s *staticSource) Discover() ([]AgentModel, string) {
	if s.cliCmd != "" && !CheckCLIExists(s.cliCmd) {
		return nil, ""
	}
	models := cloneModels(s.catalog)
	markFirstDefault(models)
	return models, ""
}

// CommandSpec is one CLI invocation to try.
type CommandSpec struct {
	Command string
	Args    []string
	// CombineStderr appends stderr to stdout before parsing. Some CLIs print
	// their model table to stderr.
	CombineStderr bool
}

// CLIOptions configures a command-based source.
type CLIOptions struct {
	// Command/Args describe a single invocation. Ignored when Commands is set.
	Command string
	Args    []string
	// CombineStderr appends stderr to stdout before parsing.
	CombineStderr bool
	// Commands lists invocations to try in order, for backends with a legacy
	// command name (e.g. codewhale before deepseek).
	Commands []CommandSpec
	// Parse turns raw output into models. Required.
	Parse func(output string) []AgentModel
	// Fallback is used when every command fails or parses to nothing. Its first
	// entry is marked default.
	Fallback []AgentModel
	// Timeout bounds each command. Defaults to 10s.
	Timeout time.Duration
}

// NewCLISource builds a source that runs a CLI subcommand and parses its output.
func NewCLISource(backend string, opts CLIOptions) ModelSource {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if len(opts.Commands) == 0 && opts.Command != "" {
		opts.Commands = []CommandSpec{{
			Command:       opts.Command,
			Args:          opts.Args,
			CombineStderr: opts.CombineStderr,
		}}
	}
	return &cliSource{backend: backend, opts: opts}
}

type cliSource struct {
	backend string
	opts    CLIOptions
}

func (s *cliSource) Backend() string       { return s.backend }
func (s *cliSource) Kind() ModelSourceKind { return SourceKindCLI }

func (s *cliSource) Discover() ([]AgentModel, string) {
	var tried []string
	for _, spec := range s.opts.Commands {
		if spec.Command == "" {
			continue
		}
		tried = append(tried, spec.Command)
		output, err := runDiscoveryCommand(spec, s.opts.Timeout)
		if err != nil {
			slog.Debug("model discovery: command failed",
				"backend", s.backend, "command", spec.Command, "error", err)
			continue
		}
		if models := s.opts.Parse(output); len(models) > 0 {
			markFirstDefault(models)
			slog.Info("model discovery succeeded",
				"backend", s.backend, "command", spec.Command, "models", len(models))
			return models, ""
		}
	}

	if len(s.opts.Fallback) > 0 {
		models := cloneModels(s.opts.Fallback)
		markFirstDefault(models)
		slog.Info("model discovery: using fallback catalog",
			"backend", s.backend, "models", len(models))
		return models, ""
	}

	if len(tried) == 0 {
		return nil, fmt.Sprintf("%s: no command configured", s.backend)
	}
	detail := fmt.Sprintf("%s: no models parsed from %s", s.backend, strings.Join(tried, ", "))
	slog.Debug("model discovery: no models parsed", "backend", s.backend, "commands", tried)
	return nil, detail
}

func runDiscoveryCommand(spec CommandSpec, timeout time.Duration) (string, error) {
	ctx, cancel := commandContext(timeout)
	defer cancel()

	stdout, stderr, err := RunCommandContext(ctx, spec.Command, spec.Args...)
	if err != nil {
		return "", err
	}
	if spec.CombineStderr {
		return stdout + stderr, nil
	}
	return stdout, nil
}

// PluginSource wraps a custom probe for backends whose layout needs more than a
// single command: config files, binary string extraction, bundled JS.
//
// The probe returns its own failure detail so the reason reaches the API
// response without any package-level state.
func PluginSource(backend string, probe func() ([]AgentModel, string)) ModelSource {
	return &pluginSource{backend: backend, probe: probe}
}

type pluginSource struct {
	backend string
	probe   func() ([]AgentModel, string)
}

func (s *pluginSource) Backend() string       { return s.backend }
func (s *pluginSource) Kind() ModelSourceKind { return SourceKindPlugin }

func (s *pluginSource) Discover() ([]AgentModel, string) {
	if s.probe == nil {
		return nil, ""
	}
	models, detail := s.probe()
	if len(models) == 0 {
		return nil, detail
	}
	// Copy before flagging the default: probes commonly return a package-level
	// catalog by reference (e.g. ClaudeCatalog), and writing into it would mutate
	// shared state that other callers read concurrently.
	models = cloneModels(models)
	markFirstDefault(models)
	slog.Info("model discovery succeeded (plugin)", "backend", s.backend, "models", len(models))
	return models, ""
}
