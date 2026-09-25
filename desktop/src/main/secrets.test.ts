import { describe, it, expect, vi, beforeEach } from 'vitest'

/**
 * Password storage is per server. The regression this guards is that a single
 * global slot made every server but the last-connected one unauthenticated:
 * the login page prefill, the startup auto-login and the SSH tunnel all read
 * the same value, so only the most recent `connectToServer` could succeed.
 */

// Encryption availability is a per-test knob so both the encrypted and the
// plaintext-fallback paths are exercised.
const { cryptoState } = vi.hoisted(() => ({ cryptoState: { available: true } }))

vi.mock('electron', () => ({
  safeStorage: {
    isEncryptionAvailable: () => cryptoState.available,
    // Prefix so a test can tell ciphertext from the plaintext marker.
    encryptString: (s: string) => Buffer.from('enc:' + s, 'utf8'),
    decryptString: (b: Buffer) => {
      const text = b.toString('utf8')
      if (!text.startsWith('enc:')) throw new Error('not decryptable')
      return text.slice('enc:'.length)
    },
  },
}))

const storeData: Record<string, unknown> = {}
vi.mock('electron-store', () => ({
  default: class {
    get(k: string) { return storeData[k] }
    set(k: string, v: unknown) { storeData[k] = v }
  },
}))

import {
  savePasswordFor, getPasswordFor, removePasswordFor, savePassword, getPassword,
  migratePasswords, getServersForRenderer,
} from './secrets'
import { initStore } from './store'

const URL_A = 'https://a.example.com:20000'
const URL_B = 'https://b.example.com:20000'

interface RawEntry { url: string; password?: string; passwordEncrypted?: string }

function entries(): RawEntry[] {
  return storeData.servers as RawEntry[]
}

function resetStore(overrides: Record<string, unknown> = {}): void {
  for (const k of Object.keys(storeData)) delete storeData[k]
  Object.assign(storeData, {
    servers: [],
    serverUrl: '',
    sshPasswordEncrypted: null,
    ...overrides,
  })
}

describe('secrets: per-server password storage', () => {
  beforeEach(() => {
    cryptoState.available = true
    resetStore()
    initStore()
  })

  it('keeps each server password independent', () => {
    savePasswordFor(URL_A, 'password-a')
    savePasswordFor(URL_B, 'password-b')

    expect(getPasswordFor(URL_A)).toBe('password-a')
    expect(getPasswordFor(URL_B)).toBe('password-b')
  })

  it('updating one server does not disturb another', () => {
    savePasswordFor(URL_A, 'old-a')
    savePasswordFor(URL_B, 'password-b')
    savePasswordFor(URL_A, 'new-a')

    expect(getPasswordFor(URL_A)).toBe('new-a')
    expect(getPasswordFor(URL_B)).toBe('password-b')
  })

  it('returns empty for a server with no stored password', () => {
    savePasswordFor(URL_A, 'password-a')
    expect(getPasswordFor('https://unknown.example.com')).toBe('')
  })

  it('stores ciphertext on the entry, never the raw password', () => {
    savePasswordFor(URL_A, 'hunter2')
    const entry = entries().find((e) => e.url === URL_A)!

    expect(entry.passwordEncrypted).toBe(Buffer.from('enc:hunter2', 'utf8').toString('base64'))
    expect(entry.passwordEncrypted).not.toContain('hunter2')
    // No plaintext field is ever written by the new path.
    expect(entry.password).toBeUndefined()
  })

  it('falls back to a plaintext marker when encryption is unavailable', () => {
    cryptoState.available = false
    savePasswordFor(URL_A, 'hunter2')

    const entry = entries().find((e) => e.url === URL_A)!
    expect(entry.passwordEncrypted!.startsWith('plain:')).toBe(true)
    expect(getPasswordFor(URL_A)).toBe('hunter2')
  })

  it('clearing one password leaves the others intact', () => {
    savePasswordFor(URL_A, 'password-a')
    savePasswordFor(URL_B, 'password-b')

    savePasswordFor(URL_A, '')

    expect(getPasswordFor(URL_A)).toBe('')
    expect(getPasswordFor(URL_B)).toBe('password-b')
    // The credential must be gone, not stored as an empty string, or re-adding
    // the URL would look like "already has a password".
    expect(entries().find((e) => e.url === URL_A)!.passwordEncrypted).toBeUndefined()
  })

  it('removePasswordFor drops only the named server credential', () => {
    savePasswordFor(URL_A, 'password-a')
    savePasswordFor(URL_B, 'password-b')

    removePasswordFor(URL_A)

    expect(getPasswordFor(URL_A)).toBe('')
    expect(getPasswordFor(URL_B)).toBe('password-b')
  })

  it('active-server helpers read and write the current serverUrl entry', () => {
    storeData.serverUrl = URL_B
    savePassword('active-pw')

    expect(getPassword()).toBe('active-pw')
    expect(getPasswordFor(URL_B)).toBe('active-pw')
    expect(getPasswordFor(URL_A)).toBe('')
  })

  it('savePasswordFor on the active server retires the legacy global slot', () => {
    // Otherwise getPasswordFor() would keep falling back to a stale global.
    storeData.serverUrl = URL_A
    storeData.sshPasswordEncrypted = Buffer.from('enc:legacy', 'utf8').toString('base64')

    savePasswordFor(URL_A, 'fresh')

    expect(storeData.sshPasswordEncrypted).toBeNull()
    expect(getPasswordFor(URL_A)).toBe('fresh')
  })
})

describe('secrets: legacy migration', () => {
  beforeEach(() => {
    cryptoState.available = true
    resetStore()
    initStore()
  })

  it('encrypts legacy plaintext entries and strips them', () => {
    storeData.servers = [{ url: URL_A, password: 'plain-a' }, { url: URL_B, password: 'plain-b' }]

    migratePasswords()

    expect(getPasswordFor(URL_A)).toBe('plain-a')
    expect(getPasswordFor(URL_B)).toBe('plain-b')
    expect(entries().every((e) => e.password === undefined)).toBe(true)
  })

  it('attributes the global password to the active server only', () => {
    storeData.serverUrl = URL_B
    storeData.sshPasswordEncrypted = Buffer.from('enc:legacy-global', 'utf8').toString('base64')
    storeData.servers = [{ url: URL_A }, { url: URL_B }]

    migratePasswords()

    expect(getPasswordFor(URL_B)).toBe('legacy-global')
    // The global slot belonged to whichever server was active — it must NOT be
    // handed to every other saved server.
    expect(getPasswordFor(URL_A)).toBe('')
  })

  it('does not overwrite an existing per-server value with legacy data', () => {
    storeData.serverUrl = URL_A
    storeData.servers = [{ url: URL_A, password: 'legacy-plain' }]
    savePasswordFor(URL_A, 'current')

    migratePasswords()

    expect(getPasswordFor(URL_A)).toBe('current')
  })

  it('is idempotent', () => {
    storeData.serverUrl = URL_A
    storeData.sshPasswordEncrypted = Buffer.from('enc:legacy', 'utf8').toString('base64')
    storeData.servers = [{ url: URL_A }, { url: URL_B, password: 'plain-b' }]

    migratePasswords()
    const afterFirst = JSON.stringify(storeData.servers)
    migratePasswords()

    expect(JSON.stringify(storeData.servers)).toBe(afterFirst)
  })

  it('keeps an unattributable global password rather than dropping it', () => {
    // Fresh install: a global was written before any server URL existed.
    storeData.serverUrl = ''
    storeData.sshPasswordEncrypted = Buffer.from('enc:orphan', 'utf8').toString('base64')

    migratePasswords()

    expect(storeData.sshPasswordEncrypted).not.toBeNull()
  })

  it('clears the global once it has been attributed', () => {
    storeData.serverUrl = URL_A
    storeData.sshPasswordEncrypted = Buffer.from('enc:legacy', 'utf8').toString('base64')
    storeData.servers = [{ url: URL_A }]

    migratePasswords()

    expect(storeData.sshPasswordEncrypted).toBeNull()
  })
})

describe('secrets: renderer-facing server list', () => {
  beforeEach(() => {
    cryptoState.available = true
    resetStore()
    initStore()
  })

  it('resolves each entry password so the login page can prefill', () => {
    savePasswordFor(URL_A, 'password-a')
    savePasswordFor(URL_B, 'password-b')

    expect(getServersForRenderer()).toEqual([
      { url: URL_B, password: 'password-b' },
      { url: URL_A, password: 'password-a' },
    ])
  })

  it('never exposes the encrypted form to the renderer', () => {
    savePasswordFor(URL_A, 'password-a')
    const out = getServersForRenderer()
    expect(JSON.stringify(out)).not.toContain('passwordEncrypted')
  })

  it('reports an empty password for a server with none stored', () => {
    storeData.servers = [{ url: URL_A }]
    expect(getServersForRenderer()).toEqual([{ url: URL_A, password: '' }])
  })
})
