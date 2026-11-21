/**
 * Merge the copy each plugin owns into the base message tables.
 *
 * A dock plugin's manager description and "this host cannot run it" reason are
 * its own property, so they live in `plugins/<id>/i18n.ts`:
 *
 *     export default {
 *       zh: { desc: '…', unavailable: '…' },
 *       en: { desc: '…', unavailable: '…' },
 *     }
 *
 * They are addressable under the keys the UI already uses, so nothing downstream
 * changes: the description is `settings.dockPlugins.desc.<id>` and the reason is
 * `settings.dockPlugins.unavailable.<id>` — the same keys a plugin declares in
 * `descriptionKey` / `unavailableReasonKey`.
 *
 * WHY A MERGE AT STARTUP, not a glob inside the locale files: `zh.ts` / `en.ts`
 * are plain literals that dozens of tests import directly, and they must not
 * pull in the plugin modules (a plugin may import the store, icons, composables —
 * loading them from a message table would turn a copy edit into a dependency
 * cycle, and would make the order in which modules load decide which language
 * wins). Merging here, once, before `createI18n` runs, keeps the message tables
 * pure data and still gives a single source per string.
 *
 * Anything shared by several features (保存/取消, the generic "unsupported host"
 * wording) stays in the base tables: it is not any one plugin's property, and
 * copying it per plugin would mean editing N files to change one word.
 */

/** What one plugin contributes. Both locales required — a missing one falls back. */
export interface PluginMessages {
  desc: string
  unavailable?: string
}

interface PluginI18nModule {
  default?: { zh?: PluginMessages; en?: PluginMessages }
}

const MODULES = import.meta.glob<PluginI18nModule>('@/plugins/*/i18n.ts', { eager: true })

/** Nested message tree (the base locale objects are `as const`, hence `any`). */
type Tree = Record<string, unknown>

function ensureObject(parent: Tree, key: string): Tree {
  const next = parent[key]
  if (next && typeof next === 'object') return next as Tree
  const created: Tree = {}
  parent[key] = created
  return created
}

/**
 * Apply every plugin's copy onto `messages` (mutated in place).
 *
 * The base tables must already contain `settings.dockPlugins`; if they do not,
 * one is created so a missing section is an empty table rather than a crash.
 */
export function mergePluginMessages(messages: Record<string, Tree>): void {
  for (const [path, mod] of Object.entries(MODULES)) {
    const id = path.match(/\/plugins\/([^/]+)\/i18n\.ts$/)?.[1]
    if (!id) continue
    for (const locale of ['zh', 'en'] as const) {
      const copy = mod.default?.[locale]
      if (!copy) continue
      const root = messages[locale]
      if (!root) continue
      const dockPlugins = ensureObject(ensureObject(root, 'settings'), 'dockPlugins')
      ensureObject(dockPlugins, 'desc')[id] = copy.desc
      if (copy.unavailable) ensureObject(dockPlugins, 'unavailable')[id] = copy.unavailable
    }
  }
}
