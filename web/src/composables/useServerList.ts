import { ref } from 'vue'
import { getNative } from '@/utils/clawbenchNative'

export interface ServerEntry {
  url: string
  password: string
  /** Optional display label. Absent/empty means "no name" — the UI shows the address. */
  name?: string
}

const STORAGE_KEY = 'clawbench-servers'

/** Normalise a name to its stored form: trimmed; empty means "no name". */
export function normalizeServerName(text: unknown): string {
  return typeof text === 'string' ? text.trim() : ''
}

/**
 * Find the server whose NAME equals `name`, ignoring `exceptUrl`.
 *
 * Mirrors findServerNameConflict() in the two login pages' url-utils.js so all
 * three UIs agree on what "duplicate" means: case-insensitive, empty names
 * never match, and the entry being edited is exempt from matching itself.
 */
export function findNameConflict(
  servers: ServerEntry[],
  name: string,
  exceptUrl?: string,
): ServerEntry | null {
  const wanted = normalizeServerName(name).toLowerCase()
  if (!wanted) return null
  return servers.find(
    (s) => s.url !== exceptUrl && normalizeServerName(s.name).toLowerCase() === wanted,
  ) ?? null
}

/** Parse server list from JSON string */
function parseList(json: string): ServerEntry[] {
  try {
    const arr = JSON.parse(json)
    if (!Array.isArray(arr)) return []
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    return arr.filter((e: any) => e && typeof e.url === 'string').map((e: any) => {
      const entry: ServerEntry = {
        url: e.url,
        password: typeof e.password === 'string' ? e.password : '',
      }
      const name = normalizeServerName(e.name)
      if (name) entry.name = name
      return entry
    })
  } catch {
    return []
  }
}

/**
 * Composable for managing the multi-server list.
 * In APP mode, reads/writes via the native bridge (async).
 * In web mode, falls back to localStorage.
 */
export function useServerList() {
  const servers = ref<ServerEntry[]>([])

  async function load() {
    const native = getNative()
    if (native?.getServerList) {
      const json = await native.getServerList()
      servers.value = json ? parseList(json) : []
    } else {
      // Fallback: localStorage (web mode, single-origin only)
      const raw = localStorage.getItem(STORAGE_KEY)
      servers.value = raw ? parseList(raw) : []
    }
  }

  async function save(url: string, password: string, name?: string) {
    const native = getNative()
    const trimmed = normalizeServerName(name)
    if (native?.saveServerNamed) {
      await native.saveServerNamed(url, password, trimmed)
    } else if (native?.saveServer) {
      // Older host without the named variant: the entry still persists, just
      // without a name. Degrading beats refusing to save.
      await native.saveServer(url, password)
    } else {
      const list = parseList(localStorage.getItem(STORAGE_KEY) || '[]')
      const idx = list.findIndex(e => e.url === url)
      if (idx >= 0) {
        list[idx].password = password
        if (trimmed) list[idx].name = trimmed
        else delete list[idx].name
      } else {
        const entry: ServerEntry = { url, password }
        if (trimmed) entry.name = trimmed
        list.push(entry)
      }
      localStorage.setItem(STORAGE_KEY, JSON.stringify(list))
    }
    await load()
  }

  async function remove(url: string) {
    const native = getNative()
    if (native?.removeServer) {
      await native.removeServer(url)
    } else {
      const list = parseList(localStorage.getItem(STORAGE_KEY) || '[]')
      localStorage.setItem(STORAGE_KEY, JSON.stringify(list.filter(e => e.url !== url)))
    }
    await load()
  }

  function getPassword(url: string): string {
    return servers.value.find(e => e.url === url)?.password || ''
  }

  /** Label to render for a server row: its name, else its address. */
  function getLabel(entry: ServerEntry): string {
    return normalizeServerName(entry.name) || entry.url
  }

  return { servers, load, save, remove, getPassword, getLabel }
}
