package service

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"clawbench/internal/api"
	"clawbench/internal/store"
)

// This file is the drift guard for numbers and table lists that the spec docs
// restate from code. Every one of them is a hand-maintained copy of a value the
// code already knows, and every one of them has silently drifted at least once:
//
//   - `docs/spec/database.md` documented a table that does not exist and missed
//     more than a dozen that do;
//   - `docs/spec/README.md` / `api/README.md` claimed 176 paths / 224 operations
//     while the embedded spec had 178 / 226;
//   - `setup-wizard.md` claimed 15 and 16 backends in two different places while
//     the registry has 18.
//
// `openapi_drift_test.go` already compares the spec against the live mux, but it
// compares *sets of routes* — it says nothing about the counts the prose quotes,
// so a stale number never failed CI. These tests close that gap.
//
// Design rule: the code is the single source of truth, the doc is the claim
// under test. Each guard computes the real value from the live code and then
// asserts the doc's stated number equals it. A guard that only checked "the doc
// contains some number" would pass on any value and is not written here.

// specDocFiles are the spec documents that restate code-derived counts. The
// historical audit under docs/dev/ is deliberately excluded: it is a frozen
// snapshot of what the docs said on a given date, so its numbers are supposed
// to differ from today's code.
func specDocFiles(t *testing.T) []string {
	t.Helper()
	root := repoRoot(t)
	var out []string
	err := filepath.WalkDir(filepath.Join(root, "docs", "spec"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs/spec: %v", err)
	}
	sort.Strings(out)
	if len(out) == 0 {
		t.Fatal("no spec docs found — the guard would be vacuous")
	}
	return out
}

// readSpecDocs returns repo-relative path → content for every spec doc.
func readSpecDocs(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	out := map[string]string{}
	for _, p := range specDocFiles(t) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			rel = p
		}
		out[rel] = string(b)
	}
	return out
}

// openAPICountRe matches the "N paths / M operations" claim the spec docs make.
//
// The unit word 个 is OPTIONAL because both spellings occur in the docs:
// api/README.md writes "178 个路径 / 227 个操作" while docs/spec/README.md
// writes "178 路径 / 227 操作". Requiring it left spec/README.md matching zero
// claims, so its count could drift freely — the exact failure this guard exists
// to prevent.
//
// A bare "N / M" is still NOT matched: the docs are full of unrelated ratios
// that look identical ("实测 116/116 全无该键", "3/112 vs 6/112", "119/119
// 逐字节保真"), and a looser pattern reported every one of them as an OpenAPI
// count drift. The 路径/操作 unit words are what disambiguate.
var openAPICountRe = regexp.MustCompile(`(\d+)\s*个?路径\s*/\s*(\d+)\s*个?操作`)

// openAPICountDocs are the spec docs that are REQUIRED to carry an
// "N paths / M operations" claim. Each is checked individually: a global
// "claims > 0" check cannot see one file losing its claim entirely, which is
// how spec/README.md went uncovered.
var openAPICountDocs = []string{
	filepath.Join("docs", "spec", "README.md"),
	filepath.Join("docs", "spec", "api", "README.md"),
}

// TestSpecDocsOpenAPICountsMatchSpec pins every "N paths / M operations" claim
// in the spec docs against the embedded OpenAPI document.
//
// The counting convention is the one api/README.md documents: every key under
// `paths:` (including the non-/api/ `/login` and `/share/{token}`), and every
// HTTP method declared on them. SpecRoutes() yields one entry per path+method,
// so the path count is the number of distinct paths.
func TestSpecDocsOpenAPICountsMatchSpec(t *testing.T) {
	routes, err := api.SpecRoutes()
	if err != nil {
		t.Fatalf("embedded OpenAPI spec must parse: %v", err)
	}
	if len(routes) == 0 {
		t.Fatal("spec yielded no routes — the guard would be vacuous")
	}

	paths := map[string]bool{}
	for _, r := range routes {
		paths[r.Path] = true
	}
	wantPaths, wantOps := len(paths), len(routes)

	docs := readSpecDocs(t)
	for _, required := range openAPICountDocs {
		body, ok := docs[required]
		if !ok {
			t.Fatalf("%s is missing — the guard cannot check it", required)
		}
		if !openAPICountRe.MatchString(body) {
			t.Errorf("%s contains no \"N (个)路径 / M (个)操作\" claim. Either the wording changed (update openAPICountRe) or the count was dropped — this file must state it", required)
		}
	}

	claims := 0
	for file, body := range docs {
		for _, m := range openAPICountRe.FindAllStringSubmatchIndex(body, -1) {
			claims++
			gotPaths, _ := strconv.Atoi(body[m[2]:m[3]])
			gotOps, _ := strconv.Atoi(body[m[4]:m[5]])
			line := lineOf(body, m[0])
			if gotPaths != wantPaths || gotOps != wantOps {
				t.Errorf("%s:%d claims %d paths / %d operations, but the embedded spec has %d / %d — update the prose (the code is authoritative)",
					file, line, gotPaths, gotOps, wantPaths, wantOps)
			}
		}
	}
	// Without this, rewording the docs (or breaking the regex) would turn the
	// guard into a no-op that always passes.
	if claims == 0 {
		t.Fatal("no \"N (个)路径 / M (个)操作\" claim found in the spec docs — the guard would be vacuous; if the wording changed, update the regex")
	}
}

// TestOpenAPICountReCoversBothSpellings is the regression test for the guard
// itself, independent of the live docs.
//
// The guard originally required the unit word 个, which silently excluded
// docs/spec/README.md ("178 路径 / 227 操作" — no 个) from every check. These
// cases pin both directions: the spellings that MUST match, and the bare ratios
// that must not (a regex loose enough to catch everything would drown the guard
// in false positives and get weakened again).
func TestOpenAPICountReCoversBothSpellings(t *testing.T) {
	mustMatch := []string{
		"178 个路径 / 227 个操作", // api/README.md
		"178 路径 / 227 操作",   // docs/spec/README.md
		"178个路径/227个操作",     // no spaces
	}
	for _, s := range mustMatch {
		m := openAPICountRe.FindStringSubmatch(s)
		if m == nil {
			t.Errorf("%q must be recognized as an OpenAPI count claim; the guard cannot see it", s)
			continue
		}
		if m[1] != "178" || m[2] != "227" {
			t.Errorf("%q parsed as %s/%s, want 178/227", s, m[1], m[2])
		}
	}

	mustNotMatch := []string{
		"实测 116/116 全无该键",
		"3/112 vs 6/112",
		"119/119 逐字节保真",
	}
	for _, s := range mustNotMatch {
		if openAPICountRe.MatchString(s) {
			t.Errorf("%q is an unrelated ratio, not an OpenAPI count; matching it makes the guard noisy", s)
		}
	}
}

// TestOpenAPICountDocsAllCarryAClaim pins the per-file requirement against the
// real docs. Without it, adding a file to openAPICountDocs that never states a
// count would be a silent no-op.
func TestOpenAPICountDocsAllCarryAClaim(t *testing.T) {
	docs := readSpecDocs(t)
	for _, required := range openAPICountDocs {
		body, ok := docs[required]
		if !ok {
			t.Errorf("%s listed as required but not found under docs/spec", required)
			continue
		}
		if !openAPICountRe.MatchString(body) {
			t.Errorf("%s is listed in openAPICountDocs but contains no count claim", required)
		}
	}
}

// TestSpecDocsBackendCountMatchesRegistry pins every "N backends" claim in the
// spec docs against the number of registered backend plugins.
//
// The count is derived by scanning internal/ai/backends/*/ for the
// `backends.Register(&backends.BackendPlugin{...})` call each sub-package makes
// in its init(). The service package cannot import those sub-packages to read
// the live registry — `internal/ai/backends` imports `internal/ai`, which
// imports `internal/model`, and importing it here would close a cycle through
// service. A source scan is the honest alternative, so the scan asserts its own
// sanity: every backend sub-directory must contain exactly one registration,
// which makes "a new backend added but the scanner missed it" fail loudly
// instead of silently under-counting.
func TestSpecDocsBackendCountMatchesRegistry(t *testing.T) {
	root := repoRoot(t)
	backendsDir := filepath.Join(root, "internal", "ai", "backends")

	entries, err := os.ReadDir(backendsDir)
	if err != nil {
		t.Fatalf("read backends dir: %v", err)
	}

	regRe := regexp.MustCompile(`backends\.Register\(&backends\.BackendPlugin\{`)
	var registered []string
	for _, e := range entries {
		if !e.IsDir() {
			continue // all.go / acp_wire.go / plugin.go live here
		}
		dir := filepath.Join(backendsDir, e.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		hits := 0
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".go") || strings.HasSuffix(f.Name(), "_test.go") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, f.Name()))
			if err != nil {
				t.Fatalf("read %s: %v", filepath.Join(dir, f.Name()), err)
			}
			hits += len(regRe.FindAll(b, -1))
		}
		// A sub-directory with no registration is not a backend (it would be
		// dead code), and one with two registrations would make the count
		// ambiguous. Both are worth failing on: the whole point of the scan is
		// that "one directory = one backend" holds.
		if hits != 1 {
			t.Fatalf("internal/ai/backends/%s contains %d backend registrations, want exactly 1 — the count guard cannot trust its own scan", e.Name(), hits)
		}
		registered = append(registered, e.Name())
	}
	sort.Strings(registered)

	want := len(registered)
	if want == 0 {
		t.Fatal("found no backend registrations — the guard would be vacuous")
	}

	re := regexp.MustCompile(`(\d+)\s*个\s*(?:后端规格|注册后端)`)

	claims := 0
	for file, body := range readSpecDocs(t) {
		for _, m := range re.FindAllStringSubmatchIndex(body, -1) {
			claims++
			got, _ := strconv.Atoi(body[m[2]:m[3]])
			line := lineOf(body, m[0])
			if got != want {
				t.Errorf("%s:%d claims %d backends, but %d are registered (%s) — update the prose (the code is authoritative)",
					file, line, got, want, strings.Join(registered, ", "))
			}
		}
	}
	if claims == 0 {
		t.Fatal("no backend-count claim found in the spec docs — the guard would be vacuous; if the wording changed, update the regex")
	}
}

// TestDatabaseDocListsExactlyTheRealTables pins the table inventory in
// docs/spec/database.md against a freshly initialized database.
//
// This is a set comparison, not a count comparison: it catches a ghost table
// (documented but never created) and a missing table (created but undocumented)
// independently, and names the offending table instead of just reporting 19≠36.
//
// Only the `service`-owned tables are compared. The RAG tables live in a
// separate connection pool with their own DDL and are documented in their own
// section; rag_vec is created lazily on first vector write, so it does not exist
// in a fresh database at all.
func TestDatabaseDocListsExactlyTheRealTables(t *testing.T) {
	setupGroupDB(t) // fresh InitDB in a temp data dir, restored on cleanup

	rows, err := store.ReadDB().Query(
		`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	defer rows.Close()

	actual := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table name: %v", err)
		}
		actual[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate sqlite_master: %v", err)
	}
	if len(actual) == 0 {
		t.Fatal("fresh database has no tables — the guard would be vacuous")
	}

	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "docs", "spec", "database.md"))
	if err != nil {
		t.Fatalf("read database.md: %v", err)
	}

	// Table sections are `### name（中文名）`. The RAG section and the prose
	// headings have no fullwidth paren, so they are skipped naturally.
	secRe := regexp.MustCompile(`(?m)^### ([a-z][a-z0-9_]*)（`)
	documented := map[string]bool{}
	for _, m := range secRe.FindAllStringSubmatch(string(body), -1) {
		documented[m[1]] = true
	}
	if len(documented) == 0 {
		t.Fatal("no table sections found in database.md — the guard would be vacuous")
	}

	// The RAG tables are documented in their own section; they are created by
	// the RAG store (separate connection pool), not by InitDB, and rag_vec is
	// created lazily on first vector write so it never exists in a fresh DB.
	ragOnly := map[string]bool{"rag_chunks": true, "rag_chunks_fts": true, "rag_vec": true}

	var ghost, missing []string
	for name := range documented {
		if ragOnly[name] {
			continue
		}
		if !actual[name] {
			ghost = append(ghost, name)
		}
	}
	for name := range actual {
		if !documented[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(ghost)
	sort.Strings(missing)

	if len(ghost) > 0 {
		t.Errorf("database.md documents %d table(s) that InitDB never creates (ghost tables):\n  %s",
			len(ghost), strings.Join(ghost, "\n  "))
	}
	if len(missing) > 0 {
		t.Errorf("database.md is missing %d table(s) that InitDB does create:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// lineOf returns the 1-based line number of byte offset off in s.
func lineOf(s string, off int) int {
	if off > len(s) {
		off = len(s)
	}
	return strings.Count(s[:off], "\n") + 1
}
