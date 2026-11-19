import { describe, expect, it } from 'vitest'

/**
 * The "Restart Server" row is useless on a server that was started by the
 * desktop app: /api/config/restart answers 403 there, and offering a button
 * that always fails reads as a bug rather than as a policy.
 *
 * "Check for updates" deliberately STAYS. A fused ("all-in-one") install does
 * upgrade from inside the app — it installs the whole package (client and
 * server together) through the desktop shell, instead of swapping the server
 * binary. Hiding the row would leave such an install with no update entry at
 * all.
 *
 * The filter is one conditional inside a large component, so this pins the
 * decision the same way sibling CSS/guard tests do: by reading the source.
 */
import fs from 'node:fs'
import path from 'node:path'

const SRC = fs.readFileSync(path.join(__dirname, '..', 'SettingsCategory.vue'), 'utf8')

describe('settings rows hidden while the desktop app owns the server', () => {
  it('filters restartServer on the managed marker but keeps checkUpgrade', () => {
    const line = SRC.split('\n').find((l) => l.includes('upgradeManagedByEmbedder.value')) || ''
    expect(line).toContain("'restartServer'")
    expect(line).toContain('continue')
    expect(line).not.toContain("'checkUpgrade'")
  })

  it('imports the marker from useUpgrade rather than keeping its own copy', () => {
    expect(SRC).toMatch(/import\s*\{\s*managedByEmbedder as upgradeManagedByEmbedder\s*\}\s*from\s*'@\/composables\/useUpgrade'/)
  })
})
