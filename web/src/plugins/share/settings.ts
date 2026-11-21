import type { CategoryEntry } from '@/components/settings/settingsFieldMap'

/**
 * Settings fields for this plugin's own settings page (`share`).
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
    { type: 'item', spec: { labelKey: 'settings.items.featureStatusShare', descriptionKey: 'settings.items.shareStatusDesc', key: 'shareStatus', type: 'info', source: 'server', statusFeature: 'share.enabled', sectionHeader: 'settings.items.featureStatusSection' } },
    // ── 分享范围 ──
    { type: 'item', spec: { labelKey: 'settings.items.shareAllowFiles', descriptionKey: 'settings.items.shareAllowFilesDesc', key: 'share.allow_files', type: 'switch', source: 'server', sectionHeader: 'settings.items.shareScopeSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.shareAllowSessions', descriptionKey: 'settings.items.shareAllowSessionsDesc', key: 'share.allow_sessions', type: 'switch', source: 'server', sectionHeader: 'settings.items.shareScopeSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.shareAllowMeetings', descriptionKey: 'settings.items.shareAllowMeetingsDesc', key: 'share.allow_meetings', type: 'switch', source: 'server', sectionHeader: 'settings.items.shareScopeSection' } },
    // Download only applies to file shares — session and meeting shares are
    // read-only pages with no download action — so it is disabled while file
    // sharing is off instead of being hidden (a hidden row looks deleted).
    { type: 'item', spec: { labelKey: 'settings.items.shareAllowDownload', descriptionKey: 'settings.items.shareAllowDownloadDesc', key: 'share.allow_download', type: 'switch', source: 'server', disableUnless: { key: 'share.allow_files', value: true }, sectionHeader: 'settings.items.shareScopeSection' } },
    // ── 链接与展示 ──
    // 0 = never expires. Existing links keep the expiry they were created with;
    // changing this does not shorten links already out there.
    { type: 'item', spec: { labelKey: 'settings.items.shareLinkTtlDays', descriptionKey: 'settings.items.shareLinkTtlDaysDesc', key: 'share.link_ttl_days', type: 'number', source: 'server', min: 0, max: 3650, sectionHeader: 'settings.items.shareLinkSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.shareShowProjectName', descriptionKey: 'settings.items.shareShowProjectNameDesc', key: 'share.show_project_name', type: 'switch', source: 'server', sectionHeader: 'settings.items.shareLinkSection' } },
] satisfies CategoryEntry[]
