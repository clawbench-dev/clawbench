import { describe, expect, it, vi, afterEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { annotateCodeBlockHeaders, annotateTableBlockHeaders, handleCodeBlockClick, handleTableBlockClick, closeAllTableBlockMenus, positionTableMenu } from '@/composables/useCodeBlockHeader.ts'
import { closeTableLandscape, tableLandscapeState } from '@/composables/useTableLandscape.ts'
// Source of the menu's positioning rule — jsdom cannot reproduce overflow
// clipping, so the escape-from-clip-chain assertion has to read the CSS.
import tableHeaderCss from '../../../css/code-block-header.css?raw'

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
  closeTableLandscape()
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

  it('adds a landscape-view button to the table header actions', () => {
    const html = '<div class="table-wrap"><table><tr><td>data</td></tr></table></div>'
    const result = annotateTableBlockHeaders(html)
    expect(result).toContain('table-block-landscape-btn')
    expect(result).toContain('data-action="landscape"')
    // Never on code blocks — a code block is already horizontally scrollable.
    expect(annotateCodeBlockHeaders('<pre><code>x</code></pre>')).not.toContain('landscape')
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

describe('table copy menu positioning', () => {
  // jsdom has no layout engine: every rect is 0x0 and offsetWidth/Height are 0.
  // These tests therefore drive `positionTableMenu` with a simulated containing
  // block (origin + scale) so the arithmetic is exercised for real.
  //
  // The regression being pinned: the menu used to be `position: absolute`
  // inside `.table-block-wrapper`, which is `overflow: hidden` — on a 1-2 row
  // table the menu is TALLER than the table, so its third item (Copy TSV) was
  // clipped away and unclickable.
  //
  // A second regression, still live: in Android app mode `.chat-message` carries
  // `will-change: transform` (the GPU-ghost fix), which makes it the CONTAINING
  // BLOCK for `position: fixed` descendants. So `top: 0` is not the viewport top
  // and CSS px are not rect px — a naive implementation places the menu tens of
  // px off, and the error grows with the ancestor's own offset. `stubGeometry`
  // models exactly that: a scaled, offset containing block.
  //
  // (This used to also model the UI scale, which was applied as CSS `zoom` on
  // <html> and produced the same two symptoms. The scale now resizes the root
  // font instead and leaves the coordinate space intact, so the transform case
  // is the only one left that needs the compensation — see
  // `fontDrivenScaling.test.ts`.)
  function stubGeometry(opts: {
    trigger: { top: number; bottom: number; left: number; right: number }
    menuWidth: number
    menuHeight: number
    originLeft?: number
    originTop?: number
    scale?: number
  }) {
    const originLeft = opts.originLeft ?? 0
    const originTop = opts.originTop ?? 0
    const scale = opts.scale ?? 1

    const btn = document.createElement('button')
    btn.className = 'table-block-copy-btn'
    btn.getBoundingClientRect = () => ({
      top: opts.trigger.top, bottom: opts.trigger.bottom,
      left: opts.trigger.left, right: opts.trigger.right,
      width: opts.trigger.right - opts.trigger.left,
      height: opts.trigger.bottom - opts.trigger.top,
      x: opts.trigger.left, y: opts.trigger.top, toJSON: () => ({}),
    }) as DOMRect

    const menu = document.createElement('div')
    menu.className = 'table-block-copy-menu'
    Object.defineProperty(menu, 'offsetWidth', { configurable: true, get: () => opts.menuWidth })
    Object.defineProperty(menu, 'offsetHeight', { configurable: true, get: () => opts.menuHeight })

    // The containing block model: a fixed child's rect = origin + CSS offset ×
    // scale. Applied to EVERY element inside the model, not just `menu`.
    //
    // That detail is load-bearing. `placeFixedAt` does not read `menu`'s rect to
    // find the containing block — it appends a CLONE as a probe and measures
    // where known offsets land (see utils/fixedPosition.ts). A stub installed on
    // the `menu` instance alone is invisible to that clone, so the probe
    // measured jsdom's all-zero rects, `measureFixedContainingBlock` returned a
    // degenerate block, and `placeFixedAt` took its "not laid out, write the
    // coordinates straight through" path — which is why this case used to fail
    // even though the production code was correct.
    const rectFor = (el: HTMLElement): DOMRect => {
      const left = originLeft + (parseFloat(el.style.left) || 0) * scale
      const top = originTop + (parseFloat(el.style.top) || 0) * scale
      return {
        top, bottom: top + opts.menuHeight * scale,
        left, right: left + opts.menuWidth * scale,
        width: opts.menuWidth * scale, height: opts.menuHeight * scale,
        x: left, y: top, toJSON: () => ({}),
      } as DOMRect
    }
    // `cloneNode` copies the element, not its own-property stubs, so the model
    // has to live on the prototype to be visible to the probe. Restored in
    // afterEach — a leaked prototype patch would poison every later test in
    // this file.
    HTMLElement.prototype.getBoundingClientRect = function (this: HTMLElement) {
      return rectFor(this)
    }
    return { btn, menu }
  }

  // `stubGeometry` patches `HTMLElement.prototype` so the measuring PROBE (a
  // clone) sees the same containing block as the menu. Captured once, restored
  // after every case.
  const realGetBoundingClientRect = HTMLElement.prototype.getBoundingClientRect

  afterEach(() => {
    document.body.innerHTML = ''
    HTMLElement.prototype.getBoundingClientRect = realGetBoundingClientRect
  })

  it('places the menu directly below the trigger, right-aligned', () => {
    const { btn, menu } = stubGeometry({
      trigger: { top: 100, bottom: 124, left: 300, right: 340 },
      menuWidth: 140,
      menuHeight: 92,
    })
    positionTableMenu(btn, menu)
    expect(menu.style.top).toBe('128px')   // bottom + 4 gap
    expect(menu.style.left).toBe('200px')  // right-aligned: 340 - 140
  })

  it('keeps the menu fully inside the viewport at the right edge', () => {
    const { btn, menu } = stubGeometry({
      trigger: { top: 100, bottom: 124, left: window.innerWidth - 40, right: window.innerWidth - 4 },
      menuWidth: 140,
      menuHeight: 92,
    })
    positionTableMenu(btn, menu)
    const left = parseFloat(menu.style.left)
    expect(left + 140).toBeLessThanOrEqual(window.innerWidth - 8)
    expect(left).toBeGreaterThanOrEqual(8)
  })

  it('flips above the trigger when there is no room below', () => {
    const { btn, menu } = stubGeometry({
      trigger: { top: window.innerHeight - 30, bottom: window.innerHeight - 6, left: 100, right: 140 },
      menuWidth: 140,
      menuHeight: 92,
    })
    positionTableMenu(btn, menu)
    const top = parseFloat(menu.style.top)
    expect(top).toBe(window.innerHeight - 30 - 4 - 92)
    expect(top + 92).toBeLessThanOrEqual(window.innerHeight - 8)
  })

  it('prefers staying below when flipping would land off the top edge', () => {
    // A viewport shorter than trigger + menu: flipping puts the menu above the
    // viewport entirely, so staying below (clamped) keeps it readable.
    const originalHeight = window.innerHeight
    Object.defineProperty(window, 'innerHeight', { configurable: true, value: 200 })
    try {
      const { btn, menu } = stubGeometry({
        trigger: { top: 10, bottom: 190, left: 100, right: 140 },
        menuWidth: 140,
        menuHeight: 92,
      })
      positionTableMenu(btn, menu)
      const top = parseFloat(menu.style.top)
      // Clamped into the viewport, not thrown to the very top by the flip.
      expect(top).toBe(200 - 92 - 8)
    } finally {
      Object.defineProperty(window, 'innerHeight', { configurable: true, value: originalHeight })
    }
  })

  it('compensates for a transformed containing block', () => {
    // Android app mode: `.chat-message` carries `will-change: transform`, which
    // makes it the containing block for `position: fixed` descendants — so
    // `top: 0` is NOT the viewport top and CSS px are not rect px. The menu must
    // still land just below the trigger, right-aligned to it, in VIEWPORT space.
    //
    // Without the compensation the written offsets would be interpreted against
    // the ancestor instead of the viewport, and the menu would appear ~100px up
    // and to the left (the ancestor's own origin) with its size stretched by the
    // ancestor's scale.
    const { btn, menu } = stubGeometry({
      trigger: { top: 300, bottom: 324, left: 500, right: 540 },
      menuWidth: 140,
      menuHeight: 92,
      // A transformed ancestor at (60, 40) scaled 1.5×: the classic
      // `will-change: transform` case on a zoomed/scaled chat bubble.
      originLeft: 60,
      originTop: 40,
      scale: 1.5,
    })
    positionTableMenu(btn, menu)

    // The invariant is stated in viewport space, which is what the user sees:
    //   top  = trigger.bottom + gap(4)      = 328
    //   left = trigger.right - menuWidth    = 400
    // Note `offsetWidth/Height` stay in the element's OWN px (140×92) while the
    // rect is scaled, exactly as a real transformed ancestor behaves.
    const rect = menu.getBoundingClientRect()
    expect(Math.round(rect.top)).toBe(328)
    expect(Math.round(rect.left)).toBe(400)

    // The written CSS offsets are the INVERSE of the containing block, so they
    // are smaller than the viewport coordinates by the ancestor's origin and
    // scale. Pinning them (rather than only the rect) is what makes the test
    // fail if someone "simplifies" `placeFixedAt` to write viewport px straight
    // through — the rect assertions alone would still pass in a browser with no
    // transformed ancestor.
    expect(parseFloat(menu.style.left)).toBeCloseTo((400 - 60) / 1.5, 5)
    expect(parseFloat(menu.style.top)).toBeCloseTo((328 - 40) / 1.5, 5)
  })

  it('opens positioned, and repositions on scroll while open', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'table-block-wrapper'
    const { btn, menu } = stubGeometry({
      trigger: { top: 100, bottom: 124, left: 300, right: 340 },
      menuWidth: 140,
      menuHeight: 92,
    })
    btn.setAttribute('data-action', 'open-copy-menu')
    wrapper.appendChild(btn)
    wrapper.appendChild(menu)
    document.body.appendChild(wrapper)

    expect(handleTableBlockClick({ target: btn, preventDefault: vi.fn(), stopPropagation: vi.fn() } as unknown as MouseEvent)).toBe(true)
    expect(menu.classList.contains('is-open')).toBe(true)
    expect(menu.style.top).toBe('128px')

    // The trigger scrolls up; the menu must follow instead of floating away.
    const moved = stubGeometry({
      trigger: { top: 40, bottom: 64, left: 300, right: 340 },
      menuWidth: 140,
      menuHeight: 92,
    })
    btn.getBoundingClientRect = moved.btn.getBoundingClientRect
    window.dispatchEvent(new Event('scroll'))
    expect(menu.style.top).toBe('68px')

    wrapper.remove()
  })

  it('closes the menu once its trigger has scrolled out of view', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'table-block-wrapper'
    const { btn, menu } = stubGeometry({
      trigger: { top: 100, bottom: 124, left: 300, right: 340 },
      menuWidth: 140,
      menuHeight: 92,
    })
    btn.setAttribute('data-action', 'open-copy-menu')
    wrapper.appendChild(btn)
    wrapper.appendChild(menu)
    document.body.appendChild(wrapper)

    handleTableBlockClick({ target: btn, preventDefault: vi.fn(), stopPropagation: vi.fn() } as unknown as MouseEvent)
    expect(menu.classList.contains('is-open')).toBe(true)

    btn.getBoundingClientRect = stubGeometry({
      trigger: { top: -200, bottom: -176, left: 300, right: 340 },
      menuWidth: 140,
      menuHeight: 92,
    }).btn.getBoundingClientRect
    window.dispatchEvent(new Event('scroll'))
    expect(menu.classList.contains('is-open')).toBe(false)

    wrapper.remove()
  })

  it('menu CSS is fixed-positioned so the block wrapper cannot clip it', () => {
    // Source guard: jsdom cannot reproduce overflow clipping, so the one thing
    // that must hold is that the menu escapes the clip chain at all.
    const rule = tableHeaderCss.match(/\.table-block-copy-menu\s*\{([^}]*)\}/)
    expect(rule).not.toBeNull()
    expect(rule![1]).toMatch(/position:\s*fixed/)
    expect(rule![1]).not.toMatch(/position:\s*absolute/)
  })

  /**
   * `position: fixed` is not enough once the row is paint-contained.
   *
   * `.chat-message` carries `contain: paint` in app mode (Android WebView GPU
   * ghost fix), which clips the row's descendants to the row's box — so a menu
   * inside the bubble is clipped there, however it is positioned. On a 1–2 row
   * table the menu is taller than the table, so its last items fell outside the
   * row and became unclickable (ISS-294).
   *
   * The escape hatch is a CSS `:has()` rule in ChatMessageItem.vue keyed on
   * `.table-block-copy-menu.is-open`. That makes the class name a CONTRACT
   * ACROSS FILES: `setTableMenuOpen` here is the only writer, and if it ever
   * stops adding `is-open` (or the class is renamed on one side only), the
   * escape silently stops matching and the defect returns with every test
   * still green. Pin both halves.
   */
  it('keeps the class the chat row’s containment escape hatch watches', () => {
    const itemSource = readFileSync(
      resolve(__dirname, '../../components/chat/ChatMessageItem.vue'),
      'utf8',
    )
    const escape = itemSource.match(
      /:root\[data-webview-mode\]\s+\.chat-message:has\(\.table-block-copy-menu\.is-open\)\s*\{([^}]*)\}/,
    )
    expect(
      escape,
      'ChatMessageItem.vue must relax `contain: paint` while a copy menu is open',
    ).not.toBeNull()
    // The rule must actually drop the paint clip (it is the only thing that
    // clips the menu — `will-change: transform` alone never did).
    const decls = escape![1].replace(/\/\*[\s\S]*?\*\//g, '')
    expect(decls).not.toMatch(/contain:[^;]*paint/)

    // And the runtime must actually produce that state.
    const wrapper = document.createElement('div')
    wrapper.className = 'table-block-wrapper'
    const btn = document.createElement('button')
    btn.className = 'table-block-copy-btn'
    btn.setAttribute('data-action', 'open-copy-menu')
    const menu = document.createElement('div')
    menu.className = 'table-block-copy-menu'
    menu.style.display = 'none'
    wrapper.appendChild(btn)
    wrapper.appendChild(menu)
    document.body.appendChild(wrapper)

    handleTableBlockClick({ target: btn, preventDefault: vi.fn(), stopPropagation: vi.fn() } as unknown as MouseEvent)
    expect(menu.classList.contains('is-open'), 'opening must add the class the :has() rule matches').toBe(true)

    handleTableBlockClick({ target: btn, preventDefault: vi.fn(), stopPropagation: vi.fn() } as unknown as MouseEvent)
    expect(menu.classList.contains('is-open'), 'closing must remove it, or the clip stays lifted').toBe(false)

    wrapper.remove()
  })

  it('the menu stays inside the row, so the escape hatch is load-bearing', () => {
    // Documents WHY the CSS escape exists rather than moving the menu out:
    // the menu must remain a descendant of `.chat-message` (it is injected
    // into the v-html content). Relocating it to <body> would ALSO fix the
    // clipping, but it breaks click delegation — ChatMessageList handles
    // `data-action` on the menu items through `.chat-messages`, so a menu
    // outside that subtree silently stops responding.
    const html = '<div class="table-wrap"><table><tr><td>1</td></tr></table></div>'
    const doc = new DOMParser().parseFromString(annotateTableBlockHeaders(html), 'text/html')
    const menu = doc.querySelector('.table-block-copy-menu')
    expect(menu).not.toBeNull()
    const dropdown = doc.querySelector('.table-block-copy-dropdown')
    expect(dropdown, 'the menu must sit with its trigger so delegation still reaches it').not.toBeNull()
    expect(dropdown!.contains(menu!)).toBe(true)
  })

  /**
   * EVERY close path must remove `is-open`.
   *
   * The chat row's containment escape is a CSS `:has(.table-block-copy-menu.is-open)`
   * rule, so the clip is lifted exactly as long as the class is present. That
   * makes a missed `classList.remove` far worse than a cosmetic bug: the row
   * keeps `contain: paint` lifted forever, and the Android WebView ghost band
   * (the reason the clip exists) comes back on that message with nothing to
   * indicate why.
   *
   * There are four distinct close paths, and they do NOT share one helper —
   * each calls `setTableMenuOpen` with its own trigger lookup:
   *   1. toggling the trigger again (`open-copy-menu`)
   *   2. `closeAllTableBlockMenus()` with no exception (outside click / App.vue)
   *   3. `closeAllTableBlockMenus(exceptBtn)` closing the OTHER blocks' menus
   *   4. picking a copy format (the menu closes itself after copying)
   *
   * A regression that clears `display` but forgets the class would pass a
   * "menu is hidden" assertion while leaving the clip lifted, so these cases
   * assert the CLASS specifically, and each drives its own path.
   */
  describe('every close path clears is-open (the containment escape key)', () => {
    const click = (target: HTMLElement) =>
      ({ target, preventDefault: vi.fn(), stopPropagation: vi.fn() }) as unknown as MouseEvent

    function buildMenuBlock() {
      const wrapper = document.createElement('div')
      wrapper.className = 'table-block-wrapper'
      const btn = document.createElement('button')
      btn.className = 'table-block-copy-btn'
      btn.setAttribute('data-action', 'open-copy-menu')
      const menu = document.createElement('div')
      menu.className = 'table-block-copy-menu'
      menu.style.display = 'none'
      const item = document.createElement('button')
      item.className = 'table-block-copy-menu-item'
      item.setAttribute('data-action', 'copy-md')
      menu.appendChild(item)
      wrapper.appendChild(btn)
      wrapper.appendChild(menu)
      // The copy path needs a table inside the same wrapper.
      const table = document.createElement('table')
      table.innerHTML = '<tr><th>H</th></tr><tr><td>v</td></tr>'
      wrapper.appendChild(table)
      document.body.appendChild(wrapper)
      return { wrapper, btn, menu, item }
    }

    it('path 1: re-clicking the trigger removes it', () => {
      const { wrapper, btn, menu } = buildMenuBlock()
      handleTableBlockClick(click(btn))
      expect(menu.classList.contains('is-open')).toBe(true)
      handleTableBlockClick(click(btn))
      expect(menu.classList.contains('is-open'), 'toggle-close must clear the class').toBe(false)
      expect(menu.style.display, 'and hide the box').toBe('none')
      wrapper.remove()
    })

    it('path 2: closeAllTableBlockMenus() with no exception removes it', () => {
      const { wrapper, btn, menu } = buildMenuBlock()
      handleTableBlockClick(click(btn))
      expect(menu.classList.contains('is-open')).toBe(true)
      closeAllTableBlockMenus()
      expect(menu.classList.contains('is-open'), 'outside-click close must clear the class').toBe(false)
      expect(menu.style.display).toBe('none')
      wrapper.remove()
    })

    it('path 3: opening another block closes this one, class included', () => {
      const a = buildMenuBlock()
      const b = buildMenuBlock()
      handleTableBlockClick(click(a.btn))
      expect(a.menu.classList.contains('is-open')).toBe(true)
      handleTableBlockClick(click(b.btn))
      expect(b.menu.classList.contains('is-open')).toBe(true)
      expect(a.menu.classList.contains('is-open'), 'the sibling that was closed must drop the class').toBe(false)
      expect(a.menu.style.display).toBe('none')
      a.wrapper.remove()
      b.wrapper.remove()
    })

    it('path 4: picking a copy format removes it', () => {
      const { wrapper, menu, item } = buildMenuBlock()
      handleTableBlockClick(click(menu.previousElementSibling as HTMLElement))
      expect(menu.classList.contains('is-open')).toBe(true)
      handleTableBlockClick(click(item))
      expect(menu.classList.contains('is-open'), 'copy-close must clear the class').toBe(false)
      expect(menu.style.display).toBe('none')
      wrapper.remove()
    })

    it('path 5: the wrap toggle dismisses an open menu, class included', () => {
      const { wrapper, menu } = buildMenuBlock()
      const wrapBtn = document.createElement('button')
      wrapBtn.className = 'table-block-wrap-btn'
      wrapBtn.setAttribute('data-action', 'wrap')
      wrapper.appendChild(wrapBtn)
      handleTableBlockClick(click(menu.previousElementSibling as HTMLElement))
      expect(menu.classList.contains('is-open')).toBe(true)
      handleTableBlockClick(click(wrapBtn))
      expect(menu.classList.contains('is-open'), 'wrap toggle must clear the class too').toBe(false)
      wrapper.remove()
    })
  })
})

describe('handleTableBlockClick — landscape view', () => {
  function click(target: HTMLElement): MouseEvent {
    return { target, preventDefault: vi.fn(), stopPropagation: vi.fn() } as unknown as MouseEvent
  }

  function buildBlock(wrapClass = 'word-wrap') {
    const wrapper = document.createElement('div')
    wrapper.className = `table-block-wrapper ${wrapClass}`
    wrapper.innerHTML = `
      <div class="table-block-header">
        <span class="table-block-header-actions">
          <button class="table-block-wrap-btn is-wrapped" data-action="wrap"></button>
          <button class="table-block-landscape-btn" data-action="landscape"></button>
        </span>
      </div>
      <div class="table-wrap"><table data-table-idx="0"><tbody><tr data-row-idx="1"><td>cell</td></tr></tbody></table></div>`
    document.body.appendChild(wrapper)
    const btn = wrapper.querySelector<HTMLElement>('.table-block-landscape-btn')!
    return { wrapper, btn }
  }

  it('opens the overlay with the whole block, header included', () => {
    const { wrapper, btn } = buildBlock('no-wrap')
    expect(handleTableBlockClick(click(btn))).toBe(true)

    const state = tableLandscapeState.value
    expect(state).not.toBeNull()
    expect(state!.html).toContain('<table')
    expect(state!.html).toContain('cell')
    // Header controls travel with the clone — they are the overlay's controls.
    expect(state!.html).toContain('table-block-header')
    expect(state!.html).toContain('table-block-wrap-btn')
    // The per-table wrap mode is part of the cloned wrapper class list.
    expect(state!.html).toContain('table-block-wrapper no-wrap')
    expect(state!.source).toBe(wrapper)
    expect(state!.trigger).toBe(btn)
    wrapper.remove()
  })

  it('keeps the row-viewer hooks so rows stay tappable in the overlay', () => {
    const { wrapper, btn } = buildBlock()
    handleTableBlockClick(click(btn))
    expect(tableLandscapeState.value!.html).toContain('data-row-idx')
    expect(tableLandscapeState.value!.html).toContain('data-table-idx')
    wrapper.remove()
  })

  it('does nothing when the block has no table', () => {
    const wrapper = document.createElement('div')
    wrapper.className = 'table-block-wrapper'
    const btn = document.createElement('button')
    btn.className = 'table-block-landscape-btn'
    btn.setAttribute('data-action', 'landscape')
    wrapper.appendChild(btn)
    document.body.appendChild(wrapper)

    handleTableBlockClick(click(btn))
    expect(tableLandscapeState.value).toBeNull()
    wrapper.remove()
  })
})
