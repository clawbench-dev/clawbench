import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { nextTick } from 'vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (k: string) => k, locale: { value: 'en' } }),
  createI18n: (opts: any) => ({
    global: {
      t: (k: string) => k,
      locale: { value: opts?.locale ?? 'en' },
    },
    install() {},
  }),
}))

vi.mock('@/composables/useLocale', () => ({
  gt: (k: string) => k,
}))

const { mockCopyText } = vi.hoisted(() => ({
  mockCopyText: vi.fn((text: string, cb?: () => void) => { if (cb) cb() }),
}))
vi.mock('@/utils/clipboard.ts', () => ({
  copyText: (text: string, cb?: () => void) => mockCopyText(text, cb),
}))

// Plain `{ value }` object (not a real ref): the component only reads
// `.value`, and hoisting keeps it out of the mock factory's TDZ.
const { touchPrimary } = vi.hoisted(() => ({ touchPrimary: { value: false } }))
vi.mock('@/composables/usePlatformDetect.ts', () => ({
  usePlatformDetect: () => ({ isTouchPrimary: touchPrimary }),
}))

import BlockLandscape from '@/components/common/BlockLandscape.vue'
import { closeBlockLandscape, openBlockLandscape, blockLandscapeState } from '@/composables/useBlockLandscape.ts'

function buildSourceBlock(body = '<tbody><tr data-row-idx="0"><td>only cell</td></tr></tbody>'): HTMLElement {
  const wrapper = document.createElement('div')
  wrapper.className = 'table-block-wrapper word-wrap'
  wrapper.innerHTML = `
    <div class="table-block-header">
      <span class="table-block-label">Table</span>
      <span class="table-block-header-actions">
        <span class="table-block-copy-dropdown">
          <button class="table-block-copy-btn" data-action="open-copy-menu" aria-expanded="false"></button>
          <div class="table-block-copy-menu" style="display: none;">
            <button class="table-block-copy-menu-item" data-action="copy-md">MD</button>
            <button class="table-block-copy-menu-item" data-action="copy-html">HTML</button>
            <button class="table-block-copy-menu-item" data-action="copy-tsv">TSV</button>
          </div>
        </span>
        <button class="table-block-wrap-btn is-wrapped" data-action="wrap"></button>
        <button class="table-block-landscape-btn" data-action="landscape"></button>
      </span>
    </div>
    <div class="table-wrap"><table data-table-idx="0">${body}</table></div>`
  document.body.appendChild(wrapper)
  return wrapper
}

function buildCodeSourceBlock(): HTMLElement {
  const wrapper = document.createElement('div')
  wrapper.className = 'code-block-wrapper word-wrap'
  wrapper.innerHTML = `
    <div class="code-block-header">
      <span class="code-block-lang">go</span>
      <span class="code-block-header-actions">
        <button class="code-block-copy-btn" data-action="copy"></button>
        <button class="code-block-wrap-btn is-wrapped" data-action="wrap"></button>
        <button class="code-block-landscape-btn" data-action="landscape"></button>
      </span>
    </div>
    <pre><code class="language-go">fmt.Println("hi")</code></pre>`
  document.body.appendChild(wrapper)
  return wrapper
}

function setViewport(width: number, height: number): void {
  Object.defineProperty(window, 'innerWidth', { configurable: true, value: width })
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: height })
}

let mounted: ReturnType<typeof mount> | null = null

// The copy feedback keeps a 1.5s timer alive; collect them so the suite does
// not end with a pending timer.
const pendingTimers: ReturnType<typeof setTimeout>[] = []
const _origSetTimeout = setTimeout
globalThis.setTimeout = ((fn: TimerHandler, ms?: number, ...args: any[]) => {
  const id = _origSetTimeout(fn, ms, ...args)
  pendingTimers.push(id)
  return id
}) as typeof setTimeout

async function mountOverlay() {
  mounted = mount(BlockLandscape, { attachTo: document.body })
  await flushPromises()
}

function overlayEl(): HTMLElement {
  return document.querySelector<HTMLElement>('.block-landscape')!
}

beforeEach(() => {
  touchPrimary.value = false
  setViewport(1024, 768)
})

afterEach(() => {
  // Order matters: resetting the state lets the still-mounted instance tear
  // down, then the instance itself goes. A leaked mount would keep reacting to
  // the module-level state in the NEXT test.
  closeBlockLandscape()
  mounted?.unmount()
  mounted = null
  document.body.innerHTML = ''
  document.body.style.overflow = ''
  mockCopyText.mockClear()
  for (const id of pendingTimers) clearTimeout(id)
  pendingTimers.length = 0
})

describe('BlockLandscape', () => {
  it('renders nothing until a table is opened', async () => {
    await mountOverlay()
    expect(document.querySelector('.block-landscape')).toBeNull()
  })

  it('shows the table with its own header, locks body scroll and focuses close', async () => {
    await mountOverlay()
    openBlockLandscape(buildSourceBlock())
    await flushPromises()
    await nextTick()

    const overlay = overlayEl()
    expect(overlay.textContent).toContain('only cell')
    // The table's own header is the overlay's header — no second toolbar.
    expect(overlay.querySelector('.table-block-header')).not.toBeNull()
    expect(overlay.querySelector('.table-block-wrap-btn')).not.toBeNull()
    expect(overlay.getAttribute('aria-modal')).toBe('true')
    expect(document.body.style.overflow).toBe('hidden')
    expect(document.activeElement).toBe(overlay.querySelector('[data-action="landscape-close"]'))

  })

  it('reuses the copy control: copying inside the overlay reads the cloned table', async () => {
    await mountOverlay()
    openBlockLandscape(buildSourceBlock())
    await flushPromises()

    overlayEl().querySelector<HTMLElement>('[data-action="open-copy-menu"]')!.click()
    await nextTick()
    overlayEl().querySelector<HTMLElement>('[data-action="copy-tsv"]')!.click()

    expect(mockCopyText).toHaveBeenCalledTimes(1)
    expect(mockCopyText.mock.calls[0][0]).toContain('only cell')
  })

  it('carries a wrap toggle made inside the overlay back to the live table', async () => {
    await mountOverlay()
    const source = buildSourceBlock()
    openBlockLandscape(source)
    await flushPromises()

    overlayEl().querySelector<HTMLElement>('[data-action="wrap"]')!.click()
    await flushPromises()
    // The clone flipped to no-wrap; the live table only follows on close.
    expect(overlayEl().querySelector('.table-block-wrapper')!.classList.contains('no-wrap')).toBe(true)
    expect(source.classList.contains('no-wrap')).toBe(false)

    overlayEl().querySelector<HTMLElement>('[data-action="landscape-close"]')!.click()
    await flushPromises()

    expect(source.classList.contains('no-wrap')).toBe(true)
    expect(source.querySelector('.table-block-wrap-btn')!.classList.contains('is-wrapped')).toBe(false)
  })

  it('opens the row viewer above the overlay when a row is tapped', async () => {
    touchPrimary.value = true
    await mountOverlay()
    openBlockLandscape(buildSourceBlock(
      '<thead><tr><th>Name</th></tr></thead><tbody><tr data-row-idx="0"><td>alpha</td></tr></tbody>',
    ))
    await flushPromises()

    // A real MouseEvent: jsdom's `.click()` carries a PointerEvent, and the row
    // viewer deliberately ignores anything whose pointerType is not 'touch'.
    overlayEl().querySelector<HTMLElement>('tbody td')!
      .dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()

    const modal = document.querySelector<HTMLElement>('.modal-overlay')
    expect(modal).not.toBeNull()
    expect(modal!.textContent).toContain('alpha')
    // Still inside the overlay session — the table did not close.
    expect(blockLandscapeState.value).not.toBeNull()

  })

  it('leaves the overlay and replays the click on the source for navigation targets', async () => {
    await mountOverlay()
    const source = buildSourceBlock(
      '<tbody><tr><td><span class="chat-file-path" data-file-path="src/a.ts">src/a.ts</span></td></tr></tbody>',
    )
    const sourcePath = source.querySelector<HTMLElement>('.chat-file-path')!
    const onSourceClick = vi.fn()
    sourcePath.addEventListener('click', onSourceClick)

    openBlockLandscape(source)
    await flushPromises()

    overlayEl().querySelector<HTMLElement>('.chat-file-path')!.click()
    await flushPromises()

    expect(blockLandscapeState.value).toBeNull()
    expect(onSourceClick).toHaveBeenCalledTimes(1)
    expect(onSourceClick.mock.calls[0][0].target).toBe(sourcePath)

  })

  it('serves a code block with the same controls and syncs its wrap toggle back', async () => {
    await mountOverlay()
    const source = buildCodeSourceBlock()
    openBlockLandscape(source)
    await flushPromises()

    const overlay = overlayEl()
    expect(overlay.textContent).toContain('fmt.Println')
    // Its own header travels with the clone — no second toolbar.
    expect(overlay.querySelector('.code-block-header')).not.toBeNull()

    overlay.querySelector<HTMLElement>('[data-action="copy"]')!.click()
    expect(mockCopyText).toHaveBeenCalledTimes(1)
    expect(mockCopyText.mock.calls[0][0]).toContain('fmt.Println')

    overlay.querySelector<HTMLElement>('[data-action="wrap"]')!.click()
    await flushPromises()
    expect(overlay.querySelector('.code-block-wrapper')!.classList.contains('word-wrap')).toBe(false)

    overlay.querySelector<HTMLElement>('[data-action="landscape-close"]')!.click()
    await flushPromises()

    expect(source.classList.contains('word-wrap')).toBe(false)
    // Code blocks have no `.no-wrap` rule — that class must not leak onto them.
    expect(source.classList.contains('no-wrap')).toBe(false)
    expect(source.querySelector('.code-block-wrap-btn')!.getAttribute('title')).toBe('codeBlock.wrapOff')
  })

  it('cancels the original click so a relative <a href> cannot navigate the SPA', async () => {
    await mountOverlay()
    const source = buildSourceBlock('<tbody><tr><td><a href="./src/a.ts">src/a.ts</a></td></tr></tbody>')
    const link = source.querySelector('a')!
    const onSourceClick = vi.fn((e: Event) => e.preventDefault())
    link.addEventListener('click', onSourceClick)

    openBlockLandscape(source)
    await flushPromises()

    const event = new MouseEvent('click', { bubbles: true, cancelable: true })
    overlayEl().querySelector('a')!.dispatchEvent(event)
    await flushPromises()

    // The replayed click does the real work; the original must not also let the
    // browser follow the href and blow the SPA away.
    expect(event.defaultPrevented).toBe(true)
    expect(onSourceClick).toHaveBeenCalledTimes(1)
    expect(blockLandscapeState.value).toBeNull()
  })

  it('leaves the overlay for an external link without replaying it', async () => {
    // installExternalLinkClick() runs at app start and claims .md-external-link
    // in the CAPTURE phase with stopPropagation(); emulate that ordering so the
    // regression (overlay left open under the confirm dialog) is caught.
    const claimed = vi.fn()
    const globalHandler = (e: MouseEvent) => {
      if ((e.target as Element)?.closest?.('.md-external-link')) {
        e.preventDefault()
        e.stopPropagation()
        claimed()
      }
    }
    document.addEventListener('click', globalHandler, true)

    await mountOverlay()
    const source = buildSourceBlock(
      '<tbody><tr><td><span class="md-external-link" data-href="https://example.test">x</span></td></tr></tbody>',
    )
    const span = source.querySelector('.md-external-link')!
    const onSourceClick = vi.fn()
    span.addEventListener('click', onSourceClick)

    openBlockLandscape(source)
    await flushPromises()

    overlayEl().querySelector('.md-external-link')!
      .dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    await flushPromises()

    expect(claimed).toHaveBeenCalledTimes(1)
    expect(blockLandscapeState.value).toBeNull()
    // Replaying would fire the confirm dialog a second time.
    expect(onSourceClick).not.toHaveBeenCalled()

    document.removeEventListener('click', globalHandler, true)
  })

  it('closes on Escape and releases the scroll lock', async () => {
    await mountOverlay()
    openBlockLandscape(buildSourceBlock())
    await flushPromises()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()

    expect(blockLandscapeState.value).toBeNull()
    expect(document.querySelector('.block-landscape')).toBeNull()
    expect(document.body.style.overflow).toBe('')

  })

  it('closes from the close button and returns focus to the trigger', async () => {
    await mountOverlay()
    const trigger = document.createElement('button')
    trigger.className = 'table-block-landscape-btn'
    document.body.appendChild(trigger)
    openBlockLandscape(buildSourceBlock(), trigger)
    await flushPromises()
    await nextTick()

    overlayEl().querySelector<HTMLElement>('[data-action="landscape-close"]')!.click()
    await flushPromises()

    expect(blockLandscapeState.value).toBeNull()
    expect(document.activeElement).toBe(trigger)

  })

  it('closes when the blank area around the table is tapped', async () => {
    await mountOverlay()
    openBlockLandscape(buildSourceBlock())
    await flushPromises()

    const scroll = document.querySelector<HTMLElement>('.block-landscape-scroll')!
    scroll.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()

    expect(blockLandscapeState.value).toBeNull()
  })

  it('traps Tab inside the overlay', async () => {
    await mountOverlay()
    openBlockLandscape(buildSourceBlock())
    await flushPromises()
    await nextTick()

    const closeBtn = overlayEl().querySelector<HTMLElement>('[data-action="landscape-close"]')!
    closeBtn.focus()

    const event = new KeyboardEvent('keydown', { key: 'Tab', cancelable: true })
    document.dispatchEvent(event)
    await nextTick()

    expect(event.defaultPrevented).toBe(true)
    expect(overlayEl().contains(document.activeElement)).toBe(true)

  })

  it('rotates on a touch device held in portrait', async () => {
    touchPrimary.value = true
    setViewport(390, 844)
    await mountOverlay()
    openBlockLandscape(buildSourceBlock())
    await flushPromises()

    expect(overlayEl().classList.contains('is-rotated')).toBe(true)
  })

  it('keeps the table upright in a narrow desktop window', async () => {
    setViewport(400, 900)
    await mountOverlay()
    openBlockLandscape(buildSourceBlock())
    await flushPromises()

    expect(overlayEl().classList.contains('is-rotated')).toBe(false)
  })

  it('closes on unmount so the layer cannot survive a route change', async () => {
    await mountOverlay()
    openBlockLandscape(buildSourceBlock())
    await flushPromises()

    mounted!.unmount()
    mounted = null
    await flushPromises()

    expect(blockLandscapeState.value).toBeNull()
    expect(document.querySelector('.block-landscape')).toBeNull()
    expect(document.body.style.overflow).toBe('')
  })
})
