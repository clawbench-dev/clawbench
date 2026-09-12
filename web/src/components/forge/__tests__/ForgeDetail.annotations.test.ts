import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref } from 'vue'

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
    ExternalLink: stub('ExternalLink'),
    Sparkles: stub('Sparkles'),
    MessageSquare: stub('MessageSquare'),
    AlertCircle: stub('AlertCircle'),
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

const { mockDetail } = vi.hoisted(() => ({
  mockDetail: {
    item: { value: null as unknown },
    comments: { value: [] as unknown[] },
    loading: { value: false },
    loadingComments: { value: false },
    error: { value: null as unknown },
    hasMoreComments: { value: false },
    open: vi.fn(),
    loadOlderComments: vi.fn(),
  },
}))

vi.mock('@/composables/useForge', () => ({
  useForgeDetail: () => mockDetail,
}))

vi.mock('@/components/common/LoadingIndicator.vue', () => ({
  default: { name: 'LoadingIndicator', template: '<div class="loading-stub" />' },
}))

vi.mock('@/components/file/CodeLinkPreview.vue', () => ({
  default: { name: 'CodeLinkPreview', template: '<div class="code-preview-stub" />' },
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
