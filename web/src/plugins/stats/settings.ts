import type { CategoryEntry } from '@/components/settings/settingsFieldMap'

/**
 * Usage statistics — its settings page's field list.
 *
 * Lives here rather than in `settingsFieldMap.ts` so that adding a setting to
 * this feature is ONE file: the plugin declares its page, and the page's fields
 * (with their i18n keys and config keys) sit next to the plugin itself.
 *
 * The import above is `import type` — erased at build time, so this module does
 * not pull `settingsFieldMap.ts` (or anything it imports) into the plugin graph.
 */
export default [
  { type: 'item', spec: { labelKey: 'settings.items.statsDefaultTab', descriptionKey: 'settings.items.statsDefaultTabDesc', key: 'statsDefaultTab', type: 'select', source: 'local', sectionHeader: 'settings.items.statsDefaultsSection', options: [
    { labelKey: 'settings.items.statsTabUsage', value: 'usage' },
    { labelKey: 'settings.items.statsTabCloc', value: 'cloc' },
    { labelKey: 'settings.items.statsTabGit', value: 'git' },
  ]}},
  { type: 'item', spec: { labelKey: 'settings.items.statsDefaultRange', descriptionKey: 'settings.items.statsDefaultRangeDesc', key: 'statsDefaultRange', type: 'select', source: 'local', sectionHeader: 'settings.items.statsDefaultsSection', options: [
    { labelKey: 'settings.items.statsRange24h', value: '24h' },
    { labelKey: 'settings.items.statsRange7d', value: '7d' },
    { labelKey: 'settings.items.statsRange30d', value: '30d' },
  ]}},
  // Off by default: the panel resets to the two defaults above, which is what
  // every existing user is used to.
  { type: 'item', spec: { labelKey: 'settings.items.statsRememberLast', descriptionKey: 'settings.items.statsRememberLastDesc', key: 'statsRememberLast', type: 'switch', source: 'local', sectionHeader: 'settings.items.statsDefaultsSection' } },
] satisfies CategoryEntry[]
