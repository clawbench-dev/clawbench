import { safeStorage } from 'electron'
import { getStore } from './store'
import type { ServerEntry } from './types'

function warnNoEncryption(): void {
  // eslint-disable-next-line no-console
  console.warn('[secrets] safeStorage encryption unavailable; storing password in plaintext')
}

/**
 * Encrypt a password for at-rest storage. Falls back to a reversible
 * `plain:<base64>` marker when the OS keychain is unavailable (headless Linux,
 * a locked keyring), which is strictly better than losing the credential.
 */
function encrypt(password: string): string {
  if (safeStorage.isEncryptionAvailable()) {
    try {
      return safeStorage.encryptString(password).toString('base64')
    } catch {
      // fall through to the plaintext marker
    }
  }
  warnNoEncryption()
  return `plain:${Buffer.from(password, 'utf8').toString('base64')}`
}

function decrypt(raw: string | null | undefined): string {
  if (!raw) return ''
  if (raw.startsWith('plain:')) {
    return Buffer.from(raw.slice('plain:'.length), 'base64').toString('utf8')
  }
  try {
    return safeStorage.decryptString(Buffer.from(raw, 'base64'))
  } catch {
    return ''
  }
}

/** Find the stored entry for a URL, or undefined. */
function findEntry(url: string): ServerEntry | undefined {
  return getStore().get('servers').find((s) => s.url === url)
}

/**
 * Save (or clear) the password for one server URL.
 *
 * Passwords live on the server entry they belong to. A single global slot made
 * every server but the most recently connected one unauthenticated: the login
 * page prefill, the startup auto-login and the SSH tunnel all read the same
 * value, so only the last `connectToServer` could succeed.
 *
 * An empty `password` CLEARS the entry's credential — the login page passes ""
 * when switching to a cookie-only server, and keeping the previous value would
 * make the tunnel authenticate with the wrong one.
 */
export function savePasswordFor(url: string, password: string): void {
  const store = getStore()
  if (!url) {
    // No active server yet (first run, before connectToServer). Keep the legacy
    // slot so the credential is not silently dropped; migration picks it up
    // once a server URL exists.
    store.set('sshPasswordEncrypted', password ? encrypt(password) : null)
    return
  }

  const servers = store.get('servers')
  const idx = servers.findIndex((s) => s.url === url)
  if (idx >= 0) {
    const entry = servers[idx]
    delete entry.password // legacy plaintext is superseded
    if (password) entry.passwordEncrypted = encrypt(password)
    else delete entry.passwordEncrypted
    store.set('servers', servers)
  } else if (password) {
    // No entry yet (e.g. connectToServer before the list was written) — create
    // it, promoted to the head as most-recently-used.
    store.set('servers', [{ url, passwordEncrypted: encrypt(password) }, ...servers])
  }

  // The legacy global slot is read-only history. Once the active server has a
  // per-entry value (including an explicit clear), the global must go: otherwise
  // getPasswordFor() would fall back to a stale credential for this URL.
  if (store.get('serverUrl') === url) store.set('sshPasswordEncrypted', null)
}

/** Read the password stored for one server URL. Empty when none is stored. */
export function getPasswordFor(url: string): string {
  if (!url) return ''
  const entry = findEntry(url)
  if (entry) {
    if (entry.passwordEncrypted) return decrypt(entry.passwordEncrypted)
    // Pre-migration plaintext on this very entry.
    if (entry.password) return entry.password
  }
  // Pre-migration data also lived in a single global slot that belonged to
  // whatever server was active at the time. Only that server may claim it.
  if (getStore().get('serverUrl') === url) {
    return decrypt(getStore().get('sshPasswordEncrypted'))
  }
  return ''
}

/** Save the password of the currently active server. */
export function savePassword(password: string): void {
  savePasswordFor(getStore().get('serverUrl'), password)
}

/** Read the password of the currently active server. */
export function getPassword(): string {
  return getPasswordFor(getStore().get('serverUrl'))
}

/** Forget the password for one server URL (used when the server is deleted). */
export function removePasswordFor(url: string): void {
  const store = getStore()
  const servers = store.get('servers')
  const entry = servers.find((s) => s.url === url)
  if (entry) {
    delete entry.passwordEncrypted
    delete entry.password
    store.set('servers', servers)
  }
  // A legacy global that belonged to this URL must not survive the deletion, or
  // re-adding the same URL would silently inherit the deleted credential.
  if (store.get('serverUrl') === url) store.set('sshPasswordEncrypted', null)
}

/**
 * Move pre-existing credentials into the per-entry encrypted field. Runs once
 * at startup; idempotent, so a second call is a no-op.
 *
 * Two legacy shapes are absorbed:
 *  - `servers[].password`: plaintext written by the old `native:save-server`.
 *    Encrypted in place and the plaintext stripped.
 *  - `sshPasswordEncrypted`: the old single global slot. It belonged to the
 *    active `serverUrl`, so it is attributed to that server only.
 */
export function migratePasswords(): void {
  const store = getStore()
  const activeUrl = store.get('serverUrl')
  const globalLegacy = store.get('sshPasswordEncrypted')

  const servers = store.get('servers')
  let serversChanged = false
  for (const entry of servers) {
    if (!entry.password) continue
    if (!entry.passwordEncrypted) entry.passwordEncrypted = encrypt(entry.password)
    // The plaintext copy is superseded either way.
    delete entry.password
    serversChanged = true
  }

  let globalAttributed = false
  if (globalLegacy && activeUrl) {
    const entry = servers.find((s) => s.url === activeUrl)
    if (entry) {
      if (!entry.passwordEncrypted) entry.passwordEncrypted = globalLegacy
      serversChanged = true
      globalAttributed = true
    }
  }

  // Clear the global only once its value has been attributed to a server, or
  // when the active server already carries its own encrypted value (making the
  // global redundant). An unattributable global — no active server URL — is
  // kept rather than silently dropped; it cannot be mis-claimed later, because
  // savePasswordFor() clears it as soon as an active server gets its own value.
  if (globalLegacy && (globalAttributed
      || Boolean(activeUrl && findEntry(activeUrl)?.passwordEncrypted))) {
    store.set('sshPasswordEncrypted', null)
  }

  if (serversChanged) store.set('servers', servers)
}

/**
 * Server list for the renderer, with each entry's password resolved (decrypted)
 * so the login page can prefill it. `passwordEncrypted` never leaves the main
 * process — the returned copies carry only the plaintext `password`.
 */
export function getServersForRenderer(): ServerEntry[] {
  return getStore().get('servers').map((s) => ({
    url: s.url,
    password: getPasswordFor(s.url),
  }))
}
