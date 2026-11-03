import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Regression guard: the meeting file timeline must be driven by what the viewer
 * is showing, not only by openFile().
 *
 * The bug: a file opened BEFORE the meeting never fires openFile() during it,
 * so the host could switch to the file tab mid-meeting, read for ten minutes,
 * and the record ("会议期间打开的文件") stayed empty. The fix is a watcher on the
 * viewer's visibility; the behaviour itself is covered by
 * useMeetingFileVisibility.test.ts, but nothing would fail if App.vue stopped
 * calling it — the hook is the whole wiring, so it gets a source-level guard
 * (App.vue has no mount-level test, same approach as
 * wideScreenFocusOnEnter.test.ts).
 */

/** Strip comments so an assertion cannot be satisfied by prose alone. */
function stripComments(src: string): string {
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/\/\/.*$/gm, '')
}

const code = stripComments(readWebFile('src/App.vue'))

/** The argument block of the useMeetingFileVisibility(...) call. */
function visibilityCallArgs(src: string): string {
  const at = src.indexOf('useMeetingFileVisibility(')
  if (at === -1) throw new Error('useMeetingFileVisibility(...) not found in App.vue')
  const open = src.indexOf('(', at)
  let depth = 0
  for (let i = open; i < src.length; i++) {
    if (src[i] === '(') depth++
    else if (src[i] === ')') {
      depth--
      if (depth === 0) return src.slice(open, i + 1)
    }
  }
  throw new Error('unbalanced parentheses in the useMeetingFileVisibility call')
}

describe('meeting file timeline follows the viewer', () => {
  it('wires the visibility watcher into App.vue', () => {
    expect(code.includes('useMeetingFileVisibility('), 'the watcher is no longer wired').toBe(true)
  })

  it('gates on the file view being on screen, not just on a file being open', () => {
    const args = visibilityCallArgs(code)
    expect(args, 'must gate on the view tab').toContain("panelIsActive('view')")
    expect(args, 'must require the viewer to be open').toContain('fileNav.overlayOpen.value')
    expect(args, 'must report the file on screen').toContain('fileNav.currentFilePath.value')
  })

  it('runs after the nav stack exists', () => {
    // A call before `const fileNav = ...` would throw at setup (TDZ) — and the
    // whole file panel would never mount.
    expect(code.indexOf('const fileNav = useFileNavStack()')).toBeGreaterThan(-1)
    expect(code.indexOf('useMeetingFileVisibility(')).toBeGreaterThan(
      code.indexOf('const fileNav = useFileNavStack()'),
    )
  })
})
