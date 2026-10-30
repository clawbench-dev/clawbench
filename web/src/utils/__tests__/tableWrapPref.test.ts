import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  _resetTableWrapPref,
  applyTableWrapToRenderedTables,
  getDefaultTableWrap,
  setDefaultTableWrap,
  tableWrapVersion,
  TABLE_WRAP_STORAGE_KEY,
} from '@/utils/tableWrapPref.ts'

vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

beforeEach(() => {
  localStorage.clear()
  _resetTableWrapPref()
  document.body.innerHTML = ''
})

afterEach(() => {
  localStorage.clear()
  _resetTableWrapPref()
  document.body.innerHTML = ''
})

function buildTable(wrapClass: string, withButton = true): HTMLElement {
  const wrapper = document.createElement('div')
  wrapper.className = `table-block-wrapper ${wrapClass}`
  wrapper.innerHTML = `${withButton ? '<div class="table-block-header"><button class="table-block-wrap-btn is-wrapped" data-action="wrap"></button></div>' : ''}
    <div class="table-wrap"><table><tr><td>a</td></tr></table></div>`
  document.body.appendChild(wrapper)
  return wrapper
}

describe('getDefaultTableWrap', () => {
  it('defaults to wrapping when nothing is stored', () => {
    expect(getDefaultTableWrap()).toBe(true)
  })

  it('reads a stored false', () => {
    localStorage.setItem(TABLE_WRAP_STORAGE_KEY, 'false')
    expect(getDefaultTableWrap()).toBe(false)
  })

  it('reads a stored true', () => {
    localStorage.setItem(TABLE_WRAP_STORAGE_KEY, 'true')
    expect(getDefaultTableWrap()).toBe(true)
  })

  it('memoises the value for the session', () => {
    expect(getDefaultTableWrap()).toBe(true)
    localStorage.setItem(TABLE_WRAP_STORAGE_KEY, 'false')
    // The stored change is picked up on the next session, not mid-session —
    // the settings writer updates the memo through setDefaultTableWrap.
    expect(getDefaultTableWrap()).toBe(true)
  })
})

describe('setDefaultTableWrap', () => {
  it('persists the value and updates the memo', () => {
    setDefaultTableWrap(false)
    expect(localStorage.getItem(TABLE_WRAP_STORAGE_KEY)).toBe('false')
    expect(getDefaultTableWrap()).toBe(false)
  })

  it('re-applies the mode to tables already in the document', () => {
    const wrapper = buildTable('word-wrap')
    setDefaultTableWrap(false)

    expect(wrapper.classList.contains('no-wrap')).toBe(true)
    expect(wrapper.classList.contains('word-wrap')).toBe(false)
    const btn = wrapper.querySelector<HTMLElement>('.table-block-wrap-btn')!
    expect(btn.classList.contains('is-wrapped')).toBe(false)
    expect(btn.getAttribute('title')).toBe('tableBlock.wrapOff')

    setDefaultTableWrap(true)
    expect(wrapper.classList.contains('word-wrap')).toBe(true)
    expect(btn.classList.contains('is-wrapped')).toBe(true)
    expect(btn.getAttribute('title')).toBe('tableBlock.wrapOn')
  })

  it('leaves a wrapper without a toggle button in the right state', () => {
    const wrapper = buildTable('word-wrap', false)
    applyTableWrapToRenderedTables(false)
    expect(wrapper.classList.contains('no-wrap')).toBe(true)
  })

  it('bumps the render-cache version only when the value actually changes', () => {
    const start = tableWrapVersion.value
    setDefaultTableWrap(false)
    expect(tableWrapVersion.value).toBe(start + 1)
    // Re-setting the same value must not invalidate every cached block.
    setDefaultTableWrap(false)
    expect(tableWrapVersion.value).toBe(start + 1)
  })

  it('ignores non-table wrappers', () => {
    const other = document.createElement('div')
    other.className = 'code-block-wrapper word-wrap'
    document.body.appendChild(other)

    applyTableWrapToRenderedTables(false)
    expect(other.classList.contains('word-wrap')).toBe(true)
  })
})
