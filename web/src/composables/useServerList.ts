import { ref } from 'vue'

export interface ServerEntry {
  url: string
  password: string
}

const STORAGE_KEY = 'clawbench-servers'

/** Get the native AndroidNative bridge (typed) */
function getNative(): {
  getServerList?: () => string
  saveServer?: (url: string, password: string) => void
  removeServer?: (url: string) => void
} | undefined {
  return (window as any).AndroidNative
}

/** Parse server list from JSON string */
function parseList(json: string): ServerEntry[] {
  try {
    const arr = JSON.parse(json)
    if (!Array.isArray(arr)) return []
    return arr.filter((e: any) => e && typeof e.url === 'string').map((e: any) => ({
      url: e.url,
      password: typeof e.password === 'string' ? e.password : '',
    }))
  } catch {
    return []
  }
}

/**
 * Composable for managing the multi-server list.
 * In APP mode, reads/writes via AndroidNative bridge (synchronous).
 * In web mode, falls back to localStorage.
 */
export function useServerList() {
  const servers = ref<ServerEntry[]>([])

  function load() {
    const native = getNative()
    if (native?.getServerList) {
      // Synchronous JS bridge call — no loading state needed
      servers.value = parseList(native.getServerList())
    } else {
      // Fallback: localStorage (web mode, single-origin only)
      const raw = localStorage.getItem(STORAGE_KEY)
      servers.value = raw ? parseList(raw) : []
    }
  }

  function save(url: string, password: string) {
    const native = getNative()
    if (native?.saveServer) {
      native.saveServer(url, password)
    } else {
      const list = parseList(localStorage.getItem(STORAGE_KEY) || '[]')
      const idx = list.findIndex(e => e.url === url)
      if (idx >= 0) {
        list[idx].password = password
      } else {
        list.push({ url, password })
      }
      localStorage.setItem(STORAGE_KEY, JSON.stringify(list))
    }
    // Refresh in-memory list
    load()
  }

  function remove(url: string) {
    const native = getNative()
    if (native?.removeServer) {
      native.removeServer(url)
    } else {
      const list = parseList(localStorage.getItem(STORAGE_KEY) || '[]')
      const filtered = list.filter(e => e.url !== url)
      localStorage.setItem(STORAGE_KEY, JSON.stringify(filtered))
    }
    load()
  }

  /** Get password for a given server URL, or empty string */
  function getPassword(url: string): string {
    return servers.value.find(e => e.url === url)?.password || ''
  }

  return { servers, load, save, remove, getPassword }
}
