import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { nextTick } from 'vue'
import { useCodeLinkPreview } from '@/composables/useCodeLinkPreview'
import { previewCache } from '@/utils/codeLinkPreview'
import { _setIsPCForTest, _resetPlatformForTest } from '@/composables/usePlatformDetect'

const { reactiveStore, reactiveLocalConfig } = await vi.hoisted(async () => {
  const { reactive } = await import('vue')
  return {
    reactiveStore: reactive<{ state: { projectRoot: string; currentFile: { path: string } | null } }>({
      state: { projectRoot: '/home/user/project', currentFile: { path: 'README.md' } },
    }),
    reactiveLocalConfig: reactive<Record<string, any>>({
      markdownCodeLinkPreview: true,
    }),
  }
})

// Mock store
vi.mock('@/stores/app', () => ({
  store: reactiveStore,
}))

// Mock settings config
vi.mock('@/composables/useSettingsConfig', () => ({
  useSettingsConfig: () => ({
    localConfig: reactiveLocalConfig,
  }),
  toFixedCSS: (v: number) => v,
  getZoomedViewport: () => ({ width: 1024, height: 768 }),
}))

// Mock apiGet
const mockApiGet = vi.fn()
vi.mock('@/utils/api', () => ({
  apiGet: (...args: any[]) => mockApiGet(...args),
}))

// Mock appLog
vi.mock('@/utils/appLog', () => ({
  appLog: {
    d: vi.fn(),
    i: vi.fn(),
    w: vi.fn(),
    e: vi.fn(),
  },
}))

// Mock openFilePath
const mockOpenFilePath = vi.fn()
vi.mock('@/composables/useFilePathAnnotation', () => ({
  openFilePath: (...args: any[]) => mockOpenFilePath(...args),
}))

describe('useCodeLinkPreview', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    previewCache.clear()
    reactiveLocalConfig.markdownCodeLinkPreview = true
    reactiveStore.state.currentFile = { path: 'README.md' }
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('exposes enabled computed from localConfig', async () => {
    const preview = useCodeLinkPreview()
    expect(preview.enabled.value).toBe(true)

    reactiveLocalConfig.markdownCodeLinkPreview = false
    await nextTick()
    expect(preview.enabled.value).toBe(false)
  })

  it('opens preview on desktop click without hover delay', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: 'function hello() {\n  return 42\n}\n',
      name: 'hello.ts',
      path: 'src/hello.ts',
      supported: true,
      size: 40,
    })

    const preview = useCodeLinkPreview()
    const anchor = document.createElement('span')
    anchor.className = 'chat-file-path'
    anchor.setAttribute('data-file-path', 'src/hello.ts')
    anchor.setAttribute('data-path-type', 'file')
    anchor.setAttribute('data-line-start', '1')
    anchor.setAttribute('data-line-end', '3')

    preview.handleClick({ target: anchor, relatedTarget: null, preventDefault: vi.fn(), stopPropagation: vi.fn(), ctrlKey: false, metaKey: false } as unknown as MouseEvent)
    expect(preview.visible.value).toBe(true)
    expect(preview.status.value).toBe('loading')

    // Wait for promise resolution
    await vi.runAllTicks()
    expect(preview.status.value).toBe('ready')
    expect(preview.slicedCode.value?.code).toContain('function hello()')
  })

  it('keeps only one active preview across composable instances', () => {
    const first = useCodeLinkPreview()
    const second = useCodeLinkPreview()
    const firstAnchor = document.createElement('span')
    firstAnchor.className = 'chat-file-path'
    firstAnchor.setAttribute('data-file-path', 'first.ts')
    firstAnchor.setAttribute('data-path-type', 'file')
    const secondAnchor = document.createElement('span')
    secondAnchor.className = 'chat-file-path'
    secondAnchor.setAttribute('data-file-path', 'second.ts')
    secondAnchor.setAttribute('data-path-type', 'file')

    first.showPreview({ filePath: 'first.ts', anchorEl: firstAnchor })
    expect(first.visible.value).toBe(true)
    second.showPreview({ filePath: 'second.ts', anchorEl: secondAnchor })

    expect(first.visible.value).toBe(false)
    expect(second.visible.value).toBe(true)
  })

  it('keeps transient preview open when pointer re-enters the target path', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: 'test content',
      name: 'test.ts',
      path: 'src/test.ts',
      supported: true,
      size: 12,
    })

    const preview = useCodeLinkPreview()
    const anchor = document.createElement('span')
    anchor.className = 'chat-file-path'
    anchor.setAttribute('data-file-path', 'src/test.ts')
    anchor.setAttribute('data-path-type', 'file')

    preview.showPreview({ filePath: 'src/test.ts', anchorEl: anchor })
    await vi.runAllTicks()
    expect(preview.visible.value).toBe(true)

    // Mouse leaves the anchor: a 200ms close is scheduled
    preview.handleMouseOut({ target: anchor, relatedTarget: null } as unknown as MouseEvent)
    vi.advanceTimersByTime(100)

    // Pointer re-enters the anchor before the timer fires -> close cancelled
    preview.handleMouseOver({ target: anchor, relatedTarget: document.body } as unknown as MouseEvent)
    vi.advanceTimersByTime(300)
    expect(preview.visible.value).toBe(true)

    // Leaving both target and card still closes after the grace period
    preview.handleMouseOut({ target: anchor, relatedTarget: null } as unknown as MouseEvent)
    vi.advanceTimersByTime(300)
    expect(preview.visible.value).toBe(false)
  })

  it('keeps preview open when moving pointer to card, closes 200ms after leaving both', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: 'test content',
      name: 'test.ts',
      path: 'src/test.ts',
      supported: true,
      size: 12,
    })

    const preview = useCodeLinkPreview()
    const anchor = document.createElement('span')
    anchor.className = 'chat-file-path'
    anchor.setAttribute('data-file-path', 'src/test.ts')
    anchor.setAttribute('data-path-type', 'file')

    preview.showPreview({ filePath: 'src/test.ts', anchorEl: anchor })
    await vi.runAllTicks()
    expect(preview.visible.value).toBe(true)

    // Mouse leaves anchor
    preview.handleMouseOut({ target: anchor, relatedTarget: null } as unknown as MouseEvent)
    // But within 100ms enters card
    vi.advanceTimersByTime(100)
    preview.onCardPointerEnter()

    // 200ms pass while in card -> should still be visible
    vi.advanceTimersByTime(300)
    expect(preview.visible.value).toBe(true)

    // Mouse leaves card
    preview.onCardPointerLeave()
    vi.advanceTimersByTime(190)
    expect(preview.visible.value).toBe(true)

    vi.advanceTimersByTime(20)
    expect(preview.visible.value).toBe(false)
  })

  it('prevents race conditions: A then B, A resolves late, B wins', async () => {
    let resolveA!: (val: any) => void
    const promiseA = new Promise((resolve) => {
      resolveA = resolve
    })
    mockApiGet.mockReturnValueOnce(promiseA)

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'fileA.ts' })
    expect(preview.target.value?.filePath).toBe('fileA.ts')

    // Immediately trigger fileB
    mockApiGet.mockResolvedValueOnce({
      content: 'content B',
      name: 'fileB.ts',
      path: 'fileB.ts',
      supported: true,
      size: 9,
    })
    preview.showPreview({ filePath: 'fileB.ts' })
    expect(preview.target.value?.filePath).toBe('fileB.ts')

    await vi.runAllTicks()
    expect(preview.slicedCode.value?.code).toBe('content B')

    // Now A resolves late
    resolveA({
      content: 'content A',
      name: 'fileA.ts',
      path: 'fileA.ts',
      supported: true,
      size: 9,
    })
    await vi.runAllTicks()

    // B should still be the rendered content
    expect(preview.target.value?.filePath).toBe('fileB.ts')
    expect(preview.slicedCode.value?.code).toBe('content B')
  })

  it('keeps pinned target until replaced with Ctrl+Click', async () => {
    mockApiGet.mockResolvedValue({
      content: 'file content',
      name: 'file.ts',
      path: 'file.ts',
      supported: true,
      size: 10,
    })

    const preview = useCodeLinkPreview()
    const anchorA = document.createElement('span')
    anchorA.className = 'chat-file-path'
    anchorA.setAttribute('data-file-path', 'fileA.ts')
    anchorA.setAttribute('data-path-type', 'file')

    const anchorB = document.createElement('span')
    anchorB.className = 'chat-file-path'
    anchorB.setAttribute('data-file-path', 'fileB.ts')
    anchorB.setAttribute('data-path-type', 'file')

    preview.showPreview({ filePath: 'fileA.ts', anchorEl: anchorA })
    preview.pin()
    expect(preview.isPinned.value).toBe(true)
    expect(preview.target.value?.filePath).toBe('fileA.ts')

    // Ctrl + Click on B
    preview.handleClick({
      target: anchorB,
      ctrlKey: true,
      preventDefault: vi.fn(),
      stopPropagation: vi.fn(),
    } as unknown as MouseEvent)

    expect(preview.target.value?.filePath).toBe('fileB.ts')
    expect(preview.isPinned.value).toBe(true)
  })

  it('keeps the pinned card placement when clicking another link', () => {
    const preview = useCodeLinkPreview()
    const anchorA = document.createElement('span')
    anchorA.className = 'chat-file-path'
    anchorA.setAttribute('data-file-path', 'fileA.ts')
    anchorA.setAttribute('data-path-type', 'file')
    anchorA.getBoundingClientRect = () => ({ left: 20, top: 30, right: 80, bottom: 50, width: 60, height: 20 } as DOMRect)

    const anchorB = document.createElement('span')
    anchorB.className = 'chat-file-path'
    anchorB.setAttribute('data-file-path', 'fileB.ts')
    anchorB.setAttribute('data-path-type', 'file')
    anchorB.getBoundingClientRect = () => ({ left: 700, top: 600, right: 760, bottom: 620, width: 60, height: 20 } as DOMRect)

    preview.showPreview({ filePath: 'fileA.ts', anchorEl: anchorA })
    const initialPlacement = preview.placement.value
    preview.pin()
    preview.showPreview({ filePath: 'fileB.ts', anchorEl: anchorB })

    expect(preview.target.value?.filePath).toBe('fileB.ts')
    expect(preview.isPinned.value).toBe(true)
    expect(preview.placement.value).toEqual(initialPlacement)
  })

  it('uses LRU cache and bypasses on refresh', async () => {
    mockApiGet.mockResolvedValue({
      content: 'cached content',
      name: 'file.ts',
      path: 'file.ts',
      supported: true,
      size: 14,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'file.ts' })
    await vi.runAllTicks()
    expect(mockApiGet).toHaveBeenCalledTimes(1)

    // Second showPreview for same file -> hit cache, no network call
    preview.showPreview({ filePath: 'file.ts' })
    await vi.runAllTicks()
    expect(mockApiGet).toHaveBeenCalledTimes(1)

    // Refresh -> forces network call
    preview.refresh()
    await vi.runAllTicks()
    expect(mockApiGet).toHaveBeenCalledTimes(2)
  })

  it('closes preview and clears cache when enabled switch is turned off', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: 'some code',
      name: 'file.ts',
      path: 'file.ts',
      supported: true,
      size: 9,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'file.ts' })
    await vi.runAllTicks()
    expect(preview.visible.value).toBe(true)
    expect(previewCache.size).toBe(1)

    // Turn off setting
    reactiveLocalConfig.markdownCodeLinkPreview = false
    await nextTick()

    expect(preview.visible.value).toBe(false)
    expect(previewCache.size).toBe(0)
  })

  it('closes preview when Markdown file in store changes', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: 'some code',
      name: 'file.ts',
      path: 'file.ts',
      supported: true,
      size: 9,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'file.ts' })
    await vi.runAllTicks()
    expect(preview.visible.value).toBe(true)

    // User navigates to another file
    reactiveStore.state.currentFile = { path: 'docs/guide.md' }
    await nextTick()

    expect(preview.visible.value).toBe(false)
  })

  it('does not preview directories (data-path-type="dir")', () => {
    const preview = useCodeLinkPreview()
    const dirAnchor = document.createElement('span')
    dirAnchor.className = 'chat-file-path'
    dirAnchor.setAttribute('data-file-path', 'src/components')
    dirAnchor.setAttribute('data-path-type', 'dir')

    preview.handleClick({ target: dirAnchor, preventDefault: vi.fn(), stopPropagation: vi.fn() } as unknown as MouseEvent)
    expect(preview.visible.value).toBe(false)
    expect(mockApiGet).not.toHaveBeenCalled()
  })

  it('maps binary and too-large error states properly', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: '',
      name: 'image.png',
      path: 'image.png',
      supported: false,
      isBinary: true,
      size: 500,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'image.png' })
    await vi.runAllTicks()

    expect(preview.status.value).toBe('error')
    expect(preview.errorCode.value).toBe('binary')
  })

  it('classifies HTTP errors by status instead of localized message text', async () => {
    mockApiGet.mockRejectedValueOnce({ message: 'Datei nicht gefunden', status: 404 })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'missing.ts' })
    await vi.runAllTicks()

    expect(preview.errorCode.value).toBe('not-found')
  })

  it('supports context expansion and shrinking', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: Array.from({ length: 200 }, (_, i) => `line ${i + 1}`).join('\n'),
      name: 'code.ts',
      path: 'code.ts',
      supported: true,
      size: 500,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'code.ts', lineStart: 100, lineEnd: 100 })
    await vi.runAllTicks()
    await Promise.resolve()

    expect(preview.slicedCode.value?.startLine).toBe(70)
    expect(preview.slicedCode.value?.endLine).toBe(130)

    preview.expandContext()
    expect(preview.contextExpansion.value).toBe(1)
    expect(preview.slicedCode.value?.startLine).toBe(65)
    expect(preview.slicedCode.value?.endLine).toBe(135)

    preview.shrinkContext()
    expect(preview.contextExpansion.value).toBe(0)
    expect(preview.slicedCode.value?.startLine).toBe(70)

    // Shrinking past 0 does nothing
    preview.shrinkContext()
    expect(preview.contextExpansion.value).toBe(0)
  })

  it('supports directional expandAbove, expandBelow, expandToTop, and expandToBottom', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: Array.from({ length: 200 }, (_, i) => `line ${i + 1}`).join('\n'),
      name: 'code.ts',
      path: 'code.ts',
      supported: true,
      size: 500,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'code.ts', lineStart: 100, lineEnd: 100 })
    await vi.runAllTicks()
    await Promise.resolve()

    // Default context 30 lines: [70, 130]
    expect(preview.slicedCode.value?.startLine).toBe(70)
    expect(preview.slicedCode.value?.endLine).toBe(130)

    // Expand above by 10 lines -> startLine 60
    preview.expandAbove(10)
    expect(preview.slicedCode.value?.startLine).toBe(60)
    expect(preview.slicedCode.value?.endLine).toBe(130)

    // Expand below by 15 lines -> endLine 145
    preview.expandBelow(15)
    expect(preview.slicedCode.value?.startLine).toBe(60)
    expect(preview.slicedCode.value?.endLine).toBe(145)

    // Expand to top -> startLine 1
    preview.expandToTop()
    expect(preview.slicedCode.value?.startLine).toBe(1)

    // Expand to bottom -> endLine 200 (clamped by MAX_RENDER_LINES=200 from startLine 1)
    preview.expandToBottom()
    expect(preview.slicedCode.value?.endLine).toBe(200)
  })

  it('handles card hover and focus events', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: 'hello world',
      name: 'test.ts',
      path: 'test.ts',
      supported: true,
      size: 11,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'test.ts' })
    await vi.runAllTicks()
    await Promise.resolve()

    preview.onCardPointerEnter()
    preview.onCardPointerLeave()
    vi.advanceTimersByTime(100)
    expect(preview.visible.value).toBe(true)
    vi.advanceTimersByTime(150)
    expect(preview.visible.value).toBe(false)

    // Focus handling
    preview.showPreview({ filePath: 'test.ts' })
    await vi.runAllTicks()
    await Promise.resolve()
    preview.onCardFocusIn()
    preview.onCardFocusOut(new FocusEvent('focusout'))
    vi.advanceTimersByTime(250)
    expect(preview.visible.value).toBe(false)
  })

  it('supports unpinning and togglePin', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: 'hello world',
      name: 'test.ts',
      path: 'test.ts',
      supported: true,
      size: 11,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'test.ts' })
    await vi.runAllTicks()

    preview.togglePin()
    expect(preview.isPinned.value).toBe(true)

    preview.togglePin()
    expect(preview.isPinned.value).toBe(false)
  })

  it('opens full file and closes preview', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: 'hello world',
      name: 'test.ts',
      path: 'test.ts',
      supported: true,
      size: 11,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'test.ts', lineStart: 5, lineEnd: 10 })
    await vi.runAllTicks()
    await Promise.resolve()

    preview.openFull()
    expect(preview.visible.value).toBe(false)
  })

  describe('touch device & emulated mouse protection', () => {
    afterEach(() => {
      _resetPlatformForTest()
    })

    it('does not trigger focusin preview on touch devices', () => {
      _setIsPCForTest(false)
      const preview = useCodeLinkPreview()
      const anchor = document.createElement('span')
      anchor.className = 'chat-file-path'
      anchor.setAttribute('data-file-path', 'src/hello.ts')
      anchor.setAttribute('data-path-type', 'file')

      preview.handleFocusIn({ target: anchor } as unknown as FocusEvent)
      expect(preview.visible.value).toBe(false)
    })

    it('ignores synthetic mouseover/focusin within 1s after touchstart', () => {
      _setIsPCForTest(true)
      const preview = useCodeLinkPreview()
      const anchor = document.createElement('span')
      anchor.className = 'chat-file-path'
      anchor.setAttribute('data-file-path', 'src/hello.ts')
      anchor.setAttribute('data-path-type', 'file')

      preview.handleTouchStart()
      preview.handleFocusIn({ target: anchor } as unknown as FocusEvent)
      expect(preview.visible.value).toBe(false)
    })

    it('opens BottomSheet mode on touch device tap and prevents modifier pinned mode', () => {
      _setIsPCForTest(false)
      const preview = useCodeLinkPreview()
      const anchor = document.createElement('span')
      anchor.className = 'chat-file-path'
      anchor.setAttribute('data-file-path', 'src/hello.ts')
      anchor.setAttribute('data-path-type', 'file')

      const event = {
        target: anchor,
        ctrlKey: true,
        preventDefault: vi.fn(),
        stopPropagation: vi.fn(),
      } as unknown as MouseEvent

      preview.handleClick(event)
      expect(preview.mode.value).toBe('sheet')
      expect(preview.visible.value).toBe(true)
    })
  })
})
