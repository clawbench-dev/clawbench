import i18n from '@/i18n'
import type { CategoryEntry } from '@/components/settings/settingsFieldMap'

/**
 * Settings fields for this plugin's own settings page (`portForward`).
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
    { type: 'panel', config: {
      panelId: 'portForward',
      // No enable switch: the feature is always available (Enabled is pinned
      // server-side in ApplyDefaults). What used to be a per-client choice is
      // now the `port_forward.transport` row below — a server-side switch the
      // clients follow, so it is visible and editable from the web too.
      commonFields: [
        { labelKey: 'settings.items.portForwardTransport', descriptionKey: 'settings.items.portForwardTransportDesc', key: 'port_forward.transport', type: 'select', source: 'server', options: [
          { labelKey: 'proxy.transportH2', value: 'h2' },
          { labelKey: 'proxy.transportSsh', value: 'ssh' },
        ] },
        { labelKey: 'settings.items.portForwardPort', descriptionKey: 'settings.items.portForwardPortDesc', key: 'port_forward.port', type: 'number', source: 'server', displayTransform: (v: unknown) => v === 0 ? i18n.global.t('settings.items.portForwardPortAuto') : v },
      ],
      // No connectivity test button: the panel's tunnel status row is the single
      // readiness signal. The server-side test could only ever answer "the
      // listener bound and accepts a loopback dial" — it runs inside the server,
      // so it cannot observe whether a client actually connected — while the
      // status row reports that plus the client-side tunnel and per-port
      // liveness. Two rows answering overlapping questions is what the merge
      // removed.
    }},
] satisfies CategoryEntry[]
