import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

/**
 * One tile language, two components: `CaptureSourceGrid` (the host's own
 * screens and windows) and `MemberScreenCard` (a member's live picture).
 *
 * The tile styling is global (`assets/capture-sources.css`) because a scoped
 * rule compiles to `.capture-source[data-v-x]` and would leave the card from
 * the other component unstyled — the box would silently collapse to a bare
 * div. jsdom applies no stylesheets, so this reads the sources.
 */

const ROOT = join(__dirname, '..', '..', '..', '..', '..')
const SHARED = 'web/src/assets/capture-sources.css'
const GRID = 'web/src/components/live/CaptureSourceGrid.vue'
const CARD = 'web/src/components/live/MemberScreenCard.vue'

const TILE_RULES = [
  '.capture-sources',
  '.capture-sources--compact',
  '.capture-source',
  '.capture-source__thumb',
  '.capture-source__kind',
  '.capture-source__name',
]

function sourceOf(file: string): string {
  return readFileSync(join(ROOT, file), 'utf8')
}

function styleOf(file: string): string {
  const src = sourceOf(file)
  const start = src.indexOf('<style')
  return start < 0 ? '' : src.slice(start)
}

describe('one source-tile language', () => {
  it('declares every tile rule in the shared stylesheet', () => {
    const css = sourceOf(SHARED)
    for (const selector of TILE_RULES) {
      expect(css, `${SHARED} must define ${selector}`).toMatch(
        new RegExp(`${selector.replace(/\./g, '\\.')}\\s*\\{`),
      )
    }
  })

  it.each([GRID, CARD])('%s loads the shared stylesheet', (file) => {
    expect(sourceOf(file)).toContain("import '@/assets/capture-sources.css'")
  })

  it.each([GRID, CARD])('%s keeps no scoped copy of the tile rules', (file) => {
    const css = styleOf(file)
    for (const selector of TILE_RULES) {
      // A scoped copy would out-rank the shared rule (0,2,0 vs 0,1,0) and the
      // two tiles would drift the moment one of them is edited.
      expect(css, `${file} must not redeclare ${selector}`).not.toMatch(
        new RegExp(`${selector.replace(/\./g, '\\.')}\\s*\\{`),
      )
    }
  })

  it('draws the member card on the same frame as a source tile', () => {
    const card = sourceOf(CARD)
    // Same box and same 16:9 frame, by class, not by copied values. No name
    // line: the card names the sharer in the pill on the picture instead.
    expect(card).toMatch(/class="capture-source member-screen-card"/)
    expect(card).toContain('capture-source__thumb')
    expect(card).not.toContain('capture-source__name')
  })

  it('leaves the selected state to the grid', () => {
    // The card is the picture that is already going out, not a choice: an
    // accent ring on it would fight the host's own selected tile below.
    const card = sourceOf(CARD)
    expect(card).not.toContain('capture-source--on')
    expect(card).not.toContain('capture-source__check')
  })
})
