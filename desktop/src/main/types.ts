export interface ServerEntry {
  url: string
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
