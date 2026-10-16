import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * MenuSelect popups are teleported to <body>, which breaks two assumptions that
 * are easy to re-introduce:
 *
 * 1. A `let` at the top level of `<script setup>` is compiled INTO setup(), so
 *    it is per-instance and coordinates nothing. The exclusive-open slot must
 *    live in a real module. (This shipped once: two menus stayed open together.)
 *
 * 2. The consumer cannot style the menu. `.notif-filters :deep(.app-menu-item)`
 *    never matches, because .notif-filters is not an ancestor of a node that was
 *    teleported to <body>. Any row fix must live in MenuSelect, whose list
 *    container IS an ancestor of the slot content.
 *
 * cwd differs between a bare `vitest` run (web/) and scripts/vitest-run.sh
 * (repo root), so probe both roots.
 */

function readWebFile(relPath: string): string {
  for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
    try {
      return readFileSync(join(base, relPath), 'utf8')
    } catch {
      // try the next candidate root
    }
  }
  throw new Error(`${relPath} not found from cwd: ` + process.cwd())
}

const menuSelect = readWebFile('src/components/common/MenuSelect.vue')
const panel = readWebFile('src/components/notifications/NotificationsPanelContent.vue')
const exclusive = readWebFile('src/utils/exclusiveMenu.ts')

/** Strip comments so a commented-out call cannot satisfy a source guard. */
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/(^|[^:])\/\/[^\n]*/g, '$1')
}

describe('teleported menu scoping', () => {
  it('keeps the exclusive-open slot in a real module, not script setup', () => {
    // The registry state must not be a top-level binding in the SFC.
    expect(exclusive).toMatch(/let openInstance/)
    expect(menuSelect).toMatch(/from '@\/utils\/exclusiveMenu'/)
    expect(menuSelect).not.toMatch(/^let openInstance/m)
  })

  it('routes every open/close through the shared slot', () => {
    // Comments stripped: a commented-out call must NOT satisfy this, which is
    // exactly how the two-menus bug would come back.
    const code = stripComments(menuSelect)
    expect(code).toMatch(/claimOpenMenu\(/)
    expect(code).toMatch(/releaseOpenMenu\(/)
    // The popup's own close (outside click / escape) must also release the slot,
    // or a later claim would try to close an already-closed instance.
    expect(code).toMatch(/function onShowUpdate/)
  })

  it('styles slot rows from MenuSelect, not from the consumer', () => {
    // The row-width fix must live where the menu actually is in the DOM.
    expect(menuSelect).toMatch(/\.menu-select-list :deep\(\.app-menu-item\)/)
    // And the consumer must not pretend it can reach the teleported rows.
    expect(panel, 'the panel cannot style teleported menu rows').not.toMatch(/deep\(\.app-menu-item/)
  })

  it('does not let a blanket background override the active highlight', () => {
    // `.menu-select-list :deep(.app-menu-item)` out-ranks the global
    // `.app-menu-item.active`, so a `background: transparent` on the bare class
    // silently kills the accent fill. It must be scoped to non-active rows.
    const css = menuSelect.replace(/\/\*[\s\S]*?\*\//g, '')
    const blanket = css.match(/\.menu-select-list\s+:deep\(\.app-menu-item\)\s*\{([^}]*)\}/)
    expect(blanket, 'the slot-row rule must exist').not.toBeNull()
    expect(blanket![1], 'a blanket background would kill the active fill').not.toMatch(/background/)
    expect(css).toMatch(/:deep\(\.app-menu-item:not\(\.active\)\)/)
  })
})
