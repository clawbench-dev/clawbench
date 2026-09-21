import Store from 'electron-store'
import type { ServerEntry } from './types'
import { DEFAULT_THEME_ID } from '../shared/theme'

export interface ServerListSchema {
  servers: ServerEntry[]
  serverUrl: string
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
}

const defaults: ServerListSchema = {
  servers: [],
  serverUrl: '',
  sshPasswordEncrypted: null,
  nativePushEnabled: true,
  theme: DEFAULT_THEME_ID,
  language: '',
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
