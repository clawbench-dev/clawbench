import type { CategoryEntry } from '@/components/settings/settingsFieldMap'

/**
 * Settings fields for this plugin's own settings page (`mail`).
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
    // Master switch status, on its own card ahead of the accounts — same shape
    // and reasoning as `forge.enabled`.
    //
    // The switch itself is in the Dock feature manager, not here: one toggle
    // there has to control both the tab and the background feature, and two
    // switches for one flag drift apart. This row reports the state and links
    // to the manager.
    //
    // Server-side, not per-device: turning mail off stops the background IMAP
    // sync and the email-triggered tasks on the server, so every client of one
    // server must agree. Accounts are kept, not deleted.
    { type: 'item', spec: { labelKey: 'settings.items.featureStatusMail', descriptionKey: 'settings.items.mailEnabledDesc', key: 'mailStatus', type: 'info', source: 'server', statusFeature: 'mail.enabled', sectionHeader: 'settings.items.featureStatusSection' } },
    // Mailbox accounts (IMAP + SMTP). The card body is MailSetting; this item
    // only opens the card, so it renders no row.
    { type: 'item', spec: { labelKey: 'settings.items.mailAccountsSection', descriptionKey: 'settings.items.mailAccountsDesc', key: 'mailAccounts', type: 'info', source: 'server', sectionHeader: 'settings.items.mailAccountsSection', cardBody: { load: () => import('@/components/settings/MailSetting.vue') } } },
    // New-mail notifications. A server switch, not a per-device preference:
    // the sync runs on the server and the same event feeds the notification
    // center, the OS notification and the IM push. Turning it off silences the
    // ALERT only — syncing, the unread badge and the mail panel keep working.
    { type: 'item', spec: { labelKey: 'settings.items.mailNotify', descriptionKey: 'settings.items.mailNotifyDesc', key: 'mail.notify', type: 'switch', source: 'server', sectionHeader: 'settings.items.mailNotifySection' } },
    // How often the server polls, how deep it caches and how much it downloads
    // up front. All three are server-side because they describe what the
    // SERVER does to the mailbox and to its own disk — a per-device value
    // would mean one client's server polling on another device's behalf.
    //
    // Deliberately NOT exposed: the flag re-read window, the per-account sync
    // timeout, the startup delay, the page size and the 25MB attachment cap.
    // Those are internal safety numbers — a user cannot make a better choice
    // than the default, and each one would be a knob nobody can explain.
    { type: 'item', spec: { labelKey: 'settings.items.mailSyncInterval', descriptionKey: 'settings.items.mailSyncIntervalDesc', key: 'mail.sync_interval_minutes', type: 'number', source: 'server', min: 1, max: 120, sectionHeader: 'settings.items.mailSyncSection' } },
    { type: 'item', spec: { labelKey: 'settings.items.mailSyncWindow', descriptionKey: 'settings.items.mailSyncWindowDesc', key: 'mail.sync_window', type: 'number', source: 'server', min: 20, max: 500, sectionHeader: 'settings.items.mailSyncSection' } },
    // 0 is a real value here (do not prefetch), so the row must not treat an
    // empty input as 0 by accident — `min: 0` documents the floor.
    { type: 'item', spec: { labelKey: 'settings.items.mailBodyPrefetch', descriptionKey: 'settings.items.mailBodyPrefetchDesc', key: 'mail.body_prefetch', type: 'number', source: 'server', min: 0, max: 50, sectionHeader: 'settings.items.mailSyncSection' } },
    // How a message body is drawn. Local, not server: it changes nothing about
    // what the server fetches or stores, only whether this screen lets the
    // mail's own <img> tags hit the network. The default is ON, so the row is
    // about letting a cautious reader turn the exposure off — the description
    // has to name the risk (tracking pixels / read receipts), not just say
    // "images".
    { type: 'item', spec: { labelKey: 'settings.items.mailLoadImages', descriptionKey: 'settings.items.mailLoadImagesDesc', key: 'mailLoadRemoteImages', type: 'switch', source: 'local', sectionHeader: 'settings.items.mailReadSection' } },
] satisfies CategoryEntry[]
