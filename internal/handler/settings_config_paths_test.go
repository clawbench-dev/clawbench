package handler

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"clawbench/internal/model"
)

// TestPatchablePathsExistInConfig keeps the settings whitelist and the config
// file in step.
//
// PatchableConfigPaths decides which keys a PATCH may write into config.yaml,
// but nothing forces those keys to exist in model.Config. A typo — or a field
// that was renamed in the struct and not here — still answers 200 and still
// lands on disk, under a key nothing ever reads: the switch looks saved, the
// file grows an orphan line, and the value silently does nothing. The reverse
// drift (a real config field that can never be written from the UI) is fine
// and common — those are the "file only" settings.
func TestPatchablePathsExistInConfig(t *testing.T) {
	known := map[string]bool{}
	collectConfigTypePaths(reflect.TypeOf(model.Config{}), "", known)
	if len(known) < 50 {
		t.Fatalf("reflection produced %d config paths; the walker is broken", len(known))
	}

	var missing []string
	for path := range PatchableConfigPaths {
		if !known[path] {
			missing = append(missing, path)
		}
	}
	sort.Strings(missing)

	assert.Empty(t, missing,
		"these PATCHable paths do not exist in model.Config; the PATCH writes "+
			"config.yaml under a key nothing reads: %v", missing)
}

// TestHotReloadFieldsArePatchable guards the other direction: a path listed as
// hot-reloadable but not writable is dead weight, and it hides the fact that
// the field it names was removed.
func TestHotReloadFieldsArePatchable(t *testing.T) {
	var orphans []string
	for path := range hotReloadFields {
		if !PatchableConfigPaths[path] {
			orphans = append(orphans, path)
		}
	}
	sort.Strings(orphans)

	assert.Empty(t, orphans,
		"these paths are marked hot-reloadable but are not PATCHable; either "+
			"the field was removed or the whitelist entry is missing: %v", orphans)
}

// collectConfigTypePaths records every dotted yaml path in a type tree, both
// leaves and the containers on the way to them (mcp.servers is a list of
// structs and is itself a whitelisted path).
func collectConfigTypePaths(t reflect.Type, prefix string, out map[string]bool) {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil {
		return
	}
	switch t.Kind() {
	case reflect.Struct:
		for i := range t.NumField() {
			field := t.Field(i)
			tag := strings.Split(field.Tag.Get("yaml"), ",")[0]
			if tag == "" || tag == "-" {
				continue
			}
			collectConfigTypePaths(field.Type, joinConfigPath(prefix, tag), out)
		}
	case reflect.Slice, reflect.Array:
		collectConfigTypePaths(t.Elem(), prefix, out)
	case reflect.Map:
		collectConfigTypePaths(t.Elem(), prefix, out)
	}
	if prefix != "" {
		out[prefix] = true
	}
}

func joinConfigPath(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "." + key
}
