import type { CategoryEntry } from '@/components/settings/settingsFieldMap'

/**
 * Settings fields for this plugin's own settings page (`gitHistory`).
 *
 * Moved out of the central `settingsFieldMap.ts` table so that adding a setting
 * to this feature is ONE directory: the plugin declares its page, and the page's
 * fields (their i18n keys and config keys) sit next to the plugin itself.
 *
 * The import above is `import type` — erased at build time, so this module pulls
 * nothing from `settingsFieldMap.ts` (or from anything it imports) into the
 * plugin graph.
 */
export default [
    // Server-side: the page size is the `git log -30` argument, so it has to be
    // part of the request. Local settings cannot change what the server reads.
    { type: 'item', spec: { labelKey: 'settings.items.gitHistoryPageSize', descriptionKey: 'settings.items.gitHistoryPageSizeDesc', key: 'git_history.page_size', type: 'number', source: 'server', min: 10, max: 200, sectionHeader: 'settings.items.gitHistoryListSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.gitHistoryHideMerges', descriptionKey: 'settings.items.gitHistoryHideMergesDesc', key: 'gitHistoryHideMerges', type: 'switch', source: 'local', sectionHeader: 'settings.items.gitHistoryListSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.gitHistoryShowGraph', descriptionKey: 'settings.items.gitHistoryShowGraphDesc', key: 'gitHistoryShowGraph', type: 'switch', source: 'local', sectionHeader: 'settings.items.gitHistoryListSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.gitHistoryTimeFormat', descriptionKey: 'settings.items.gitHistoryTimeFormatDesc', key: 'gitHistoryTimeFormat', type: 'select', source: 'local', sectionHeader: 'settings.items.gitHistoryTimeSection', options: [
      { labelKey: 'settings.items.gitHistoryTimeRelative', value: 'relative' },
      { labelKey: 'settings.items.gitHistoryTimeAbsolute', value: 'absolute' },
    ]}},
] satisfies CategoryEntry[]
