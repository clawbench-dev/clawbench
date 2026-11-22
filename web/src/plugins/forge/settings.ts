import type { CategoryEntry } from '@/components/settings/settingsFieldMap'

/** Settings category id (the plugin's directory is `forge`). */
export const category = 'forgeIntegration'

/**
 * Settings fields for this plugin's own settings page (`forgeIntegration`).
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
    // The master switch gets its OWN card, ahead of everything else: it decides
    // whether the rest of this page applies at all (issue #590).
    //
    // The switch itself now lives in the Dock feature manager. Turning it OFF is
    // destructive — the server clears every project's repository binding and
    // re-enabling does NOT bring them back — so the confirmation belongs to the
    // manager, where the switch is. This page reports the state and links there.
    //
    // Server-side, not per-device: "stop talking to GitHub" is about the
    // credentials and the outbound polling, which live on the server, so every
    // client of one server must agree.
    // Title carries one line under it: the feature is called 代码托管集成
    // everywhere now, so this is what tells the user WHICH services that means
    // (GitHub / GitLab / Gitea). Without it the renamed entry reads as "some
    // generic integration" and the user looking for GitHub loses the trail.
    // A description on the status row (not a separate header row) because the
    // status row is already the page's first element.
    { type: 'item', spec: { labelKey: 'settings.items.featureStatusForge', descriptionKey: 'settings.items.forgePlatformsDesc', key: 'forgeStatus', type: 'info', source: 'server', statusFeature: 'forge.enabled', sectionHeader: 'settings.items.featureStatusSection' } },
    // Credentials row is a dedicated component (tokens are write-only; the
    // server never returns them, so a plain text field cannot round-trip).
    // Declaring it as the card body keeps the component choice in this file —
    // the settings module only knows there is a body, not which one.
    { type: 'item', spec: { labelKey: 'settings.items.forgeCredentials', descriptionKey: 'settings.items.forgeCredentialsDesc', key: 'forgeCredentials', type: 'info', source: 'server', sectionHeader: 'settings.items.forgeSection', cardBody: { load: () => import('@/components/settings/ForgeCredentialsRow.vue'), descriptionKey: 'settings.items.forgeCredentialsDesc' } } },
    { type: 'item', spec: { labelKey: 'settings.items.forgeNotifyOpened', descriptionKey: 'settings.items.forgeNotifyOpenedDesc', key: 'forge.notify.opened', type: 'switch', source: 'server', sectionHeader: 'settings.items.forgeNotifySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.forgeNotifyClosed', descriptionKey: 'settings.items.forgeNotifyClosedDesc', key: 'forge.notify.closed', type: 'switch', source: 'server', sectionHeader: 'settings.items.forgeNotifySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.forgeNotifyMerged', descriptionKey: 'settings.items.forgeNotifyMergedDesc', key: 'forge.notify.merged', type: 'switch', source: 'server', sectionHeader: 'settings.items.forgeNotifySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.forgeNotifyReopened', descriptionKey: 'settings.items.forgeNotifyReopenedDesc', key: 'forge.notify.reopened', type: 'switch', source: 'server', sectionHeader: 'settings.items.forgeNotifySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.forgeNotifyCommented', descriptionKey: 'settings.items.forgeNotifyCommentedDesc', key: 'forge.notify.commented', type: 'switch', source: 'server', sectionHeader: 'settings.items.forgeNotifySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.forgeNotifyPipeline', descriptionKey: 'settings.items.forgeNotifyPipelineDesc', key: 'forge.notify.pipeline', type: 'switch', source: 'server', sectionHeader: 'settings.items.forgeNotifySection' } },
    { type: 'item', spec: { labelKey: 'settings.items.forgeInsecureTLS', descriptionKey: 'settings.items.forgeInsecureTLSDesc', key: 'forge.insecure_tls', type: 'switch', source: 'server', sectionHeader: 'settings.items.forgeTlsSection' } },
    // Global kill-switch for event-triggered AI tasks. Notifications and unread
    // keep working while it is on; only automatic task firing stops.
    { type: 'item', spec: { labelKey: 'settings.items.forgePauseEventTasks', descriptionKey: 'settings.items.forgePauseEventTasksDesc', key: 'forge.pause_event_tasks', type: 'switch', source: 'server', sectionHeader: 'settings.items.forgeAutomationSection' } },
] satisfies CategoryEntry[]
