import Store from 'electron-store'
import type { ServerEntry } from './types'
import type { TransportPreference } from './transport'
import type { WindowBounds } from './windowGeometry'
import type { FloatingStats, Rect, ResolvedColors } from './floatingPolicy'
import { DEFAULT_THEME_ID } from '../shared/theme'

export interface ServerListSchema {
  servers: ServerEntry[]
  serverUrl: string
  /**
   * Legacy single-slot password (pre per-server storage). Read-only: migrated
   * into the matching `servers[].passwordEncrypted` at startup and then
   * cleared. Kept in the schema so an existing install's value is not dropped
   * by the store's defaults merge.
   */
  sshPasswordEncrypted: string | null
  nativePushEnabled: boolean
  /**
   * Full theme ID (e.g. 'github-dark', 'nord'), never a collapsed
   * 'dark'/'light'. The login page resolves its colours from
   * `[data-theme="<id>"]` blocks and has no rule for a bare 'dark', so a
   * collapsed value leaves every colour variable undefined and the page renders
   * with transparent inputs and buttons.
   *
   * This default matters on a FRESH INSTALL: nothing has called setTheme yet,
   * so this value is what the first login page renders with.
   */
  theme: string
  // Language selected in the web UI, mirrored here so the native surfaces that
  // render OUTSIDE the web page (context-menu labels, the first-run login page)
  // follow the user's choice instead of the OS locale.
  language: string
  /**
   * Which tunnel transport the desktop shell uses ('ssh' | 'h2'). The settings
   * row in the port-forward panel writes this; `initTransportPreference()`
   * hydrates the tunnel module from it at startup, so the choice survives a
   * restart.
   *
   * Defaults to 'ssh' — the shell's long-standing behaviour. A stored value is
   * re-validated on read (a hand-edited or downgraded config could hold
   * anything, including the retired 'both'), and an unrecognized one falls back
   * to this default rather than reaching the transport dispatch.
   */
  tunnelTransport: TransportPreference
  /**
   * Last main-window geometry, so the app reopens where the user left it.
   *
   * `null` on a fresh install. Deliberately NOT validated here — the store is
   * a dumb persistence layer, and the reachability check needs the live display
   * list (see windowGeometry.ts). A rect saved on a monitor that has since been
   * unplugged is therefore kept on disk but ignored at startup, so replugging
   * the monitor brings the old position back.
   */
  windowBounds: WindowBounds | null
  /** Whether the window was maximized at close (restored on next launch). */
  windowMaximized: boolean
  /**
   * Whether the always-on-top status capsule (and its tray icon) is on.
   *
   * Persisted rather than derived: the user switched it on deliberately, and it
   * must survive a restart without waiting for the web app to re-assert it.
   */
  floatingEnabled: boolean
  /** Where the user dragged the capsule, in DIPs. Clamped on read. */
  floatingBounds: Rect | null
  /**
   * Last counters seen, kept so a restart shows the previous state instead of
   * blinking 0/0/0 until the first overview lands.
   */
  floatingStats: FloatingStats | null
  /** Palette the web UI pushed through `setTheme`, for the capsule. */
  floatingColors: ResolvedColors | null
}

const defaults: ServerListSchema = {
  servers: [],
  serverUrl: '',
  sshPasswordEncrypted: null,
  nativePushEnabled: true,
  theme: DEFAULT_THEME_ID,
  language: '',
  tunnelTransport: 'ssh',
  windowBounds: null,
  windowMaximized: false,
  floatingEnabled: false,
  floatingBounds: null,
  floatingStats: null,
  floatingColors: null,
}

let store: Store<ServerListSchema> | null = null

export function initStore(): Store<ServerListSchema> {
  if (!store) {
    store = new Store<ServerListSchema>({ name: 'clawbench', defaults })
  }
  return store
}

export function getStore(): Store<ServerListSchema> {
  if (!store) throw new Error('store not initialized — call initStore() first')
  return store
}
