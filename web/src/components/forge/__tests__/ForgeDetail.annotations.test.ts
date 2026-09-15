import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { mount, enableAutoUnmount } from '@vue/test-utils'
import { ref } from 'vue'

// The forge detail composable is a shared mock whose refs every mounted
// instance watches. Without unmounting, a component from an earlier test keeps
// reacting to later ref changes and re-running verification, which pollutes
// per-test call counts.
enableAutoUnmount(afterEach)

// ── Mocks ────────────────────────────────────────────────────
// The regression this file pins: the forge detail body used to be rendered
// through renderMarkdownHtml(), which discards the detectedPaths the pipeline
// produces. Nothing then ran verifyFilePaths against the detail container, so
// every path-looking string in an issue/PR body kept a live-looking but dead
// annotation. There was also no click handler at all on the body, so even a
// real path did nothing. These tests cover both halves of the fix.

const { mockVerifyFilePaths, mockOpenFilePath, mockVerifyCommitHashes, mockPreviewClose, mockHandleVerifiedFilePathClick } = vi.hoisted(() => ({
  mockVerifyFilePaths: vi.fn().mockResolvedValue(undefined),
  mockOpenFilePath: vi.fn().mockResolvedValue(true),
  mockVerifyCommitHashes: vi.fn().mockResolvedValue(undefined),
  mockPreviewClose: vi.fn(),
  mockHandleVerifiedFilePathClick: vi.fn().mockReturnValue(false),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

vi.mock('lucide-vue-next', () => {
  const stub = (name: string) => ({ name, template: `<svg data-icon="${name}" />` })
  return {
    ChevronLeft: stub('ChevronLeft'),
    ChevronRight: stub('ChevronRight'),
    ExternalLink: stub('ExternalLink'),
    MessageSquare: stub('MessageSquare'),
    AlertCircle: stub('AlertCircle'),
    Activity: stub('Activity'),
  }
})

// The body is what carries the annotations, so renderMarkdown must produce
// realistic annotated markup. Tests that need different markup reassign
// `bodyHtml` before mounting.
const { bodyHtml } = vi.hoisted(() => ({ bodyHtml: { value: '' } }))
bodyHtml.value = '<p>see <span class="chat-file-path" data-file-path="src/real.ts">src/real.ts</span>'
  + '<button class="chat-file-open-btn" data-file-path="src/real.ts"></button>'
  + ' and <span class="chat-file-path" data-file-path="src/ghost.ts">src/ghost.ts</span>'
  + '<button class="chat-file-open-btn" data-file-path="src/ghost.ts"></button></p>'

vi.mock('@/composables/useMarkdownRenderer', () => ({
  renderMarkdownHtml: () => bodyHtml.value,
}))

vi.mock('@/composables/useFilePathAnnotation', () => ({
  useFilePathAnnotation: () => ({
    verifyFilePaths: mockVerifyFilePaths,
    openFilePath: mockOpenFilePath,
    readLineTargetFromEl: (el: Element) => {
      const startAttr = el.getAttribute('data-line-start')
      const endAttr = el.getAttribute('data-line-end')
      return {
        filePath: el.getAttribute('data-file-path'),
        lineStart: startAttr ? parseInt(startAttr, 10) : undefined,
        lineEnd: endAttr ? parseInt(endAttr, 10) : undefined,
        lineRanges: el.getAttribute('data-line-ranges') || undefined,
      }
    },
  }),
}))

vi.mock('@/composables/useCommitHashAnnotation', () => ({
  verifyCommitHashes: mockVerifyCommitHashes,
}))

vi.mock('@/composables/useCodeLinkPreview', () => ({
  useCodeLinkPreview: () => ({
    enabled: ref(false),
    visible: ref(false),
    close: mockPreviewClose,
  }),
  handleVerifiedFilePathClick: (...a: unknown[]) => mockHandleVerifiedFilePathClick(...a),
}))

vi.mock('@/composables/useLocalhostAnnotation', () => ({
  useLocalhostUrlClickHandler: () => ({ handleLocalhostUrlClick: () => false }),
}))

vi.mock('@/composables/useCodeBlockHeader', () => ({
  handleCodeBlockClick: vi.fn().mockReturnValue(false),
  handleTableBlockClick: vi.fn().mockReturnValue(false),
}))

// Real refs, not plain `{ value }` objects: the component's verification
// watcher must actually re-run when loading/item/comments change. With plain
// objects Vue has no dependency to track, so the watcher would fire once and
// never again — which is precisely the class of bug these tests guard.
const { mockDetail } = vi.hoisted(() => ({
  mockDetail: {
    item: null as unknown,
    comments: null as unknown,
    loading: null as unknown,
    loadingComments: null as unknown,
    error: null as unknown,
    hasMoreComments: null as unknown,
    open: vi.fn(),
    loadOlderComments: vi.fn(),
  },
}))

vi.mock('@/composables/useForge', async () => {
  const { ref } = await import('vue')
  Object.assign(mockDetail, {
    item: ref(null),
    comments: ref([]),
    loading: ref(false),
    loadingComments: ref(false),
    error: ref(null),
    hasMoreComments: ref(false),
  })
  return {
    useForgeDetail: () => mockDetail,
    // The CI section is a sibling of the body under test; it must exist for the
    // component to mount, but these tests assert nothing about it.
    useForgeItemPipelines: () => ({
      pipelines: ref([]),
      loading: ref(false),
      error: ref(null),
      loaded: ref(false),
      load: vi.fn(),
      reset: vi.fn(),
    }),
  }
})

vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', template: '<div class="loading-stub" />' },
}))

vi.mock('@/components/file/CodeLinkPreview.vue', () => ({
  default: { name: 'CodeLinkPreview', template: '<div class="code-preview-stub" />' },
}))

// Double-click copy + quote bar: capture the options object the component
// registers so tests can drive its onCopy callback, and record that the click
// chain actually reaches handleDblClick.
const { mockShowBar, mockHandleDblClick, capturedDoubleClickOptions } = vi.hoisted(() => ({
  mockShowBar: vi.fn(),
  mockHandleDblClick: vi.fn(),
  capturedDoubleClickOptions: { value: null as null | { onCopy?: (t: EventTarget | null, text: string) => void } },
}))

vi.mock('@/composables/useQuoteQuestion', () => ({
  useQuoteQuestion: () => ({ showBar: mockShowBar }),
}))

vi.mock('@/composables/useDoubleClickCopy', () => ({
  useDoubleClickCopy: (opts: { onCopy?: (t: EventTarget | null, text: string) => void }) => {
    capturedDoubleClickOptions.value = opts
    return { handleDblClick: mockHandleDblClick }
  },
}))

import ForgeDetail from '@/components/forge/ForgeDetail.vue'

function mountDetail() {
  return mount(ForgeDetail, { props: { type: 'issue' as const, number: 7 } })
}

const DEFAULT_BODY_HTML = '<p>see <span class="chat-file-path" data-file-path="src/real.ts">src/real.ts</span>'
  + '<button class="chat-file-open-btn" data-file-path="src/real.ts"></button>'
  + ' and <span class="chat-file-path" data-file-path="src/ghost.ts">src/ghost.ts</span>'
  + '<button class="chat-file-open-btn" data-file-path="src/ghost.ts"></button></p>'

describe('ForgeDetail file-path annotations', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    bodyHtml.value = DEFAULT_BODY_HTML
    mockHandleVerifiedFilePathClick.mockReturnValue(false)
    mockDetail.item.value = {
      type: 'issue', number: 7, title: 'A bug', state: 'open', author: 'alice',
      url: 'https://example.com/7', slug: 'a/b', body: 'see src/real.ts',
      createdAt: '2026-09-10T00:00:00Z', updatedAt: '2026-09-10T00:00:00Z',
      commentCount: 0, platform: 'github', host: 'github.com', owner: 'a', repo: 'b',
    }
    mockDetail.comments.value = []
    mockDetail.loading.value = false
    mockDetail.error.value = null
  })

  it('verifies annotated paths so non-existent ones lose their annotation', async () => {
    const wrapper = mountDetail()
    await new Promise(r => setTimeout(r, 0))
    await wrapper.vm.$nextTick()

    // Both annotated paths are handed to verification — that pass is what
    // removes the annotation for a path that does not exist on disk.
    expect(mockVerifyFilePaths).toHaveBeenCalledTimes(1)
    const [paths, container] = mockVerifyFilePaths.mock.calls[0]
    expect(paths).toEqual(['src/real.ts', 'src/ghost.ts'])
    // Scoped to the detail body, not a global container: chat verifies against
    // #aiChatMessages, which the forge panel is not part of.
    expect((container as HTMLElement).classList.contains('forge-detail-body')).toBe(true)
  })

  it('verifies after the async load lands (body mounts only once loading ends)', async () => {
    // The real sequence: open() sets loading=true, then item/comments, and only
    // clears loading at the very end. The body — and therefore bodyRef — does
    // not exist until loading is false. Watching only item/comments fired while
    // bodyRef was still null, so verification silently never happened and every
    // path in an issue/PR body stayed unclickable.
    mockDetail.loading.value = true
    mockDetail.item.value = null
    mockDetail.comments.value = []

    const wrapper = mountDetail()
    await new Promise(r => setTimeout(r, 0))
    await wrapper.vm.$nextTick()

    // Nothing to verify yet — the body is not rendered, so there is no
    // container to scan. (Verification may still have run earlier in the suite;
    // what matters is that it runs AGAIN once the body exists.)
    expect(wrapper.find('.forge-detail-body').exists()).toBe(false)
    const callsBeforeBody = mockVerifyFilePaths.mock.calls.length

    // The item arrives while still loading (as open() does), then loading ends.
    mockDetail.item.value = {
      type: 'issue', number: 7, title: 'A bug', state: 'open', author: 'alice',
      url: 'https://example.com/7', slug: 'a/b', body: 'see src/real.ts',
      createdAt: '2026-09-10T00:00:00Z', updatedAt: '2026-09-10T00:00:00Z',
      commentCount: 0, platform: 'github', host: 'github.com', owner: 'a', repo: 'b',
    }
    await wrapper.vm.$nextTick()
    mockDetail.loading.value = false
    await new Promise(r => setTimeout(r, 0))
    await wrapper.vm.$nextTick()
    await new Promise(r => setTimeout(r, 0))

    // The body is now rendered AND its paths were handed to verification.
    expect(wrapper.find('.forge-detail-body').exists()).toBe(true)
    expect(mockVerifyFilePaths.mock.calls.length).toBeGreaterThan(callsBeforeBody)
    const [paths] = mockVerifyFilePaths.mock.calls.at(-1)!
    expect(paths).toContain('src/real.ts')
  })

  it('deduplicates paths before verifying', async () => {
    mockDetail.item.value = {
      ...(mockDetail.item.value as Record<string, unknown>),
      body: 'dup',
    }
    const wrapper = mountDetail()
    await new Promise(r => setTimeout(r, 0))
    await wrapper.vm.$nextTick()
    const [paths] = mockVerifyFilePaths.mock.calls[0]
    expect(new Set(paths).size).toBe(paths.length)
  })

  it('opens the file when a file-open button is clicked', async () => {
    const wrapper = mountDetail()
    await new Promise(r => setTimeout(r, 0))
    await wrapper.vm.$nextTick()

    const btn = wrapper.find('.chat-file-open-btn[data-file-path="src/real.ts"]')
    expect(btn.exists()).toBe(true)
    await btn.trigger('click')

    expect(mockOpenFilePath).toHaveBeenCalledWith('src/real.ts', undefined, undefined, 'forge')
  })

  it('opens a directory annotation clicked as text', async () => {
    bodyHtml.value = '<span class="chat-file-path" data-file-path="src/components" data-path-type="dir">src/components</span>'
    const wrapper = mountDetail()
    await new Promise(r => setTimeout(r, 0))
    await wrapper.vm.$nextTick()

    await wrapper.find('.chat-file-path[data-path-type="dir"]').trigger('click')

    expect(mockOpenFilePath).toHaveBeenCalledWith('src/components', undefined, undefined, 'forge')
  })

  it('lets the code link preview handle verified file-path clicks first', async () => {
    mockHandleVerifiedFilePathClick.mockReturnValue(true)
    const wrapper = mountDetail()
    await new Promise(r => setTimeout(r, 0))
    await wrapper.vm.$nextTick()

    await wrapper.find('.chat-file-open-btn[data-file-path="src/real.ts"]').trigger('click')
    // The preview consumed the event, so we must not also open the file.
    expect(mockOpenFilePath).not.toHaveBeenCalled()
  })

  it('verifies commit hashes found in the body', async () => {
    bodyHtml.value = '<p><span class="chat-commit-hash-pending" data-commit-sha="abc1234">abc1234</span></p>'
    mountDetail()
    await new Promise(r => setTimeout(r, 0))

    expect(mockVerifyCommitHashes).toHaveBeenCalled()
    const [shas, container] = mockVerifyCommitHashes.mock.calls.at(-1)!
    expect(shas).toContain('abc1234')
    expect((container as HTMLElement).classList.contains('forge-detail-body')).toBe(true)
  })

  it('does not verify when the item has no body', async () => {
    mockDetail.item.value = {
      ...(mockDetail.item.value as Record<string, unknown>),
      body: '',
    }
    mountDetail()
    await new Promise(r => setTimeout(r, 0))
    // Nothing annotated → nothing to check.
    expect(mockVerifyFilePaths).not.toHaveBeenCalled()
  })
})

describe('ForgeDetail quote action (header)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    bodyHtml.value = DEFAULT_BODY_HTML
    mockDetail.item.value = {
      type: 'issue', number: 7, title: 'A bug', state: 'open', author: 'alice',
      url: 'https://example.com/7', slug: 'a/b', body: 'see src/real.ts',
      createdAt: '2026-09-10T00:00:00Z', updatedAt: '2026-09-10T00:00:00Z',
      commentCount: 0, platform: 'github', host: 'github.com', owner: 'a', repo: 'b',
    }
    mockDetail.comments.value = []
    mockDetail.loading.value = false
    mockDetail.error.value = null
  })

  function quoteButton(wrapper: ReturnType<typeof mountDetail>) {
    return wrapper.findAll('.forge-detail-header button')
      .find(b => b.attributes('aria-label') === 'forge.detail.quote')
  }

  it('offers a quote button in the detail header', () => {
    const wrapper = mountDetail()
    expect(quoteButton(wrapper), 'the header must expose a quote action').toBeTruthy()
  })

  it('uses the plain message bubble, not the plus variant', () => {
    // MessageSquarePlus draws a "+" inside the bubble, which reads as a stray
    // ring/cross next to the external-link icon. The plain bubble is intended.
    const wrapper = mountDetail()
    const icon = quoteButton(wrapper)!.find('svg')
    expect(icon.attributes('data-icon')).toBe('MessageSquare')
  })

  it('emits quote without the full body, so nothing is quoted by default', () => {
    // The old flow forwarded `body` and injected the entire issue text. The
    // quote must now come from the user's selection instead.
    const wrapper = mountDetail()
    quoteButton(wrapper)!.trigger('click')

    const emitted = wrapper.emitted('quote')
    expect(emitted).toBeTruthy()
    expect(emitted![0][0]).toEqual({
      item: { type: 'issue', number: 7, title: 'A bug', url: 'https://example.com/7', slug: 'a/b' },
    })
    expect(JSON.stringify(emitted![0][0])).not.toContain('see src/real.ts')
  })

  it('no longer renders the bottom analyze button', () => {
    const wrapper = mountDetail()
    expect(wrapper.find('.forge-analyze-btn').exists()).toBe(false)
  })

  it('labels the body region so selected text is tagged with the issue', () => {
    const wrapper = mountDetail()
    const body = wrapper.find('.forge-detail-body')
    expect(body.attributes('data-quote-source')).toBe('a/b#7')
    expect(body.attributes('data-quote-language')).toBe('issue')
  })

  it('does not mark the body as a file, which would grow attach buttons', () => {
    // `data-file-path` on a .markdown-body is what mdBlockAttach/mdMermaidAttach
    // key off; setting it would add "add to chat" buttons to every code block.
    const wrapper = mountDetail()
    expect(wrapper.find('.forge-detail-body').attributes('data-file-path')).toBeUndefined()
  })
})

// Double-click on an issue/PR body block reuses the markdown preview's
// copy + quote pipeline: the shared composable copies the block, and onCopy
// opens the quote bar tagged with the issue/PR identity instead of a file path.
describe('ForgeDetail double-click copy → quote', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    bodyHtml.value = DEFAULT_BODY_HTML
    capturedDoubleClickOptions.value = null
    mockDetail.item.value = {
      type: 'issue', number: 7, title: 'A bug', state: 'open', author: 'alice',
      url: 'https://example.com/7', slug: 'a/b', body: 'see src/real.ts',
      createdAt: '2026-09-10T00:00:00Z', updatedAt: '2026-09-10T00:00:00Z',
      commentCount: 0, platform: 'github', host: 'github.com', owner: 'a', repo: 'b',
    }
    mockDetail.comments.value = []
    mockDetail.loading.value = false
    mockDetail.error.value = null
  })

  function quoteSourceEl(wrapper: ReturnType<typeof mountDetail>): HTMLElement {
    return wrapper.find('.forge-detail-body').element as HTMLElement
  }

  it('registers an onCopy handler with the shared double-click composable', () => {
    mountDetail()
    expect(capturedDoubleClickOptions.value?.onCopy, 'onCopy must be wired').toBeTypeOf('function')
  })

  it('opens the quote bar tagged with the issue identity, not a file path', () => {
    const wrapper = mountDetail()
    capturedDoubleClickOptions.value!.onCopy!(quoteSourceEl(wrapper), 'quoted paragraph')

    expect(mockShowBar).toHaveBeenCalledTimes(1)
    expect(mockShowBar).toHaveBeenCalledWith({
      text: 'quoted paragraph',
      // The issue/PR label (not a file path) is what getQuoteSource resolves;
      // issue text has no file line numbers, so those stay 0 and the fence
      // gets no ":N" suffix.
      filePath: 'a/b#7',
      language: 'issue',
      startLine: 0,
      endLine: 0,
    })
  })

  it('tags a PR body with the pr language', () => {
    mockDetail.item.value = {
      ...(mockDetail.item.value as Record<string, unknown>),
      type: 'pr', number: 9,
    }
    const wrapper = mountDetail()
    capturedDoubleClickOptions.value!.onCopy!(quoteSourceEl(wrapper), 'pr text')

    expect(mockShowBar).toHaveBeenCalledWith(expect.objectContaining({
      filePath: 'a/b#9', language: 'pr', startLine: 0, endLine: 0,
    }))
  })

  it('falls back to an unlabelled quote when the block is outside the body', () => {
    // A block with no [data-quote-source] ancestor cannot claim an issue
    // identity; the quote must still open rather than throw.
    mountDetail()
    const orphan = document.createElement('p')
    orphan.textContent = 'loose'
    capturedDoubleClickOptions.value!.onCopy!(orphan, 'loose')

    expect(mockShowBar).toHaveBeenCalledWith(expect.objectContaining({
      text: 'loose', filePath: '', language: '',
    }))
  })

  it('reaches handleDblClick from a plain body click', async () => {
    // The body's click handler must hand non-interactive targets to the
    // double-click composable, otherwise the second click is never seen.
    const wrapper = mountDetail()
    await new Promise(r => setTimeout(r, 0))
    await wrapper.find('.forge-detail-body').trigger('click')

    expect(mockHandleDblClick).toHaveBeenCalled()
  })
})

// jsdom has no CSS engine and does not apply the UA stylesheet, so a bare
// <button>'s default border cannot be observed by mounting. Sniff the source
// instead — the same pattern the repo's other CSS guard tests use.
describe('ForgeDetail icon button styling', () => {
  /**
   * Read the source that declares `.forge-icon-btn`.
   *
   * The rule is shared chrome: ForgePipelineDetail renders the same round icon
   * buttons, so the declaration moved to the shared stylesheet. This still
   * asserts the same behavior (the UA border must be cleared) — it just no
   * longer pins which file happens to hold it.
   */
  function readIconBtnSource(): string {
    const candidates = [
      'src/components/forge/ForgeDetail.vue',
      'css/components.css',
    ]
    for (const base of [process.cwd(), join(process.cwd(), 'web')]) {
      for (const rel of candidates) {
        try {
          const src = readFileSync(join(base, rel), 'utf8')
          if (/\.forge-icon-btn\s*\{/.test(src)) return src
        } catch {
          // try the next candidate
        }
      }
    }
    throw new Error('.forge-icon-btn declaration not found from cwd: ' + process.cwd())
  }

  it('clears the UA border on .forge-icon-btn', () => {
    // The class is used by BOTH <a> and <button>. A bare <button> keeps the
    // UA's default border, which renders as a stray ring around the round icon
    // — visible only after a <button> started using this class.
    const src = readIconBtnSource()
    const rule = src.match(/\.forge-icon-btn\s*\{([\s\S]*?)\}/)
    expect(rule, '.forge-icon-btn rule must exist').toBeTruthy()
    expect(rule![1]).toMatch(/^\s*border:\s*none\s*;?\s*$/m)
  })

  it('keeps the round shape the border would otherwise distort', () => {
    const src = readIconBtnSource()
    const rule = src.match(/\.forge-icon-btn\s*\{([\s\S]*?)\}/)![1]
    expect(rule).toContain('border-radius: var(--radius-lg)')
    expect(rule).toContain('width: 28px')
    expect(rule).toContain('height: 28px')
  })
})
