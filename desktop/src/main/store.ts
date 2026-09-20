import Store from 'electron-store'
import type { ServerEntry } from './types'

export interface ServerListSchema {
  servers: ServerEntry[]
  serverUrl: string
  sshPasswordEncrypted: string | null
  nativePushEnabled: boolean
  theme: 'dark' | 'light'
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
  theme: 'dark',
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
