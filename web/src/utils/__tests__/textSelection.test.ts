import { describe, expect, it, afterEach, vi } from 'vitest'
import { hasActiveTextSelection } from '@/utils/textSelection'

function stubSelection(text: string) {
  vi.spyOn(window, 'getSelection').mockReturnValue({
    toString: () => text,
  } as unknown as Selection)
}

describe('hasActiveTextSelection', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('is false when nothing is selected', () => {
    stubSelection('')
    expect(hasActiveTextSelection()).toBe(false)
  })

  it('is true when text is selected', () => {
    stubSelection('feature/login')
    expect(hasActiveTextSelection()).toBe(true)
  })

  it('is false when the environment has no getSelection', () => {
    // Older WebViews / non-browser contexts: must not throw.
    const original = window.getSelection
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    delete (window as any).getSelection
    try {
      expect(hasActiveTextSelection()).toBe(false)
    } finally {
      window.getSelection = original
    }
  })

  it('is false when getSelection returns null', () => {
    vi.spyOn(window, 'getSelection').mockReturnValue(null)
    expect(hasActiveTextSelection()).toBe(false)
  })
})
