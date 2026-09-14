package model

import (
	"context"
	"regexp"
	"strings"
	"time"
)

// commandContext returns a context bounded by timeout, for CLI probes.
func commandContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), timeout)
}

// ---------------------------------------------------------------------------
// Shared post-processing
// ---------------------------------------------------------------------------

// markFirstDefault marks the first entry as default when nothing else is marked.
// Discovery output frequently omits the flag entirely, and every consumer
// (default model selection, the model picker's badge) expects exactly one.
func markFirstDefault(models []AgentModel) {
	if len(models) == 0 {
		return
	}
	for _, m := range models {
		if m.Default {
			return
		}
	}
	models[0].Default = true
}

// dedupeModels collapses duplicate IDs, keeps the first occurrence, and drops
// blank IDs. Callers pass freshly-parsed slices, so it trims in place.
func dedupeModels(models []AgentModel) []AgentModel {
	out := make([]AgentModel, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, m := range models {
		id := strings.TrimSpace(m.ID)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		m.ID = id
		out = append(out, m)
	}
	return out
}

// displayName resolves a model's display name, falling back to the raw ID.
func displayName(id string, names map[string]string) string {
	if name, ok := names[id]; ok && name != "" {
		return name
	}
	return id
}

// ---------------------------------------------------------------------------
// PlainLineOptions / ParsePlainLines — one model ID per line
// ---------------------------------------------------------------------------

// PlainLineOptions configures ParsePlainLines.
type PlainLineOptions struct {
	// Names maps model IDs to display names; unmapped IDs use the raw ID.
	Names map[string]string
	// Skip drops lines that are diagnostics rather than model entries.
	Skip func(line string) bool
	// SkipExact drops lines equal to one of these strings.
	SkipExact []string
	// SkipPrefix drops lines with one of these prefixes.
	SkipPrefix []string
	// SkipContains drops lines containing one of these substrings.
	SkipContains []string
}

// ParsePlainLines parses output where each non-empty line is a model ID.
// Used by backends whose CLI prints a bare list (antigravity).
func ParsePlainLines(output string, opts PlainLineOptions) []AgentModel {
	exact := make(map[string]struct{}, len(opts.SkipExact))
	for _, s := range opts.SkipExact {
		exact[s] = struct{}{}
	}

	var models []AgentModel
	seen := make(map[string]struct{})
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if _, skip := exact[line]; skip {
			continue
		}
		if hasAnyPrefix(line, opts.SkipPrefix) || hasAnySubstring(line, opts.SkipContains) {
			continue
		}
		if opts.Skip != nil && opts.Skip(line) {
			continue
		}
		if _, dup := seen[line]; dup {
			continue
		}
		seen[line] = struct{}{}
		models = append(models, AgentModel{ID: line, Name: displayName(line, opts.Names)})
	}
	if len(models) == 0 {
		return nil
	}
	markFirstDefault(models)
	return models
}

// ---------------------------------------------------------------------------
// ParseTabular — "provider model ..." rows
// ---------------------------------------------------------------------------

// ParseTabular parses whitespace-separated tables whose first two columns are
// provider and model (pi's `--list-models`). Models are named "provider/model"
// so identical model names from different providers stay distinguishable.
func ParseTabular(output string) []AgentModel {
	var models []AgentModel
	for _, raw := range strings.Split(output, "\n") {
		fields := strings.Fields(raw)
		if len(fields) < 2 {
			continue
		}
		provider, modelID := fields[0], fields[1]
		// Header row, e.g. "provider  model  context  max-out".
		if strings.EqualFold(provider, "provider") || strings.EqualFold(modelID, "model") {
			continue
		}
		full := provider + "/" + modelID
		models = append(models, AgentModel{ID: full, Name: full})
	}
	models = dedupeModels(models)
	if len(models) == 0 {
		return nil
	}
	markFirstDefault(models)
	return models
}

// ---------------------------------------------------------------------------
// ParseProviderModel — "provider/model" one per line
// ---------------------------------------------------------------------------

var providerModelRe = regexp.MustCompile(`^(\S+)/(\S+)$`)

// ParseProviderModel parses output of one "provider/model" per line (opencode).
// The full "provider/model" string is the ID, since that is what the CLI expects
// when selecting a model.
func ParseProviderModel(output string) []AgentModel {
	var models []AgentModel
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || !providerModelRe.MatchString(line) {
			continue
		}
		models = append(models, AgentModel{ID: line, Name: line})
	}
	models = dedupeModels(models)
	if len(models) == 0 {
		return nil
	}
	markFirstDefault(models)
	return models
}

// ---------------------------------------------------------------------------
// RegexOptions / ParseRegexCapture — regex-driven extraction
// ---------------------------------------------------------------------------

// RegexOptions configures ParseRegexCapture.
type RegexOptions struct {
	// Pattern is applied per line. An invalid pattern yields no models.
	Pattern string
	// IDGroup selects the capture group holding the model ID. 0 (or a group
	// that did not participate) uses the whole match.
	IDGroup int
	// NameGroup optionally selects a display-name capture group.
	NameGroup int
	// DefaultGroup/DefaultValue mark the default: when the capture equals
	// DefaultValue, the entry is flagged.
	DefaultGroup int
	DefaultValue string
	// DefaultLinePattern matches a header line naming the default model, e.g.
	// "Available models (default: deepseek-v4-pro)". Lines matching it are not
	// treated as entries. DefaultLineGroup selects the ID capture group.
	DefaultLinePattern string
	DefaultLineGroup   int
	// Transform, when set, receives the submatch and returns the final ID.
	// Use it to compose an ID from several groups (e.g. "provider/id").
	Transform func(submatches []string) string
	// Filter, when set, receives the submatch and reports whether the line is a
	// usable model entry.
	Filter func(submatches []string) bool
	// Names maps model IDs to display names.
	Names map[string]string
}

// regexEntry is one parsed line, carrying both the final ID and the ID as it
// appeared in the output.
type regexEntry struct {
	model AgentModel
	// rawID is the ID before Transform. A header-named default refers to this
	// form, while the model list uses the transformed form.
	rawID string
}

// ParseRegexCapture extracts models from line-oriented output using a regular
// expression (deepseek, grok). Lines that do not match are skipped, which is
// what makes this robust against banners and progress noise.
func ParseRegexCapture(output string, opts RegexOptions) []AgentModel {
	re, err := regexp.Compile(opts.Pattern)
	if err != nil {
		return nil
	}
	var defaultLineRe *regexp.Regexp
	if opts.DefaultLinePattern != "" {
		if defaultLineRe, err = regexp.Compile(opts.DefaultLinePattern); err != nil {
			return nil
		}
	}

	var entries []regexEntry
	seen := make(map[string]struct{})
	defaultID := ""

	for _, raw := range strings.Split(output, "\n") {
		if defaultLineRe != nil {
			if m := defaultLineRe.FindStringSubmatch(raw); m != nil {
				defaultID = captureGroup(m, opts.DefaultLineGroup)
				continue // a header line is never a model entry
			}
		}

		entry, ok := parseRegexEntry(re, raw, opts, seen)
		if !ok {
			continue
		}
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		return nil
	}

	models := make([]AgentModel, 0, len(entries))
	for _, e := range entries {
		models = append(models, e.model)
	}

	// A header-named default beats the first-entry fallback. Match against the
	// raw ID (the form the header uses) as well as the final ID.
	if defaultID != "" && !hasDefault(models) {
		applyNamedDefault(models, entries, defaultID)
	}
	markFirstDefault(models)
	return models
}

// parseRegexEntry turns one output line into a model entry. It reports false when
// the line does not match, is filtered out, or yields a blank/duplicate ID.
func parseRegexEntry(re *regexp.Regexp, raw string, opts RegexOptions, seen map[string]struct{}) (regexEntry, bool) {
	m := re.FindStringSubmatch(raw)
	if m == nil {
		return regexEntry{}, false
	}
	if opts.Filter != nil && !opts.Filter(m) {
		return regexEntry{}, false
	}

	rawID := captureGroup(m, opts.IDGroup)
	if rawID == "" {
		rawID = strings.TrimSpace(m[0])
	}

	id := rawID
	if opts.Transform != nil {
		id = strings.TrimSpace(opts.Transform(m))
	}
	if id == "" {
		return regexEntry{}, false
	}
	if _, dup := seen[id]; dup {
		return regexEntry{}, false
	}
	seen[id] = struct{}{}

	name := captureGroup(m, opts.NameGroup)
	if name == "" {
		name = displayName(id, opts.Names)
	}

	isDefault := opts.DefaultValue != "" &&
		captureGroup(m, opts.DefaultGroup) == opts.DefaultValue

	return regexEntry{
		model: AgentModel{ID: id, Name: name, Default: isDefault},
		rawID: rawID,
	}, true
}

// applyNamedDefault flags the entry named by a header line as the default.
func applyNamedDefault(models []AgentModel, entries []regexEntry, defaultID string) {
	for i, e := range entries {
		if e.rawID == defaultID || e.model.ID == defaultID {
			models[i].Default = true
			return
		}
	}
}

// captureGroup returns submatch n, trimmed, or "" when n is out of range.
func captureGroup(m []string, n int) string {
	if n <= 0 || n >= len(m) {
		return ""
	}
	return strings.TrimSpace(m[n])
}

// hasDefault reports whether any entry is already flagged as default.
func hasDefault(models []AgentModel) bool {
	for _, m := range models {
		if m.Default {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// BulletOptions / ParseBulletList — bulleted lists with a default line
// ---------------------------------------------------------------------------

// BulletOptions configures ParseBulletList.
type BulletOptions struct {
	// Names maps model IDs to display names.
	Names map[string]string
	// Skip drops lines that are headers rather than entries.
	Skip func(line string) bool
}

// defaultLinePrefixes are recognized "Default model: X" style markers.
var defaultLinePrefixes = []string{"default model:", "default:"}

// headerWords are bullet-list entries that are section headers, not models.
var headerWords = map[string]struct{}{
	"available": {}, "models": {}, "model": {}, "default": {}, "available models": {},
}

// bulletRe matches a bulleted entry and captures the leading token.
var bulletRe = regexp.MustCompile(`^[\s*+\-]*([A-Za-z0-9][A-Za-z0-9._\-]*)`)

// ParseBulletList parses output where models appear as bulleted entries, with an
// optional "(default)" suffix and an optional "Default model: X" line (grok).
// Exactly one entry is marked default: the "(default)" suffix wins, then the
// explicit default line, then the first entry.
func ParseBulletList(output string, opts BulletOptions) []AgentModel {
	state := &bulletParseState{seen: make(map[string]struct{})}

	for _, raw := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if id, ok := captureDefaultLine(trimmed); ok {
			state.defaultID = id
			continue
		}
		if id, ok := bulletEntryID(trimmed, opts); ok {
			state.append(id, trimmed, opts)
		}
	}

	if len(state.models) == 0 {
		return nil
	}
	state.finalizeDefault()
	return state.models
}

// bulletParseState accumulates entries while scanning a bullet list.
type bulletParseState struct {
	models        []AgentModel
	seen          map[string]struct{}
	defaultID     string
	defaultMarked bool
}

// append records an entry, unless its ID was already seen.
func (st *bulletParseState) append(id, line string, opts BulletOptions) {
	if _, dup := st.seen[id]; dup {
		return
	}
	st.seen[id] = struct{}{}

	isDefault := strings.Contains(strings.ToLower(line), "(default)") && !st.defaultMarked
	if isDefault {
		st.defaultMarked = true
	}
	st.models = append(st.models, AgentModel{
		ID:      id,
		Name:    displayName(id, opts.Names),
		Default: isDefault,
	})
}

// finalizeDefault ensures exactly one entry is the default: an explicit
// "Default model:" line wins, then the first entry.
func (st *bulletParseState) finalizeDefault() {
	if !st.defaultMarked && st.defaultID != "" {
		for i := range st.models {
			if st.models[i].ID == st.defaultID {
				st.models[i].Default = true
				st.defaultMarked = true
				return
			}
		}
	}
	if !st.defaultMarked {
		st.models[0].Default = true
	}
}

// bulletEntryID extracts a model ID from one line, skipping section headers,
// prose and caller-supplied exclusions. It reports false when the line is not a
// usable entry.
func bulletEntryID(trimmed string, opts BulletOptions) (string, bool) {
	if opts.Skip != nil && opts.Skip(trimmed) {
		return "", false
	}
	if _, header := headerWords[strings.ToLower(trimmed)]; header {
		return "", false
	}

	// A line with no bullet marker is only usable when it is a bare token.
	if !strings.ContainsAny(trimmed, "-*+") && !isModelToken(trimmed) {
		return "", false
	}

	m := bulletRe.FindStringSubmatch(trimmed)
	if len(m) < 2 {
		return "", false
	}
	id := m[1]
	if _, header := headerWords[strings.ToLower(id)]; header {
		return "", false
	}
	return id, true
}

// captureDefaultLine recognizes an explicit "Default model: X" line.
func captureDefaultLine(trimmed string) (string, bool) {
	lower := strings.ToLower(trimmed)
	for _, prefix := range defaultLinePrefixes {
		if !strings.HasPrefix(lower, prefix) {
			continue
		}
		if id := strings.TrimSpace(trimmed[len(prefix):]); id != "" {
			return id, true
		}
		return "", true
	}
	return "", false
}

// isModelToken reports whether a bare (unbulleted) line looks like a model ID
// rather than prose: a single token, no spaces.
func isModelToken(line string) bool {
	return !strings.ContainsAny(line, " \t")
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

func hasAnyPrefix(line string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

func hasAnySubstring(line string, subs []string) bool {
	for _, s := range subs {
		if s != "" && strings.Contains(line, s) {
			return true
		}
	}
	return false
}
