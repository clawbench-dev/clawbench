package main

import (
	"net/http"
	"reflect"
	"strings"
	"testing"

	"clawbench/internal/handler"
	"clawbench/internal/i18n"
	"clawbench/internal/model"
	"clawbench/internal/plugins"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registeredPluginsForTest rebuilds the real registry and returns it.
//
// Every guard below runs against `registerPlugins()` — the same function main
// calls at startup. A guard that iterated a hand-written list would pass while
// production stayed broken, which is exactly the failure mode these tests exist
// to prevent.
func registeredPluginsForTest(t *testing.T) []*plugins.Plugin {
	t.Helper()
	plugins.Reset()
	t.Cleanup(plugins.Reset)
	registerPlugins(0) // hooks are not invoked here; port only matters to terminal.Start
	all := plugins.All()
	require.NotEmpty(t, all, "registerPlugins registered nothing")
	return all
}

// nonPatchableByDesign 记录"故意不开放 PATCH"的字段及原因。
//
// 守卫的要求是：Section 下每个 yaml 字段要么可 PATCH、要么是"可 PATCH 字段的
// 容器"、要么在这里显式登记。新加一个字段时若它不该被设置页修改，就在这登记
// ——这正是守卫的目的（漏登记 = 红），而不是把白名单改成反射派生。
var nonPatchableByDesign = map[string]string{
	"forge.credentials":       "凭据由 /api/forge/credentials 单独写入（只写不回显），不走 PATCH",
	"forge.schemes":           "与凭据同源：在写入该 host 的凭据时记录",
	"terminal.max_line_bytes": "内部防爆上限，不暴露给设置页",
	"terminal.max_buffer_mb":  "内部内存上限，不暴露给设置页",
}

func TestPluginRegistry_IDsAreUniqueAndLowercase(t *testing.T) {
	all := registeredPluginsForTest(t)

	seen := map[string]bool{}
	for _, p := range all {
		assert.NotEmpty(t, p.ID)
		assert.False(t, seen[p.ID], "duplicate plugin ID %q", p.ID)
		seen[p.ID] = true
		assert.Equal(t, strings.ToLower(p.ID), p.ID,
			"plugin ID %q must be lowercase: it is also the frontend tab id and the i18n key prefix", p.ID)
	}
}

func TestPluginRegistry_EverySectionIsFullyWhitelisted(t *testing.T) {
	all := registeredPluginsForTest(t)

	checkedSections := 0
	for _, p := range all {
		if p.Section == "" {
			continue
		}
		checkedSections++
		fields := sectionYAMLFields(t, p.Section)
		require.NotEmptyf(t, fields, "section %q of plugin %q has no yaml fields — the reflection lookup is broken", p.Section, p.ID)

		for _, path := range fields {
			if handler.PatchableConfigPaths[path] {
				continue
			}
			if hasPatchableDescendant(path) {
				continue // container of patchable leaves (e.g. forge.notify)
			}
			if reason, ok := nonPatchableByDesign[path]; ok {
				assert.NotEmpty(t, reason, "%s is exempted without a reason", path)
				continue
			}
			t.Errorf("%s is a config field of plugin %q but is not in PatchableConfigPaths "+
				"(add it to the whitelist, or register it in nonPatchableByDesign with a reason)", path, p.ID)
		}
	}
	assert.NotZero(t, checkedSections, "no plugin declares a Section — the guard would be vacuous")
}

func TestPluginRegistry_NonPatchableExemptionsAreReal(t *testing.T) {
	all := registeredPluginsForTest(t)

	// Collect every section field once, so a stale exemption (field renamed or
	// removed) fails instead of lingering forever.
	known := map[string]bool{}
	for _, p := range all {
		if p.Section == "" {
			continue
		}
		for _, path := range sectionYAMLFields(t, p.Section) {
			known[path] = true
		}
	}
	for path := range nonPatchableByDesign {
		assert.True(t, known[path], "exemption %q does not name a config field of any registered section", path)
		assert.False(t, handler.PatchableConfigPaths[path],
			"exemption %q is also patchable — remove it from nonPatchableByDesign", path)
	}
}

func TestPluginRegistry_EveryDisabledKeyIsLocalized(t *testing.T) {
	all := registeredPluginsForTest(t)

	// Only switchable plugins can ever be rejected, so only they need the key.
	// Pure-preference plugins (Enabled == nil) are never refused and must NOT
	// grow a "XxxDisabled" message nobody can ever see.
	checked := 0
	for _, p := range all {
		if p.Enabled == nil {
			continue
		}
		checked++
		key := p.DisabledKey()
		require.NotEmpty(t, key, "plugin %q derives an empty disabled key", p.ID)
		for _, locale := range []string{"en", "zh"} {
			msg := i18n.T(i18n.LocalizerForLocale(locale), key)
			assert.NotEqualf(t, key, msg,
				"plugin %q: i18n key %q is missing from the %s locale (a missing key ships the raw id to the user)",
				p.ID, key, locale)
		}
	}
	assert.NotZero(t, checked, "no plugin has an Enabled func — the guard would be vacuous")
}

// TestPluginRegistry_DestructiveDisablesDeclareHooks pins the two features whose
// "off" is destructive by contract: forge clears every repository binding, share
// revokes every issued link. Losing the hook would leave the switch claiming
// "disabled" while the bindings/links keep working.
//
// The tasks master switch is deliberately absent: its effect is bidirectional
// (off cancels runs, on restarts cron) and stays in
// handler.applyFeatureDisableEffects.
func TestPluginRegistry_DestructiveDisablesDeclareHooks(t *testing.T) {
	registeredPluginsForTest(t)

	for _, id := range []string{"forge", "share"} {
		p := plugins.Lookup(id)
		require.NotNilf(t, p, "plugin %q is not registered", id)
		assert.NotNilf(t, p.OnDisable, "plugin %q must clean up on disable", id)
	}
}

func TestPluginRegistry_SectionsDeclareRoutes(t *testing.T) {
	all := registeredPluginsForTest(t)

	for _, p := range all {
		if p.Section == "" {
			continue
		}
		assert.NotEmptyf(t, p.APIPrefixes,
			"plugin %q has a Section but no APIPrefixes: its endpoints would never be gated", p.ID)
	}
}

func TestPluginRegistry_DeclaredPrefixesOwnRealRoutes(t *testing.T) {
	all := registeredPluginsForTest(t)

	// Populate the route table, then check that every declared prefix actually
	// covers at least one registered pattern and that the registry attributes
	// that pattern back to the same plugin. A typo ("/api/mails/") would
	// otherwise leave the feature silently ungated.
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	routes := handler.RegisteredRoutes()
	require.NotEmpty(t, routes)

	for _, p := range all {
		for _, prefix := range p.APIPrefixes {
			matched := 0
			for _, route := range routes {
				if plugins.ForRoute(route.Pattern) == p {
					matched++
				}
			}
			assert.NotZerof(t, matched, "plugin %q declares prefix %q but no registered route maps to it", p.ID, prefix)
		}
	}
}

// sectionYAMLFields returns "<section>.<yaml field>" for every exported yaml
// field of the model.Config struct tagged with the given section.
func sectionYAMLFields(t *testing.T, section string) []string {
	t.Helper()
	cfgType := reflect.TypeOf(model.Config{})
	for i := range cfgType.NumField() {
		field := cfgType.Field(i)
		if strings.Split(field.Tag.Get("yaml"), ",")[0] != section {
			continue
		}
		structType := field.Type
		if structType.Kind() == reflect.Ptr {
			structType = structType.Elem()
		}
		if structType.Kind() != reflect.Struct {
			return nil
		}
		fields := make([]string, 0, structType.NumField())
		for j := range structType.NumField() {
			sub := structType.Field(j)
			if !sub.IsExported() {
				continue
			}
			tag := strings.Split(sub.Tag.Get("yaml"), ",")[0]
			if tag == "" || tag == "-" {
				continue
			}
			fields = append(fields, section+"."+tag)
		}
		return fields
	}
	return nil
}

// hasPatchableDescendant reports whether some whitelisted path lives under the
// given object path (e.g. "forge.notify" → "forge.notify.opened").
func hasPatchableDescendant(path string) bool {
	prefix := path + "."
	for candidate := range handler.PatchableConfigPaths {
		if strings.HasPrefix(candidate, prefix) {
			return true
		}
	}
	return false
}
