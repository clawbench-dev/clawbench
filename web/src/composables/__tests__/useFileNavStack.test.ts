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

  it('goForward: restores a file after going back', () => {
    const nav = useFileNavStack()
    nav.openFile('/src/a.ts')
    nav.openFile('/src/b.ts')
    nav.goBack()

    expect(nav.canGoForward.value).toBe(true)
    expect(nav.goForward()).toBe('/src/b.ts')
    expect(nav.currentFilePath.value).toBe('/src/b.ts')
    expect(nav.canGoForward.value).toBe(false)
  })

  it('preserves line targets and view mode across back and forward navigation', () => {
    const nav = useFileNavStack()
    nav.openFile('/docs/guide.md', { viewMode: 'rendered' })
    nav.openFile('/src/main.rs', { lineStart: 42, lineEnd: 45, viewMode: 'raw' })

    expect(nav.currentLocation.value).toEqual({
      path: '/src/main.rs',
      lineStart: 42,
      lineEnd: 45,
      viewMode: 'raw',
    })

    nav.goBack()
    expect(nav.currentLocation.value).toEqual({ path: '/docs/guide.md', viewMode: 'rendered' })

    nav.goForward()
    expect(nav.currentLocation.value?.lineStart).toBe(42)
    expect(nav.currentLocation.value?.viewMode).toBe('raw')
  })

  it('updates the current visit without changing history depth', () => {
    const nav = useFileNavStack()
    nav.openFile('/docs/guide.md')
    nav.updateCurrent({ viewMode: 'rendered' })

    expect(nav.currentLocation.value).toEqual({ path: '/docs/guide.md', viewMode: 'rendered' })
    expect(nav.canGoBack.value).toBe(false)
  })

  it('keeps different line targets in the same file as separate visits', () => {
    const nav = useFileNavStack()
    nav.openFile('/src/main.rs', { lineStart: 10 })
    nav.openFile('/src/main.rs', { lineStart: 20 })

    expect(nav.canGoBack.value).toBe(true)
    nav.goBack()
    expect(nav.currentLocation.value?.lineStart).toBe(10)
  })

  it('openFile after going back discards the forward branch', () => {
    const nav = useFileNavStack()
    nav.openFile('/src/a.ts')
    nav.openFile('/src/b.ts')
    nav.openFile('/src/c.ts')
    nav.goBack()
    nav.openFile('/src/d.ts')

    expect(nav.currentFilePath.value).toBe('/src/d.ts')
    expect(nav.canGoForward.value).toBe(false)
    expect(nav.goBack()).toBe('/src/b.ts')
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

  it('openFile same path consecutively: deduplicates top of stack', () => {
    const nav = useFileNavStack()
    nav.openFile('/src/a.ts')
    nav.openFile('/src/a.ts')
    expect(nav.canGoBack.value).toBe(false)
    expect(nav.currentFilePath.value).toBe('/src/a.ts')
  })

  it('openFile same path non-consecutively: keeps both entries', () => {
    const nav = useFileNavStack()
    nav.openFile('/src/a.ts')
    nav.openFile('/src/b.ts')
    nav.openFile('/src/a.ts')
    expect(nav.canGoBack.value).toBe(true)
    expect(nav.currentFilePath.value).toBe('/src/a.ts')
    const back = nav.goBack()
    expect(back).toBe('/src/b.ts')
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

  describe('removePath', () => {
    it('removes a path from the middle of the stack', () => {
      const nav = useFileNavStack()
      nav.openFile('/src/a.ts')
      nav.openFile('/src/b.ts')
      nav.openFile('/src/c.ts')
      nav.removePath('/src/b.ts')
      expect(nav.currentFilePath.value).toBe('/src/c.ts')
      expect(nav.canGoBack.value).toBe(true)
      // goBack should skip the removed entry
      const back = nav.goBack()
      expect(back).toBe('/src/a.ts')
    })

    it('removes the top path from the stack', () => {
      const nav = useFileNavStack()
      nav.openFile('/src/a.ts')
      nav.openFile('/src/b.ts')
      nav.removePath('/src/b.ts')
      expect(nav.currentFilePath.value).toBe('/src/a.ts')
      expect(nav.canGoBack.value).toBe(false)
    })

    it('closes overlay when stack becomes empty', () => {
      const nav = useFileNavStack()
      nav.openFile('/src/a.ts')
      nav.removePath('/src/a.ts')
      expect(nav.overlayOpen.value).toBe(false)
      expect(nav.currentFilePath.value).toBeNull()
    })

    it('no-op for path not in stack', () => {
      const nav = useFileNavStack()
      nav.openFile('/src/a.ts')
      nav.openFile('/src/b.ts')
      nav.removePath('/src/missing.ts')
      expect(nav.currentFilePath.value).toBe('/src/b.ts')
      expect(nav.canGoBack.value).toBe(true)
    })

    it('removes only the last occurrence of a duplicate path', () => {
      const nav = useFileNavStack()
      nav.openFile('/src/a.ts')
      nav.openFile('/src/b.ts')
      nav.openFile('/src/a.ts')
      nav.removePath('/src/a.ts')
      // Should remove the top occurrence, leaving a.ts at the bottom
      expect(nav.currentFilePath.value).toBe('/src/b.ts')
      const back = nav.goBack()
      expect(back).toBe('/src/a.ts')
    })
  })
})
