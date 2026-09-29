import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * The drag-vs-click guard only works if it is actually INSTALLED.
 *
 * Every row component relies on it: their own selection checks were removed in
 * favour of this one document-level listener. If the App.vue registration is
 * dropped (a refactor, a merge conflict resolved the wrong way), every row
 * silently goes back to firing on drag-select — and no behavioural test notices,
 * because each component test installs the guard itself to stay independent of
 * app wiring.
 *
 * So the wiring is asserted at the source level: the guard must be installed on
 * mount and disposed on unmount.
 */
describe('dragClickGuard wiring', () => {
  const APP = 'src/App.vue'

  it('installs the guard on mount', () => {
    const src = readWebFile(APP)

    expect(src).toMatch(/import \{[^}]*installDragClickGuard[^}]*\} from '\.\/utils\/dragClickGuard'/)
    expect(src).toMatch(/onMounted\(\(\) => \{[\s\S]*?installDragClickGuard\(\)/)
  })

  it('disposes the guard on unmount', () => {
    const src = readWebFile(APP)

    // A leaked document-level capture listener would outlive the app and
    // suppress clicks for whatever mounted next (e.g. across HMR reloads).
    expect(src).toMatch(/onUnmounted\(\(\) => \{[\s\S]*?stopDragClickGuard\?\.\(\)/)
  })

  // The whole point of the refactor: no component should re-grow its own
  // selection guard, because that is how the coverage drifted to 4-of-50 in the
  // first place.
  it('no component reintroduces a per-row text-selection guard', () => {
    const rows = [
      'src/components/git/GitBranchRow.vue',
      'src/components/git/GitWorktreeCard.vue',
      'src/components/git/GitTagList.vue',
      'src/components/common/AppHeader.vue',
    ]
    for (const path of rows) {
      expect(readWebFile(path), `${path} must rely on the global guard`)
        .not.toContain('hasActiveTextSelection')
    }
  })
})
