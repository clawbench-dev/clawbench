import { afterEach, describe, expect, it } from 'vitest'
import {
  closeTableLandscape,
  openTableLandscape,
  shouldRotateTableLandscape,
  tableLandscapeState,
} from '@/composables/useTableLandscape.ts'

afterEach(() => {
  closeTableLandscape()
  document.body.innerHTML = ''
})

function buildWrapper(wrapClass: string, tableHtml: string): HTMLElement {
  const wrapper = document.createElement('div')
  wrapper.className = `table-block-wrapper ${wrapClass}`
  const holder = document.createElement('div')
  holder.className = 'table-wrap'
  holder.innerHTML = tableHtml
  wrapper.appendChild(holder)
  document.body.appendChild(wrapper)
  return wrapper
}

describe('openTableLandscape', () => {
  it('clones the table and reports the source wrap mode', () => {
    const wrapper = buildWrapper('word-wrap', '<table><tr><td>a</td></tr></table>')
    const trigger = document.createElement('button')
    expect(openTableLandscape(wrapper, trigger)).toBe(true)

    expect(tableLandscapeState.value?.html).toContain('<td>a</td>')
    expect(tableLandscapeState.value?.wrapClass).toBe('word-wrap')
    expect(tableLandscapeState.value?.trigger).toBe(trigger)
  })

  it('reports no-wrap when the per-table toggle is off', () => {
    const wrapper = buildWrapper('no-wrap', '<table><tr><td>a</td></tr></table>')
    openTableLandscape(wrapper)
    expect(tableLandscapeState.value?.wrapClass).toBe('no-wrap')
  })

  it('drops the row-viewer hooks without mutating the source table', () => {
    const wrapper = buildWrapper(
      'word-wrap',
      '<table data-table-idx="2"><tbody><tr data-row-idx="1"><td>a</td></tr></tbody></table>',
    )
    openTableLandscape(wrapper)

    const html = tableLandscapeState.value!.html
    expect(html).not.toContain('data-row-idx')
    expect(html).not.toContain('data-table-idx')
    // The live table keeps its hooks — the chat row viewer still works there.
    expect(wrapper.querySelector('tr')!.getAttribute('data-row-idx')).toBe('1')
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
