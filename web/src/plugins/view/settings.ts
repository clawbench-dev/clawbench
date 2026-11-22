import type { CategoryEntry } from '@/components/settings/settingsFieldMap'

/** Settings category id (the plugin's directory is `view`). */
export const category = 'fileView'

/**
 * Settings fields for this plugin's own settings page (`fileView`).
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
    { type: 'item', spec: { labelKey: 'settings.items.recentFilesCount', descriptionKey: 'settings.items.recentFilesCountDesc', key: 'recentFilesCount', type: 'number', source: 'local', min: 1, max: 50, sectionHeader: 'settings.items.fileDisplaySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.wordWrap', descriptionKey: 'settings.items.wordWrapDesc', key: 'wordWrap', type: 'switch', source: 'local', sectionHeader: 'settings.items.fileDisplaySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.lineNumbers', descriptionKey: 'settings.items.lineNumbersDesc', key: 'lineNumbers', type: 'switch', source: 'local', sectionHeader: 'settings.items.fileDisplaySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.stickyScroll', descriptionKey: 'settings.items.stickyScrollDesc', key: 'stickyScroll', type: 'switch', source: 'local', sectionHeader: 'settings.items.fileDisplaySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.markdownCodeLinkPreview', descriptionKey: 'settings.items.markdownCodeLinkPreviewDesc', key: 'markdownCodeLinkPreview', type: 'switch', source: 'local', sectionHeader: 'settings.items.fileDisplaySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.filePreviewMode', descriptionKey: 'settings.items.filePreviewModeDesc', key: 'filePreviewMode', type: 'switch', source: 'local', sectionHeader: 'settings.items.fileDisplaySection' } },
    // draw.io editor is downloaded on first use rather than shipped. The URL
    // only matters where github.com and the public proxies are unreachable —
    // an air-gapped deployment points it at its own mirror.
    { type: 'item', spec: { labelKey: 'settings.items.drawioDownloadUrl', descriptionKey: 'settings.items.drawioDownloadUrlDesc', key: 'drawio.download_url', type: 'text', source: 'server', sectionHeader: 'settings.items.drawioSection' } },
] satisfies CategoryEntry[]
