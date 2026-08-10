import { ref } from 'vue'
import { getNative } from '@/utils/clawbenchNative'

export interface ServerEntry {
  url: string
  password: string
}

const STORAGE_KEY = 'clawbench-servers'

/** Parse server list from JSON string */
function parseList(json: string): ServerEntry[] {
  try {
    const arr = JSON.parse(json)
    if (!Array.isArray(arr)) return []
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
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

  async function save(url: string, password: string) {
    const native = getNative()
    if (native?.saveServer) {
      await native.saveServer(url, password)
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

  return { servers, load, save, remove, getPassword }
}
