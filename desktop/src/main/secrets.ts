import { safeStorage } from 'electron'
import { getStore } from './store'

function warnNoEncryption(): void {
  // eslint-disable-next-line no-console
  console.warn('[secrets] safeStorage encryption unavailable; storing password in plaintext')
}

export function savePassword(password: string): void {
  const store = getStore()
  try {
    if (safeStorage.isEncryptionAvailable()) {
      store.set('sshPasswordEncrypted', safeStorage.encryptString(password).toString('base64'))
    } else {
      warnNoEncryption()
      store.set('sshPasswordEncrypted', `plain:${Buffer.from(password, 'utf8').toString('base64')}`)
    }
  } catch {
    warnNoEncryption()
    store.set('sshPasswordEncrypted', `plain:${Buffer.from(password, 'utf8').toString('base64')}`)
  }
}

export function getPassword(): string {
  const raw = getStore().get('sshPasswordEncrypted')
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
