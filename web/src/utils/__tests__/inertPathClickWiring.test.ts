import { describe, expect, it } from 'vitest'
import { readWebFile } from '@/testUtils/readWebFile'

/**
 * Wiring contract for the inert-path click layer.
 *
 * Two things can silently break this feature, and neither is observable from a
 * component test (each installs its own listener to stay independent of app
 * wiring), so both are asserted at the source level.
 *
 * 1. The layer must actually be installed on mount and disposed on unmount. A
 *    leaked document-level capture listener would outlive the app and swallow
 *    clicks for whatever mounted next (e.g. across HMR reloads).
 *
 * 2. It must be installed AFTER dragClickGuard. The drag guard suppresses
 *    drag-select clicks with `preventDefault()` (not `stopImmediatePropagation`),
 *    so a same-node listener still runs — the inert layer relies on seeing
 *    `defaultPrevented` to tell a text selection from a real click. Installing
 *    it first would make that flag always false and open the search panel on
 *    every drag-select inside a chip.
 */
describe('inertPathClick wiring', () => {
  const APP = 'src/App.vue'

  it('imports and installs the layer on mount', () => {
    const src = readWebFile(APP)

    expect(src).toMatch(/import \{[^}]*installInertPathClick[^}]*\} from '\.\/utils\/inertPathClick'/)
    expect(src).toMatch(/onMounted\(\(\) => \{[\s\S]*?installInertPathClick\(\)/)
  })

  it('disposes the layer on unmount', () => {
    const src = readWebFile(APP)
    expect(src).toMatch(/onUnmounted\(\(\) => \{[\s\S]*?stopInertPathClick\?\.\(\)/)
  })

  it('installs AFTER dragClickGuard so defaultPrevented is meaningful', () => {
    const src = readWebFile(APP)

    const dragIdx = src.indexOf('installDragClickGuard()')
    const inertIdx = src.indexOf('installInertPathClick()')

    expect(dragIdx, 'dragClickGuard must be installed in App.vue').toBeGreaterThan(-1)
    expect(inertIdx, 'inertPathClick must be installed in App.vue').toBeGreaterThan(-1)
    expect(
      inertIdx,
      'installInertPathClick must come after installDragClickGuard: the inert layer '
      + 'reads e.defaultPrevented to ignore drag-select clicks, and the drag guard is '
      + 'what sets it.',
    ).toBeGreaterThan(dragIdx)
  })

  it('mounts the picker component', () => {
    const src = readWebFile(APP)
    expect(src).toContain('InertPathPicker')
  })
})
