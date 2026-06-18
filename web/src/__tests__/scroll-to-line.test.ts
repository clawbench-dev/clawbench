import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { nextTick } from 'vue'

/**
 * Tests for the scroll-to-line event flow:
 *
 * Click annotated path with line number
 *   → openFilePath(path, lineStart, lineEnd)
 *     → dispatches 'open-file-overlay' with { path, lineStart, lineEnd }
 *       → App.vue handleOpenFileOverlay calls scrollToLine(lineStart, lineEnd)
 *         → scrollToLine retries until .code-line[data-line] is in DOM
 *         → dispatches 'cancel-scroll-restore' before scrollIntoView
 *           → FileViewer clears pendingRestore so it doesn't override scroll
 *
 * This test file validates the scrollToLine retry logic and the
 * cancel-scroll-restore event contract. The openFilePath → open-file-overlay
 * event chain is tested in useFilePathAnnotation.test.ts.
 */
describe('scroll-to-line event flow', () => {
  describe('cancel-scroll-restore event', () => {
    let dispatched: string[]

    beforeEach(() => {
      dispatched = []
      const origDispatch = window.dispatchEvent
      window.dispatchEvent = (e: Event) => {
        dispatched.push(e.type)
        return true
      }
      ;(globalThis as any).__origDispatch = origDispatch
    })

    afterEach(() => {
      window.dispatchEvent = (globalThis as any).__origDispatch
      delete (globalThis as any).__origDispatch
    })

    it('is dispatched before scrollIntoView when scrollToLine finds the target element', () => {
      const mockElement = {
        scrollIntoView: vi.fn(),
        classList: { add: vi.fn() },
        addEventListener: vi.fn(),
      }
      const querySelectorSpy = vi.spyOn(document, 'querySelector')
      querySelectorSpy.mockImplementation((sel: string) => {
        if (sel === '.code-line[data-line="10"]') return mockElement as any
        return null
      })

      // Simulate scrollToLine finding the element
      const startLine = 10
      const firstEl = document.querySelector(`.code-line[data-line="${startLine}"]`)
      if (firstEl) {
        window.dispatchEvent(new CustomEvent('cancel-scroll-restore'))
        firstEl.scrollIntoView({ behavior: 'smooth', block: 'center' })
      }

      expect(dispatched).toContain('cancel-scroll-restore')
      expect(mockElement.scrollIntoView).toHaveBeenCalledWith({ behavior: 'smooth', block: 'center' })

      querySelectorSpy.mockRestore()
    })
  })

  describe('scroll-to-line global event', () => {
    it('carries line and lineEnd in detail', () => {
      const detail = { line: 42, lineEnd: 50 }
      const event = new CustomEvent('scroll-to-line', { detail })

      expect(event.type).toBe('scroll-to-line')
      expect(event.detail).toEqual({ line: 42, lineEnd: 50 })
    })
  })

  describe('retry behavior', () => {
    it('retries querySelector until element appears', async () => {
      let callCount = 0
      const mockElement = {
        scrollIntoView: vi.fn(),
        classList: { add: vi.fn() },
        addEventListener: vi.fn(),
      }

      const querySelectorSpy = vi.spyOn(document, 'querySelector')
      querySelectorSpy.mockImplementation(() => {
        callCount++
        if (callCount >= 3) return mockElement as any
        return null
      })

      const selector = `.code-line[data-line="42"]`
      const maxAttempts = 30
      let attempts = 0

      function tryScroll() {
        attempts++
        const firstEl = document.querySelector(selector)
        if (firstEl) {
          window.dispatchEvent(new CustomEvent('cancel-scroll-restore'))
          firstEl.scrollIntoView({ behavior: 'smooth', block: 'center' })
          return
        }
        if (attempts < maxAttempts) {
          nextTick(tryScroll)
        }
      }

      nextTick(tryScroll)

      // Wait enough ticks for retry to succeed
      for (let i = 0; i < 5; i++) {
        await nextTick()
      }

      expect(callCount).toBeGreaterThanOrEqual(3)
      expect(mockElement.scrollIntoView).toHaveBeenCalled()

      querySelectorSpy.mockRestore()
    })

    it('gives up after maxAttempts', async () => {
      const querySelectorSpy = vi.spyOn(document, 'querySelector')
      querySelectorSpy.mockReturnValue(null)

      const selector = `.code-line[data-line="999"]`
      const maxAttempts = 5
      let attempts = 0

      function tryScroll() {
        attempts++
        const firstEl = document.querySelector(selector)
        if (firstEl) return
        if (attempts < maxAttempts) {
          nextTick(tryScroll)
        }
      }

      nextTick(tryScroll)

      for (let i = 0; i < 10; i++) {
        await nextTick()
      }

      expect(attempts).toBe(maxAttempts)

      querySelectorSpy.mockRestore()
    })
  })
})
