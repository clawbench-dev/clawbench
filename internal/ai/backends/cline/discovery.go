package cline

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"clawbench/internal/model"
)

// ---------------------------------------------------------------------------
// Model discovery.
//
// cline has no `models` subcommand (`cline --help` lists auth / config /
// plugin / skill / mcp / … and nothing that prints a model list). The list the
// ACP agent reports at session/new comes from cline's own provider catalog,
// which is EMBEDDED in the shipped bundle:
//
//	@cline/llms/dist/models.js  →  MODEL_COLLECTIONS_BY_PROVIDER_ID
//	{"<provider id>": {"provider": {…, "defaultModelId", …},
//	                   "models": {"<model id>": {"id","name", …}, …}}, …}
//
// Measured on 3.0.75: 328 providers / 7570 entries / 3239 distinct ids.
//
// The export is a Proxy over several merged tables, so the module is scanned as
// text rather than evaluated (evaluating it would run the CLI's JS in our
// process). Model entries are recognized by shape, not by which table they came
// from: `"<id>":{id:"<id>",name:"…",…,contextWindow:N,…`. The contextWindow key
// separates them from unrelated `{id, name}` records (plugin manifests,
// commands), and every model entry in the file carries it.
//
// The probe never starts a session, so the picker has a list before the first
// sign-in. A detail string with an empty result is deliberate: the picker then
// falls back to what the ACP session reports once an account is connected.
// ---------------------------------------------------------------------------

// clineLLMSModelsRelPath is the catalog file inside @cline/llms.
var clineLLMSModelsRelPath = filepath.Join("@cline", "llms", "dist", "models.js")

// clineModelEntryRe matches the start of one model entry: its key, its id and
// (usually) its display name.
var clineModelEntryRe = regexp.MustCompile(`"([^"\\]+)":\{id:"([^"]+)",(?:name:"([^"]*)",)?`)

// clineModelWindowLookahead bounds how far past the entry start contextWindow
// may appear. In the generated table it is a handful of keys in; a wide window
// only exists so a reordering upstream does not empty the whole list.
const clineModelWindowLookahead = 400

// clinePackageRoots lists the node roots probed for @cline/llms, most specific
// first. The CLI is normally a global npm package, so the global prefix covers
// the common case; CLAWBENCH_CLINE_ROOT lets a non-standard install say where
// its node_modules lives.
func clinePackageRoots() []string {
	var roots []string
	if explicit := strings.TrimSpace(os.Getenv("CLAWBENCH_CLINE_ROOT")); explicit != "" {
		roots = append(roots, explicit)
	}
	if bin, err := exec.LookPath("cline"); err == nil {
		binDir := filepath.Dir(bin)
		if resolved, rerr := filepath.EvalSymlinks(bin); rerr == nil {
			binDir = filepath.Dir(resolved)
		}
		if idx := strings.LastIndex(binDir, "node_modules"); idx >= 0 {
			roots = append(roots, binDir[:idx])
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, "npm", "root", "-g").Output(); err == nil {
		if p := strings.TrimSpace(string(out)); p != "" {
			roots = append(roots, p)
		}
	}
	return roots
}

// catalogPathFor returns the catalog path under one node root. A root may be
// the directory holding node_modules or node_modules itself; both are accepted
// because `npm root -g` names the latter while a resolved bin directory points
// inside the former.
func catalogPathFor(root string) string {
	if filepath.Base(root) == "node_modules" {
		return filepath.Join(root, clineLLMSModelsRelPath)
	}
	return filepath.Join(root, "node_modules", clineLLMSModelsRelPath)
}

// discoverClineModels reads cline's bundled provider catalog.
func discoverClineModels() ([]model.AgentModel, string) {
	return discoverClineModelsFrom(clinePackageRoots)
}

// discoverClineModelsFrom is discoverClineModels with the roots supplied, so a
// test can point it at a fixture without depending on what is installed here.
func discoverClineModelsFrom(roots func() []string) ([]model.AgentModel, string) {
	var lastErr string
	for _, root := range roots() {
		path := catalogPathFor(root)
		data, err := os.ReadFile(path)
		if err != nil {
			lastErr = fmt.Sprintf("cline: %s not readable: %v", path, err)
			continue
		}
		models := parseClineModelCatalog(string(data))
		if len(models) > 0 {
			return models, ""
		}
		lastErr = fmt.Sprintf("cline: %s contains no model entries", path)
	}
	if lastErr == "" {
		lastErr = "cline: bundled model catalog not found; set CLAWBENCH_CLINE_ROOT to the directory holding node_modules"
	}
	return nil, lastErr
}

// parseClineModelCatalog extracts every model entry from models.js.
//
// Order is the file's order and duplicates keep their first occurrence, so the
// result is stable across calls (the picker marks the first entry as default).
func parseClineModelCatalog(src string) []model.AgentModel {
	matches := clineModelEntryRe.FindAllStringSubmatchIndex(src, -1)
	var out []model.AgentModel
	seen := make(map[string]bool, len(matches))
	for _, m := range matches {
		key, id := src[m[2]:m[3]], src[m[4]:m[5]]
		if key != id || id == "" {
			// The generator writes the key and the id identically. A mismatch
			// means this is not a model entry.
			continue
		}
		window := src[m[1]:minInt(m[1]+clineModelWindowLookahead, len(src))]
		if !strings.Contains(window, "contextWindow:") {
			continue
		}
		if seen[id] {
			continue
		}
		name := id
		if m[6] >= 0 {
			if n := src[m[6]:m[7]]; n != "" {
				name = n
			}
		}
		seen[id] = true
		out = append(out, model.AgentModel{ID: id, Name: name})
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
