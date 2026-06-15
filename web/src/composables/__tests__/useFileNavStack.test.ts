import { describe, expect, it, beforeEach } from 'vitest'
import { useFileNavStack, _resetForTesting } from '@/composables/useFileNavStack'

beforeEach(() => {
  _resetForTesting()
})

describe('useFileNavStack', () => {
  it('initial state: overlay closed, no path, empty stack', () => {
    const nav = useFileNavStack()
    expect(nav.overlayOpen.value).toBe(false)
    expect(nav.currentFilePath.value).toBeNull()
    expect(nav.canGoBack.value).toBe(false)
  })

  it('openFile: opens overlay and sets current path', () => {
    const nav = useFileNavStack()
    nav.openFile('/src/main.ts')
    expect(nav.overlayOpen.value).toBe(true)
    expect(nav.currentFilePath.value).toBe('/src/main.ts')
    expect(nav.canGoBack.value).toBe(false)
  })

  it('openFile multiple times: builds navigation stack', () => {
    const nav = useFileNavStack()
    nav.openFile('/src/a.ts')
    nav.openFile('/src/b.ts')
    nav.openFile('/src/c.ts')
    expect(nav.currentFilePath.value).toBe('/src/c.ts')
    expect(nav.canGoBack.value).toBe(true)
  })

  it('goBack: steps back through the stack', () => {
    const nav = useFileNavStack()
    nav.openFile('/src/a.ts')
    nav.openFile('/src/b.ts')
    nav.openFile('/src/c.ts')

    const back1 = nav.goBack()
    expect(back1).toBe('/src/b.ts')
    expect(nav.currentFilePath.value).toBe('/src/b.ts')

    const back2 = nav.goBack()
    expect(back2).toBe('/src/a.ts')
    expect(nav.currentFilePath.value).toBe('/src/a.ts')
  })

  it('closeOverlay: closes overlay and clears stack', () => {
    const nav = useFileNavStack()
    nav.openFile('/src/a.ts')
    nav.openFile('/src/b.ts')
    nav.closeOverlay()
    expect(nav.overlayOpen.value).toBe(false)
    expect(nav.currentFilePath.value).toBeNull()
    expect(nav.canGoBack.value).toBe(false)
  })

  it('goBack at stack bottom does not close overlay', () => {
    const nav = useFileNavStack()
    nav.openFile('/src/a.ts')
    // At bottom: canGoBack is false, goBack returns null
    expect(nav.canGoBack.value).toBe(false)
    const result = nav.goBack()
    expect(result).toBeNull()
    expect(nav.overlayOpen.value).toBe(true)
    expect(nav.currentFilePath.value).toBe('/src/a.ts')
  })

  it('goBack when stack is empty is a no-op', () => {
    const nav = useFileNavStack()
    expect(nav.canGoBack.value).toBe(false)
    const result = nav.goBack()
    expect(result).toBeNull()
    expect(nav.overlayOpen.value).toBe(false)
  })

  it('openFile same path twice: stack has two entries', () => {
    const nav = useFileNavStack()
    nav.openFile('/src/a.ts')
    nav.openFile('/src/a.ts')
    expect(nav.canGoBack.value).toBe(true)
    const back = nav.goBack()
    expect(back).toBe('/src/a.ts')
    expect(nav.canGoBack.value).toBe(false)
  })

  it('after closeOverlay, openFile starts fresh stack', () => {
    const nav = useFileNavStack()
    nav.openFile('/src/a.ts')
    nav.openFile('/src/b.ts')
    nav.closeOverlay()

    nav.openFile('/src/c.ts')
    expect(nav.overlayOpen.value).toBe(true)
    expect(nav.currentFilePath.value).toBe('/src/c.ts')
    expect(nav.canGoBack.value).toBe(false)
  })

  it('module-level singleton: multiple calls return same instance', () => {
    const nav1 = useFileNavStack()
    nav1.openFile('/src/a.ts')

    const nav2 = useFileNavStack()
    expect(nav2.overlayOpen.value).toBe(true)
    expect(nav2.currentFilePath.value).toBe('/src/a.ts')

    // Mutating through one reference is visible through the other
    nav2.openFile('/src/b.ts')
    expect(nav1.currentFilePath.value).toBe('/src/b.ts')
  })
})
