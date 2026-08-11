import Store from 'electron-store'
import type { ServerEntry } from './types'

export interface ServerListSchema {
  servers: ServerEntry[]
  serverUrl: string
  sshPasswordEncrypted: string | null
  nativePushEnabled: boolean
  theme: 'dark' | 'light'
}

const defaults: ServerListSchema = {
  servers: [],
  serverUrl: '',
  sshPasswordEncrypted: null,
  nativePushEnabled: true,
  theme: 'dark',
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
