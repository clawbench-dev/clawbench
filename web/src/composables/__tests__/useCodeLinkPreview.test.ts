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
    // No /api/file call — media is served as raw bytes by /api/fs/raw/.
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

  it('previews directories by listing them (data-path-type="dir")', async () => {
    mockApiGet.mockResolvedValueOnce({
      items: [
        { name: 'a.ts', type: 'file' },
        { name: 'nested', type: 'dir' },
      ],
    })
    const preview = useCodeLinkPreview()
    const dirAnchor = document.createElement('span')
    dirAnchor.className = 'chat-file-path'
    dirAnchor.setAttribute('data-file-path', 'src/components')
    dirAnchor.setAttribute('data-path-type', 'dir')

    preview.handleClick({ target: dirAnchor, preventDefault: vi.fn(), stopPropagation: vi.fn() } as unknown as MouseEvent)
    expect(preview.visible.value).toBe(true)
    expect(preview.isDirTarget.value).toBe(true)

    await vi.runAllTicks()
    await Promise.resolve()

    // Lists via /api/dir, never /api/file (a directory has no file content).
    const url = mockApiGet.mock.calls[0][0] as string
    expect(url).toContain('/api/dir?path=src%2Fcomponents')
    expect(preview.dirEntries.value.map(e => e.name)).toEqual(['a.ts', 'nested'])
  })

  it('lists a directory even when its name ends in a media/markdown extension', async () => {
    // `getFileType` is purely extension-based, so a DIRECTORY named `assets.png`
    // (or `docs.md`) looks like a media/markdown file. The directory check must
    // therefore win over the media branch, or the card would render a media
    // body / markdown toggle for a directory and never fetch its listing.
    mockApiGet.mockResolvedValueOnce({
      items: [{ name: 'logo.png', type: 'file' }],
    })
    const preview = useCodeLinkPreview()
    const dirAnchor = document.createElement('span')
    dirAnchor.className = 'chat-file-path'
    dirAnchor.setAttribute('data-file-path', 'assets.png')
    dirAnchor.setAttribute('data-path-type', 'dir')

    preview.handleClick({ target: dirAnchor, preventDefault: vi.fn(), stopPropagation: vi.fn() } as unknown as MouseEvent)

    await vi.runAllTicks()
    await Promise.resolve()

    // The listing is fetched (not treated as a media file).
    const url = mockApiGet.mock.calls[0]?.[0] as string
    expect(url).toContain('/api/dir?path=assets.png')
    expect(preview.dirEntries.value.map(e => e.name)).toEqual(['logo.png'])
    // And the card is not put into the media view.
    expect(preview.isMediaTarget.value).toBe(false)
  })

  it('ignores a line annotation on a directory target', () => {
    const preview = useCodeLinkPreview()
    const dirAnchor = document.createElement('span')
    dirAnchor.className = 'chat-file-path'
    dirAnchor.setAttribute('data-file-path', 'src/components')
    dirAnchor.setAttribute('data-path-type', 'dir')
    // A dir annotation has no meaningful lines; they must not reach the fetch.
    dirAnchor.setAttribute('data-line-start', '10')
    dirAnchor.setAttribute('data-line-end', '20')

    preview.handleClick({ target: dirAnchor, preventDefault: vi.fn(), stopPropagation: vi.fn() } as unknown as MouseEvent)
    expect(preview.target.value?.isDir).toBe(true)
    expect(preview.target.value?.lineStart).toBeUndefined()
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

  it('requests only a line window instead of the whole file', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: 'const a = 1',
      name: 'big.ts',
      path: 'big.ts',
      supported: true,
      size: 50 * 1024 * 1024,
      totalLines: 1_000_000,
      windowStart: 470,
      windowEnd: 830,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'big.ts', lineStart: 500, lineEnd: 500 })
    await vi.runAllTicks()
    await Promise.resolve()

    const url = mockApiGet.mock.calls[0][0] as string
    expect(url).toContain('lineStart=270')
    expect(url).toContain('lineEnd=730')
    // A 50 MB file must not be fetched whole.
    expect(url).not.toBe('/api/fs/file/big.ts')
  })

  it('reports total lines from the windowed response, not the window length', async () => {
    // The server echoes back exactly the requested window (270..730), plus the
    // file's true length — which the window itself cannot reveal.
    mockApiGet.mockResolvedValueOnce({
      content: Array.from({ length: 461 }, (_, i) => `line ${270 + i}`).join('\n'),
      name: 'big.ts',
      path: 'big.ts',
      supported: true,
      size: 50 * 1024 * 1024,
      totalLines: 1_000_000,
      windowStart: 270,
      windowEnd: 730,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'big.ts', lineStart: 500, lineEnd: 500 })
    await vi.runAllTicks()
    await Promise.resolve()

    expect(preview.slicedCode.value?.totalLines).toBe(1_000_000)
    // Render window is 470..530 in absolute file coordinates.
    expect(preview.slicedCode.value?.startLine).toBe(470)
    expect(preview.slicedCode.value?.code.split('\n')[30]).toBe('line 500')
  })

  it('caches a windowed slice of a large file', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: Array.from({ length: 461 }, (_, i) => `line ${270 + i}`).join('\n'),
      name: 'big.ts',
      path: 'big.ts',
      supported: true,
      size: 50 * 1024 * 1024,
      totalLines: 1_000_000,
      windowStart: 270,
      windowEnd: 730,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'big.ts', lineStart: 500, lineEnd: 500 })
    await vi.runAllTicks()
    await Promise.resolve()
    expect(mockApiGet).toHaveBeenCalledTimes(1)

    // Re-opening the same target must be served from the LRU, not re-fetched.
    preview.close()
    preview.showPreview({ filePath: 'big.ts', lineStart: 500, lineEnd: 500 })
    await vi.runAllTicks()
    await Promise.resolve()
    expect(mockApiGet).toHaveBeenCalledTimes(1)
    expect(preview.slicedCode.value?.totalLines).toBe(1_000_000)
  })

  it('widens the window when expansion runs past the fetched slice', async () => {
    // First response covers the planned 270..730; expanding to the top needs
    // lines above 270, which triggers exactly one wider re-fetch.
    mockApiGet
      .mockResolvedValueOnce({
        content: Array.from({ length: 461 }, (_, i) => `line ${270 + i}`).join('\n'),
        name: 'big.ts',
        path: 'big.ts',
        supported: true,
        size: 50 * 1024 * 1024,
        totalLines: 1_000_000,
        windowStart: 270,
        windowEnd: 730,
      })
      .mockResolvedValueOnce({
        content: Array.from({ length: 200 }, (_, i) => `line ${i + 1}`).join('\n'),
        name: 'big.ts',
        path: 'big.ts',
        supported: true,
        size: 50 * 1024 * 1024,
        totalLines: 1_000_000,
        windowStart: 1,
        windowEnd: 200,
      })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'big.ts', lineStart: 500, lineEnd: 500 })
    await vi.runAllTicks()
    await Promise.resolve()
    expect(preview.slicedCode.value?.startLine).toBe(470)
    expect(mockApiGet).toHaveBeenCalledTimes(1)

    preview.expandToTop()
    await vi.runAllTicks()
    await Promise.resolve()

    expect(mockApiGet).toHaveBeenCalledTimes(2)
    const widenedUrl = mockApiGet.mock.calls[1][0] as string
    expect(widenedUrl).toContain('lineStart=1')
    expect(preview.slicedCode.value?.startLine).toBe(1)
  })

  it('does not flip back to loading while widening the window', async () => {
    mockApiGet
      .mockResolvedValueOnce({
        content: Array.from({ length: 461 }, (_, i) => `line ${270 + i}`).join('\n'),
        name: 'big.ts',
        path: 'big.ts',
        supported: true,
        size: 50 * 1024 * 1024,
        totalLines: 1_000_000,
        windowStart: 270,
        windowEnd: 730,
      })
      .mockResolvedValueOnce({
        content: Array.from({ length: 200 }, (_, i) => `line ${i + 1}`).join('\n'),
        name: 'big.ts',
        path: 'big.ts',
        supported: true,
        size: 50 * 1024 * 1024,
        totalLines: 1_000_000,
        windowStart: 1,
        windowEnd: 200,
      })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'big.ts', lineStart: 500, lineEnd: 500 })
    await vi.runAllTicks()
    await Promise.resolve()
    expect(preview.status.value).toBe('ready')

    // The widen fetch is silent: the existing slice stays on screen (the expand
    // handler is anchoring scroll around it), so status never returns to loading.
    preview.expandToTop()
    await vi.runAllTicks()
    await Promise.resolve()
    expect(preview.status.value).toBe('ready')
  })

  it('keeps the existing slice when a silent widen fails', async () => {
    mockApiGet
      .mockResolvedValueOnce({
        content: Array.from({ length: 461 }, (_, i) => `line ${270 + i}`).join('\n'),
        name: 'big.ts',
        path: 'big.ts',
        supported: true,
        size: 50 * 1024 * 1024,
        totalLines: 1_000_000,
        windowStart: 270,
        windowEnd: 730,
      })
      .mockRejectedValueOnce({ message: 'network down', status: 500 })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'big.ts', lineStart: 500, lineEnd: 500 })
    await vi.runAllTicks()
    await Promise.resolve()
    expect(preview.status.value).toBe('ready')

    // The background widen fails: the user must keep reading what was already
    // rendered, not get an error card in place of a good preview. The slice falls
    // back to the fetched window's top edge, which is still real content.
    preview.expandToTop()
    await vi.runAllTicks()
    await Promise.resolve()

    expect(mockApiGet).toHaveBeenCalledTimes(2)
    expect(preview.status.value).toBe('ready')
    expect(preview.slicedCode.value?.startLine).toBe(270)
    expect(preview.slicedCode.value?.code).toContain('line 270')
  })

  it('surfaces a server-truncated window instead of a blank pane', async () => {
    // The server hit its byte cap on an oversized line: empty content, but
    // windowStart=1/windowEnd=0 marks "no lines captured".
    mockApiGet.mockResolvedValueOnce({
      content: '',
      name: 'giant.ts',
      path: 'giant.ts',
      supported: true,
      size: 3 * 1024 * 1024,
      totalLines: 3,
      windowStart: 1,
      windowEnd: 0,
      windowTruncated: true,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'giant.ts', lineStart: 1, lineEnd: 3 })
    await vi.runAllTicks()
    await Promise.resolve()

    expect(preview.windowTruncated.value).toBe(true)
    // The empty window is reported, not silently treated as a valid slice.
    expect(preview.slicedCode.value?.endLine).toBeLessThan(
      preview.slicedCode.value?.startLine ?? 0
    )
    // Widening cannot help — the cap would just trip again.
    expect(mockApiGet).toHaveBeenCalledTimes(1)
  })

  it('self-heals an annotation past EOF by widening to the real end of file', async () => {
    // A stale/hallucinated line annotation (lineStart beyond the file's length)
    // must not leave the pane blank. The window planner asks for lines past EOF,
    // so the server returns the empty-window encoding; the card must then widen
    // to the real last lines — the same self-heal the pre-window code did.
    mockApiGet
      .mockResolvedValueOnce({
        content: '',
        name: 'short.ts',
        path: 'short.ts',
        supported: true,
        size: 100,
        totalLines: 100,
        windowStart: 4770,
        windowEnd: 4769,
      })
      .mockResolvedValueOnce({
        // The widen asks for 71..100; the server returns just that window.
        content: Array.from({ length: 30 }, (_, i) => `line ${71 + i}`).join('\n'),
        name: 'short.ts',
        path: 'short.ts',
        supported: true,
        size: 100,
        totalLines: 100,
        windowStart: 71,
        windowEnd: 100,
      })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'short.ts', lineStart: 5000, lineEnd: 5000 })
    await vi.runAllTicks()
    await Promise.resolve()
    await vi.runAllTicks()
    await Promise.resolve()

    // The pane shows real lines from the end of the file, not an empty body.
    expect(preview.slicedCode.value?.code).not.toBe('')
    expect(preview.slicedCode.value?.code).toContain('line 100')
    expect(preview.slicedCode.value?.lineOutOfRange).toBe(true)
    // Widening is what recovered it.
    expect(mockApiGet.mock.calls.length).toBeGreaterThan(1)
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
    mockApiGet.mockResolvedValue({
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
    await preview.expandAbove(10)
    expect(preview.slicedCode.value?.startLine).toBe(60)
    expect(preview.slicedCode.value?.endLine).toBe(130)

    // Expand below by 15 lines -> endLine 145
    await preview.expandBelow(15)
    expect(preview.slicedCode.value?.startLine).toBe(60)
    expect(preview.slicedCode.value?.endLine).toBe(145)

    // Expand to top -> startLine 1
    await preview.expandToTop()
    expect(preview.slicedCode.value?.startLine).toBe(1)

    // Expand to bottom -> the file's last line (line count is no longer capped)
    await preview.expandToBottom()
    expect(preview.slicedCode.value?.endLine).toBe(200)
  })

  it('walks a long file in chunks instead of capping the slice', async () => {
    // A 5000-line file: the opening window is bounded, and each scroll-driven
    // load pulls the next chunk. The slice must keep growing past the old
    // 200-line ceiling — that ceiling is what this feature removes.
    const line = (n: number) => `line ${n}`
    const window = (start: number, end: number) => ({
      content: Array.from({ length: end - start + 1 }, (_, i) => line(start + i)).join('\n'),
      name: 'big.ts',
      path: 'big.ts',
      supported: true,
      size: 200 * 1024,
      totalLines: 5000,
      windowStart: start,
      windowEnd: end,
    })
    mockApiGet.mockImplementation((url: string) => {
      const m = /lineStart=(\d+)&lineEnd=(\d+)/.exec(url)
      if (!m) return Promise.resolve(window(1, 30))
      return Promise.resolve(window(Number(m[1]), Number(m[2])))
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'big.ts' })
    await vi.runAllTicks()
    await Promise.resolve()

    // Opens on the file head, well under the whole file.
    expect(preview.slicedCode.value?.startLine).toBe(1)
    const opened = preview.slicedCode.value!.endLine
    expect(opened).toBeLessThan(5000)

    // Each load extends the slice; nothing is clamped to 200 lines.
    await preview.expandBelow(800)
    expect(preview.slicedCode.value!.endLine).toBeGreaterThan(opened)
    const afterOne = preview.slicedCode.value!.endLine
    expect(afterOne).toBeGreaterThan(200)

    await preview.expandBelow(800)
    expect(preview.slicedCode.value!.endLine).toBeGreaterThan(afterOne)

    // The lines really are there, in file order — not a re-sliced window.
    expect(preview.slicedCode.value!.code).toContain('line 1')
    expect(preview.slicedCode.value!.code.split('\n')[0]).toBe('line 1')
  })

  it('blocks further loading once a byte ceiling cuts the slice short', async () => {
    // A line range spanning 60 lines of ~10 KiB: the 512 KiB render budget trips
    // partway through, so no amount of extra loading can extend the slice.
    const bigLine = 'a'.repeat(10 * 1024)
    const content = Array.from({ length: 60 }, () => bigLine).join('\n')
    mockApiGet.mockImplementation((url: string) => {
      const m = /lineStart=(\d+)&lineEnd=(\d+)/.exec(url)
      const start = m ? Number(m[1]) : 1
      const end = m ? Number(m[2]) : 260
      return Promise.resolve({
        content: start === 1 && end >= 60 ? content : bigLine,
        name: 'fat.ts',
        path: 'fat.ts',
        supported: true,
        size: 600 * 1024,
        totalLines: 5000,
        windowStart: start,
        windowEnd: Math.min(end, start === 1 && end >= 60 ? 60 : end),
      })
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'fat.ts', lineStart: 1, lineEnd: 60 })
    await vi.runAllTicks()
    await Promise.resolve()

    expect(preview.slicedCode.value?.renderTruncated).toBe(true)
    expect(preview.slicedCode.value?.truncateReason).toBe('bytes')
    expect(preview.loadMoreBlocked.value).toBe(true)

    // The file has thousands more lines and the wanted range grows well past the
    // held window — so without the gate this would fetch. Blocked means blocked:
    // a byte-capped slice cannot grow, so no request may go out.
    const callsBefore = mockApiGet.mock.calls.length
    await preview.expandBelow(800)
    await preview.loadMore()
    expect(mockApiGet.mock.calls.length).toBe(callsBefore)
  })

  it('renders a whole-file response that ignores the requested line window', async () => {
    // Non-text files (LICENSE, *.bak, .env, extensionless scripts) take the
    // server's whole-file branch, which ignores ?lineStart/?lineEnd: the body
    // is the FULL content and windowStart is absent while windowEnd stays 0.
    // Reading that as the empty-window encoding blanked the pane and showed
    // "requested line is out of file range" for every such file.
    mockApiGet.mockResolvedValue({
      content: 'MIT License\n\nPermission is hereby granted',
      name: 'LICENSE',
      path: 'LICENSE',
      supported: false,
      size: 41,
      windowEnd: 0,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'LICENSE' })
    await vi.runAllTicks()
    await Promise.resolve()

    expect(preview.status.value).toBe('ready')
    expect(preview.slicedCode.value?.code).toContain('MIT License')
    expect(preview.slicedCode.value?.lineOutOfRange).toBe(false)
    expect(preview.slicedCode.value?.totalLines).toBe(3)
    // The whole file is held, so no widening fetch is needed or attempted.
    expect(mockApiGet).toHaveBeenCalledTimes(1)
  })

  it('renders a one-line whole-file response without the out-of-range notice', async () => {
    // The reported case: a file whose entire content is a single line with no
    // trailing newline. totalLines is 1 and the line is perfectly in range.
    mockApiGet.mockResolvedValue({
      content: '{"sessionId":"01a0866a-eb91-7c"}',
      name: 'scheduled_tasks.lock',
      path: 'scheduled_tasks.lock',
      supported: false,
      size: 31,
      windowEnd: 0,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'scheduled_tasks.lock' })
    await vi.runAllTicks()
    await Promise.resolve()

    expect(preview.status.value).toBe('ready')
    expect(preview.slicedCode.value?.code).toBe('{"sessionId":"01a0866a-eb91-7c"}')
    expect(preview.slicedCode.value?.startLine).toBe(1)
    expect(preview.slicedCode.value?.endLine).toBe(1)
    expect(preview.slicedCode.value?.lineOutOfRange).toBe(false)
  })

  it('shows an empty file as blank, without the out-of-range notice', async () => {
    // The reported case: a zero-byte text file. The server's empty-file path
    // returns an empty window carrying no windowStart, which the pane reads as
    // the whole file — 0 lines. Nothing can be out of range when a file has no
    // lines at all, so the notice must stay off.
    mockApiGet.mockResolvedValue({
      content: '',
      name: 'empty.txt',
      path: 'empty.txt',
      supported: true,
      size: 0,
      windowEnd: 0,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'empty.txt' })
    await vi.runAllTicks()
    await Promise.resolve()

    expect(preview.status.value).toBe('ready')
    expect(preview.slicedCode.value?.code).toBe('')
    expect(preview.slicedCode.value?.totalLines).toBe(0)
    expect(preview.slicedCode.value?.lineOutOfRange).toBe(false)
  })

  it('still reports an out-of-range annotation on a windowed response', async () => {
    // The empty-window encoding (windowStart present, windowEnd = Start-1) is
    // unchanged: an annotation past EOF must still surface the notice.
    mockApiGet.mockResolvedValue({
      content: '',
      name: 'short.ts',
      path: 'short.ts',
      supported: true,
      size: 100,
      totalLines: 100,
      windowStart: 5000,
      windowEnd: 4999,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'short.ts', lineStart: 5000, lineEnd: 5000 })
    await vi.runAllTicks()
    await Promise.resolve()

    expect(preview.slicedCode.value?.lineOutOfRange).toBe(true)
  })

  it('opens a long file on a small head and is not blocked', async () => {
    mockApiGet.mockResolvedValueOnce({
      content: Array.from({ length: 400 }, (_, i) => `line ${i + 1}`).join('\n'),
      name: 'long.ts',
      path: 'long.ts',
      supported: true,
      size: 4000,
      totalLines: 400,
      windowStart: 1,
      windowEnd: 400,
    })

    const preview = useCodeLinkPreview()
    preview.showPreview({ filePath: 'long.ts' })
    await vi.runAllTicks()
    await Promise.resolve()

    // No annotation: the slice opens on the file head (30 lines) and grows as
    // the user scrolls — it is not the whole file, and it is not blocked.
    expect(preview.slicedCode.value?.startLine).toBe(1)
    expect(preview.slicedCode.value?.endLine).toBe(30)
    expect(preview.loadMoreBlocked.value).toBe(false)
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

  it('unverified paths fall through (return false)', () => {
    // Only a *verified* path is intercepted. Without data-path-type the
    // annotation has not been confirmed yet, so the container's own handler
    // must get the click.
    const { preview, handleClick } = makePreview()
    const handled = handleVerifiedFilePathClick(makeEvent(makeElement(null)), preview as never)
    expect(handled).toBe(false)
    expect(handleClick).not.toHaveBeenCalled()
  })

  it('intercepts directory paths too (they preview a listing)', () => {
    const { preview, handleClick } = makePreview()
    const handled = handleVerifiedFilePathClick(makeEvent(makeElement('dir')), preview as never)
    expect(handled).toBe(true)
    expect(handleClick).toHaveBeenCalledTimes(1)
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
