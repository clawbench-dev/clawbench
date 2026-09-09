import { describe, expect, it, vi, afterEach } from 'vitest'
import { annotateCodeBlockHeaders, annotateTableBlockHeaders, handleCodeBlockClick, handleTableBlockClick, closeAllTableBlockMenus } from '@/composables/useCodeBlockHeader.ts'

// Mock clipboard
const { copyText } = vi.hoisted(() => ({ copyText: vi.fn() }))
vi.mock('@/utils/clipboard.ts', () => ({
  copyText,
}))

// Mock locale
vi.mock('@/composables/useLocale', () => ({
  gt: (key: string) => key,
}))

// Track setTimeout IDs to clean up after each test
const pendingTimers: ReturnType<typeof setTimeout>[] = []
const _origSetTimeout = setTimeout
globalThis.setTimeout = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetTimeout(fn, ms, ...args)
  pendingTimers.push(id)
  return id
}) as typeof setTimeout

afterEach(() => {
  // Clear any pending timers from copy button feedback
  for (const id of pendingTimers) {
    clearTimeout(id)
  }
  pendingTimers.length = 0
  copyText.mockClear()
})

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
    // Default: word-wrap on
    expect(result).toContain('word-wrap')
  })

  it('adds copy and wrap buttons', () => {
    const html = '<pre><code>code</code></pre>'
    const result = annotateCodeBlockHeaders(html)
    expect(result).toContain('code-block-copy-btn')
    expect(result).toContain('code-block-wrap-btn')
    expect(result).toContain('data-action="copy"')
    expect(result).toContain('data-action="wrap"')
    // Default: wrap on, so button has is-wrapped class
    expect(result).toContain('is-wrapped')
  })

  it('adds an attach-to-chat button to the header actions', () => {
    const html = '<pre><code>code</code></pre>'
    const result = annotateCodeBlockHeaders(html)
    expect(result).toContain('code-block-attach-btn')
    expect(result).toContain('data-action="attach"')
    expect(result).toContain('class="code-block-header-actions"')
  })

  it('skips mermaid blocks', () => {
    const html = '<pre class="mermaid"><code>graph TD</code></pre>'
    const result = annotateCodeBlockHeaders(html)
    expect(result).not.toContain('code-block-wrapper')
  })

  it('does not inject attach buttons in share mode', async () => {
    const { setShareToken } = await import('@/share/shareMode')
    const html = '<pre><code>code</code></pre>'
    setShareToken('tok-share')
    try {
      const result = annotateCodeBlockHeaders(html)
      expect(result).not.toContain('code-block-attach-btn')
      // copy/wrap still injected
      expect(result).toContain('code-block-copy-btn')
    } finally {
      setShareToken(null)
    }
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
    wrapper.className = 'code-block-wrapper word-wrap'
    const pre = document.createElement('pre')
    const btn = document.createElement('button')
    btn.className = 'code-block-wrap-btn is-wrapped'
    btn.setAttribute('data-action', 'wrap')
    wrapper.appendChild(btn)
    wrapper.appendChild(pre)
    document.body.appendChild(wrapper)

    const event = createClickEvent(btn)
    expect(handleCodeBlockClick(event)).toBe(true)
    // Click toggles off: word-wrap removed
    expect(wrapper.classList.contains('word-wrap')).toBe(false)
    expect(btn.classList.contains('is-wrapped')).toBe(false)

    // Toggle back on
    const event2 = createClickEvent(btn)
    handleCodeBlockClick(event2)
    expect(wrapper.classList.contains('word-wrap')).toBe(true)

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

  it('adds an attach-to-chat button to the table header actions', () => {
    const html = '<div class="table-wrap"><table><tr><td>data</td></tr></table></div>'
    const result = annotateTableBlockHeaders(html)
    expect(result).toContain('table-block-attach-btn')
    expect(result).toContain('data-action="attach"')
  })

  it('builds a copy dropdown menu with Markdown/HTML/TSV items', () => {
    const html = '<div class="table-wrap"><table><tr><td>data</td></tr></table></div>'
    const result = annotateTableBlockHeaders(html)
    expect(result).toContain('table-block-copy-dropdown')
    expect(result).toContain('table-block-copy-menu')
    expect(result).toContain('data-action="copy-md"')
    expect(result).toContain('data-action="copy-html"')
    expect(result).toContain('data-action="copy-tsv"')
    // Trigger uses the open-copy-menu action with aria-haspopup
    expect(result).toContain('data-action="open-copy-menu"')
    expect(result).toContain('aria-haspopup="true"')
    // Menu is initially hidden
    expect(result).toContain('style="display: none;"')
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

  function buildBlock(tableHtml?: string) {
    const wrapper = document.createElement('div')
    wrapper.className = 'table-block-wrapper'
    const btn = document.createElement('button')
    btn.className = 'table-block-copy-btn'
    btn.setAttribute('data-action', 'open-copy-menu')
    btn.setAttribute('title', 'Copy')
    btn.setAttribute('aria-label', 'Copy')
    const menu = document.createElement('div')
    menu.className = 'table-block-copy-menu'
    menu.style.display = 'none'

    const itemMd = document.createElement('button')
    itemMd.className = 'table-block-copy-menu-item'
    itemMd.setAttribute('data-action', 'copy-md')
    itemMd.textContent = 'Copy Markdown'
    const itemHtml = document.createElement('button')
    itemHtml.className = 'table-block-copy-menu-item'
    itemHtml.setAttribute('data-action', 'copy-html')
    itemHtml.textContent = 'Copy HTML'
    const itemTsv = document.createElement('button')
    itemTsv.className = 'table-block-copy-menu-item'
    itemTsv.setAttribute('data-action', 'copy-tsv')
    itemTsv.textContent = 'Copy TSV'
    menu.appendChild(itemMd)
    menu.appendChild(itemHtml)
    menu.appendChild(itemTsv)

    wrapper.appendChild(btn)
    wrapper.appendChild(menu)

    if (tableHtml !== undefined) {
      const holder = document.createElement('div')
      holder.innerHTML = tableHtml
      const table = holder.querySelector('table')
      if (table) wrapper.appendChild(table)
    }
    document.body.appendChild(wrapper)
    return { wrapper, btn, menu, itemMd, itemHtml, itemTsv }
  }

  function buildTheadTable() {
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
    return table
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

  it('opens the copy menu on trigger click', () => {
    const { wrapper, btn, menu } = buildBlock()
    const event = createClickEvent(btn)
    expect(handleTableBlockClick(event)).toBe(true)
    expect(menu.classList.contains('is-open')).toBe(true)
    expect(menu.style.display).toBe('block')
    expect(btn.getAttribute('aria-expanded')).toBe('true')

    // Second click closes it
    handleTableBlockClick(createClickEvent(btn))
    expect(menu.classList.contains('is-open')).toBe(false)
    expect(btn.getAttribute('aria-expanded')).toBe('false')

    wrapper.remove()
  })

  it('closes menus in other blocks when one opens', () => {
    const a = buildBlock()
    const b = buildBlock()
    // Open A's menu
    handleTableBlockClick(createClickEvent(a.btn))
    expect(a.menu.classList.contains('is-open')).toBe(true)
    // Open B's menu — A's should close
    handleTableBlockClick(createClickEvent(b.btn))
    expect(b.menu.classList.contains('is-open')).toBe(true)
    expect(a.menu.classList.contains('is-open')).toBe(false)

    a.wrapper.remove()
    b.wrapper.remove()
  })

  it('copies Markdown via copy-md menu item', () => {
    const { wrapper, menu, itemMd } = buildBlock()
    wrapper.appendChild(buildTheadTable())
    handleTableBlockClick(createClickEvent(itemMd))
    expect(copyText).toHaveBeenCalledWith('| Name | Value |\n| --- | --- |\n| foo | bar |')
    expect(itemMd.classList.contains('is-copied')).toBe(true)
    // Menu closes after copying
    expect(menu.classList.contains('is-open')).toBe(false)

    wrapper.remove()
  })

  it('copies TSV via copy-tsv menu item', () => {
    const { wrapper, itemTsv } = buildBlock()
    wrapper.appendChild(buildTheadTable())
    handleTableBlockClick(createClickEvent(itemTsv))
    expect(copyText).toHaveBeenCalledWith('Name\tValue\nfoo\tbar')

    wrapper.remove()
  })

  it('copies HTML via copy-html menu item', () => {
    const { wrapper, itemHtml } = buildBlock()
    wrapper.appendChild(buildTheadTable())
    handleTableBlockClick(createClickEvent(itemHtml))
    expect(copyText).toHaveBeenCalledWith(expect.stringContaining('<table><thead><tr><th>Name</th><th>Value</th>'))

    wrapper.remove()
  })

  it('escapes pipes in Markdown cells', () => {
    const { wrapper, itemMd } = buildBlock()
    const table = document.createElement('table')
    const tr = document.createElement('tr')
    const td = document.createElement('td'); td.textContent = 'a|b'
    tr.appendChild(td)
    table.appendChild(tr)
    wrapper.appendChild(table)
    handleTableBlockClick(createClickEvent(itemMd))
    expect(copyText).toHaveBeenCalledWith(expect.stringContaining('a\\|b'))

    wrapper.remove()
  })

  it('open-copy-menu without table still toggles menu', () => {
    const { wrapper, btn, menu } = buildBlock()
    const event = createClickEvent(btn)
    expect(handleTableBlockClick(event)).toBe(true)
    expect(menu.classList.contains('is-open')).toBe(true)

    wrapper.remove()
  })

  it('returns true for open-copy-menu click without table', () => {
    const { wrapper, btn } = buildBlock()
    const event = createClickEvent(btn)
    expect(handleTableBlockClick(event)).toBe(true)

    wrapper.remove()
  })
})

describe('closeAllTableBlockMenus', () => {
  function buildMenu() {
    const btn = document.createElement('button')
    btn.className = 'table-block-copy-btn'
    btn.setAttribute('data-action', 'open-copy-menu')
    const menu = document.createElement('div')
    menu.className = 'table-block-copy-menu is-open'
    menu.style.display = 'block'
    btn.appendChild(menu) // sibling structure: btn then menu
    return { btn, menu }
  }

  it('closes all open menus', () => {
    const { btn, menu } = buildMenu()
    document.body.appendChild(btn)
    document.body.appendChild(menu)

    closeAllTableBlockMenus()
    expect(menu.classList.contains('is-open')).toBe(false)
    expect(menu.style.display).toBe('none')

    btn.remove()
    menu.remove()
  })

  it('keeps the excepted trigger open', () => {
    const a = buildMenu()
    const b = buildMenu()
    document.body.appendChild(a.btn)
    document.body.appendChild(a.menu)
    document.body.appendChild(b.btn)
    document.body.appendChild(b.menu)

    closeAllTableBlockMenus(a.btn)
    expect(a.menu.classList.contains('is-open')).toBe(true)
    expect(b.menu.classList.contains('is-open')).toBe(false)

    a.btn.remove(); a.menu.remove()
    b.btn.remove(); b.menu.remove()
  })
})
