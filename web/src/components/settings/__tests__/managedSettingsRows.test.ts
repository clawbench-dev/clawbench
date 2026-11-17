import { describe, expect, it } from 'vitest'

/**
 * The "Restart Server" row and "Check for updates" row are useless on a server
 * that was started by the desktop app: /api/config/restart and
 * /api/upgrade/start both answer 403 there, and offering a button that always
 * fails reads as a bug rather than as a policy.
 *
 * The filter is one conditional inside a large component, so this pins the
 * decision the same way sibling CSS/guard tests do: by reading the source.
 */
import fs from 'node:fs'
import path from 'node:path'

const SRC = fs.readFileSync(path.join(__dirname, '..', 'SettingsCategory.vue'), 'utf8')

describe('settings rows hidden while the desktop app owns the server', () => {
  it('filters both restartServer and checkUpgrade on the managed marker', () => {
    const line = SRC.split('\n').find((l) => l.includes('upgradeManagedByEmbedder.value')) || ''
    expect(line).toContain("'restartServer'")
    expect(line).toContain("'checkUpgrade'")
    expect(line).toContain('continue')
  })

  it('imports the marker from useUpgrade rather than keeping its own copy', () => {
    expect(SRC).toMatch(/import\s*\{\s*managedByEmbedder as upgradeManagedByEmbedder\s*\}\s*from\s*'@\/composables\/useUpgrade'/)
  })
})
