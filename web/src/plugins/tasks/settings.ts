import type { CategoryEntry } from '@/components/settings/settingsFieldMap'

/**
 * Settings fields for this plugin's own settings page (`tasks`).
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
    { type: 'item', spec: { labelKey: 'settings.items.featureStatusTasks', descriptionKey: 'settings.items.tasksStatusDesc', key: 'tasksStatus', type: 'info', source: 'server', statusFeature: 'tasks.enabled', sectionHeader: 'settings.items.featureStatusSection' } },
    // ── 执行记录 ──
    // Same switch-plus-days shape as the upload and archive retention rows: 0
    // days would otherwise be indistinguishable from "off".
    { type: 'item', spec: { labelKey: 'settings.items.tasksRetentionEnabled', descriptionKey: 'settings.items.tasksRetentionEnabledDesc', key: 'tasks.retention_enabled', type: 'switch', source: 'server', sectionHeader: 'settings.items.tasksRetentionSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.tasksRetentionDays', descriptionKey: 'settings.items.tasksRetentionDaysDesc', key: 'tasks.retention_days', type: 'number', source: 'server', min: 1, max: 3650, disableUnless: { key: 'tasks.retention_enabled', value: true }, sectionHeader: 'settings.items.tasksRetentionSection' } },
    // ── 通知 ──
    { type: 'item', spec: { labelKey: 'settings.items.tasksNotifySuccess', descriptionKey: 'settings.items.tasksNotifySuccessDesc', key: 'tasks.notify_on_success', type: 'switch', source: 'server', sectionHeader: 'settings.items.tasksNotifySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.tasksNotifyFailure', descriptionKey: 'settings.items.tasksNotifyFailureDesc', key: 'tasks.notify_on_failure', type: 'switch', source: 'server', sectionHeader: 'settings.items.tasksNotifySection' } },
    // ── 调度 ──
    // Free text (an IANA name such as Asia/Shanghai). A select would have to
    // ship a timezone table and would still be incomplete.
    { type: 'item', spec: { labelKey: 'settings.items.tasksTimezone', descriptionKey: 'settings.items.tasksTimezoneDesc', key: 'tasks.timezone', type: 'text', source: 'server', sectionHeader: 'settings.items.tasksScheduleSection' } },
    // Local: which filter the task list opens with. A per-device view
    // preference, like the stats panel's default tab.
    { type: 'item', spec: { labelKey: 'settings.items.tasksDefaultFilter', descriptionKey: 'settings.items.tasksDefaultFilterDesc', key: 'tasksDefaultFilter', type: 'select', source: 'local', sectionHeader: 'settings.items.tasksScheduleSection', options: [
      { labelKey: 'settings.items.tasksFilterAll', value: 'all' },
      { labelKey: 'settings.items.tasksFilterEnabled', value: 'enabled' },
      { labelKey: 'settings.items.tasksFilterPaused', value: 'paused' },
    ]}},
] satisfies CategoryEntry[]
