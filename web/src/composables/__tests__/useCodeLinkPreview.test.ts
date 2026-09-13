import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { nextTick, ref } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { useCodeLinkPreview, handleVerifiedFilePathClick } from '@/composables/useCodeLinkPreview'
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

// Real file-type lookup drives the markdown detection in the composable.
// getFileType is pure (reads the built-in extension table) so no mock needed.

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

  it('honors a caller-supplied enabled gate over the global config', async () => {
    // The file manager's preview mode must work independently of the global
    // markdown-link-preview switch.
    reactiveLocalConfig.markdownCodeLinkPreview = false
    const gate = ref(false)
    const preview = useCodeLinkPreview({ enabled: gate })
    expect(preview.enabled.value).toBe(false)

    gate.value = true
    await nextTick()
    expect(preview.enabled.value).toBe(true)

    gate.value = false
    await nextTick()
    expect(preview.enabled.value).toBe(false)
  })

  it('exposes outsideClickIgnoreSelector from options', () => {
    const withSelector = useCodeLinkPreview({ outsideClickIgnoreSelector: '.file-item' })
    expect(withSelector.outsideClickIgnoreSelector).toBe('.file-item')

    const without = useCodeLinkPreview()
    expect(without.outsideClickIgnoreSelector).toBe('')
  })

  it('closes an open preview when the caller gate turns off', async () => {
    const gate = ref(true)
    mockApiGet.mockResolvedValueOnce({
      content: 'const a = 1',
      name: 'a.ts',
      path: 'a.ts',
      supported: true,
      size: 12,
    })
    const preview = useCodeLinkPreview({ enabled: gate })
    preview.showPreview({ filePath: 'a.ts' })
    await vi.runAllTicks()
    expect(preview.visible.value).toBe(true)

    gate.value = false
    await nextTick()
    expect(preview.visible.value).toBe(false)
  })

  it('detects media targets and short-circuits the JSON fetch', async () => {
    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'assets/logo.png' })
    await vi.runAllTicks()

    expect(preview.isImageTarget.value).toBe(true)
    expect(preview.isMediaTarget.value).toBe(true)
    // No /api/file call — media is served as raw bytes by /api/local-file/.
    expect(mockApiGet).not.toHaveBeenCalled()
    expect(preview.status.value).toBe('ready')
    expect(preview.errorCode.value).toBeNull()
  })

  it.each([
    ['diagram.svg', 'isImageTarget'],
    ['clip.mp4', 'isVideoTarget'],
    ['voice.mp3', 'isAudioTarget'],
    ['report.pdf', 'isPdfTarget'],
  ])('classifies %s as a media target (%s)', async (filePath, flag) => {
    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath })
    await vi.runAllTicks()

    expect(preview.isMediaTarget.value).toBe(true)
    expect((preview as unknown as Record<string, { value: boolean }>)[flag].value).toBe(true)
    expect(mockApiGet).not.toHaveBeenCalled()
  })

  it('does not treat a plain text file as a media target', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: 'const a = 1',
      name: 'a.ts',
      path: 'a.ts',
      supported: true,
      size: 12,
    })
    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'a.ts' })
    await vi.runAllTicks()

    expect(preview.isMediaTarget.value).toBe(false)
    expect(mockApiGet).toHaveBeenCalledTimes(1)
  })

  it('refresh bumps the media nonce instead of re-fetching for a media target', async () => {
    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'assets/logo.png' })
    await vi.runAllTicks()

    const before = preview.mediaRefreshNonce.value
    preview.refresh()
    expect(preview.mediaRefreshNonce.value).toBe(before + 1)
    // Media has no JSON body to re-fetch.
    expect(mockApiGet).not.toHaveBeenCalled()
  })

  it('refresh still re-fetches for a text target', async () => {
    mockApiGet.mockResolvedValue({
      content: 'const a = 1',
      name: 'a.ts',
      path: 'a.ts',
      supported: true,
      size: 12,
    })
    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'a.ts' })
    await vi.runAllTicks()
    mockApiGet.mockClear()

    preview.refresh()
    await vi.runAllTicks()
    expect(mockApiGet).toHaveBeenCalledTimes(1)
  })

  it('resets the media nonce when a new target opens', async () => {
    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'a.png' })
    await vi.runAllTicks()
    preview.refresh()
    expect(preview.mediaRefreshNonce.value).toBe(1)

    preview.showPreview({ filePath: 'b.png' })
    await vi.runAllTicks()
    expect(preview.mediaRefreshNonce.value).toBe(0)
  })

  it('clears the media nonce to 0 on close', async () => {
    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'a.png' })
    await vi.runAllTicks()
    preview.refresh()

    preview.close()
    expect(preview.mediaRefreshNonce.value).toBe(0)
  })

  it('defaults a line-less Markdown file to the rendered document view', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: '# Title\n\nSome **markdown**.',
      name: 'README.md',
      path: 'README.md',
      supported: true,
      size: 40,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'README.md' })
    await vi.runAllTicks()

    expect(preview.isMarkdown.value).toBe(true)
    expect(preview.hasExplicitLineRange.value).toBe(false)
    expect(preview.canRenderMarkdown.value).toBe(true)
    expect(preview.effectiveRenderMode.value).toBe('rendered')
  })

  it('keeps a line-annotated Markdown file on the source slice view by default', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: '# Title\n\nBody.',
      name: 'README.md',
      path: 'README.md',
      supported: true,
      size: 40,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'README.md', lineStart: 3, lineEnd: 5 })
    await vi.runAllTicks()

    expect(preview.isMarkdown.value).toBe(true)
    expect(preview.hasExplicitLineRange.value).toBe(true)
    // A Markdown file stays render-capable even with a line annotation — only
    // the DEFAULT view is the code slice so the user can pinpoint the lines.
    expect(preview.canRenderMarkdown.value).toBe(true)
    expect(preview.effectiveRenderMode.value).toBe('source')
  })

  it('lets a line-annotated Markdown file switch to the rendered view', async () => {
    mockApiGet.mockResolvedValue({
      content: '# Title\n\nBody.',
      name: 'README.md',
      path: 'README.md',
      supported: true,
      size: 40,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'README.md', lineStart: 3, lineEnd: 5 })
    await vi.runAllTicks()

    expect(preview.effectiveRenderMode.value).toBe('source')
    preview.toggleRenderMode()
    expect(preview.effectiveRenderMode.value).toBe('rendered')
    preview.toggleRenderMode()
    expect(preview.effectiveRenderMode.value).toBe('source')
  })

  it('keeps non-Markdown files on the source slice view', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: 'const x = 1',
      name: 'main.ts',
      path: 'main.ts',
      supported: true,
      size: 20,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'main.ts' })
    await vi.runAllTicks()

    expect(preview.isMarkdown.value).toBe(false)
    expect(preview.canRenderMarkdown.value).toBe(false)
    expect(preview.effectiveRenderMode.value).toBe('source')
  })

  it('toggleRenderMode switches a renderable Markdown target between views', async () => {
    mockApiGet.mockResolvedValue({
      content: '# Title\n\nBody.',
      name: 'README.md',
      path: 'README.md',
      supported: true,
      size: 40,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'README.md' })
    await vi.runAllTicks()

    expect(preview.effectiveRenderMode.value).toBe('rendered')
    preview.toggleRenderMode()
    expect(preview.renderMode.value).toBe('source')
    expect(preview.effectiveRenderMode.value).toBe('source')
    preview.toggleRenderMode()
    expect(preview.effectiveRenderMode.value).toBe('rendered')
  })

  it('toggleRenderMode is a no-op when the target cannot render Markdown', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: 'code',
      name: 'main.ts',
      path: 'main.ts',
      supported: true,
      size: 20,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'main.ts', lineStart: 1 })
    await vi.runAllTicks()

    preview.toggleRenderMode()
    expect(preview.effectiveRenderMode.value).toBe('source')
  })

  it('resets renderMode to source when the preview closes', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: '# Title',
      name: 'README.md',
      path: 'README.md',
      supported: true,
      size: 20,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'README.md' })
    await vi.runAllTicks()
    expect(preview.effectiveRenderMode.value).toBe('rendered')

    preview.close()
    expect(preview.visible.value).toBe(false)
    expect(preview.renderMode.value).toBe('source')
    expect(preview.effectiveRenderMode.value).toBe('source')
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

  it('keeps a click-opened preview open after the pointer leaves the path', async () => {
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

    // Moving the pointer off the path (and never returning) must NOT dismiss
    // the card — click-opened previews persist until explicitly closed.
    anchor.dispatchEvent(new MouseEvent('mouseout', { bubbles: true }))
    vi.advanceTimersByTime(2000)
    expect(preview.visible.value).toBe(true)

    // Explicit dismiss still works.
    preview.close()
    expect(preview.visible.value).toBe(false)
  })

  it('keeps a click-opened preview open after the pointer leaves the card', async () => {
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

    // Pointer enters and later leaves the card — neither schedules a close.
    preview.onCardPointerEnter()
    preview.onCardPointerLeave()
    vi.advanceTimersByTime(2000)
    expect(preview.visible.value).toBe(true)

    preview.close()
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
    // A non-media binary file (images/video/audio/PDF now render a media body
    // and never reach the JSON fetch), so the binary rejection path stays covered.
    mockApiGet.mockResolvedValueOnce({
      content: '',
      name: 'archive.bin',
      path: 'archive.bin',
      supported: false,
      isBinary: true,
      size: 500,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'archive.bin' })
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

  it('keeps preview open across card hover / focus events (no auto-close)', async () => {
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

    // Pointer / focus enter-leave cycles used to drive a transient close
    // timer; click-opened cards now persist until an explicit dismiss.
    preview.onCardPointerEnter()
    preview.onCardPointerLeave()
    preview.onCardFocusIn()
    preview.onCardFocusOut(new FocusEvent('focusout'))
    vi.advanceTimersByTime(2000)
    expect(preview.visible.value).toBe(true)

    preview.close()
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

  it('opens full file and closes preview with undefined source when none provided', async () => {
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
    expect(mockOpenFilePath).toHaveBeenCalledWith('test.ts', 5, 10, undefined)
    expect(preview.visible.value).toBe(false)
  })

  it('opens full file with custom source when provided', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: 'hello world',
      name: 'test.ts',
      path: 'test.ts',
      supported: true,
      size: 11,
    })

    const preview = useCodeLinkPreview({ source: 'chat' })
    preview.showPreview({ filePath: 'test.ts', lineStart: 5, lineEnd: 10 })
    await vi.runAllTicks()
    await Promise.resolve()

    preview.openFull()
    expect(mockOpenFilePath).toHaveBeenCalledWith('test.ts', 5, 10, 'chat')
    expect(preview.visible.value).toBe(false)
  })

  it('opens full file with file source when explicitly configured', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: 'hello world',
      name: 'test.ts',
      path: 'test.ts',
      supported: true,
      size: 11,
    })

    const preview = useCodeLinkPreview({ source: 'file' })
    preview.showPreview({ filePath: 'test.ts', lineStart: 5, lineEnd: 10 })
    await vi.runAllTicks()
    await Promise.resolve()

    preview.openFull()
    expect(mockOpenFilePath).toHaveBeenCalledWith('test.ts', 5, 10, 'file')
    expect(preview.visible.value).toBe(false)
  })

  describe('touch device & emulated mouse protection', () => {
    afterEach(() => {
      _resetPlatformForTest()
    })

    it('never opens a preview from focusin alone (keyboard/touch focus)', () => {
      _setIsPCForTest(false)
      const preview = useCodeLinkPreview()
      const anchor = document.createElement('span')
      anchor.className = 'chat-file-path'
      anchor.setAttribute('data-file-path', 'src/hello.ts')
      anchor.setAttribute('data-path-type', 'file')

      anchor.dispatchEvent(new FocusEvent('focusin', { bubbles: true }))
      expect(preview.visible.value).toBe(false)

      // A preview only opens via an explicit click/tap.
      preview.handleClick({ target: anchor, ctrlKey: false, preventDefault: vi.fn(), stopPropagation: vi.fn() } as unknown as MouseEvent)
      expect(preview.visible.value).toBe(true)
    })

    it('does not open a preview on desktop keyboard focus alone', () => {
      _setIsPCForTest(true)
      const preview = useCodeLinkPreview()
      const anchor = document.createElement('span')
      anchor.className = 'chat-file-path'
      anchor.setAttribute('data-file-path', 'src/hello.ts')
      anchor.setAttribute('data-path-type', 'file')

      anchor.dispatchEvent(new FocusEvent('focusin', { bubbles: true }))
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

describe('handleVerifiedFilePathClick (shared container interceptor)', () => {
  function makePreview(overrides: Partial<{ enabled: boolean; touch: boolean; handleClick: ReturnType<typeof vi.fn> }> = {}) {
    const handleClick = overrides.handleClick ?? vi.fn()
    const preview = {
      enabled: { value: overrides.enabled ?? true },
      isTouchDevice: () => overrides.touch ?? false,
      handleClick,
    }
    return { preview, handleClick }
  }

  function makeElement(pathType: string | null, extraClass = ''): HTMLElement {
    const el = document.createElement(extraClass.includes('open-btn') ? 'button' : 'span')
    el.className = extraClass || (pathType ? 'chat-file-path' : '')
    if (pathType !== null) el.setAttribute('data-file-path', '/repo/src/main.ts')
    if (pathType !== null) el.setAttribute('data-path-type', pathType)
    return el
  }

  function makeEvent(target: HTMLElement, ctrl = false) {
    return {
      target,
      ctrlKey: ctrl,
      metaKey: false,
    } as unknown as MouseEvent
  }

  it('returns false when the preview is disabled', () => {
    const { preview, handleClick } = makePreview({ enabled: false })
    expect(handleVerifiedFilePathClick(makeEvent(makeElement('file')), preview as never)).toBe(false)
    expect(handleClick).not.toHaveBeenCalled()
  })

  it('desktop click on verified file path text opens a transient preview', () => {
    const { preview, handleClick } = makePreview()
    const handled = handleVerifiedFilePathClick(makeEvent(makeElement('file')), preview as never)
    expect(handled).toBe(true)
    expect(handleClick).toHaveBeenCalledTimes(1)
  })

  it('desktop modifier-click on the open button opens the preview', () => {
    const { preview, handleClick } = makePreview()
    const el = makeElement('file', 'chat-file-open-btn')
    const handled = handleVerifiedFilePathClick(makeEvent(el, true), preview as never)
    expect(handled).toBe(true)
    expect(handleClick).toHaveBeenCalledTimes(1)
  })

  it('plain desktop click on the open button falls through (returns false)', () => {
    const { preview, handleClick } = makePreview()
    const el = makeElement('file', 'chat-file-open-btn')
    const handled = handleVerifiedFilePathClick(makeEvent(el), preview as never)
    expect(handled).toBe(false)
    expect(handleClick).not.toHaveBeenCalled()
  })

  it('touch tap on verified file path text opens the sheet preview', () => {
    const { preview, handleClick } = makePreview({ touch: true })
    const handled = handleVerifiedFilePathClick(makeEvent(makeElement('file')), preview as never)
    expect(handled).toBe(true)
    expect(handleClick).toHaveBeenCalledTimes(1)
  })

  it('directories and unverified paths fall through (return false)', () => {
    for (const pathType of ['dir', null]) {
      const { preview, handleClick } = makePreview()
      const handled = handleVerifiedFilePathClick(makeEvent(makeElement(pathType)), preview as never)
      expect(handled).toBe(false)
      expect(handleClick).not.toHaveBeenCalled()
    }
  })
})

describe('useCodeLinkPreview — docked mode', () => {
  it('exposes isDocked only while a docked preview is open', async () => {
    mockApiGet.mockResolvedValue({
      content: 'const a = 1',
      name: 'a.ts',
      path: 'a.ts',
      supported: true,
      size: 12,
    })
    const preview = useCodeLinkPreview()
    expect(preview.isDocked.value).toBe(false)

    preview.showPreview({ filePath: 'a.ts' }, 'docked')
    await flushPromises()
    expect(preview.mode.value).toBe('docked')
    expect(preview.isDocked.value).toBe(true)

    preview.close()
    expect(preview.isDocked.value).toBe(false)
  })

  it('does not compute a float placement for a docked preview', async () => {
    mockApiGet.mockResolvedValue({
      content: 'const a = 1',
      name: 'a.ts',
      path: 'a.ts',
      supported: true,
      size: 12,
    })
    const anchor = document.createElement('div')
    anchor.getBoundingClientRect = () => ({ left: 10, top: 20, width: 100, height: 10, bottom: 30, right: 110, x: 10, y: 20, toJSON() {} })
    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'a.ts', anchorEl: anchor }, 'docked')
    await flushPromises()

    // Docked panes are laid out by the caller's split — no float placement.
    expect(preview.placement.value).toBeNull()
  })

  it('pin/unpin are no-ops in docked mode', async () => {
    mockApiGet.mockResolvedValue({
      content: 'const a = 1',
      name: 'a.ts',
      path: 'a.ts',
      supported: true,
      size: 12,
    })
    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'a.ts' }, 'docked')
    await flushPromises()

    preview.pin()
    expect(preview.mode.value).toBe('docked')
    preview.togglePin()
    expect(preview.mode.value).toBe('docked')
  })
})
