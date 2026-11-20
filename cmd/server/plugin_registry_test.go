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
	"forge.platforms":         "与凭据同源：在绑定仓库或写入该 host 的凭据时记录（setForgePlatformHint）",
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
			// 三个合法出口，顺序对应"这个字段归谁"：
			//  1. 插件自己声明了（缺口二的新语义）；
			//  2. 是某个可改字段的容器（forge.notify 这类中间层）；
			//  3. 显式登记为"故意不可改"。
			if p.ConfigFields[path] {
				continue
			}
			if hasPatchableDescendant(path) {
				continue // container of patchable leaves (e.g. forge.notify)
			}
			if reason, ok := nonPatchableByDesign[path]; ok {
				assert.NotEmpty(t, reason, "%s is exempted without a reason", path)
				continue
			}
			t.Errorf("%s is a config field of plugin %q but the plugin does not declare it in ConfigFields "+
				"(declare it with a reason, or register it in nonPatchableByDesign)", path, p.ID)
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
// The tasks master switch used to be deliberately absent here: its effect is
// bidirectional (off cancels runs and discards queued events, on restarts cron)
// while OnDisable only has one direction. It is now covered by SwitchEffect.
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
	// that pattern back to the same plugin.
	//
	// The match is counted PER PREFIX, not per plugin. Counting per plugin hides
	// the failure this guard exists for: stats declares "/api/usage/",
	// "/api/git/stats" and "/api/git/cloc", so a typo in the last one still
	// leaves the other two matching and the guard stays green while that route
	// silently drops out of the gate.
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	routes := handler.RegisteredRoutes()
	require.NotEmpty(t, routes)

	for _, p := range all {
		for _, prefix := range p.APIPrefixes {
			matched := 0
			for _, route := range routes {
				if plugins.ForRoute(route.Pattern) == p && plugins.PrefixMatchesPath(prefix, route.Pattern) {
					matched++
				}
			}
			assert.NotZerof(t, matched,
				"plugin %q declares prefix %q but no registered route is covered by THAT prefix", p.ID, prefix)
		}
	}
}

// TestPluginRegistry_FeatureSwitchesAreHot pins the derived half of the PATCH
// classification: every registered section's "<section>.enabled" must be treated
// as hot, so a new feature switch does not have to be added to hotReloadFields
// (and can never make the UI say "restart required" for a switch that already
// took effect).
func TestPluginRegistry_FeatureSwitchesAreHot(t *testing.T) {
	all := registeredPluginsForTest(t)

	checked := 0
	for _, p := range all {
		if p.Section == "" {
			continue
		}
		checked++
		assert.Truef(t, plugins.IsFeatureSwitchPath(p.Section+".enabled"),
			"%s.enabled must classify as hot (the lifecycle already started/stopped it)", p.Section)
	}
	assert.NotZero(t, checked, "no plugin declares a Section — the guard would be vacuous")

	assert.False(t, plugins.IsFeatureSwitchPath("nosuchsection.enabled"))
	assert.False(t, plugins.IsFeatureSwitchPath("mail.sync_interval_minutes"))
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
//
// 走 handler.IsPatchablePath 而不是直接查 PatchableConfigPaths：功能字段
// 已经不在那张核心表里了（它们由 builtinConfigFields 持有），只查核心表会
// 让 forge.notify 这类中间层被判成"没人管"。
func hasPatchableDescendant(path string) bool {
	prefix := path + "."
	for candidate := range handler.AllPatchablePaths() {
		if strings.HasPrefix(candidate, prefix) {
			return true
		}
	}
	return false
}

// TestPluginRegistry_ConfigFieldsMatchHandler pins the one duplication this
// refactor deliberately keeps: a feature's patchable fields are declared twice
// — once on the plugin (ownership: which feature owns which field, used by
// /api/features and the guard above) and once in handler.builtinConfigFields
// (enforcement: what PATCH accepts, evaluated without the registry so unit
// tests do not need to register plugins).
//
// Two lists that "should" match will drift. This is the pin. It catches both
// directions:
//   - a field declared on the plugin but missing from the handler list → the
//     plugin claims a field that PATCH silently rejects;
//   - a field enforced by handler but not declared by any plugin → an orphan
//     nobody owns, which is what "not fully modularized" looks like.
//
// The three non-patchable exemptions are exempt on both sides by construction,
// so they are not compared.
func TestPluginRegistry_ConfigFieldsMatchHandler(t *testing.T) {
	all := registeredPluginsForTest(t)

	declared := map[string]string{} // path -> plugin ID
	for _, p := range all {
		for path := range p.ConfigFields {
			require.Emptyf(t, declared[path],
				"config path %q is declared by both %q and %q", path, declared[path], p.ID)
			declared[path] = p.ID
		}
	}

	for path, id := range declared {
		assert.Truef(t, handler.IsPatchablePath(path),
			"%q is declared by plugin %q but PATCH would reject it "+
				"(add it to handler.builtinConfigFields)", path, id)
	}

	// 反向：handler 认的每个功能字段都必须有插件认领。核心表（无主字段）
	// 不适用，只查功能前缀。
	for prefix, id := range map[string]string{
		"forge.": "forge", "kb.": "kb", "mail.": "mail",
		"share.": "share", "tasks.": "tasks", "terminal.": "terminal",
	} {
		p := plugins.Lookup(id)
		require.NotNilf(t, p, "plugin %q is not registered", id)
		for path := range handler.AllPatchablePaths() {
			if !strings.HasPrefix(path, prefix) {
				continue
			}
			if _, exempt := nonPatchableByDesign[path]; exempt {
				continue
			}
			assert.Truef(t, p.ConfigFields[path],
				"%q is PATCHable but plugin %q does not declare it in ConfigFields "+
					"(declare it, or it is an orphan nobody owns)", path, id)
		}
	}
}

// TestPluginRegistry_EveryRoutePrefixHasMountedRoutes pins the other half of
// "a feature owns its endpoints": every plugin that declares APIPrefixes must
// actually have interfaces mounted under that prefix.
//
// The two facts live in different places by design (the prefix on the plugin
// literal in main.go, the mounts in handler/mount_*.go), so one can be updated
// without the other. What that looks like: a plugin claims /api/foo/ for gating
// and the settings page renders its switch, but /api/foo/list was never mounted
// — every call 404s and the feature appears broken "for no reason".
//
// It is the same shape as TestPluginRegistry_DeclaredPrefixesOwnRealRoutes
// (which checks the ROUTE TABLE agrees with the prefix); this one checks the
// MOUNT LIST produced the routes in the first place. A prefix with no routes at
// all is caught by that test; this one fires earlier and names the file.
func TestPluginRegistry_EveryRoutePrefixHasMountedRoutes(t *testing.T) {
	all := registeredPluginsForTest(t)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	routes := handler.RegisteredRoutes()
	require.NotEmpty(t, routes)

	for _, p := range all {
		for _, prefix := range p.APIPrefixes {
			covered := 0
			for _, r := range routes {
				if plugins.PrefixMatchesPath(prefix, r.Pattern) {
					covered++
				}
			}
			assert.NotZerof(t, covered,
				"plugin %q declares prefix %q but no route under it is mounted — "+
					"add the endpoints to handler/mount_%s.go (or drop the prefix)",
				p.ID, prefix, p.ID)
		}
	}
}

// TestPluginRegistry_SwitchHooksAreExclusive pins the constraint documented on
// Plugin.SwitchEffect: a plugin declares OnDisable OR SwitchEffect, never both.
//
// The handler calls SwitchEffect for every direction and OnDisable only for the
// "off" direction, so a plugin declaring both would run its cleanup TWICE on
// disable (once per hook). For forge (clear every binding) and share (revoke
// every link) that is a second destructive pass over data that is already gone
// — cheap, but it also means the two hooks' warnings are reported twice.
func TestPluginRegistry_SwitchHooksAreExclusive(t *testing.T) {
	all := registeredPluginsForTest(t)

	for _, p := range all {
		assert.Falsef(t, p.OnDisable != nil && p.SwitchEffect != nil,
			"plugin %q declares both OnDisable and SwitchEffect — a disable would "+
				"run both hooks; keep the one that matches the switch's shape", p.ID)
	}
}

// TestPluginRegistry_TasksUseSwitchEffect pins the bi-directional switch that
// used to live in handler.applyFeatureDisableEffects.
//
// The tasks master switch must reach the scheduler in BOTH directions (off
// cancels running executions and discards queued events; on restarts cron).
// OnDisable alone cannot express the "on" direction, so registering the tasks
// plugin without SwitchEffect would silently make "turn tasks back on" a no-op
// — the switch would flip in the settings UI while the cron never restarted.
func TestPluginRegistry_TasksUseSwitchEffect(t *testing.T) {
	registeredPluginsForTest(t)

	p := plugins.Lookup("tasks")
	require.NotNil(t, p, "the tasks plugin is not registered")
	assert.NotNil(t, p.SwitchEffect, "the tasks switch is bi-directional; it must declare SwitchEffect")
	assert.Nil(t, p.OnDisable, "SwitchEffect already covers the off direction")
}
