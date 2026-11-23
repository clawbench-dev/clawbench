/**
 * Safe wrappers around `localStorage`.
 *
 * A bare `localStorage.getItem(...)` crashes in two environments:
 *
 * - Node 25+/26 enable Web Storage by default but, without `--localstorage-file`,
 *   expose an incomplete `globalThis.localStorage` whose `getItem` is
 *   `undefined`. Modules that read it at import time die before anything runs.
 * - Privacy modes (Safari private browsing, some embedded WebViews) keep the
 *   API but throw on every access.
 *
 * Anything read at module scope — or any call that must not take the page down —
 * should go through these instead. A blocked store reads as "no value" and a
 * failed write is silently dropped, which is what callers want in both cases.
 */

/** The global storage if it is usable right now, otherwise null. */
function usableStorage(): Storage | null {
  try {
    if (typeof localStorage === 'undefined') return null
    if (typeof localStorage.getItem !== 'function' || typeof localStorage.setItem !== 'function') return null
    return localStorage
  } catch {
    // Reading `localStorage` itself can throw when storage is blocked.
    return null
  }
}

/** Read a key. Returns null when storage is missing, incomplete, or blocked. */
export function storageGet(key: string): string | null {
  try {
    return usableStorage()?.getItem(key) ?? null
  } catch {
    // getItem can still throw after the checks above (blocked mid-session).
    return null
  }
}

/** Write a key. No-op when storage is missing, incomplete, or blocked. */
export function storageSet(key: string, value: string): void {
  try {
    usableStorage()?.setItem(key, value)
  } catch {
    // Quota exceeded / blocked — callers treat storage as best-effort.
  }
}

/** Remove a key. No-op when storage is missing, incomplete, or blocked. */
export function storageRemove(key: string): void {
  try {
    usableStorage()?.removeItem(key)
  } catch {
    // Blocked mid-session — callers treat storage as best-effort.
  }
}
