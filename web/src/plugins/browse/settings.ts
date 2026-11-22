import type { CategoryEntry } from '@/components/settings/settingsFieldMap'

/** Settings category id (the plugin's directory is `browse`). */
export const category = 'files'

/**
 * Settings fields for this plugin's own settings page (`files`).
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
    { type: 'item', spec: { labelKey: 'settings.items.fileSearchDisplayLimit', descriptionKey: 'settings.items.fileSearchDisplayLimitDesc', key: 'file_search.display_limit', type: 'number', source: 'server', min: 10, max: 500, sectionHeader: 'settings.items.searchSectionHeader' } },
    { type: 'item', spec: { labelKey: 'settings.items.showHidden', descriptionKey: 'settings.items.showHiddenDesc', key: 'showHidden', type: 'switch', source: 'local', sectionHeader: 'settings.items.fileDisplaySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.fileView', descriptionKey: 'settings.items.fileViewDesc', key: 'fileView', type: 'select', source: 'local', sectionHeader: 'settings.items.fileDisplaySection', options: [
      { labelKey: 'settings.items.fileViewList', value: 'list' },
      { labelKey: 'settings.items.fileViewGrid', value: 'grid' },
    ]}},
    { type: 'item', spec: { labelKey: 'settings.items.sortField', descriptionKey: 'settings.items.sortFieldDesc', key: 'sortField', type: 'select', source: 'local', sectionHeader: 'settings.items.fileDisplaySection', options: [
      { labelKey: 'settings.items.sortFieldDefault', value: null },
      { labelKey: 'settings.items.sortFieldName', value: 'name' },
      { labelKey: 'settings.items.sortFieldTime', value: 'time' },
      { labelKey: 'settings.items.sortFieldType', value: 'type' },
      { labelKey: 'settings.items.sortFieldSize', value: 'size' },
    ]}},
    { type: 'item', spec: { labelKey: 'settings.items.sortDir', descriptionKey: 'settings.items.sortDirHint', key: 'sortDir', type: 'select', source: 'local', dependsOn: { key: 'sortField', values: ['name', 'time', 'type', 'size'] }, sectionHeader: 'settings.items.fileDisplaySection', options: [
      { labelKey: 'settings.items.sortDirAsc', value: 'asc' },
      { labelKey: 'settings.items.sortDirDesc', value: 'desc' },
    ]}},
    { type: 'item', spec: { labelKey: 'settings.items.uploadMaxSize', descriptionKey: 'settings.items.uploadMaxSizeDesc', key: 'upload.max_size_mb', type: 'number', source: 'server', sectionHeader: 'settings.items.uploadSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.uploadMaxFiles', descriptionKey: 'settings.items.uploadMaxFilesDesc', key: 'upload.max_files', type: 'number', source: 'server', sectionHeader: 'settings.items.uploadSection' } },
    // Retention deletes files, so it is a switch plus a day count rather than a
    // single number: 0 days would otherwise be indistinguishable from "off".
    { type: 'item', spec: { labelKey: 'settings.items.uploadRetentionEnabled', descriptionKey: 'settings.items.uploadRetentionEnabledDesc', key: 'upload.retention_enabled', type: 'switch', source: 'server', sectionHeader: 'settings.items.uploadSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.uploadRetentionDays', descriptionKey: 'settings.items.uploadRetentionDaysDesc', key: 'upload.retention_days', type: 'number', source: 'server', min: 1, disableUnless: { key: 'upload.retention_enabled', value: true }, sectionHeader: 'settings.items.uploadSection' } },
] satisfies CategoryEntry[]
