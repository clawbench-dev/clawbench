export interface ServerEntry {
  url: string
  /**
   * Optional user-facing label. Absent (or empty) means "no name": the login
   * page then shows the address instead. Names are unique — the login page
   * refuses to save a duplicate — but that is enforced in the UI, not here.
   */
  name?: string
  /**
   * safeStorage-encrypted password (or a `plain:` base64 marker when the OS
   * keychain is unavailable). Per server, so multiple saved servers can each
   * authenticate. See `secrets.ts`.
   */
  passwordEncrypted?: string
  /**
   * Legacy plaintext password, written by the pre-per-server
   * `native:save-server`. Read-only: `migratePasswords()` encrypts it into
   * `passwordEncrypted` and removes it, so persisted entries should not carry
   * it. Renderer-facing copies DO populate `password` (decrypted) so the login
   * page can prefill; `passwordEncrypted` never leaves the main process.
   */
  password?: string
}
export interface SavedServerConfig {
  protocol: string
  host: string
  port: string
  password: string
}
