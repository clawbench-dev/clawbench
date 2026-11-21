import type { CategoryEntry } from '@/components/settings/settingsFieldMap'

/**
 * Settings fields for this plugin's own settings page (`terminal`).
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
    // The master switch is a Dock feature switch — it also decides whether the
    // tab can exist, and turning it off closes every open PTY — so it lives in
    // the feature manager. This page reports the state and links there; the
    // fields below only describe how an ENABLED terminal behaves.
    { type: 'item', spec: { labelKey: 'settings.items.featureStatusTerminal', descriptionKey: 'settings.items.terminalStatusDesc', key: 'terminalStatus', type: 'info', source: 'server', statusFeature: 'terminal.enabled', sectionHeader: 'settings.items.featureStatusSection' } },
    { type: 'panel', config: {
      panelId: 'terminal',
      commonFields: [
        { labelKey: 'settings.items.terminalTheme', descriptionKey: 'settings.items.terminalThemeDesc', key: 'terminalTheme', type: 'select', source: 'local', defaultValue: 'auto' },
        { labelKey: 'settings.items.terminalFontSize', descriptionKey: 'settings.items.terminalFontSizeDesc', key: 'terminalFontSize', type: 'slider', source: 'local', min: 10, max: 24, step: 1, defaultValue: 12 },
        { labelKey: 'settings.items.terminalCopyOnSelect', descriptionKey: 'settings.items.terminalCopyOnSelectDesc', key: 'terminalCopyOnSelect', type: 'switch', source: 'local' },
        // Recording is on by default; this is the escape hatch for anyone who
        // does not want their shell commands stored at all. It gates capture
        // and the import from the shell history files — already-recorded rows
        // stay until cleared from the history panel.
        { labelKey: 'settings.items.terminalHistoryEnabled', descriptionKey: 'settings.items.terminalHistoryEnabledDesc', key: 'terminalHistoryEnabled', type: 'switch', source: 'local', defaultValue: true },
        { labelKey: 'settings.items.terminalIdleTimeout', descriptionKey: 'settings.items.terminalIdleTimeoutDesc', key: 'terminal.idle_timeout', type: 'text', source: 'server' },
        { labelKey: 'settings.items.terminalMaxSessions', descriptionKey: 'settings.items.terminalMaxSessionsDesc', key: 'terminal.max_sessions', type: 'number', source: 'server' },
        { labelKey: 'settings.items.terminalBufferLines', descriptionKey: 'settings.items.terminalBufferLinesDesc', key: 'terminal.buffer_lines', type: 'number', source: 'server' },
      ],
    }},
] satisfies CategoryEntry[]
