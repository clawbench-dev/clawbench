import type { CategoryEntry } from '@/components/settings/settingsFieldMap'

/**
 * Settings fields for this plugin's own settings page (`kb`).
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
    // Master switch status. `kb.enabled` is a Dock feature switch (it stops the
    // indexer and closes the database), so the switch itself lives in the
    // manager — but every module page reports its state and links there, and
    // the knowledge base is no exception.
    { type: 'item', spec: { labelKey: 'settings.items.featureStatusKb', descriptionKey: 'settings.items.kbEnabledDesc', key: 'kbEnabledStatus', type: 'info', source: 'server', statusFeature: 'kb.enabled', sectionHeader: 'settings.items.featureStatusSection' } },
    // Status card (KbSetting.vue): collection/document counters plus the
    // reindex action. The item is a placeholder that opens the card and renders
    // no row — the body is declared here, next to the fields.
    { type: 'item', spec: { labelKey: 'settings.items.kbStatusSection', key: 'kbStatus', type: 'info', source: 'server', sectionHeader: 'settings.items.kbStatusSection', cardBody: { load: () => import('@/components/settings/KbSetting.vue') } } },
    // ── 自动注入 ──
    // What the agent receives from the knowledge base on every turn.
    { type: 'item', spec: { labelKey: 'settings.items.kbAutoInject', descriptionKey: 'settings.items.kbAutoInjectDesc', key: 'kb.auto_inject', type: 'switch', source: 'server', sectionHeader: 'settings.items.kbInjectSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.kbInjectTopN', descriptionKey: 'settings.items.kbInjectTopNDesc', key: 'kb.inject_top_n', type: 'number', source: 'server', min: 1, max: 20, disableUnless: { key: 'kb.auto_inject', value: true }, sectionHeader: 'settings.items.kbInjectSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.kbInjectMaxChars', descriptionKey: 'settings.items.kbInjectMaxCharsDesc', key: 'kb.inject_max_chars', type: 'number', source: 'server', min: 200, disableUnless: { key: 'kb.auto_inject', value: true }, sectionHeader: 'settings.items.kbInjectSection' } },
    // 0–1 slider, shown as a percentage. Vector hits below it are dropped; FTS
    // hits are not thresholded (a matched term is a match by definition).
    { type: 'item', spec: { labelKey: 'settings.items.kbInjectMinSimilarity', descriptionKey: 'settings.items.kbInjectMinSimilarityDesc', key: 'kb.inject_min_similarity', type: 'slider', source: 'server', min: 0, max: 1, step: 0.05, displayFormat: 'percent', disableUnless: { key: 'kb.auto_inject', value: true }, sectionHeader: 'settings.items.kbInjectSection' } },
    // ── 索引与限额 ──
    { type: 'item', spec: { labelKey: 'settings.items.kbMaxFileMb', descriptionKey: 'settings.items.kbMaxFileMbDesc', key: 'kb.max_file_mb', type: 'number', source: 'server', min: 1, max: 1024, sectionHeader: 'settings.items.kbIndexSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.kbMaxTextMb', descriptionKey: 'settings.items.kbMaxTextMbDesc', key: 'kb.max_text_mb', type: 'number', source: 'server', min: 1, max: 512, sectionHeader: 'settings.items.kbIndexSection' } },
    // Free text on purpose: it is a Go duration ("2s", "500ms"). A number row
    // would have to pick a unit, and the server accepts either.
    { type: 'item', spec: { labelKey: 'settings.items.kbPollInterval', descriptionKey: 'settings.items.kbPollIntervalDesc', key: 'kb.poll_interval', type: 'text', source: 'server', sectionHeader: 'settings.items.kbIndexSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.kbEmbedBatchSize', descriptionKey: 'settings.items.kbEmbedBatchSizeDesc', key: 'kb.embed_batch_size', type: 'number', source: 'server', min: 1, max: 128, sectionHeader: 'settings.items.kbIndexSection' } },
] satisfies CategoryEntry[]
