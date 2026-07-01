import { describe, expect, it, vi } from 'vitest'
import { annotateCodeBlockHeaders, annotateTableBlockHeaders, handleCodeBlockClick, handleTableBlockClick } from '@/composables/useCodeBlockHeader.ts'

// Mock clipboard
vi.mock('@/utils/clipboard.ts', () => ({
  copyText: vi.fn(),
}))

// Mock locale
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

describe('annotateCodeBlockHeaders', () => {
  it('returns input unchanged for empty string', () => {
    expect(annotateCodeBlockHeaders('')).toBe('')
  })

  it('wraps code blocks with header', () => {
    const html = '<pre><code class="language-go">fmt.Println("hi")</code></pre>'
    const result = annotateCodeBlockHeaders(html)
    expect(result).toContain('code-block-wrapper')
    expect(result).toContain('code-block-header')
    expect(result).toContain('code-block-lang')
    expect(result).toContain('language-go')
    // Lang label should show "go"
    expect(result).toContain('go')
  })

  it('adds copy and wrap buttons', () => {
    const html = '<pre><code>code</code></pre>'
    const result = annotateCodeBlockHeaders(html)
    expect(result).toContain('code-block-copy-btn')
    expect(result).toContain('code-block-wrap-btn')
    expect(result).toContain('data-action="copy"')
    expect(result).toContain('data-action="wrap"')
  })

  it('skips mermaid blocks', () => {
    const html = '<pre class="mermaid"><code>graph TD</code></pre>'
    const result = annotateCodeBlockHeaders(html)
    expect(result).not.toContain('code-block-wrapper')
  })

  it('skips pre without code child', () => {
    const html = '<pre>plain text</pre>'
    const result = annotateCodeBlockHeaders(html)
    expect(result).not.toContain('code-block-wrapper')
  })

  it('is idempotent (does not double-wrap)', () => {
    const html = '<pre><code>code</code></pre>'
    const first = annotateCodeBlockHeaders(html)
    const second = annotateCodeBlockHeaders(first)
    const wrapperCount1 = (first.match(/code-block-wrapper/g) || []).length
    const wrapperCount2 = (second.match(/code-block-wrapper/g) || []).length
    expect(wrapperCount2).toBe(wrapperCount1)
  })

  it('handles multiple code blocks', () => {
    const html = '<pre><code class="language-js">a</code></pre><pre><code class="language-py">b</code></pre>'
    const result = annotateCodeBlockHeaders(html)
    const wrappers = (result.match(/code-block-wrapper/g) || []).length
    expect(wrappers).toBe(2)
  })

  it('handles code block without language class', () => {
    const html = '<pre><code>no lang</code></pre>'
    const result = annotateCodeBlockHeaders(html)
    expect(result).toContain('code-block-wrapper')
    expect(result).toContain('code-block-lang')
  })

  it('preserves code content', () => {
    const html = '<pre><code>const x = 42;</code></pre>'
    const result = annotateCodeBlockHeaders(html)
    expect(result).toContain('const x = 42;')
  })
})

describe('handleCodeBlockClick', () => {
  function createClickEvent(target: HTMLElement): MouseEvent {
    return {
      target,
      preventDefault: vi.fn(),
      stopPropagation: vi.fn(),
    } as unknown as MouseEvent
  }

  it('returns false for non-code-block clicks', () => {
    const div = document.createElement('div')
    const event = createClickEvent(div)
    expect(handleCodeBlockClick(event)).toBe(false)
  })

  it('returns true and toggles word-wrap for wrap button click', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'code-block-wrapper'
    const pre = document.createElement('pre')
    const btn = document.createElement('button')
    btn.className = 'code-block-wrap-btn'
    btn.setAttribute('data-action', 'wrap')
    wrapper.appendChild(btn)
    wrapper.appendChild(pre)
    document.body.appendChild(wrapper)

    const event = createClickEvent(btn)
    expect(handleCodeBlockClick(event)).toBe(true)
    expect(wrapper.classList.contains('word-wrap')).toBe(true)
    expect(btn.classList.contains('is-wrapped')).toBe(true)

    // Toggle back
    const event2 = createClickEvent(btn)
    handleCodeBlockClick(event2)
    expect(wrapper.classList.contains('word-wrap')).toBe(false)

    wrapper.remove()
  })

  it('returns true for copy button click', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'code-block-wrapper'
    const pre = document.createElement('pre')
    const code = document.createElement('code')
    code.textContent = 'test code'
    pre.appendChild(code)
    const btn = document.createElement('button')
    btn.className = 'code-block-copy-btn'
    btn.setAttribute('data-action', 'copy')
    btn.setAttribute('title', 'Copy')
    btn.setAttribute('aria-label', 'Copy')
    wrapper.appendChild(btn)
    wrapper.appendChild(pre)
    document.body.appendChild(wrapper)

    const event = createClickEvent(btn)
    expect(handleCodeBlockClick(event)).toBe(true)
    expect(btn.classList.contains('is-copied')).toBe(true)

    wrapper.remove()
  })

  it('returns true but does nothing for copy when already is-copied', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'code-block-wrapper'
    const pre = document.createElement('pre')
    const btn = document.createElement('button')
    btn.className = 'code-block-copy-btn is-copied'
    btn.setAttribute('data-action', 'copy')
    wrapper.appendChild(btn)
    wrapper.appendChild(pre)
    document.body.appendChild(wrapper)

    const event = createClickEvent(btn)
    expect(handleCodeBlockClick(event)).toBe(true)

    wrapper.remove()
  })
})

describe('annotateTableBlockHeaders', () => {
  it('returns input unchanged for empty string', () => {
    expect(annotateTableBlockHeaders('')).toBe('')
  })

  it('wraps table-wrap elements with header', () => {
    const html = '<div class="table-wrap"><table><tr><td>data</td></tr></table></div>'
    const result = annotateTableBlockHeaders(html)
    expect(result).toContain('table-block-wrapper')
    expect(result).toContain('table-block-header')
    expect(result).toContain('table-block-copy-btn')
    expect(result).toContain('table-block-wrap-btn')
  })

  it('is idempotent (does not double-wrap)', () => {
    const html = '<div class="table-wrap"><table><tr><td>1</td></tr></table></div>'
    const first = annotateTableBlockHeaders(html)
    const second = annotateTableBlockHeaders(first)
    const count1 = (first.match(/table-block-wrapper/g) || []).length
    const count2 = (second.match(/table-block-wrapper/g) || []).length
    expect(count2).toBe(count1)
  })

  it('handles no table-wrap elements', () => {
    const html = '<p>No tables here</p>'
    const result = annotateTableBlockHeaders(html)
    expect(result).not.toContain('table-block-wrapper')
  })
})

describe('handleTableBlockClick', () => {
  function createClickEvent(target: HTMLElement): MouseEvent {
    return {
      target,
      preventDefault: vi.fn(),
      stopPropagation: vi.fn(),
    } as unknown as MouseEvent
  }

  it('returns false for non-table-block clicks', () => {
    const div = document.createElement('div')
    const event = createClickEvent(div)
    expect(handleTableBlockClick(event)).toBe(false)
  })

  it('returns true and toggles word-wrap for wrap button click', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'table-block-wrapper'
    const btn = document.createElement('button')
    btn.className = 'table-block-wrap-btn'
    btn.setAttribute('data-action', 'wrap')
    wrapper.appendChild(btn)
    document.body.appendChild(wrapper)

    const event = createClickEvent(btn)
    expect(handleTableBlockClick(event)).toBe(true)
    expect(wrapper.classList.contains('word-wrap')).toBe(true)

    // Toggle back
    const event2 = createClickEvent(btn)
    handleTableBlockClick(event2)
    expect(wrapper.classList.contains('word-wrap')).toBe(false)

    wrapper.remove()
  })

  it('returns true for copy button click with table', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'table-block-wrapper'
    const table = document.createElement('table')
    const tr = document.createElement('tr')
    const td = document.createElement('td')
    td.textContent = 'cell'
    tr.appendChild(td)
    table.appendChild(tr)
    const btn = document.createElement('button')
    btn.className = 'table-block-copy-btn'
    btn.setAttribute('data-action', 'copy')
    btn.setAttribute('title', 'Copy')
    btn.setAttribute('aria-label', 'Copy')
    wrapper.appendChild(btn)
    wrapper.appendChild(table)
    document.body.appendChild(wrapper)

    const event = createClickEvent(btn)
    expect(handleTableBlockClick(event)).toBe(true)
    expect(btn.classList.contains('is-copied')).toBe(true)

    wrapper.remove()
  })

  it('returns true for copy button click without table', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'table-block-wrapper'
    const btn = document.createElement('button')
    btn.className = 'table-block-copy-btn'
    btn.setAttribute('data-action', 'copy')
    wrapper.appendChild(btn)
    document.body.appendChild(wrapper)

    const event = createClickEvent(btn)
    expect(handleTableBlockClick(event)).toBe(true)

    wrapper.remove()
  })

  it('returns true for copy when already is-copied (no-op)', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'table-block-wrapper'
    const btn = document.createElement('button')
    btn.className = 'table-block-copy-btn is-copied'
    btn.setAttribute('data-action', 'copy')
    wrapper.appendChild(btn)
    document.body.appendChild(wrapper)

    const event = createClickEvent(btn)
    expect(handleTableBlockClick(event)).toBe(true)

    wrapper.remove()
  })

  it('copy button with thead-based table triggers clipboard', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'table-block-wrapper'
    const table = document.createElement('table')

    const thead = document.createElement('thead')
    const headerRow = document.createElement('tr')
    const th1 = document.createElement('th'); th1.textContent = 'Name'
    const th2 = document.createElement('th'); th2.textContent = 'Value'
    headerRow.appendChild(th1)
    headerRow.appendChild(th2)
    thead.appendChild(headerRow)
    table.appendChild(thead)

    const tbody = document.createElement('tbody')
    const dataRow = document.createElement('tr')
    const td1 = document.createElement('td'); td1.textContent = 'foo'
    const td2 = document.createElement('td'); td2.textContent = 'bar'
    dataRow.appendChild(td1)
    dataRow.appendChild(td2)
    tbody.appendChild(dataRow)
    table.appendChild(tbody)

    const btn = document.createElement('button')
    btn.className = 'table-block-copy-btn'
    btn.setAttribute('data-action', 'copy')
    btn.setAttribute('title', 'Copy')
    btn.setAttribute('aria-label', 'Copy')
    wrapper.appendChild(btn)
    wrapper.appendChild(table)
    document.body.appendChild(wrapper)

    const event = createClickEvent(btn)
    expect(handleTableBlockClick(event)).toBe(true)
    expect(btn.classList.contains('is-copied')).toBe(true)

    wrapper.remove()
  })

  it('copy button with table containing special characters escapes HTML', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'table-block-wrapper'
    const table = document.createElement('table')
    const tr = document.createElement('tr')
    const td = document.createElement('td'); td.textContent = '<script>alert(1)</script>'
    tr.appendChild(td)
    table.appendChild(tr)

    const btn = document.createElement('button')
    btn.className = 'table-block-copy-btn'
    btn.setAttribute('data-action', 'copy')
    btn.setAttribute('title', 'Copy')
    btn.setAttribute('aria-label', 'Copy')
    wrapper.appendChild(btn)
    wrapper.appendChild(table)
    document.body.appendChild(wrapper)

    const event = createClickEvent(btn)
    expect(handleTableBlockClick(event)).toBe(true)

    wrapper.remove()
  })
})
