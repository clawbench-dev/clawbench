import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  closeTableLandscape,
  openTableLandscape,
  shouldRotateTableLandscape,
  tableLandscapeState,
} from '@/composables/useTableLandscape.ts'

vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

afterEach(() => {
  closeTableLandscape()
  document.body.innerHTML = ''
})

function buildWrapper(wrapClass: string, tableHtml: string): HTMLElement {
  const wrapper = document.createElement('div')
  wrapper.className = `table-block-wrapper ${wrapClass}`
  wrapper.innerHTML = `
    <div class="table-block-header">
      <span class="table-block-label">Table</span>
      <span class="table-block-header-actions">
        <button class="table-block-copy-btn" data-action="open-copy-menu"></button>
        <button class="table-block-wrap-btn is-wrapped" data-action="wrap"></button>
        <button class="table-block-landscape-btn" data-action="landscape" title="tableBlock.landscape"></button>
      </span>
    </div>
    <div class="table-wrap">${tableHtml}</div>`
  document.body.appendChild(wrapper)
  return wrapper
}

describe('openTableLandscape', () => {
  it('clones the whole wrapper, header included, so the table controls come along', () => {
    const wrapper = buildWrapper('word-wrap', '<table><tr><td>a</td></tr></table>')
    expect(openTableLandscape(wrapper)).toBe(true)

    const html = tableLandscapeState.value!.html
    expect(html).toContain('table-block-wrapper')
    expect(html).toContain('table-block-header')
    expect(html).toContain('table-block-copy-btn')
    expect(html).toContain('table-block-wrap-btn')
    expect(html).toContain('<td>a</td>')
  })

  it('keeps the source wrap mode by cloning it verbatim', () => {
    const wrapper = buildWrapper('no-wrap', '<table><tr><td>a</td></tr></table>')
    openTableLandscape(wrapper)
    expect(tableLandscapeState.value!.html).toContain('table-block-wrapper no-wrap')
  })

  it('turns the landscape button into the close button', () => {
    const wrapper = buildWrapper('word-wrap', '<table><tr><td>a</td></tr></table>')
    openTableLandscape(wrapper)

    const holder = document.createElement('div')
    holder.innerHTML = tableLandscapeState.value!.html
    const btn = holder.querySelector('.table-block-landscape-btn')!

    expect(btn.getAttribute('data-action')).toBe('landscape-close')
    expect(btn.getAttribute('title')).toBe('tableBlock.landscapeClose')
    // The glyph is replaced too — a rotate icon that closes would mislead.
    expect(btn.innerHTML).toContain('M18 6 6 18')
    // The source button is untouched: the table behind still opens the overlay.
    expect(wrapper.querySelector('.table-block-landscape-btn')!.getAttribute('data-action')).toBe('landscape')
  })

  it('keeps the row-viewer hooks so rows stay tappable inside the overlay', () => {
    const wrapper = buildWrapper(
      'word-wrap',
      '<table data-table-idx="2"><tbody><tr data-row-idx="1"><td>a</td></tr></tbody></table>',
    )
    openTableLandscape(wrapper)

    const html = tableLandscapeState.value!.html
    expect(html).toContain('data-row-idx')
    expect(html).toContain('data-table-idx')
  })

  it('records the source wrapper and the trigger', () => {
    const wrapper = buildWrapper('word-wrap', '<table><tr><td>a</td></tr></table>')
    const trigger = document.createElement('button')
    openTableLandscape(wrapper, trigger)

    expect(tableLandscapeState.value!.source).toBe(wrapper)
    expect(tableLandscapeState.value!.trigger).toBe(trigger)
  })

  it('returns false and leaves the state alone when there is no table', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'table-block-wrapper'
    document.body.appendChild(wrapper)

    expect(openTableLandscape(wrapper)).toBe(false)
    expect(tableLandscapeState.value).toBeNull()
  })
})

describe('shouldRotateTableLandscape', () => {
  it('rotates a touch device held in portrait', () => {
    expect(shouldRotateTableLandscape({ isTouchPrimary: true, width: 390, height: 844 })).toBe(true)
  })

  it('does not rotate a touch device already in landscape', () => {
    expect(shouldRotateTableLandscape({ isTouchPrimary: true, width: 844, height: 390 })).toBe(false)
  })

  it('does not rotate a narrow desktop window (no touch)', () => {
    // A merely narrow mouse+keyboard window must keep the table upright.
    expect(shouldRotateTableLandscape({ isTouchPrimary: false, width: 400, height: 900 })).toBe(false)
  })
})
