import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  _resetBlockWrapPref,
  applyBlockWrapToRenderedBlocks,
  getDefaultBlockWrap,
  setDefaultBlockWrap,
  blockWrapVersion,
  BLOCK_WRAP_STORAGE_KEY,
} from '@/utils/blockWrapPref.ts'

vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

beforeEach(() => {
  localStorage.clear()
  _resetBlockWrapPref()
  document.body.innerHTML = ''
})

afterEach(() => {
  localStorage.clear()
  _resetBlockWrapPref()
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

describe('getDefaultBlockWrap', () => {
  it('defaults to wrapping when nothing is stored', () => {
    expect(getDefaultBlockWrap()).toBe(true)
  })

  it('reads a stored false', () => {
    localStorage.setItem(BLOCK_WRAP_STORAGE_KEY, 'false')
    expect(getDefaultBlockWrap()).toBe(false)
  })

  it('reads a stored true', () => {
    localStorage.setItem(BLOCK_WRAP_STORAGE_KEY, 'true')
    expect(getDefaultBlockWrap()).toBe(true)
  })

  it('memoises the value for the session', () => {
    expect(getDefaultBlockWrap()).toBe(true)
    localStorage.setItem(BLOCK_WRAP_STORAGE_KEY, 'false')
    // The stored change is picked up on the next session, not mid-session —
    // the settings writer updates the memo through setDefaultBlockWrap.
    expect(getDefaultBlockWrap()).toBe(true)
  })
})

describe('setDefaultBlockWrap', () => {
  it('persists the value and updates the memo', () => {
    setDefaultBlockWrap(false)
    expect(localStorage.getItem(BLOCK_WRAP_STORAGE_KEY)).toBe('false')
    expect(getDefaultBlockWrap()).toBe(false)
  })

  it('re-applies the mode to tables already in the document', () => {
    const wrapper = buildTable('word-wrap')
    setDefaultBlockWrap(false)

    expect(wrapper.classList.contains('no-wrap')).toBe(true)
    expect(wrapper.classList.contains('word-wrap')).toBe(false)
    const btn = wrapper.querySelector<HTMLElement>('.table-block-wrap-btn')!
    expect(btn.classList.contains('is-wrapped')).toBe(false)
    expect(btn.getAttribute('title')).toBe('tableBlock.wrapOff')

    setDefaultBlockWrap(true)
    expect(wrapper.classList.contains('word-wrap')).toBe(true)
    expect(btn.classList.contains('is-wrapped')).toBe(true)
    expect(btn.getAttribute('title')).toBe('tableBlock.wrapOn')
  })

  it('leaves a wrapper without a toggle button in the right state', () => {
    const wrapper = buildTable('word-wrap', false)
    applyBlockWrapToRenderedBlocks(false)
    expect(wrapper.classList.contains('no-wrap')).toBe(true)
  })

  it('bumps the render-cache version only when the value actually changes', () => {
    const start = blockWrapVersion.value
    setDefaultBlockWrap(false)
    expect(blockWrapVersion.value).toBe(start + 1)
    // Re-setting the same value must not invalidate every cached block.
    setDefaultBlockWrap(false)
    expect(blockWrapVersion.value).toBe(start + 1)
  })

  it('re-applies the mode to code blocks too, without inventing a no-wrap class', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'code-block-wrapper word-wrap'
    wrapper.innerHTML = '<div class="code-block-header"><button class="code-block-wrap-btn is-wrapped" data-action="wrap"></button></div><pre><code>x</code></pre>'
    document.body.appendChild(wrapper)

    setDefaultBlockWrap(false)

    expect(wrapper.classList.contains('word-wrap')).toBe(false)
    // Code blocks have no `.no-wrap` rule; the class must not appear.
    expect(wrapper.classList.contains('no-wrap')).toBe(false)
    const btn = wrapper.querySelector<HTMLElement>('.code-block-wrap-btn')!
    expect(btn.classList.contains('is-wrapped')).toBe(false)
    expect(btn.getAttribute('title')).toBe('codeBlock.wrapOff')
  })

  it('ignores wrappers that are neither tables nor code blocks', () => {
    const other = document.createElement('div')
    other.className = 'diff-block-wrapper word-wrap'
    document.body.appendChild(other)

    applyBlockWrapToRenderedBlocks(false)
    expect(other.classList.contains('word-wrap')).toBe(true)
  })
})
