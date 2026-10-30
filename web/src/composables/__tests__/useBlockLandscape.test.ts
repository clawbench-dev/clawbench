import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  closeBlockLandscape,
  openBlockLandscape,
  shouldRotateBlockLandscape,
  blockLandscapeState,
} from '@/composables/useBlockLandscape.ts'

vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

afterEach(() => {
  closeBlockLandscape()
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

describe('openBlockLandscape', () => {
  it('clones the whole wrapper, header included, so the table controls come along', () => {
    const wrapper = buildWrapper('word-wrap', '<table><tr><td>a</td></tr></table>')
    expect(openBlockLandscape(wrapper)).toBe(true)

    const html = blockLandscapeState.value!.html
    expect(html).toContain('table-block-wrapper')
    expect(html).toContain('table-block-header')
    expect(html).toContain('table-block-copy-btn')
    expect(html).toContain('table-block-wrap-btn')
    expect(html).toContain('<td>a</td>')
  })

  it('keeps the source wrap mode by cloning it verbatim', () => {
    const wrapper = buildWrapper('no-wrap', '<table><tr><td>a</td></tr></table>')
    openBlockLandscape(wrapper)
    expect(blockLandscapeState.value!.html).toContain('table-block-wrapper no-wrap')
  })

  it('turns the landscape button into the close button', () => {
    const wrapper = buildWrapper('word-wrap', '<table><tr><td>a</td></tr></table>')
    openBlockLandscape(wrapper)

    const holder = document.createElement('div')
    holder.innerHTML = blockLandscapeState.value!.html
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
    openBlockLandscape(wrapper)

    const html = blockLandscapeState.value!.html
    expect(html).toContain('data-row-idx')
    expect(html).toContain('data-table-idx')
  })

  it('records the source wrapper and the trigger', () => {
    const wrapper = buildWrapper('word-wrap', '<table><tr><td>a</td></tr></table>')
    const trigger = document.createElement('button')
    openBlockLandscape(wrapper, trigger)

    expect(blockLandscapeState.value!.source).toBe(wrapper)
    expect(blockLandscapeState.value!.trigger).toBe(trigger)
  })

  it('returns false and leaves the state alone when there is no table', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'table-block-wrapper'
    document.body.appendChild(wrapper)

    expect(openBlockLandscape(wrapper)).toBe(false)
    expect(blockLandscapeState.value).toBeNull()
  })
})

describe('openBlockLandscape — code blocks', () => {
  function buildCodeWrapper(): HTMLElement {
    const wrapper = document.createElement('div')
    wrapper.className = 'code-block-wrapper word-wrap'
    wrapper.innerHTML = `
      <div class="code-block-header">
        <span class="code-block-lang">go</span>
        <span class="code-block-header-actions">
          <button class="code-block-copy-btn" data-action="copy"></button>
          <button class="code-block-wrap-btn is-wrapped" data-action="wrap"></button>
          <button class="code-block-landscape-btn" data-action="landscape" title="codeBlock.landscape"></button>
        </span>
      </div>
      <pre><code class="language-go">fmt.Println("hi")</code></pre>`
    document.body.appendChild(wrapper)
    return wrapper
  }

  it('opens for a code block and clones its own header', () => {
    const wrapper = buildCodeWrapper()
    expect(openBlockLandscape(wrapper)).toBe(true)

    const html = blockLandscapeState.value!.html
    expect(html).toContain('code-block-wrapper')
    expect(html).toContain('code-block-header')
    expect(html).toContain('fmt.Println')
    expect(blockLandscapeState.value!.source).toBe(wrapper)
  })

  it('turns the code-block landscape button into the close button', () => {
    const wrapper = buildCodeWrapper()
    openBlockLandscape(wrapper)

    const holder = document.createElement('div')
    holder.innerHTML = blockLandscapeState.value!.html
    const btn = holder.querySelector('.code-block-landscape-btn')!

    expect(btn.getAttribute('data-action')).toBe('landscape-close')
    // Code blocks carry their own i18n namespace, not the table one.
    expect(btn.getAttribute('title')).toBe('codeBlock.landscapeClose')
    expect(wrapper.querySelector('.code-block-landscape-btn')!.getAttribute('data-action')).toBe('landscape')
  })

  it('still refuses a wrapper with neither table nor pre', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'code-block-wrapper'
    document.body.appendChild(wrapper)
    expect(openBlockLandscape(wrapper)).toBe(false)
    expect(blockLandscapeState.value).toBeNull()
  })
})

describe('shouldRotateBlockLandscape', () => {
  it('rotates a touch device held in portrait', () => {
    expect(shouldRotateBlockLandscape({ isTouchPrimary: true, width: 390, height: 844 })).toBe(true)
  })

  it('does not rotate a touch device already in landscape', () => {
    expect(shouldRotateBlockLandscape({ isTouchPrimary: true, width: 844, height: 390 })).toBe(false)
  })

  it('does not rotate a narrow desktop window (no touch)', () => {
    // A merely narrow mouse+keyboard window must keep the table upright.
    expect(shouldRotateBlockLandscape({ isTouchPrimary: false, width: 400, height: 900 })).toBe(false)
  })
})
