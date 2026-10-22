import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { defineComponent, h, isRef } from 'vue'
import { mount } from '@vue/test-utils'
import {
  useChatMessageInteractions,
  type ChatMessageInteractionsOptions,
  type ChatMessageInteractions,
} from '@/composables/useChatMessageInteractions'

// Clipboard is asserted, not exercised.
const { copyText } = vi.hoisted(() => ({ copyText: vi.fn() }))
vi.mock('@/utils/clipboard', () => ({ copyText }))
vi.mock('@/utils/clipboard.ts', () => ({ copyText }))

// i18n lookups in the injected buttons — the keys themselves are irrelevant here.
vi.mock('@/composables/useLocale', () => ({ gt: (key: string) => key }))

// Platform axes: the touch/mouse split is what decides whether the table-row
// viewer and the double-tap copy fire at all, so the tests flip these directly.
const platform = vi.hoisted(() => ({
  isTouchPrimary: { value: true },
  isAndroidApp: { value: false },
}))
vi.mock('@/composables/usePlatformDetect', () => ({
  usePlatformDetect: () => ({
    isTouchPrimary: platform.isTouchPrimary,
    isAndroidApp: platform.isAndroidApp,
  }),
}))

/**
 * Mount a throwaway component so `useTextSelectionActive` (which registers
 * document listeners in onMounted) runs inside a real setup scope instead of
 * warning about a missing instance.
 *
 * Every wrapper is tracked and unmounted in afterEach: the composable installs a
 * DOCUMENT-level click listener, so a leftover mount would keep dismissing menus
 * in later tests (that is exactly what made the "stops listening after unmount"
 * case fail before the tracking was added).
 */
const mounted: Array<ReturnType<typeof mount>> = []

function setupComposable(options?: ChatMessageInteractionsOptions): {
  api: ChatMessageInteractions
  wrapper: ReturnType<typeof mount>
} {
  let api: ChatMessageInteractions | null = null
  const Host = defineComponent({
    setup() {
      api = useChatMessageInteractions(options)
      return () => h('div')
    },
  })
  const wrapper = mount(Host)
  mounted.push(wrapper)
  if (!api) throw new Error('composable did not run')
  return { api, wrapper }
}

/**
 * Dispatch a real click so `event.target` is set the way it is in production.
 * Hand-assigning `target` would let a handler that reads `currentTarget` pass.
 */
function clickOn(el: Element): MouseEvent {
  const event = new MouseEvent('click', { bubbles: true, cancelable: true })
  el.dispatchEvent(event)
  return event
}

function mountCodeBlock(): { wrapper: HTMLElement; copyBtn: HTMLElement; wrapBtn: HTMLElement } {
  const host = document.createElement('div')
  host.innerHTML = `
    <div class="code-block-wrapper word-wrap">
      <div class="code-block-header">
        <span class="code-block-header-actions">
          <button class="code-block-copy-btn" data-action="copy" type="button">copy</button>
          <button class="code-block-wrap-btn is-wrapped" data-action="wrap" type="button">wrap</button>
        </span>
      </div>
      <pre><code>fmt.Println("hi")</code></pre>
    </div>`
  document.body.appendChild(host)
  return {
    wrapper: host.querySelector('.code-block-wrapper') as HTMLElement,
    copyBtn: host.querySelector('.code-block-copy-btn') as HTMLElement,
    wrapBtn: host.querySelector('.code-block-wrap-btn') as HTMLElement,
  }
}

function mountTable(): { copyItem: HTMLElement; table: HTMLTableElement } {
  const host = document.createElement('div')
  host.innerHTML = `
    <div class="table-block-wrapper word-wrap">
      <div class="table-block-header">
        <span class="table-block-header-actions">
          <span class="table-block-copy-dropdown">
            <button class="table-block-copy-btn" data-action="open-copy-menu" type="button">copy</button>
            <div class="table-block-copy-menu is-open" role="menu">
              <button class="table-block-copy-menu-item" data-action="copy-md" type="button">md</button>
            </div>
          </span>
        </span>
      </div>
      <div class="table-wrap">
        <table data-table-idx="0">
          <thead><tr><th>Name</th><th>Age</th></tr></thead>
          <tbody>
            <tr data-row-idx="0"><td>Alice</td><td>30</td></tr>
            <tr data-row-idx="1"><td>Bob</td><td>25</td></tr>
          </tbody>
        </table>
      </div>
    </div>`
  document.body.appendChild(host)
  return {
    copyItem: host.querySelector('.table-block-copy-menu-item') as HTMLElement,
    table: host.querySelector('table') as HTMLTableElement,
  }
}

function appendAnchor(attrs: Record<string, string>): HTMLAnchorElement {
  const anchor = document.createElement('a')
  for (const [k, v] of Object.entries(attrs)) anchor.setAttribute(k, v)
  anchor.textContent = 'link'
  document.body.appendChild(anchor)
  return anchor
}

beforeEach(() => {
  vi.useFakeTimers()
  copyText.mockClear()
  platform.isTouchPrimary.value = true
  platform.isAndroidApp.value = false
})

afterEach(() => {
  vi.useRealTimers()
  // Unmount BEFORE clearing the DOM so onBeforeUnmount can detach its listeners.
  while (mounted.length > 0) mounted.pop()?.unmount()
  document.body.innerHTML = ''
})

describe('useChatMessageInteractions — code block header', () => {
  it('consumes a copy-button click and copies the code text', () => {
    const { api } = setupComposable()
    const { copyBtn } = mountCodeBlock()

    const event = clickOn(copyBtn)

    expect(api.handleContainerClick(event)).toBe(true)
    expect(copyText).toHaveBeenCalledWith('fmt.Println("hi")')
  })

  it('consumes a wrap-button click and toggles the wrapper class', () => {
    const { api } = setupComposable()
    const { wrapper, wrapBtn } = mountCodeBlock()
    expect(wrapper.classList.contains('word-wrap')).toBe(true)

    const event = clickOn(wrapBtn)

    expect(api.handleContainerClick(event)).toBe(true)
    expect(wrapper.classList.contains('word-wrap')).toBe(false)
    expect(wrapBtn.classList.contains('is-wrapped')).toBe(false)
  })
})

describe('useChatMessageInteractions — table block header', () => {
  it('consumes a copy-menu item click and copies the table as Markdown', () => {
    const { api } = setupComposable()
    const { copyItem } = mountTable()

    const event = clickOn(copyItem)

    expect(api.handleContainerClick(event)).toBe(true)
    expect(copyText).toHaveBeenCalledTimes(1)
    expect(copyText.mock.calls[0][0]).toContain('| Name | Age |')
    expect(copyText.mock.calls[0][0]).toContain('| Alice | 30 |')
  })
})

describe('useChatMessageInteractions — table row viewer', () => {
  it('opens the row modal on a touch row click', () => {
    const { api } = setupComposable()
    const { table } = mountTable()
    const cell = table.querySelector('tbody tr[data-row-idx="1"] td') as HTMLElement

    const event = clickOn(cell)

    expect(api.handleContainerClick(event)).toBe(true)
    expect(api.tableRowModal.value).not.toBeNull()
    expect(api.tableRowModal.value?.headers).toEqual(['Name', 'Age'])
    expect(api.tableRowModal.value?.rows).toEqual([['Alice', '30'], ['Bob', '25']])
    expect(api.tableRowModal.value?.currentIndex).toBe(1)
  })

  it('does not open the row modal on a desktop (mouse) click', () => {
    platform.isTouchPrimary.value = false
    const { api } = setupComposable()
    const { table } = mountTable()
    const cell = table.querySelector('tbody tr[data-row-idx="1"] td') as HTMLElement

    const event = clickOn(cell)

    expect(api.handleContainerClick(event)).toBe(false)
    expect(api.tableRowModal.value).toBeNull()
  })

  it('exposes navigation and close over the opened modal', () => {
    const { api } = setupComposable()
    const { table } = mountTable()
    api.handleContainerClick(clickOn(table.querySelector('tbody tr[data-row-idx="0"] td') as HTMLElement))
    expect(api.tableRowModal.value?.currentIndex).toBe(0)

    api.tableRowNext()
    expect(api.tableRowModal.value?.currentIndex).toBe(1)
    api.tableRowPrev()
    expect(api.tableRowModal.value?.currentIndex).toBe(0)
    api.closeTableRowModal()
    expect(api.tableRowModal.value).toBeNull()
  })
})

describe('useChatMessageInteractions — double click', () => {
  it('copies the paragraph text on a touch double tap', () => {
    const { api } = setupComposable()
    const paragraph = document.createElement('p')
    paragraph.textContent = 'hello world'
    document.body.appendChild(paragraph)

    // The first click only records the target; the second (same target, inside
    // the threshold) is the one that copies. Feeding only one click here would
    // pass even if the double-click detection never fired.
    api.handleContainerClick(clickOn(paragraph))
    expect(copyText).not.toHaveBeenCalled()
    api.handleContainerClick(clickOn(paragraph))

    expect(copyText).toHaveBeenCalledWith('hello world', expect.any(Function))
  })

  it('does not copy on a desktop double click', () => {
    platform.isTouchPrimary.value = false
    const { api } = setupComposable()
    const paragraph = document.createElement('p')
    paragraph.textContent = 'hello world'
    document.body.appendChild(paragraph)

    api.handleContainerClick(clickOn(paragraph))
    api.handleContainerClick(clickOn(paragraph))

    expect(copyText).not.toHaveBeenCalled()
  })
})

describe('useChatMessageInteractions — relative links', () => {
  it('preventDefaults a relative link when no onOpenFile is provided (share page)', () => {
    const { api } = setupComposable()
    const anchor = appendAnchor({ href: 'docs/guide.md' })

    const event = clickOn(anchor)
    api.handleContainerClick(event)

    // A share page has no file viewer; letting the browser follow the href would
    // navigate the reader away from the conversation.
    expect(event.defaultPrevented).toBe(true)
  })

  it('leaves external links alone when no onOpenFile is provided', () => {
    const { api } = setupComposable()
    const anchor = appendAnchor({ href: 'https://example.com/x' })

    const event = clickOn(anchor)
    api.handleContainerClick(event)

    expect(event.defaultPrevented).toBe(false)
  })

  it('opens the annotated path with line ranges when onOpenFile is provided', () => {
    const onOpenFile = vi.fn()
    const { api } = setupComposable({ onOpenFile })
    const anchor = appendAnchor({
      href: '../../src/main.rs#L42-L50',
      'data-file-path': 'src/main.rs',
      'data-line-start': '42',
      'data-line-end': '50',
      'data-line-ranges': '42-50,60-62',
    })

    const event = clickOn(anchor)
    api.handleContainerClick(event)

    expect(event.defaultPrevented).toBe(true)
    expect(onOpenFile).toHaveBeenCalledWith('src/main.rs', 42, 50, '42-50,60-62')
  })

  it('runs beforeOpenFile before opening the file', () => {
    const order: string[] = []
    const { api } = setupComposable({
      onOpenFile: () => { order.push('open') },
      beforeOpenFile: () => { order.push('before') },
    })
    const anchor = appendAnchor({ href: 'src/a.ts', 'data-file-path': 'src/a.ts' })

    const event = clickOn(anchor)
    api.handleContainerClick(event)

    expect(order).toEqual(['before', 'open'])
  })
})

describe('useChatMessageInteractions — table copy menu dismissal', () => {
  it('closes an open copy menu when clicking elsewhere in the document', () => {
    const { wrapper } = setupComposable()
    const { copyItem } = mountTable()
    const menu = copyItem.parentElement as HTMLElement
    expect(menu.classList.contains('is-open')).toBe(true)

    // A click on unrelated page content. The menu is a fixed-position overlay, so
    // without a document-level listener it would stay open forever on any host
    // that does not implement its own dismissal (the share page).
    const elsewhere = document.createElement('div')
    document.body.appendChild(elsewhere)
    elsewhere.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))

    expect(menu.classList.contains('is-open')).toBe(false)
    wrapper.unmount()
  })

  it('keeps the menu open when the click is on the menu itself', () => {
    const { wrapper } = setupComposable()
    const { copyItem } = mountTable()
    const menu = copyItem.parentElement as HTMLElement

    copyItem.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))

    expect(menu.classList.contains('is-open')).toBe(true)
    wrapper.unmount()
  })

  it('stops listening after unmount', () => {
    const { wrapper } = setupComposable()
    wrapper.unmount()

    const { copyItem } = mountTable()
    const menu = copyItem.parentElement as HTMLElement
    const elsewhere = document.createElement('div')
    document.body.appendChild(elsewhere)
    elsewhere.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))

    // No listener left behind → the menu is untouched by this composable.
    expect(menu.classList.contains('is-open')).toBe(true)
  })
})

describe('useChatMessageInteractions — surface', () => {
  it('exposes textSelecting as a ref', () => {
    const { api, wrapper } = setupComposable()
    expect(isRef(api.textSelecting)).toBe(true)
    wrapper.unmount()
  })
})
