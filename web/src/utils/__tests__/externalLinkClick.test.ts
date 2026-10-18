import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { installExternalLinkClick } from '@/utils/externalLinkClick'

/**
 * External links are rendered as spans (see markedConfig's `link` renderer) so
 * that a long-press selects the text instead of starting WebView's link gesture.
 * That makes tap handling our responsibility, and the whole point is the
 * ORDERING/capture semantics of a document-level listener — so these tests drive
 * a real listener against real DOM rather than calling the handler directly.
 */

const mockOpenExternalUrl = vi.fn()
const mockConfirm = vi.fn()

vi.mock('@/utils/externalLink', () => ({
  openExternalUrl: (url: string) => mockOpenExternalUrl(url),
}))

vi.mock('@/composables/useDialog', () => ({
  useDialog: () => ({ confirm: mockConfirm }),
}))

vi.mock('@/i18n', () => ({
  default: { global: { t: (k: string) => k } },
}))

let dispose: (() => void) | null = null

beforeEach(() => {
  mockOpenExternalUrl.mockReset()
  mockConfirm.mockReset()
  document.body.innerHTML = ''
})

afterEach(() => {
  dispose?.()
  dispose = null
  document.body.innerHTML = ''
})

/** Render a link span into the document and return it. */
function renderSpan(href: string | null = 'https://example.com'): HTMLElement {
  const host = document.createElement('div')
  host.innerHTML = href === null
    ? '<span class="md-external-link" role="link" tabindex="0">no href</span>'
    : `<span class="md-external-link" data-href="${href}" role="link" tabindex="0">${href}</span>`
  document.body.appendChild(host)
  return host.querySelector<HTMLElement>('.md-external-link')!
}

function click(el: Element, opts: MouseEventInit = {}) {
  el.dispatchEvent(new MouseEvent('click', {
    bubbles: true, cancelable: true, button: 0, detail: 1, ...opts,
  }))
}

describe('installExternalLinkClick', () => {
  it('asks before opening, and opens only AFTER confirmation', async () => {
    dispose = installExternalLinkClick()
    const span = renderSpan('https://example.com/a')

    // Hold the confirmation open so we can assert nothing has opened yet.
    let resolveConfirm!: (v: boolean) => void
    mockConfirm.mockReturnValue(new Promise<boolean>((r) => { resolveConfirm = r }))

    click(span)
    await vi.waitFor(() => expect(mockConfirm).toHaveBeenCalled())
    // The dialog must have been shown with the URL itself, so the user can see
    // where they are going (and copy it if that was the real intent).
    expect(mockConfirm).toHaveBeenCalledWith('https://example.com/a', expect.any(Object))
    // Still pending: opening here would defeat the confirmation entirely.
    expect(mockOpenExternalUrl, 'must not open while the dialog is open').not.toHaveBeenCalled()

    resolveConfirm(true)
    await vi.waitFor(() => expect(mockOpenExternalUrl).toHaveBeenCalledWith('https://example.com/a'))
  })

  it('does not open when the dialog is cancelled', async () => {
    dispose = installExternalLinkClick()
    const span = renderSpan()

    mockConfirm.mockResolvedValue(false)
    click(span)
    await vi.waitFor(() => expect(mockConfirm).toHaveBeenCalled())
    expect(mockOpenExternalUrl).not.toHaveBeenCalled()
  })

  it('never navigates on the click itself — the page must not be disturbed', () => {
    dispose = installExternalLinkClick()
    const span = renderSpan()

    mockConfirm.mockResolvedValue(false)
    const ev = new MouseEvent('click', { bubbles: true, cancelable: true, button: 0 })
    span.dispatchEvent(ev)

    // preventDefault + stopPropagation: a host that still expects an anchor
    // (the local-link guard) must not also act on this tap.
    expect(ev.defaultPrevented).toBe(true)
  })

  it('ignores clicks that are not on an external link', () => {
    dispose = installExternalLinkClick()
    const host = document.createElement('div')
    host.innerHTML = '<span class="other">plain</span>'
    document.body.appendChild(host)

    click(host.querySelector('.other')!)
    expect(mockConfirm).not.toHaveBeenCalled()
  })

  it('ignores a span with no data-href', () => {
    dispose = installExternalLinkClick()
    click(renderSpan(null))
    expect(mockConfirm).not.toHaveBeenCalled()
  })

  it('ignores modified clicks (ctrl/cmd/shift/alt)', () => {
    dispose = installExternalLinkClick()
    const span = renderSpan()

    click(span, { ctrlKey: true })
    click(span, { metaKey: true })
    click(span, { shiftKey: true })
    click(span, { altKey: true })

    expect(mockConfirm).not.toHaveBeenCalled()
  })

  it('ignores a click already suppressed by the drag guard', () => {
    dispose = installExternalLinkClick()
    const span = renderSpan()

    // dragClickGuard (installed before this layer) preventDefaults a click that
    // ended a text selection; honouring the flag keeps selecting the URL from
    // opening the dialog.
    const ev = new MouseEvent('click', { bubbles: true, cancelable: true, button: 0 })
    ev.preventDefault()
    span.dispatchEvent(ev)

    expect(mockConfirm).not.toHaveBeenCalled()
  })

  it('activates from the keyboard (Enter / Space)', async () => {
    dispose = installExternalLinkClick()
    const span = renderSpan()

    mockConfirm.mockResolvedValue(true)
    span.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    await vi.waitFor(() => expect(mockConfirm).toHaveBeenCalled())

    mockConfirm.mockClear()
    span.dispatchEvent(new KeyboardEvent('keydown', { key: ' ', bubbles: true, cancelable: true }))
    await vi.waitFor(() => expect(mockConfirm).toHaveBeenCalled())
  })

  /**
   * `data-href` is NOT covered by DOMPurify's URI allowlist (that inspects
   * `href`/`src` only), and raw HTML in an AI message is a legal markdown token.
   * So a `javascript:` value can reach this listener — the sink must refuse it.
   */
  it('refuses to open a non-external scheme planted in data-href', async () => {
    dispose = installExternalLinkClick()
    const span = renderSpan('javascript:alert(1)')

    mockConfirm.mockResolvedValue(true)
    click(span)

    // No dialog and no navigation: the value never leaves the page.
    await new Promise((r) => setTimeout(r, 0))
    expect(mockConfirm).not.toHaveBeenCalled()
    expect(mockOpenExternalUrl).not.toHaveBeenCalled()
  })

  it('refuses data:, file: and other non-external schemes', async () => {
    dispose = installExternalLinkClick()
    mockConfirm.mockResolvedValue(true)

    for (const bad of ['data:text/html,<script>alert(1)</script>', 'file:///etc/passwd', 'vbscript:x']) {
      click(renderSpan(bad))
    }
    await new Promise((r) => setTimeout(r, 0))

    expect(mockConfirm).not.toHaveBeenCalled()
    expect(mockOpenExternalUrl).not.toHaveBeenCalled()
  })

  it('stops listening after dispose', () => {
    const off = installExternalLinkClick()
    off()

    click(renderSpan())
    expect(mockConfirm).not.toHaveBeenCalled()
  })
})
