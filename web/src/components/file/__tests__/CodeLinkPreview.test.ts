import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, reactive, computed, nextTick } from 'vue'
import { createI18n } from 'vue-i18n'
import CodeLinkPreview from '@/components/file/CodeLinkPreview.vue'
import { store } from '@/stores/app'
import type { useCodeLinkPreview } from '@/composables/useCodeLinkPreview'
import { useChatContext } from '@/composables/useChatContext'

// Mock highlightCode
vi.mock('@/utils/globals', () => ({
  highlightCode: (code: string) => `<span class="hl">${code}</span>`,
}))

// Mock useToast
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ show: vi.fn() }),
}))

// Mock fileType
vi.mock('@/utils/fileType', () => ({
  getFileType: () => ({ lang: 'typescript', label: 'TS' }),
  formatFileSize: (bytes: number) => {
    if (bytes < 1024) return bytes + ' B'
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
    return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
  },
}))

// Mock settings config
const mockLocalConfig = reactive<Record<string, any>>({
  uiScale: 1,
  markdownCodeLinkPreview: true,
})
vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    localConfig: mockLocalConfig,
  }),
  toFixedCSS: (val: number) => val,
  getZoomedViewport: () => ({ width: 1024, height: 768 }),
}))

const i18n = createI18n({
  legacy: false,
  locale: 'en',
  messages: {
    en: {
      file: {
        codePreview: {
          title: 'Code Preview',
          dragToMove: 'Drag to move',
          copy: 'Copy code',
          copied: 'Copied',
          refresh: 'Refresh',
          pin: 'Pin preview',
          unpin: 'Unpin preview',
          openFull: 'Open full file',
          viewDetails: 'View details / Download',
          close: 'Close preview',
          collapse: 'Collapse',
          expand: 'Expand context (+5)',
          shrink: 'Shrink context (-5)',
          expandAbove: 'Expand {n} lines above',
          expandBelow: 'Expand {n} lines below',
          expandToTop: 'Expand to top',
          expandToBottom: 'Expand to bottom',
          linesRemaining: '{n} lines remaining',
          wrap: 'Wrap lines',
          unwrap: 'Unwrap lines',
          loading: 'Loading code...',
          retry: 'Retry',
          largeFileNotice: 'Large file: preview shows partial content and may load slower',
          truncatedNotice: 'Preview truncated (up to {n} lines / {size})',
          lineOutOfRange: 'Requested line is out of file range',
          binaryNotSupported: 'Binary file cannot be previewed',
          fileTooLarge: 'File exceeds 10MiB limit, cannot be previewed online',
          dirNotSupported: 'Directories cannot be previewed as code',
          notFound: 'File not found',
          accessDenied: 'Access denied',
          loadError: 'Failed to load code',
          quoteToChat: 'Quote to chat',
          quotedToChat: 'Quoted code added to chat',
          copyPath: 'Copy path',
          pathCopied: 'File path copied',
          revealInTree: 'Reveal in file tree',
          revealedInTree: 'Located in file list: {dir}',
          toggleFullscreen: 'Full screen',
          exitFullscreen: 'Exit full screen',
          fontSize: 'Toggle font size',
          findInPreview: 'Find',
          findPlaceholder: 'Find in preview...',
          findPrev: 'Previous match (Shift+Enter)',
          findNext: 'Next match (Enter)',
          findClose: 'Close find (Esc)',
          noMatches: 'No matches',
          matchIndex: '{current} of {total}',
          linesCount: '{n} lines',
        },
      },
    },
  },
})

function createMockPreviewController(overrides: Partial<ReturnType<typeof useCodeLinkPreview>> = {}) {
  const visible = ref(true)
  const status = ref<'idle' | 'loading' | 'ready' | 'error'>('ready')
  const mode = ref<'transient' | 'pinned' | 'sheet'>('transient')
  const isPinned = ref(false)
  const target = ref<any>({
    filePath: 'src/main.ts',
    lineStart: 10,
    lineEnd: 20,
    anchorEl: document.createElement('span'),
  })
  const fileContent = ref<any>({
    content: 'line 1\nline 2',
    name: 'main.ts',
    path: 'src/main.ts',
    supported: true,
    size: 20,
  })
  const slicedCode = ref<any>({
    code: 'const x = 10\nconst y = 20',
    startLine: 10,
    endLine: 11,
    totalLines: 50,
    highlightStart: 10,
    highlightEnd: 10,
    lineOutOfRange: false,
    renderTruncated: false,
  })
  const errorCode = ref<any>(null)
  const errorMessage = ref<string | null>(null)
  const isLargeFile = ref(false)
  const contextExpansion = ref(0)
  const placement = ref<any>({
    viewportX: 100,
    viewportY: 100,
    cssLeft: '100px',
    cssTop: '100px',
    quadrant: 'bottom-right',
  })

  return {
    enabled: ref(true),
    visible,
    status,
    mode,
    isPinned,
    target,
    fileContent,
    slicedCode,
    errorCode,
    errorMessage,
    isLargeFile,
    contextExpansion,
    placement,
    showPreview: vi.fn(),
    close: vi.fn(() => {
      visible.value = false
    }),
    pin: vi.fn(() => {
      isPinned.value = true
      mode.value = 'pinned'
    }),
    unpin: vi.fn(() => {
      isPinned.value = false
      mode.value = 'transient'
    }),
    togglePin: vi.fn(() => {
      if (isPinned.value) {
        isPinned.value = false
        mode.value = 'transient'
      } else {
        isPinned.value = true
        mode.value = 'pinned'
      }
    }),
    refresh: vi.fn(),
    expandContext: vi.fn(),
    shrinkContext: vi.fn(),
    expandAbove: vi.fn(),
    expandBelow: vi.fn(),
    expandToTop: vi.fn(),
    expandToBottom: vi.fn(),
    openFull: vi.fn(),
    onCardPointerEnter: vi.fn(),
    onCardPointerLeave: vi.fn(),
    onCardFocusIn: vi.fn(),
    onCardFocusOut: vi.fn(),
    handleMouseOver: vi.fn(),
    handleMouseOut: vi.fn(),
    handleFocusIn: vi.fn(),
    handleFocusOut: vi.fn(),
    handleClick: vi.fn(),
    updatePlacement: vi.fn(),
    bindEvents: vi.fn(),
    unbindEvents: vi.fn(),
    ...overrides,
  } as unknown as ReturnType<typeof useCodeLinkPreview>
}

describe('CodeLinkPreview.vue', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
    localStorage.clear()
    useChatContext().clearAll()
  })

  it('renders loading status with aria-live="polite"', () => {
    const preview = createMockPreviewController({
      status: ref('loading'),
    })
    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const floating = document.querySelector('.code-link-preview-floating')
    expect(floating).not.toBeNull()
    const loadingEl = floating?.querySelector('[aria-live="polite"]')
    expect(loadingEl).not.toBeNull()
    expect(loadingEl?.textContent).toContain('Loading code...')
  })

  it('renders code snippet, line numbers, and highlight target background', () => {
    const preview = createMockPreviewController({
      status: ref('ready'),
      slicedCode: ref({
        code: 'const a = 1\nconst b = 2',
        startLine: 10,
        endLine: 11,
        totalLines: 50,
        highlightStart: 10,
        highlightEnd: 10,
        lineOutOfRange: false,
        renderTruncated: false,
      }),
    })

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const floating = document.querySelector('.code-link-preview-floating')
    expect(floating).not.toBeNull()

    // Gutter line numbers
    const lineNumbers = floating?.querySelectorAll('.code-preview-line-number')
    expect(lineNumbers?.length).toBe(2)
    expect(lineNumbers?.[0].textContent?.trim()).toBe('10')
    expect(lineNumbers?.[0].classList.contains('is-target-line')).toBe(true)
    expect(lineNumbers?.[1].textContent?.trim()).toBe('11')
    expect(lineNumbers?.[1].classList.contains('is-target-line')).toBe(false)

    // Code content
    const codeEl = floating?.querySelector('code.hljs')
    expect(codeEl).not.toBeNull()
    expect(codeEl?.textContent).toContain('const a = 1')
  })

  it('renders large file notice when isLargeFile is true', () => {
    const preview = createMockPreviewController({
      isLargeFile: ref(true),
    })
    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const notice = document.querySelector('.code-preview-notice.notice-warning')
    expect(notice).not.toBeNull()
    expect(notice?.textContent).toContain('Large file')
  })

  it('renders binary file error with open full file button', () => {
    const preview = createMockPreviewController({
      status: ref('error'),
      errorCode: ref('binary'),
    })
    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const floating = document.querySelector('.code-link-preview-floating')
    expect(floating?.textContent).toContain('Binary file cannot be previewed')
    // Header openFull button is still available
    const openBtn = floating?.querySelector('button[title="Open full file"]')
    expect(openBtn).not.toBeNull()
  })

  it('renders too-large error with view details button instead of openFull', () => {
    const preview = createMockPreviewController({
      status: ref('error'),
      errorCode: ref('too-large'),
    })
    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const floating = document.querySelector('.code-link-preview-floating')
    expect(floating?.textContent).toContain('File exceeds 10MiB limit')
    const detailsBtn = floating?.querySelector('button[title="View details / Download"]')
    expect(detailsBtn).not.toBeNull()
    const openFullBtn = floating?.querySelector('button[title="Open full file"]')
    expect(openFullBtn).toBeNull()
  })

  it('toggles pin and updates aria-pressed', async () => {
    const isPinned = ref(false)
    const mode = ref<'transient' | 'pinned' | 'sheet'>('transient')
    const preview = createMockPreviewController({
      isPinned,
      mode,
      togglePin: vi.fn(() => {
        isPinned.value = !isPinned.value
        mode.value = isPinned.value ? 'pinned' : 'transient'
      }),
    })

    const wrapper = mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const pinBtn = document.querySelector('button[title="Pin preview"]') as HTMLButtonElement
    expect(pinBtn).not.toBeNull()
    expect(pinBtn.getAttribute('aria-pressed')).toBe('false')

    pinBtn.click()
    expect(preview.togglePin).toHaveBeenCalled()
  })

  it('renders BottomSheet when in sheet mode', () => {
    const preview = createMockPreviewController({
      mode: ref('sheet'),
    })

    const wrapper = mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    // In sheet mode, teleported floating dialog is not rendered
    expect(document.querySelector('.code-link-preview-floating')).toBeNull()
    // BottomSheet component is rendered
    expect(wrapper.findComponent({ name: 'BottomSheet' }).exists()).toBe(true)
  })

  it('handles Escape to close and focus origin', () => {
    const anchor = document.createElement('a')
    document.body.appendChild(anchor)
    anchor.focus = vi.fn()

    const preview = createMockPreviewController({
      target: ref({
        filePath: 'test.ts',
        anchorEl: anchor,
      }),
    })

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const floating = document.querySelector('.code-link-preview-floating') as HTMLElement
    floating.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))

    expect(preview.close).toHaveBeenCalled()
    expect(anchor.focus).toHaveBeenCalled()
  })

  it('renders top and bottom expand bars and triggers directional expansion', async () => {
    const preview = createMockPreviewController({
      status: ref('ready'),
      slicedCode: ref({
        code: 'line 20\nline 21',
        startLine: 20,
        endLine: 21,
        totalLines: 100,
        highlightStart: 20,
        highlightEnd: 20,
        lineOutOfRange: false,
        renderTruncated: false,
      }),
    })

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const floating = document.querySelector('.code-link-preview-floating') as HTMLElement
    const expandAboveBtn = floating.querySelector('.code-preview-expand-bar.expand-above .code-preview-expand-btn') as HTMLButtonElement
    const expandBelowBtn = floating.querySelector('.code-preview-expand-bar.expand-below .code-preview-expand-btn') as HTMLButtonElement

    expect(expandAboveBtn).not.toBeNull()
    expect(expandBelowBtn).not.toBeNull()

    expandAboveBtn.click()
    expect(preview.expandAbove).toHaveBeenCalledWith(10)

    expandBelowBtn.click()
    expect(preview.expandBelow).toHaveBeenCalledWith(10)
  })

  it('supports header dragging with pointer events', () => {
    const preview = createMockPreviewController()

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const header = document.querySelector('.code-preview-header') as HTMLElement
    expect(header).not.toBeNull()

    // Mock setPointerCapture and releasePointerCapture
    header.setPointerCapture = vi.fn()
    header.releasePointerCapture = vi.fn()

    header.dispatchEvent(new PointerEvent('pointerdown', { clientX: 100, clientY: 100, pointerId: 1, bubbles: true }))
    expect(header.setPointerCapture).toHaveBeenCalledWith(1)

    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 150, clientY: 160 }))
    const floating = document.querySelector('.code-link-preview-floating') as HTMLElement
    expect(floating.style.left).toBeDefined()

    header.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
    expect(header.releasePointerCapture).toHaveBeenCalledWith(1)
  })

  it('allows dragging from anywhere on the titlebar header', async () => {
    const preview = createMockPreviewController()

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const title = document.querySelector('.code-preview-title') as HTMLElement
    expect(title).not.toBeNull()

    const header = document.querySelector('.code-preview-header') as HTMLElement
    header.setPointerCapture = vi.fn()
    header.releasePointerCapture = vi.fn()

    // Dragging from title
    title.dispatchEvent(new PointerEvent('pointerdown', { clientX: 200, clientY: 200, pointerId: 2, bubbles: true }))
    expect(header.setPointerCapture).toHaveBeenCalledWith(2)

    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 240, clientY: 240 }))
    const floating = document.querySelector('.code-link-preview-floating') as HTMLElement
    expect(floating.style.left).toBeDefined()

    title.dispatchEvent(new PointerEvent('pointerup', { pointerId: 2, bubbles: true }))
  })

  it('handles F2 shortcut to focus first action button', () => {
    const preview = createMockPreviewController()

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const firstBtn = document.querySelector('.code-preview-actions button') as HTMLButtonElement
    expect(firstBtn).not.toBeNull()
    firstBtn.focus = vi.fn()

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'F2' }))

    expect(firstBtn.focus).toHaveBeenCalled()
  })

  it('toggles word-wrap and updates class and aria-pressed', async () => {
    const preview = createMockPreviewController()

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const wrapBtn = document.querySelector('button[title="Unwrap lines"]') as HTMLButtonElement
    expect(wrapBtn).not.toBeNull()
    expect(wrapBtn.getAttribute('aria-pressed')).toBe('true')

    const scrollPane = document.querySelector('.code-preview-scroll') as HTMLElement
    expect(scrollPane.classList.contains('is-word-wrap')).toBe(true)

    // Click toggle
    wrapBtn.click()
    await nextTick()

    expect(scrollPane.classList.contains('is-word-wrap')).toBe(false)
    expect(wrapBtn.getAttribute('aria-pressed')).toBe('false')
  })

  it('centers target line when opening from cache (visible+ready flip in one flush)', async () => {
    // Reproduces a cache-hit reopen: showPreview resolves from the LRU cache, so
    // visible=true and status='ready' flip within the same tick — the parent's
    // pre-flush watchers run before CodePreviewBody has mounted. The target-line
    // centering must still reach the body once the pane exists in that flush.
    const origOffsetTop = Object.getOwnPropertyDescriptor(Element.prototype, 'offsetTop')
    const origClientHeight = Object.getOwnPropertyDescriptor(Element.prototype, 'clientHeight')
    Object.defineProperty(Element.prototype, 'offsetTop', { configurable: true, get: () => 480 })
    Object.defineProperty(Element.prototype, 'clientHeight', { configurable: true, get: () => 300 })
    const origScrollTop = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollTop')
    let capturedScrollTop: number | null = null
    Object.defineProperty(Element.prototype, 'scrollTop', {
      configurable: true,
      get: () => capturedScrollTop ?? 0,
      set: (v: number) => {
        capturedScrollTop = v
      },
    })

    const preview = createMockPreviewController({
      visible: ref(false),
      status: ref('ready'),
      slicedCode: ref({
        code: Array.from({ length: 50 }, (_, i) => `line ${i + 1}`).join('\n'),
        startLine: 1,
        endLine: 50,
        totalLines: 100,
        highlightStart: 25,
        highlightEnd: 25,
        lineOutOfRange: false,
        renderTruncated: false,
      }),
    })

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })
    await nextTick()
    expect(document.querySelector('.code-preview-scroll')).toBeNull()

    // Cache-hit open: reveal while status is already ready.
    preview.visible.value = true
    await nextTick()
    await nextTick()
    await nextTick()

    const scrollPane = document.querySelector('.code-preview-scroll') as HTMLElement
    expect(scrollPane).not.toBeNull()
    expect(scrollPane.querySelectorAll('.code-preview-line-row.is-target-line').length).toBeGreaterThan(0)

    // rangeHeight (480+300-480=300) >= container (300) -> scrollTop = rangeTop = 480.
    // jsdom's un-laid-out geometry may collapse the exact value to 0, but the
    // important regression contract is that the deferred centering RUNS at all:
    // without the parent's nextTick deferral this stays null (the call is
    // dropped while bodyRef is still unset in the same flush).
    expect(capturedScrollTop).not.toBeNull()

    if (origOffsetTop) Object.defineProperty(Element.prototype, 'offsetTop', origOffsetTop)
    else delete (Element.prototype as Record<string, unknown>).offsetTop
    if (origClientHeight) Object.defineProperty(Element.prototype, 'clientHeight', origClientHeight)
    else delete (Element.prototype as Record<string, unknown>).clientHeight
    if (origScrollTop) Object.defineProperty(Element.prototype, 'scrollTop', origScrollTop)
    else delete (Element.prototype as Record<string, unknown>).scrollTop
  })

  it('centers target line on scrollPane when ready', async () => {
    const preview = createMockPreviewController({
      status: ref('ready'),
      slicedCode: ref({
        code: Array.from({ length: 50 }, (_, i) => `line ${i + 1}`).join('\n'),
        startLine: 1,
        endLine: 50,
        totalLines: 100,
        highlightStart: 25,
        highlightEnd: 25,
        lineOutOfRange: false,
        renderTruncated: false,
      }),
    })

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const scrollPane = document.querySelector('.code-preview-scroll') as HTMLElement
    expect(scrollPane).not.toBeNull()

    // Mock clientHeight and offsetTop for testing scroll calculation
    Object.defineProperty(scrollPane, 'clientHeight', { value: 300, configurable: true })
    let mockScrollTop = 0
    Object.defineProperty(scrollPane, 'scrollTop', {
      get: () => mockScrollTop,
      set: (val: number) => {
        mockScrollTop = val
      },
      configurable: true,
    })

    const targetRow = scrollPane.querySelector('.code-preview-line-row.is-target-line') as HTMLElement
    expect(targetRow).not.toBeNull()
    Object.defineProperty(targetRow, 'clientHeight', { value: 20, configurable: true })
    Object.defineProperty(targetRow, 'offsetTop', { value: 480, configurable: true })

    const wrapBtn = document.querySelector('button[title="Unwrap lines"]') as HTMLButtonElement
    expect(wrapBtn).not.toBeNull()
    wrapBtn.click()
    await nextTick()
    await nextTick()

    // Target line offsetTop=480, containerHeight=300, targetHeight=20 -> (300-20)/2 = 140 -> scrollTop = 480 - 140 = 340
    expect(scrollPane.scrollTop).toBe(340)
  })

  it('applies dynamic maxHeight to cardStyle when placement.maxHeight is present', () => {
    const preview = createMockPreviewController({
      placement: ref({
        viewportX: 100,
        viewportY: 60,
        cssLeft: '100px',
        cssTop: '60px',
        maxHeight: 320,
        quadrant: 'clamped',
      }),
    })

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const floating = document.querySelector('.code-link-preview-floating') as HTMLElement
    expect(floating).not.toBeNull()
    expect(floating.style.maxHeight).toContain('320px')
  })

  it('copies file path with line numbers when clicking copy path action button', async () => {
    const writeTextMock = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: writeTextMock },
      configurable: true,
      writable: true,
    })

    const preview = createMockPreviewController()
    const wrapper = mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const copyBtn = document.querySelector('button[aria-label="Copy path"], button[aria-label="File path copied"], button[aria-label="复制路径"], button[aria-label="已复制文件路径"]') as HTMLButtonElement
    expect(copyBtn).not.toBeNull()
    copyBtn.click()
    await nextTick()

    expect(writeTextMock).toHaveBeenCalledWith('src/main.ts:10-20')
    wrapper.unmount()
  })

  it('triggers quoteToChat and adds a staged quote without duplicate file attachment', async () => {
    const switchTabMock = vi.fn()
    const preview = createMockPreviewController()
    mount(CodeLinkPreview, {
      props: { preview },
      global: {
        plugins: [i18n],
        provide: { switchTab: switchTabMock },
      },
    })

    const quoteBtn = document.querySelector('button[title="Quote to chat"]') as HTMLButtonElement
    expect(quoteBtn).not.toBeNull()
    quoteBtn.click()
    await nextTick()

    expect(preview.close).toHaveBeenCalled()
    expect(switchTabMock).toHaveBeenCalledWith('chat')
    const chatContext = useChatContext()
    expect(chatContext.stagedQuotes.value).toHaveLength(1)
    expect(chatContext.stagedQuotes.value[0].filePath).toBe('src/main.ts')
    expect(chatContext.attachedFiles.value).toHaveLength(0)
  })

  it('triggers revealInTree and calls store.loadFiles and switches tab to browse', async () => {
    const switchTabMock = vi.fn()
    const loadFilesSpy = vi.spyOn(store, 'loadFiles').mockResolvedValue(undefined as any)
    const preview = createMockPreviewController()
    mount(CodeLinkPreview, {
      props: { preview },
      global: {
        plugins: [i18n],
        provide: { switchTab: switchTabMock },
      },
    })

    const revealBtn = document.querySelector('button[title="Reveal in file tree"]') as HTMLButtonElement
    expect(revealBtn).not.toBeNull()
    revealBtn.click()
    await nextTick()

    expect(loadFilesSpy).toHaveBeenCalledWith('src')
    expect(preview.close).toHaveBeenCalled()
    expect(switchTabMock).toHaveBeenCalledWith('browse')
  })

  it('decomposes long file paths into filename, subdued line reference, and directory path in sheet mode', async () => {
    const writeTextMock = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, {
      clipboard: { writeText: writeTextMock },
    })

    const preview = createMockPreviewController({
      mode: ref('sheet'),
      target: ref({
        filePath: 'packages/agent/src/agent-loop.ts',
        lineStart: 245,
        lineEnd: 250,
        anchorEl: document.createElement('span'),
      }),
    })

    const wrapper = mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const filenameEl = document.querySelector('.code-preview-sheet-filename')
    expect(filenameEl).not.toBeNull()
    expect(filenameEl?.textContent).toBe('agent-loop.ts')

    const rangeEl = document.querySelector('.code-preview-sheet-header .code-preview-line-ref')
    expect(rangeEl).not.toBeNull()
    expect(rangeEl?.textContent).toBe(':245-250')

    const dirEl = document.querySelector('.code-preview-sheet-dir')
    expect(dirEl).not.toBeNull()
    expect(dirEl?.textContent).toBe('packages/agent/src/')

    const copyBtn = document.querySelector('.code-preview-sheet-row1-actions .copy-path-btn') as HTMLButtonElement
    expect(copyBtn).not.toBeNull()

    // Clicking the copy button copies the full path
    copyBtn.click()
    await nextTick()
    expect(writeTextMock).toHaveBeenCalledWith('packages/agent/src/agent-loop.ts:245-250')
    wrapper.unmount()
  })

  it('renders thumb-friendly footer actions in sheet mode', async () => {
    const switchTabMock = vi.fn()
    const loadFilesSpy = vi.spyOn(store, 'loadFiles').mockResolvedValue(undefined as any)
    const preview = createMockPreviewController({
      mode: ref('sheet'),
      openFull: vi.fn(),
    })

    mount(CodeLinkPreview, {
      props: { preview },
      global: {
        plugins: [i18n],
        provide: { switchTab: switchTabMock },
      },
    })

    const footer = document.querySelector('.code-preview-sheet-footer')
    expect(footer).not.toBeNull()

    // Test Open Full button in footer
    const openFullBtn = footer?.querySelector('.primary-btn') as HTMLButtonElement
    expect(openFullBtn).not.toBeNull()
    openFullBtn.click()
    expect(preview.openFull).toHaveBeenCalled()

    // Test Refresh button in footer
    const refreshBtn = footer?.querySelector('.refresh-btn') as HTMLButtonElement
    expect(refreshBtn).not.toBeNull()
    refreshBtn.click()
    expect(preview.refresh).toHaveBeenCalled()

    // Test Reveal in file tree in row2 tools
    const row2 = document.querySelector('.code-preview-sheet-row2')
    const revealBtn = row2?.querySelector('button[title="Reveal in file tree"]') as HTMLButtonElement
    expect(revealBtn).not.toBeNull()
    revealBtn.click()
    await nextTick()
    expect(loadFilesSpy).toHaveBeenCalledWith('src')
    expect(switchTabMock).toHaveBeenCalledWith('browse')

    // Test Collapse button in footer
    const collapseBtn = footer?.querySelector('.collapse-btn') as HTMLButtonElement
    expect(collapseBtn).not.toBeNull()
    collapseBtn.click()
    expect(preview.close).toHaveBeenCalled()

    // Test Quote to Chat button in footer
    const quoteBtn = footer?.querySelector('.quote-btn') as HTMLButtonElement
    expect(quoteBtn).not.toBeNull()
    quoteBtn.click()
    await nextTick()
    expect(preview.close).toHaveBeenCalled()
    expect(switchTabMock).toHaveBeenCalledWith('chat')
  })

  it('displays context metadata badge with language, line count, and size', () => {
    const preview = createMockPreviewController({
      fileContent: ref({
        content: 'line 1\nline 2',
        name: 'main.ts',
        path: 'src/main.ts',
        supported: true,
        size: 2048,
      }),
      slicedCode: ref({
        code: 'const x = 10\nconst y = 20',
        startLine: 10,
        endLine: 11,
        totalLines: 120,
        highlightStart: 10,
        highlightEnd: 10,
        lineOutOfRange: false,
        renderTruncated: false,
      }),
    })

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const metaEl = document.querySelector('.code-preview-meta') as HTMLElement
    expect(metaEl).not.toBeNull()
    expect(metaEl.textContent).toContain('TS')
    expect(metaEl.textContent).toContain('120 lines')
    expect(metaEl.textContent).toContain('2.0 KB')
  })

  it('performs in-preview search and highlights matches', async () => {
    const preview = createMockPreviewController({
      slicedCode: ref({
        code: 'const apple = 1\nconst banana = 2\nconst cherry = 3',
        startLine: 1,
        endLine: 3,
        totalLines: 3,
        highlightStart: 1,
        highlightEnd: 1,
        lineOutOfRange: false,
        renderTruncated: false,
      }),
    })

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const findBtn = document.querySelector('button[title="Find"]') as HTMLButtonElement
    expect(findBtn).not.toBeNull()

    // Open search bar
    findBtn.click()
    await nextTick()

    const searchInput = document.querySelector('.code-preview-search-input') as HTMLInputElement
    expect(searchInput).not.toBeNull()

    // Type query matching all lines
    searchInput.value = 'banana'
    searchInput.dispatchEvent(new Event('input'))
    await nextTick()

    const lineRows = document.querySelectorAll('.code-preview-line-row')
    expect(lineRows[1].classList.contains('is-search-match')).toBe(true)
    expect(lineRows[1].classList.contains('is-current-search-match')).toBe(true)
    expect(lineRows[0].classList.contains('is-search-match')).toBe(false)

    // Close search
    const closeBtn = document.querySelector('button[title="Close find (Esc)"]') as HTMLButtonElement
    expect(closeBtn).not.toBeNull()
    closeBtn.click()
    await nextTick()

    expect(document.querySelector('.code-preview-search-bar')).toBeNull()
  })

  it('automatically closes preview when activeTab switches away (mobile or unpinned)', async () => {
    const activeTab = ref('view')
    const closeSpy = vi.fn()
    const preview = createMockPreviewController({
      visible: ref(true),
      mode: ref('sheet'),
      close: closeSpy,
    })

    mount(CodeLinkPreview, {
      props: { preview },
      global: {
        plugins: [i18n],
        provide: {
          activeTab,
        },
      },
    })

    // Switch tab from 'view' to 'chat'
    activeTab.value = 'chat'
    await nextTick()

    expect(closeSpy).toHaveBeenCalledTimes(1)
  })

  it('preserves preview on tab switch if desktop pinned', async () => {
    const activeTab = ref('view')
    const closeSpy = vi.fn()
    const preview = createMockPreviewController({
      visible: ref(true),
      mode: ref('pinned'),
      isPinned: computed(() => true),
      close: closeSpy,
    })

    mount(CodeLinkPreview, {
      props: { preview },
      global: {
        plugins: [i18n],
        provide: {
          activeTab,
        },
      },
    })

    activeTab.value = 'chat'
    await nextTick()

    expect(closeSpy).not.toHaveBeenCalled()
  })

  it('automatically closes preview when store active file path changes', async () => {
    const closeSpy = vi.fn()
    const preview = createMockPreviewController({
      visible: ref(true),
      mode: ref('sheet'),
      close: closeSpy,
    })

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    // Simulate switching file
    store.state.currentFile = { name: 'other.md', path: '/workspace/other.md', content: '' }
    await nextTick()

    expect(closeSpy).toHaveBeenCalled()
  })

  it('preserves and adapts maxHeight in cardStyle during drag state', async () => {
    const preview = createMockPreviewController({
      placement: ref({
        viewportX: 100,
        viewportY: 300,
        cssLeft: '100px',
        cssTop: '300px',
        maxHeight: 250,
        quadrant: 'bottom-right',
      }),
    })

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const floating = document.querySelector('.code-link-preview-floating') as HTMLElement
    expect(floating.style.maxHeight).toContain('250px')

    const header = document.querySelector('.code-preview-header') as HTMLElement
    header.setPointerCapture = vi.fn()
    header.releasePointerCapture = vi.fn()

    // Trigger drag
    header.dispatchEvent(new PointerEvent('pointerdown', { clientX: 100, clientY: 300, pointerId: 1, bubbles: true }))
    await nextTick()

    // During drag, maxHeight must still be defined and clamped within safe bounds
    expect(floating.style.maxHeight).toBeDefined()
    expect(floating.style.maxHeight).not.toBe('')

    // Move pointer slightly
    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 100, clientY: 310 }))
    await nextTick()

    expect(floating.style.maxHeight).toBeDefined()
    expect(floating.style.top).toBeDefined()

    header.dispatchEvent(new PointerEvent('pointerup', { pointerId: 1, bubbles: true }))
  })

  it('smoothly follows pointer without jumping when dragging a card positioned near bottom', async () => {
    const preview = createMockPreviewController({
      placement: ref({
        viewportX: 100,
        viewportY: 500,
        cssLeft: '100px',
        cssTop: '500px',
        maxHeight: 200,
        quadrant: 'bottom-right',
      }),
    })

    mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const floating = document.querySelector('.code-link-preview-floating') as HTMLElement
    const header = document.querySelector('.code-preview-header') as HTMLElement
    header.setPointerCapture = vi.fn()
    header.releasePointerCapture = vi.fn()

    // Drag start at y=500
    header.dispatchEvent(new PointerEvent('pointerdown', { clientX: 100, clientY: 500, pointerId: 3, bubbles: true }))
    await nextTick()

    // Move up by 5px
    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 100, clientY: 495 }))
    await new Promise((r) => requestAnimationFrame(r))
    await nextTick()

    // Should smoothly track upwards without an abrupt upward jump
    expect(floating.style.top).toBeDefined()

    header.dispatchEvent(new PointerEvent('pointerup', { pointerId: 3, bubbles: true }))
  })

  it('provides a dedicated copy path button in header actions and copies path on click', async () => {
    const writeTextMock = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, {
      clipboard: {
        writeText: writeTextMock,
      },
    })

    const preview = createMockPreviewController({
      target: ref({
        filePath: 'src/components/Test.vue',
        lineStart: 5,
        lineEnd: 15,
      }),
    })

    const wrapper = mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const copyBtn = document.querySelector('button[aria-label="Copy path"], button[aria-label="File path copied"], button[aria-label="复制路径"], button[aria-label="已复制文件路径"]') as HTMLButtonElement
    expect(copyBtn).not.toBeNull()
    copyBtn.click()
    await nextTick()

    expect(writeTextMock).toHaveBeenCalledWith('src/components/Test.vue:5-15')
    wrapper.unmount()
  })

  it('does not copy path when dragging titlebar', async () => {
    const writeTextMock = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, {
      clipboard: {
        writeText: writeTextMock,
      },
    })

    const preview = createMockPreviewController({
      target: ref({
        filePath: 'src/utils/math.ts',
        lineStart: 42,
      }),
    })

    const wrapper = mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const header = document.querySelector('.code-preview-header') as HTMLElement
    const titleEl = document.querySelector('.code-preview-title') as HTMLElement
    header.setPointerCapture = vi.fn()
    header.releasePointerCapture = vi.fn()

    // Drag from title
    titleEl.dispatchEvent(new PointerEvent('pointerdown', { clientX: 100, clientY: 100, pointerId: 5, bubbles: true }))
    await nextTick()

    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 120, clientY: 120 }))
    await new Promise((r) => requestAnimationFrame(r))
    await nextTick()

    header.dispatchEvent(new PointerEvent('pointerup', { pointerId: 5, bubbles: true }))
    await nextTick()

    expect(writeTextMock).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('locks card height during dragging to prevent reflow and stretching', async () => {
    const preview = createMockPreviewController({
      placement: ref({
        viewportX: 100,
        viewportY: 300,
        cssLeft: '100px',
        cssTop: '300px',
        maxHeight: 280,
        quadrant: 'bottom-right',
      }),
    })

    const wrapper = mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })

    const floating = document.querySelector('.code-link-preview-floating') as HTMLElement
    const header = document.querySelector('.code-preview-header') as HTMLElement
    header.setPointerCapture = vi.fn()
    header.releasePointerCapture = vi.fn()

    // Mock bounding rect
    Object.defineProperty(floating, 'getBoundingClientRect', {
      value: () => ({
        left: 100,
        top: 300,
        width: 500,
        height: 250,
        right: 600,
        bottom: 550,
      }),
      configurable: true,
    })

    // Pointer down starts dragging
    header.dispatchEvent(new PointerEvent('pointerdown', { clientX: 100, clientY: 300, pointerId: 6, bubbles: true }))
    await nextTick()

    // During drag, style.height should be explicitly locked to cached height
    expect(floating.style.height).toBe('250px')
    expect(floating.classList.contains('is-dragging')).toBe(true)

    // Move pointer
    window.dispatchEvent(new PointerEvent('pointermove', { clientX: 100, clientY: 280 }))
    await new Promise((r) => requestAnimationFrame(r))
    await nextTick()

    expect(floating.style.height).toBe('250px')

    // Stop dragging
    header.dispatchEvent(new PointerEvent('pointerup', { pointerId: 6, bubbles: true }))
    await nextTick()

    expect(floating.classList.contains('is-dragging')).toBe(false)
    wrapper.unmount()
  })

  it('displays fast hover tooltip with full path on left title area', async () => {
    vi.useFakeTimers()
    const preview = createMockPreviewController({
      mode: ref('pinned'),
      target: ref({ filePath: 'web/src/components/file/CodeLinkPreview.vue', lineStart: 10, lineEnd: 20 }),
    })

    const wrapper = mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })
    await nextTick()

    const floating = document.querySelector('.code-link-preview-floating') as HTMLElement
    Object.defineProperty(floating, 'getBoundingClientRect', {
      value: () => ({ left: 100, top: 100, width: 600, height: 400, right: 700, bottom: 500 }),
      configurable: true,
    })

    const titleEl = document.querySelector('.code-preview-title') as HTMLElement
    expect(titleEl).not.toBeNull()
    expect(titleEl.getAttribute('data-tooltip')).toBe('web/src/components/file/CodeLinkPreview.vue:10-20')

    // Hover on title area
    titleEl.dispatchEvent(new PointerEvent('pointerenter', { bubbles: true }))
    expect(document.querySelector('.code-preview-tooltip')).toBeNull()

    // Fast delay 70ms triggers unified full path tooltip
    vi.advanceTimersByTime(70)
    await nextTick()
    const tooltip = document.querySelector('.code-preview-tooltip')
    expect(tooltip).not.toBeNull()
    expect(tooltip?.textContent?.trim()).toBe('web/src/components/file/CodeLinkPreview.vue:10-20')

    // Mouse leave hides tooltip
    titleEl.dispatchEvent(new PointerEvent('pointerleave', { bubbles: true }))
    await nextTick()
    expect(document.querySelector('.code-preview-tooltip')).toBeNull()

    wrapper.unmount()
    vi.useRealTimers()
  })

  it('displays tooltip on action buttons and updates dynamically on click', async () => {
    vi.useFakeTimers()
    const writeTextMock = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', {
      value: { writeText: writeTextMock },
      configurable: true,
      writable: true,
    })

    const preview = createMockPreviewController({
      mode: ref('pinned'),
      target: ref({ filePath: 'src/main.ts' }),
    })

    const wrapper = mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })
    await nextTick()

    const floating = document.querySelector('.code-link-preview-floating') as HTMLElement
    Object.defineProperty(floating, 'getBoundingClientRect', {
      value: () => ({ left: 100, top: 100, width: 600, height: 400, right: 700, bottom: 500 }),
      configurable: true,
    })

    const copyPathBtn = document.querySelector('button[data-tooltip="Copy path"]') as HTMLButtonElement
    expect(copyPathBtn).not.toBeNull()

    copyPathBtn.dispatchEvent(new PointerEvent('pointerenter', { bubbles: true }))
    vi.advanceTimersByTime(120)
    await nextTick()

    let tooltip = document.querySelector('.code-preview-tooltip')
    expect(tooltip?.textContent?.trim()).toBe('Copy path')

    // Click copy path updates tooltip text to 'File path copied'
    copyPathBtn.click()
    await Promise.resolve()
    await nextTick()
    tooltip = document.querySelector('.code-preview-tooltip')
    expect(tooltip?.textContent?.trim()).toBe('File path copied')

    wrapper.unmount()
    vi.useRealTimers()
  })

  it('clears a close-button tooltip when the preview is closed and reopened', async () => {
    vi.useFakeTimers()
    const preview = createMockPreviewController({ mode: ref('pinned') })
    const wrapper = mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })
    await nextTick()

    const closeBtn = document.querySelector('.code-preview-btn.close') as HTMLElement
    closeBtn.dispatchEvent(new PointerEvent('pointerenter', { bubbles: true }))
    vi.advanceTimersByTime(120)
    await nextTick()
    expect(document.querySelector('.code-preview-tooltip')).not.toBeNull()

    preview.visible.value = false
    await nextTick()
    expect(document.querySelector('.code-preview-tooltip')).toBeNull()

    preview.visible.value = true
    await nextTick()
    expect(document.querySelector('.code-preview-tooltip')).toBeNull()

    wrapper.unmount()
    vi.useRealTimers()
  })

  it('displays clean filename on row 1 and directory path in context meta bar on desktop', async () => {
    const preview = createMockPreviewController({
      mode: ref('pinned'),
      target: ref({
        filePath: 'packages/coding-agent/src/agent-session-handler.ts',
        lineStart: 542,
        lineEnd: 559,
      }),
      fileContent: ref({
        content: 'line 1\nline 2',
        size: 117300,
      }),
      slicedCode: ref({
        code: 'const a = 1',
        totalLines: 3525,
      }),
    })

    const wrapper = mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })
    await nextTick()

    // Row 1: File path (dir + filename + line range) & copy path button
    const titleDir = document.querySelector('.code-preview-title-dir')
    expect(titleDir?.textContent).toBe('packages/coding-agent/src/')

    const filenameEl = document.querySelector('.code-preview-filename')
    expect(filenameEl?.textContent).toBe('agent-session-handler.ts')

    const lineRef = document.querySelector('.code-preview-line-ref')
    expect(lineRef?.textContent).toBe(':542-559')

    const copyPathBtn = document.querySelector('.code-preview-header-actions .copy-path-btn')
    expect(copyPathBtn).not.toBeNull()

    // Row 2: File type/size/lines & remaining action buttons
    const metaInfo = document.querySelector('.code-preview-meta-info')
    expect(metaInfo?.textContent).toContain('3525')

    const actionBtns = document.querySelectorAll('.code-preview-meta .code-preview-actions button')
    expect(actionBtns.length).toBeGreaterThanOrEqual(8)

    wrapper.unmount()
  })

  it('renders mobile sheet in two rows with path and copy button on row 1, meta and tools on row 2', async () => {
    const preview = createMockPreviewController({
      mode: ref('sheet'),
      target: ref({
        filePath: 'packages/agent/src/agent-loop.ts',
        lineStart: 179,
      }),
      fileContent: ref({
        content: 'console.log("hello")',
        size: 22800,
      }),
      slicedCode: ref({
        code: 'console.log("hello")',
        totalLines: 804,
      }),
    })

    const wrapper = mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })
    await nextTick()

    // Mobile Row 1
    const row1 = document.querySelector('.code-preview-sheet-row1')
    expect(row1).not.toBeNull()
    const pathBox = row1?.querySelector('.code-preview-sheet-path-box')
    expect(pathBox).not.toBeNull()
    const titleFile = pathBox?.querySelector('.code-preview-sheet-file')
    expect(titleFile).not.toBeNull()
    expect(titleFile?.querySelector('.code-preview-sheet-filename')?.textContent).toBe('agent-loop.ts')
    expect(titleFile?.querySelector('.code-preview-line-ref')?.textContent).toBe(':179')

    const copyBtn = row1?.querySelector('.copy-path-btn')
    expect(copyBtn).not.toBeNull()

    // Mobile Row 2
    const row2 = document.querySelector('.code-preview-sheet-row2')
    expect(row2).not.toBeNull()
    const metaInfo = row2?.querySelector('.code-preview-sheet-meta-info')
    expect(metaInfo?.textContent).toContain('804')

    const tools = row2?.querySelectorAll('.code-preview-sheet-tools button')
    expect(tools?.length).toBe(4)
    // Ordered for left-hand thumb ergonomics: Search, Wrap, Copy Code, Reveal in Tree
    expect(tools?.[0]?.getAttribute('aria-label') || tools?.[0]?.getAttribute('title')).toContain('Find')
    expect(tools?.[1]?.getAttribute('aria-label') || tools?.[1]?.getAttribute('title')).toMatch(/wrap/i)
    expect(tools?.[2]?.getAttribute('aria-label') || tools?.[2]?.getAttribute('title')).toMatch(/copy/i)
    expect(tools?.[3]?.getAttribute('aria-label') || tools?.[3]?.getAttribute('title')).toContain('Reveal in file tree')

    // Bottom Sheet Footer has collapse button
    const footer = document.querySelector('.code-preview-sheet-footer')
    const collapseBtn = footer?.querySelector('.collapse-btn')
    expect(collapseBtn).not.toBeNull()

    wrapper.unmount()
  })

  it('guarantees filename and line range are preserved atomically in code-preview-title-file', async () => {
    const preview = createMockPreviewController({
      target: ref({
        filePath: 'packages/deeply/nested/core/services/session/orchestrator/runner/controllers/super-long-module-name-handler.ts',
        lineStart: 120,
        lineEnd: 155,
      }),
    })

    const wrapper = mount(CodeLinkPreview, {
      props: { preview },
      global: { plugins: [i18n] },
    })
    await nextTick()

    const titlePath = document.querySelector('.code-preview-title-path')
    expect(titlePath).not.toBeNull()

    const dirEl = titlePath?.querySelector('.code-preview-title-dir')
    expect(dirEl?.textContent).toContain('packages/deeply/nested/')

    const titleFile = titlePath?.querySelector('.code-preview-title-file')
    expect(titleFile).not.toBeNull()

    const filenameEl = titleFile?.querySelector('.code-preview-filename')
    expect(filenameEl?.textContent).toBe('super-long-module-name-handler.ts')

    const lineRefEl = titleFile?.querySelector('.code-preview-line-ref')
    expect(lineRefEl?.textContent).toBe(':120-155')

    wrapper.unmount()
  })

  it('supports left-hand optimized mobile sheet layout: click-to-copy path on row1, tool order on row2, collapse & refresh in footer', async () => {
    const writeTextMock = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, {
      clipboard: { writeText: writeTextMock },
    })
    const switchTabMock = vi.fn()
    const loadFilesSpy = vi.spyOn(store, 'loadFiles').mockResolvedValue(undefined as any)

    const preview = createMockPreviewController({
      mode: ref('sheet'),
      target: ref({
        filePath: 'packages/agent/src/types.ts',
        lineStart: 415,
        lineEnd: 420,
      }),
      slicedCode: ref({
        code: 'export interface AgentContext { ... }',
        totalLines: 447,
      }),
      refresh: vi.fn(),
      close: vi.fn(),
    })

    const wrapper = mount(CodeLinkPreview, {
      props: { preview },
      global: {
        plugins: [i18n],
        provide: { switchTab: switchTabMock },
      },
    })
    await nextTick()

    // 1. Row 1: Click entire row1 / path-box to copy path
    const row1 = document.querySelector('.code-preview-sheet-row1') as HTMLElement
    expect(row1).not.toBeNull()
    row1.click()
    await nextTick()
    expect(writeTextMock).toHaveBeenCalledWith('packages/agent/src/types.ts:415-420')

    // 2. Row 2: Tools ordered for left-hand comfort: Search -> Wrap -> Copy Code -> Reveal in Tree
    const row2 = document.querySelector('.code-preview-sheet-row2')
    const toolBtns = row2?.querySelectorAll('.code-preview-sheet-tools button')
    expect(toolBtns?.length).toBe(4)

    // Tool 0: Search
    expect(toolBtns?.[0]?.getAttribute('aria-label') || toolBtns?.[0]?.getAttribute('title')).toContain('Find')
    ;(toolBtns?.[0] as HTMLElement).click()
    await nextTick()
    expect(document.querySelector('.code-preview-search-bar')).not.toBeNull()

    // Tool 1: Wrap toggle
    expect(toolBtns?.[1]?.getAttribute('aria-label') || toolBtns?.[1]?.getAttribute('title')).toMatch(/wrap/i)

    // Tool 2: Copy Code
    expect(toolBtns?.[2]?.getAttribute('aria-label') || toolBtns?.[2]?.getAttribute('title')).toMatch(/copy/i)
    ;(toolBtns?.[2] as HTMLElement).click()
    await nextTick()
    expect(writeTextMock).toHaveBeenCalledWith('export interface AgentContext { ... }')

    // Tool 3: Reveal in tree (pushed to top-right to avoid accidental taps)
    expect(toolBtns?.[3]?.getAttribute('aria-label') || toolBtns?.[3]?.getAttribute('title')).toContain('Reveal in file tree')
    ;(toolBtns?.[3] as HTMLElement).click()
    await nextTick()
    expect(loadFilesSpy).toHaveBeenCalledWith('packages/agent/src')
    expect(switchTabMock).toHaveBeenCalledWith('browse')

    // 3. Footer: Leftmost is collapse, followed by refresh
    const footer = document.querySelector('.code-preview-sheet-footer')
    expect(footer).not.toBeNull()

    const collapseBtn = footer?.querySelector('.collapse-btn') as HTMLElement
    expect(collapseBtn).not.toBeNull()
    collapseBtn.click()
    expect(preview.close).toHaveBeenCalled()

    const refreshBtn = footer?.querySelector('.refresh-btn') as HTMLElement
    expect(refreshBtn).not.toBeNull()
    refreshBtn.click()
    expect(preview.refresh).toHaveBeenCalled()

    wrapper.unmount()
  })
})
