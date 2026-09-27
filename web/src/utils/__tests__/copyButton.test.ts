import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { COPY_ICON_SVG, CHECK_ICON_SVG, COPY_FEEDBACK_MS, copyWithFlash } from '@/utils/copyButton'

const copyTextMock = vi.fn()
vi.mock('@/utils/clipboard', () => ({
  copyText: (...args: unknown[]) => copyTextMock(...args),
}))

vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => `t:${key}`,
}))

/** A hand-built icon button, shaped like the ones the imperative callers emit. */
function makeButton(attrs: Record<string, string> = {}) {
  const btn = document.createElement('button')
  btn.className = 'code-block-copy-btn'
  btn.innerHTML = COPY_ICON_SVG
  for (const [k, v] of Object.entries(attrs)) btn.setAttribute(k, v)
  document.body.appendChild(btn)
  return btn
}

/**
 * The glyph currently rendered in the button.
 *
 * Comparing `innerHTML` to the SVG constants does not work: the parser
 * normalizes self-closing tags (`<path/>` → `<path></path>`), so the string
 * never round-trips. The `d` attribute of the path is the glyph's identity and
 * survives parsing.
 */
function glyphOf(btn: HTMLElement): string | null {
  return btn.querySelector('path')?.getAttribute('d') ?? null
}

function pathOf(svg: string): string | null {
  const host = document.createElement('div')
  host.innerHTML = svg
  return host.querySelector('path')?.getAttribute('d') ?? null
}

const COPY_D = pathOf(COPY_ICON_SVG)
const CHECK_D = pathOf(CHECK_ICON_SVG)

describe('copyButton toolkit', () => {
  beforeEach(() => {
    copyTextMock.mockReset()
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
    document.body.innerHTML = ''
  })

  it('exports the glyphs and timing used by every hand-built copy button', () => {
    expect(COPY_ICON_SVG).toContain('<svg')
    expect(CHECK_ICON_SVG).toContain('<svg')
    // Distinct glyphs — a shared one would make the swap invisible.
    expect(CHECK_D).toBeTruthy()
    expect(COPY_D).toBeTruthy()
    expect(CHECK_D).not.toBe(COPY_D)
    expect(COPY_FEEDBACK_MS).toBeGreaterThan(0)
  })

  it('copies the text, swaps to the check and restores the original glyph', () => {
    const btn = makeButton({ title: 'Copy', 'aria-label': 'Copy' })
    expect(glyphOf(btn)).toBe(COPY_D)

    copyWithFlash(btn, 'payload')

    expect(copyTextMock).toHaveBeenCalledWith('payload')
    expect(glyphOf(btn)).toBe(CHECK_D)
    expect(btn.classList.contains('is-copied')).toBe(true)
    expect(btn.getAttribute('title')).toBe('t:common.copied')
    expect(btn.getAttribute('aria-label')).toBe('t:common.copied')

    vi.advanceTimersByTime(COPY_FEEDBACK_MS)

    expect(glyphOf(btn)).toBe(COPY_D)
    expect(btn.classList.contains('is-copied')).toBe(false)
    // The idle wording is captured on entry and restored verbatim.
    expect(btn.getAttribute('title')).toBe('Copy')
    expect(btn.getAttribute('aria-label')).toBe('Copy')
  })

  it('removes the attributes it added when the button had none', () => {
    const btn = makeButton()

    copyWithFlash(btn, 'payload')
    vi.advanceTimersByTime(COPY_FEEDBACK_MS)

    expect(btn.hasAttribute('title')).toBe(false)
    expect(btn.hasAttribute('aria-label')).toBe(false)
  })

  it('does not copy empty text and leaves the button untouched', () => {
    const btn = makeButton({ title: 'Copy' })

    copyWithFlash(btn, '')

    expect(copyTextMock).not.toHaveBeenCalled()
    expect(glyphOf(btn)).toBe(COPY_D)
    expect(btn.classList.contains('is-copied')).toBe(false)
  })

  it('ignores a second click while the check is showing', () => {
    // Without the re-entrancy guard the second click re-captures the "Copied"
    // title as the idle one, leaving the button permanently stuck on it.
    const btn = makeButton({ title: 'Copy' })

    copyWithFlash(btn, 'first')
    copyWithFlash(btn, 'second')

    expect(copyTextMock).toHaveBeenCalledTimes(1)
    expect(copyTextMock).toHaveBeenCalledWith('first')

    vi.advanceTimersByTime(COPY_FEEDBACK_MS)
    expect(btn.getAttribute('title')).toBe('Copy')
    expect(btn.classList.contains('is-copied')).toBe(false)
  })

  it('honours a custom feedback duration', () => {
    const btn = makeButton()

    copyWithFlash(btn, 'payload', 100)
    vi.advanceTimersByTime(99)
    expect(btn.classList.contains('is-copied')).toBe(true)

    vi.advanceTimersByTime(1)
    expect(btn.classList.contains('is-copied')).toBe(false)
  })
})
